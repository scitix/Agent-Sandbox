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

// testRevision is the settled revision hash used by the condition tests; a
// rollout is simulated by pointing UpdateRevision at a different value.
const testRevision = "hash-1"

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

// syncStatusForTest runs syncStatus against a fake client seeded with env +
// pools and returns the persisted Env.
func syncStatusForTest(t *testing.T, env *agentsv1alpha1.SandboxEnv, pools ...*agentsv1alpha1.SandboxPool) *agentsv1alpha1.SandboxEnv {
	t.Helper()
	scheme := newReconcileTestScheme(t)
	b := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(env).
		WithStatusSubresource(&agentsv1alpha1.SandboxEnv{})
	for _, p := range pools {
		b = b.WithObjects(p)
	}
	c := b.Build()
	r := &SandboxEnvReconciler{Client: c, Scheme: scheme, LocalClusterID: testLocalCluster}
	if err := r.syncStatus(context.Background(), env); err != nil {
		t.Fatalf("syncStatus: %v", err)
	}
	got := &agentsv1alpha1.SandboxEnv{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: env.Namespace, Name: env.Name}, got); err != nil {
		t.Fatalf("get env: %v", err)
	}
	return got
}

// assertCondition fails unless the named condition carries the wanted status
// and reason.
func assertCondition(t *testing.T, env *agentsv1alpha1.SandboxEnv, condType string, want metav1.ConditionStatus, wantReason string) {
	t.Helper()
	for _, c := range env.Status.Conditions {
		if c.Type != condType {
			continue
		}
		if c.Status != want || c.Reason != wantReason {
			t.Errorf("%s = %s/%s, want %s/%s (message: %q)", condType, c.Status, c.Reason, want, wantReason, c.Message)
		}
		return
	}
	t.Errorf("condition %s not found in %+v", condType, env.Status.Conditions)
}

// localMember returns the single observed member of the local cluster segment.
func localMember(t *testing.T, env *agentsv1alpha1.SandboxEnv) agentsv1alpha1.EnvObservedMember {
	t.Helper()
	for _, c := range env.Status.Clusters {
		if c.ClusterID != testLocalCluster {
			continue
		}
		if len(c.ObservedMembers) != 1 {
			t.Fatalf("observedMembers len = %d, want 1", len(c.ObservedMembers))
		}
		return c.ObservedMembers[0]
	}
	t.Fatalf("no local cluster segment in %+v", env.Status.Clusters)
	return agentsv1alpha1.EnvObservedMember{}
}

// TestSyncStatus_TemplateVersionDriftDoesNotAffectReady asserts that a member
// rendered from a newer Template spec.version than the one recorded in
// spec.templateRef.version stays Active. The Env has no way to serve the older
// version (Templates are single mutable objects) so the drift is unresolvable
// and must not be reported as an Env-level problem; the version the member
// actually carries is surfaced as an observation instead.
func TestSyncStatus_TemplateVersionDriftDoesNotAffectReady(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})
	env.Spec.TemplateRef.Version = "0.2.12"

	pool := poolWithReplicas("env-a-foo", 80, 78, 2)
	pool.Annotations = map[string]string{
		agentsv1alpha1.SandboxPoolTemplateVersionAnnotationKey: "0.2.23",
	}
	// Fully converged onto the current revision: no rollout in flight.
	pool.Status.UpdateRevision = testRevision
	pool.Status.CurrentRevision = testRevision
	pool.Status.UpdatedReplicas = 80

	got := syncStatusForTest(t, env, pool)

	m := localMember(t, got)
	if m.State != agentsv1alpha1.ObservedMemberStateActive {
		t.Errorf("member state = %q, want Active", m.State)
	}
	if m.TemplateVersion != "0.2.23" {
		t.Errorf("member templateVersion = %q, want 0.2.23 (the version actually rendered)", m.TemplateVersion)
	}
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionReady, metav1.ConditionTrue, "AllMembersActive")
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionTemplateConsistent, metav1.ConditionTrue, "TemplatesMatch")
}

// TestSyncStatus_TemplateNameMismatchFlipsReady asserts the remaining
// Inconsistent trigger — a member Pool bound to a different SandboxTemplate
// than the Env's templateRef.name — still marks the member non-Active and
// fails both conditions.
func TestSyncStatus_TemplateNameMismatchFlipsReady(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})

	pool := poolWithReplicas("env-a-foo", 10, 10, 0)
	pool.Spec.TemplateName = "some-other-template"

	got := syncStatusForTest(t, env, pool)

	if m := localMember(t, got); m.State != agentsv1alpha1.ObservedMemberStateInconsistent {
		t.Errorf("member state = %q, want Inconsistent", m.State)
	}
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionReady, metav1.ConditionFalse, "Inconsistent")
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionTemplateConsistent, metav1.ConditionFalse, "TemplateMismatch")
}

// TestSyncStatus_RolloutInProgressKeepsMembersActive asserts that an
// unfinished rollout (the revision-hash signal that replaced the version
// comparison) is reported through TemplateConsistent without taking the Env
// out of Ready — the member still serves requests while its Pods roll.
func TestSyncStatus_RolloutInProgressKeepsMembersActive(t *testing.T) {
	env := envWithMembers(agentsv1alpha1.EnvClusterMember{Name: "env-a-foo"})

	pool := poolWithReplicas("env-a-foo", 10, 10, 0)
	pool.Status.UpdateRevision = "hash-2"
	pool.Status.CurrentRevision = testRevision
	pool.Status.UpdatedReplicas = 4

	got := syncStatusForTest(t, env, pool)

	if m := localMember(t, got); m.State != agentsv1alpha1.ObservedMemberStateActive {
		t.Errorf("member state = %q, want Active", m.State)
	}
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionReady, metav1.ConditionTrue, "AllMembersActive")
	assertCondition(t, got, agentsv1alpha1.SandboxEnvConditionTemplateConsistent, metav1.ConditionFalse, "RolloutInProgress")
}
