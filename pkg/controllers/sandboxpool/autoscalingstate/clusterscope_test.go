// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package autoscalingstate

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// multiSegmentEnv builds an Env whose members appear under two cluster
// segments under the SAME pool name — the shape a cross-cluster Env has, and
// the shape a renamed cluster leaves behind. The stale segment is listed
// first so a scan that ignores ClusterID finds it before the local one.
func multiSegmentEnv(poolName string, staleReplicas, localReplicas int32) *agentsv1alpha1.SandboxEnv {
	return &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: testEnvName, Namespace: testNS},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{
				{
					ClusterID: "departed-cluster",
					Members: []agentsv1alpha1.EnvClusterMember{{
						Name:   poolName,
						Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "other-group"},
						Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: staleReplicas},
					}},
				},
				{
					ClusterID: "local",
					Members: []agentsv1alpha1.EnvClusterMember{{
						Name:   poolName,
						Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: testGroup},
						Spec:   agentsv1alpha1.SandboxPoolSpec{Replicas: localReplicas},
					}},
				},
			},
		},
	}
}

// The regression this whole change exists for: a scale-up target must land in
// the local cluster's segment. Writing it into a foreign segment records the
// decision where no reconciler in this cluster reads it, so the Pool never
// grows and the autoscaler re-decides the same scale-up forever.
func TestCommit_SpecPatch_WritesLocalSegmentOnly(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	env := multiSegmentEnv(pool.Name, 1, 1)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env, LocalClusterID: "local"})
	m.SetTargetReplicas(6)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if r := got.Spec.Clusters[0].Members[0].Spec.Replicas; r != 1 {
		t.Errorf("stale segment replicas = %d, want 1 (autoscaler must not write a foreign segment)", r)
	}
	if r := got.Spec.Clusters[1].Members[0].Spec.Replicas; r != 6 {
		t.Errorf("local segment replicas = %d, want 6", r)
	}
}

// With no LOCAL_CLUSTER_ID (single-cluster deployments) the write still has to
// land, so the lookup falls back to scanning every segment.
func TestCommit_SpecPatch_EmptyClusterIDScansAllSegments(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	env := envFixture{
		members: []agentsv1alpha1.EnvClusterMember{{
			Name: pool.Name,
			Spec: agentsv1alpha1.SandboxPoolSpec{Replicas: 1},
		}},
	}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env}) // LocalClusterID unset
	m.SetTargetReplicas(4)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := c.Get(context.Background(),
		client.ObjectKey{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if r := got.Spec.Clusters[0].Members[0].Spec.Replicas; r != 4 {
		t.Errorf("replicas = %d, want 4", r)
	}
}

// A pool that exists only in a foreign segment is not ours to scale. The error
// is what surfaces the misconfiguration instead of silently writing elsewhere.
func TestCommit_SpecPatch_MemberOnlyInForeignSegment(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 1}.build()
	env := &agentsv1alpha1.SandboxEnv{
		ObjectMeta: metav1.ObjectMeta{Name: testEnvName, Namespace: testNS},
		Spec: agentsv1alpha1.SandboxEnvSpec{
			Clusters: []agentsv1alpha1.EnvClusterSpec{{
				ClusterID: "somewhere-else",
				Members:   []agentsv1alpha1.EnvClusterMember{{Name: pool.Name}},
			}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, env).Build()

	m := NewMutator(&Snapshot{Pool: pool, Env: env, LocalClusterID: "local"})
	m.SetTargetReplicas(3)

	if err := m.Commit(context.Background(), c, nil); err == nil {
		t.Error("expected an error when the member exists only in a foreign cluster segment")
	}
}

// findMemberConfig must resolve this cluster's config, not whichever segment
// happens to come first. Reading the wrong one silently applies another
// cluster's scaling group — and therefore another group's policy and ceiling.
func TestFindMemberConfig_ScopedToLocalSegment(t *testing.T) {
	env := multiSegmentEnv("p", 1, 1)

	if cfg := findMemberConfig(env, "local", "p"); cfg == nil {
		t.Fatal("findMemberConfig returned nil for a member present in the local segment")
	} else if cfg.ScalingGroup != testGroup {
		t.Errorf("ScalingGroup = %q, want %q (read the foreign segment's config)", cfg.ScalingGroup, testGroup)
	}

	if cfg := findMemberConfig(env, "", "p"); cfg == nil {
		t.Error("empty localClusterID should fall back to scanning every segment")
	}

	if cfg := findMemberConfig(env, "not-a-cluster", "p"); cfg != nil {
		t.Error("expected nil for a cluster with no segment")
	}
}

// The sibling index decides which local Pools count toward a scaling group's
// aggregate. Built from a foreign segment it would include or exclude the
// wrong Pools, skewing the group ceiling.
func TestLoad_SiblingIndexScopedToLocalSegment(t *testing.T) {
	scheme := newTestScheme(t)
	self := poolFixture{name: "p", replicas: 1}.build()
	env := multiSegmentEnv("p", 1, 1)
	env.Spec.Autoscaling = &agentsv1alpha1.EnvAutoscalingSpec{Groups: []agentsv1alpha1.EnvAutoscalingGroup{makeGroup(true)}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(self, env).Build()

	l := &Loader{Client: c, LocalClusterID: "local", Clock: staticClock{t: metav1.Now().Time}}
	snap, err := l.Load(context.Background(), self)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.MemberConfig == nil || snap.MemberConfig.ScalingGroup != testGroup {
		t.Fatalf("MemberConfig resolved from the wrong segment: %+v", snap.MemberConfig)
	}
	if snap.Group == nil {
		t.Fatal("Group should resolve from the local member's scaling group")
	}
	if names := poolNames(snap.SiblingPools); len(names) != 1 || names[0] != "p" {
		t.Errorf("SiblingPools = %v, want [p]", names)
	}
}
