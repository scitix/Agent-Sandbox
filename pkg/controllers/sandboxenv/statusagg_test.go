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

package sandboxenv

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// poolWithReplicas returns a member SandboxPool whose template matches the
// test Env's templateRef (so syncStatus marks it Active) and whose desired /
// idle / running counts are set for the aggregation assertions.
func poolWithReplicas(name string, desired, idle, running int32) *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			TemplateName: "tmpl",
			Replicas:     desired,
		},
		Status: agentsv1alpha1.SandboxPoolStatus{
			IdleReplicas:    idle,
			RunningReplicas: running,
		},
	}
}

// TestSyncStatus_AggregatesReplicas asserts that syncStatus sums desired /
// running / idle across every member (grouped and ungrouped) into the
// env-wide status scalars that back the printer columns, and counts members
// in MemberCount. Grouped members additionally roll up into ScalingGroups.
func TestSyncStatus_AggregatesReplicas(t *testing.T) {
	env := envWithMembers(
		agentsv1alpha1.EnvClusterMember{
			Name:   "env-a-foo",
			Config: agentsv1alpha1.EnvClusterMemberConfig{ScalingGroup: "g1"},
		},
		agentsv1alpha1.EnvClusterMember{
			// Ungrouped member: contributes to env-wide totals but not to
			// any ScalingGroup rollup.
			Name: "env-a-bar",
		},
	)

	foo := poolWithReplicas("env-a-foo", 10, 4, 6)
	bar := poolWithReplicas("env-a-bar", 3, 3, 0)

	scheme := newReconcileTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(env, foo, bar).
		WithStatusSubresource(&agentsv1alpha1.SandboxEnv{}).
		Build()
	r := &SandboxEnvReconciler{Client: c, Scheme: scheme, LocalClusterID: testLocalCluster}

	if err := r.syncStatus(context.Background(), env); err != nil {
		t.Fatalf("syncStatus: %v", err)
	}

	got := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}

	if got.Status.MemberCount != 2 {
		t.Errorf("MemberCount = %d, want 2", got.Status.MemberCount)
	}
	if got.Status.DesiredReplicas != 13 {
		t.Errorf("DesiredReplicas = %d, want 13", got.Status.DesiredReplicas)
	}
	if got.Status.RunningReplicas != 6 {
		t.Errorf("RunningReplicas = %d, want 6", got.Status.RunningReplicas)
	}
	if got.Status.IdleReplicas != 7 {
		t.Errorf("IdleReplicas = %d, want 7", got.Status.IdleReplicas)
	}

	// Only the grouped member rolls up into ScalingGroups.
	if len(got.Status.ScalingGroups) != 1 {
		t.Fatalf("ScalingGroups len = %d, want 1", len(got.Status.ScalingGroups))
	}
	g := got.Status.ScalingGroups[0]
	if g.Name != "g1" || g.TotalDesired != 10 || g.TotalRunning != 6 || g.TotalIdle != 4 {
		t.Errorf("group g1 = %+v, want {g1 idle=4 running=6 desired=10}", g)
	}
}
