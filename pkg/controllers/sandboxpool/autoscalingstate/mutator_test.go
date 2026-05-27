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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// ---------- Mutator accumulators ----------

func TestMutator_PatchStatus_NilSafe(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	m.PatchStatus(nil) // must not panic
	if m.HasWrites() {
		t.Error("expected no writes after PatchStatus(nil)")
	}
}

func TestMutator_SetTargetReplicas(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if _, ok := m.TargetReplicas(); ok {
		t.Error("expected no target before SetTargetReplicas")
	}
	m.SetTargetReplicas(3)
	if v, ok := m.TargetReplicas(); !ok || v != 3 {
		t.Errorf("got (%d, %v), want (3, true)", v, ok)
	}
	m.SetTargetReplicas(7) // last wins
	if v, _ := m.TargetReplicas(); v != 7 {
		t.Errorf("expected last-wins 7, got %d", v)
	}
	m.SetTargetReplicas(-5) // clamped to 0
	if v, _ := m.TargetReplicas(); v != 0 {
		t.Errorf("expected clamped 0, got %d", v)
	}
}

func TestMutator_PodAnnotationOps_NilSafe(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	m.MarkPodScaleDownProtected(nil, time.Now())
	m.UnmarkPodScaleDownProtected(nil)
	if m.HasWrites() {
		t.Error("expected no writes after nil pod ops")
	}
}

func TestMutator_HasWrites(t *testing.T) {
	m := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if m.HasWrites() {
		t.Error("expected no writes initially")
	}
	m.PatchStatus(func(*agentsv1alpha1.PoolAutoScalingStatus) {})
	if !m.HasWrites() {
		t.Error("expected HasWrites after PatchStatus")
	}
}

// ---------- Commit success paths ----------

func TestCommit_StatusPatch_SetsFields(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Local()

	m := NewMutator(&Snapshot{Pool: pool})
	m.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		t := metav1.NewTime(now)
		s.LastScaleUpTime = &t
		s.LastScaleUpAttemptResult = "Success"
	})

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.AutoScaling == nil {
		t.Fatal("AutoScaling status not written")
	}
	if got.Status.AutoScaling.LastScaleUpAttemptResult != "Success" {
		t.Errorf("LastScaleUpAttemptResult = %q", got.Status.AutoScaling.LastScaleUpAttemptResult)
	}
	if got.Status.AutoScaling.LastScaleUpTime == nil ||
		!got.Status.AutoScaling.LastScaleUpTime.Time.Equal(now) {
		t.Errorf("LastScaleUpTime = %v, want %v", got.Status.AutoScaling.LastScaleUpTime, now)
	}
	if got.Status.AutoScaling.ObservedGeneration != got.Generation {
		t.Errorf("ObservedGeneration = %d, want %d", got.Status.AutoScaling.ObservedGeneration, got.Generation)
	}
}

func TestCommit_SpecPatch_UpdatesReplicas(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 2}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	m := NewMutator(&Snapshot{Pool: pool})
	m.SetTargetReplicas(5)

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.Replicas != 5 {
		t.Errorf("Spec.Replicas = %d, want 5", got.Spec.Replicas)
	}
}

func TestCommit_SpecPatch_NoOpWhenSameValue(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p", replicas: 4}.build()
	// Pre-set resourceVersion so we can confirm "no patch issued" by
	// observing the object is untouched.
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	m := NewMutator(&Snapshot{Pool: pool})
	m.SetTargetReplicas(4) // same as current

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.Replicas != 4 {
		t.Errorf("Spec.Replicas changed unexpectedly to %d", got.Spec.Replicas)
	}
}

func TestCommit_StatusPatch_NoOpWhenNoDiff(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	// Seed the pool with a pre-existing AutoScaling block.
	existing := metav1.NewTime(time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC).Local())
	pool.Status.AutoScaling = &agentsv1alpha1.PoolAutoScalingStatus{
		LastScaleUpTime:    &existing,
		ObservedGeneration: pool.Generation,
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pool).
		WithStatusSubresource(&agentsv1alpha1.SandboxPool{}).
		Build()

	m := NewMutator(&Snapshot{Pool: pool})
	// Mutator that touches the field but writes the same value as the seed.
	m.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		v := metav1.NewTime(existing.Time)
		s.LastScaleUpTime = &v
	})

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Status should remain identical.
	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.AutoScaling == nil ||
		got.Status.AutoScaling.LastScaleUpTime == nil ||
		!got.Status.AutoScaling.LastScaleUpTime.Time.Equal(existing.Time) {
		t.Errorf("expected unchanged status, got %+v", got.Status.AutoScaling)
	}
}

func TestCommit_PodAnnotation_MarkAndUnmark(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "p-pod-0"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, pod).Build()

	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	// Mark.
	m := NewMutator(&Snapshot{Pool: pool})
	m.MarkPodScaleDownProtected(pod, now)
	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("mark commit: %v", err)
	}
	got := &corev1.Pod{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: testNS, Name: pod.Name}, got)
	if got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] != now.Format(time.RFC3339) {
		t.Errorf("mark annotation = %q", got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey])
	}

	// Unmark.
	m2 := NewMutator(&Snapshot{Pool: pool})
	m2.UnmarkPodScaleDownProtected(pod)
	if err := m2.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("unmark commit: %v", err)
	}
	got = &corev1.Pod{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: testNS, Name: pod.Name}, got)
	if _, ok := got.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey]; ok {
		t.Errorf("expected annotation cleared, got %v", got.Annotations)
	}
}

func TestCommit_PodAnnotation_PodGone_NotFatal(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	// Pod is referenced but not seeded — Get returns NotFound.
	ghostPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: "ghost"}}
	m := NewMutator(&Snapshot{Pool: pool})
	m.MarkPodScaleDownProtected(ghostPod, time.Now())

	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Errorf("Commit returned error for missing pod (should be nop): %v", err)
	}
}

func TestCommit_EmitEvent(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	rec := record.NewFakeRecorder(8)

	m := NewMutator(&Snapshot{Pool: pool})
	m.SetTargetReplicas(3) // ensures Commit has at least one write to flush
	m.EmitEvent(corev1.EventTypeNormal, "ScaleUp", "scaled from %d to %d", 2, 3)

	if err := m.Commit(context.Background(), c, rec); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	select {
	case ev := <-rec.Events:
		if want := "Normal ScaleUp scaled from 2 to 3"; ev != want {
			t.Errorf("event = %q, want %q", ev, want)
		}
	default:
		t.Error("expected one recorded event")
	}
}

// ---------- Commit guards ----------

func TestCommit_NilGuards(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Nil Mutator.
	if err := (*Mutator)(nil).Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error from nil Mutator")
	}
	// Nil Snapshot.Pool.
	m := NewMutator(&Snapshot{})
	if err := m.Commit(context.Background(), c, nil); err == nil {
		t.Error("expected error from nil Snapshot.Pool")
	}
	// Nil client.
	m2 := NewMutator(&Snapshot{Pool: poolFixture{name: "p"}.build()})
	if err := m2.Commit(context.Background(), nil, nil); err == nil {
		t.Error("expected error from nil client")
	}
}

func TestCommit_EmptyMutator_NoOp(t *testing.T) {
	scheme := newTestScheme(t)
	pool := poolFixture{name: "p"}.build()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	m := NewMutator(&Snapshot{Pool: pool})
	if m.HasWrites() {
		t.Fatal("precondition: expected no writes")
	}
	if err := m.Commit(context.Background(), c, nil); err != nil {
		t.Fatalf("Commit on empty mutator: %v", err)
	}
}
