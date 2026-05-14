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

package sandboxpool

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

// ── helpers ────────────────────────────────────────────────────────────────────

func makePool(name string, replicas int32, minReplicas *int32, scaleDown *agentsv1alpha1.PoolScaleDownPolicy) *agentsv1alpha1.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:    replicas,
			MinReplicas: minReplicas,
		},
	}
	if scaleDown != nil {
		pool.Spec.Autoscaling = &agentsv1alpha1.PoolAutoscalingSpec{
			Enabled:         true,
			ScaleDownPolicy: scaleDown,
		}
	}
	return pool
}

func i32ptr(v int32) *int32 { return &v }

// makeIdlePodWithAge creates an idle pod whose inplace-update-state timestamp
// indicates it has been idle for the given duration.
func makeIdlePodWithAge(name string, idleFor time.Duration) corev1.Pod {
	since := time.Now().UTC().Add(-idleFor).Truncate(time.Second)
	stateJSON := `{"phase":"completed","targetPodPhase":"idle","updateTimestamp":"` + since.Format(time.RFC3339) + `"}`
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: stateJSON,
			},
		},
	}
}

// ── TestOldestIdleSince ────────────────────────────────────────────────────────

func TestOldestIdleSince_Empty(t *testing.T) {
	if got := oldestIdleSince(nil); got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

func TestOldestIdleSince_SinglePod(t *testing.T) {
	pod := makeIdlePodWithAge("pod-1", 5*time.Minute)
	got := oldestIdleSince([]corev1.Pod{pod})
	if got == nil {
		t.Fatal("expected non-nil timestamp")
	}
	elapsed := time.Since(*got)
	if elapsed < 4*time.Minute || elapsed > 6*time.Minute {
		t.Errorf("unexpected idle-since: %v (elapsed %v)", got, elapsed)
	}
}

func TestOldestIdleSince_PicksOldest(t *testing.T) {
	pods := []corev1.Pod{
		makeIdlePodWithAge("pod-young", 2*time.Minute),
		makeIdlePodWithAge("pod-old", 10*time.Minute),
		makeIdlePodWithAge("pod-medium", 5*time.Minute),
	}
	got := oldestIdleSince(pods)
	if got == nil {
		t.Fatal("expected non-nil timestamp")
	}
	// The oldest pod was idle for ~10 minutes, so since-time should be ~10 min ago.
	elapsed := time.Since(*got)
	if elapsed < 9*time.Minute || elapsed > 11*time.Minute {
		t.Errorf("should have selected the oldest pod; elapsed=%v", elapsed)
	}
}

// ── TestEffectiveMinReplicas ───────────────────────────────────────────────────

func TestEffectiveMinReplicas_Nil(t *testing.T) {
	pool := makePool("p", 5, nil, nil)
	if got := effectiveMinReplicas(pool); got != 0 {
		t.Errorf("expected 0 for nil MinReplicas, got %d", got)
	}
}

func TestEffectiveMinReplicas_NonNil(t *testing.T) {
	pool := makePool("p", 5, i32ptr(2), nil)
	if got := effectiveMinReplicas(pool); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// ── TestSyncIdleZeroSince ─────────────────────────────────────────────────────

func TestSyncIdleZeroSince_SetWhenIdleZero(t *testing.T) {
	pool := makePool("pool-a", 3, nil, nil)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	if err := r.syncIdleZeroSince(context.Background(), pool, 0); err != nil {
		t.Fatalf("syncIdleZeroSince: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Status.IdleZeroSince == nil {
		t.Error("expected IdleZeroSince to be set, got nil")
	}
}

func TestSyncIdleZeroSince_ClearWhenIdleNonZero(t *testing.T) {
	now := metav1.Now()
	pool := makePool("pool-a", 3, nil, nil)
	pool.Status.IdleZeroSince = &now
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	if err := r.syncIdleZeroSince(context.Background(), pool, 2); err != nil {
		t.Fatalf("syncIdleZeroSince: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Status.IdleZeroSince != nil {
		t.Errorf("expected IdleZeroSince to be cleared, got %v", updated.Status.IdleZeroSince)
	}
}

func TestSyncIdleZeroSince_NoOpWhenAlreadySet(t *testing.T) {
	ts := metav1.NewTime(time.Now().Add(-time.Minute))
	pool := makePool("pool-a", 3, nil, nil)
	pool.Status.IdleZeroSince = &ts
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	if err := r.syncIdleZeroSince(context.Background(), pool, 0); err != nil {
		t.Fatalf("syncIdleZeroSince: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	// IdleZeroSince should remain unchanged (should not be refreshed to now).
	if updated.Status.IdleZeroSince == nil {
		t.Error("expected IdleZeroSince to remain set, got nil")
	}
	// It should still point to roughly a minute ago, not 'now'.
	if time.Since(updated.Status.IdleZeroSince.Time) < 30*time.Second {
		t.Errorf("IdleZeroSince was unexpectedly reset to a recent time: %v", updated.Status.IdleZeroSince)
	}
}

// ── TestReconcileScaleDown ─────────────────────────────────────────────────────

func makeScaleDownPolicy(idleTimeoutSec, stabilizationSec int32) *agentsv1alpha1.PoolScaleDownPolicy {
	return &agentsv1alpha1.PoolScaleDownPolicy{
		IdleTimeoutSeconds:      idleTimeoutSec,
		StabilizationSeconds:    stabilizationSec,
		ProtectionWindowSeconds: 10,
	}
}

func TestReconcileScaleDown_Disabled(t *testing.T) {
	pool := makePool("pool-a", 5, i32ptr(1), nil)
	// autoscaling is nil → disabled
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	result, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("spec.replicas should be unchanged, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_NoIdlePods(t *testing.T) {
	pool := makePool("pool-a", 5, i32ptr(1), makeScaleDownPolicy(60, 60))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleDown(context.Background(), pool, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue when no idle pods, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("spec.replicas should be unchanged, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_AtMinReplicas(t *testing.T) {
	pool := makePool("pool-a", 2, i32ptr(2), makeScaleDownPolicy(60, 60))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	result, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue at minReplicas, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 2 {
		t.Errorf("spec.replicas should not drop below minReplicas, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_StabilizationNotPassed(t *testing.T) {
	pool := makePool("pool-a", 5, i32ptr(1), makeScaleDownPolicy(60, 120))
	// Last scale-down was 30s ago; stabilization is 120s.
	recentScaleDown := metav1.NewTime(time.Now().Add(-30 * time.Second))
	pool.Status.LastScaleDownTime = &recentScaleDown
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	result, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue for the remaining stabilization time (~90s ± jitter).
	if result.RequeueAfter == 0 {
		t.Error("expected a RequeueAfter when stabilization window has not passed")
	}
	if result.RequeueAfter > 95*time.Second {
		t.Errorf("RequeueAfter looks too large: %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("spec.replicas should not change during stabilization, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_PodNotYetExpired(t *testing.T) {
	// Idle timeout is 5 minutes; pod has only been idle for 2 minutes.
	pool := makePool("pool-a", 5, i32ptr(1), makeScaleDownPolicy(300, 60))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 2*time.Minute)}
	result, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue for ~3 minutes remaining.
	if result.RequeueAfter == 0 {
		t.Error("expected a RequeueAfter when pod idle time < idleTimeout")
	}
	if result.RequeueAfter > 3*time.Minute+10*time.Second {
		t.Errorf("RequeueAfter is too large: %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("spec.replicas should not change before idle timeout, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_ShouldScale(t *testing.T) {
	// Pod has been idle for 10 minutes; timeout is 5 minutes → scale down.
	pool := makePool("pool-a", 5, i32ptr(1), makeScaleDownPolicy(300, 60))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	result, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue after stabilization window.
	if result.RequeueAfter == 0 {
		t.Error("expected a RequeueAfter (stabilization period) after scale-down")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 4 {
		t.Errorf("expected spec.replicas to decrease to 4, got %d", updated.Spec.Replicas)
	}
	if updated.Status.LastScaleDownTime == nil {
		t.Error("expected LastScaleDownTime to be set after scale-down")
	}
}

func TestReconcileScaleDown_RespectsMinReplicas_ZeroMin(t *testing.T) {
	// No minReplicas set → effective min is 0; should still scale down.
	pool := makePool("pool-a", 1, nil, makeScaleDownPolicy(60, 30))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	_, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 0 {
		t.Errorf("expected spec.replicas=0 when minReplicas is nil/0 and one idle expired pod, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleDown_MultipleScaleDownsConverge(t *testing.T) {
	// 3 idle pods all expired; autoscaler should scale down by 1 per call.
	pool := makePool("pool-a", 4, i32ptr(1), makeScaleDownPolicy(60, 0))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{
		makeIdlePodWithAge("pod-1", 5*time.Minute),
		makeIdlePodWithAge("pod-2", 6*time.Minute),
		makeIdlePodWithAge("pod-3", 7*time.Minute),
	}

	// First call: replicas 4 → 3
	if _, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 0); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Reload pool (Patch changes spec.replicas in-cluster but our local pool object was patched in-place)
	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("after 1st call: expected replicas=3, got %d", updated.Spec.Replicas)
	}

	// Second call (stabilization=0 so no cooldown): replicas 3 → 2
	if _, err := r.reconcileScaleDown(context.Background(), updated, idlePods, 0); err != nil {
		t.Fatalf("second call: %v", err)
	}
	updated2 := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated2); err != nil {
		t.Fatalf("get pool after 2nd call: %v", err)
	}
	if updated2.Spec.Replicas != 2 {
		t.Errorf("after 2nd call: expected replicas=2, got %d", updated2.Spec.Replicas)
	}
}

// ── helpers for scale-up tests ────────────────────────────────────────────────

// makeScaleUpPool creates a SandboxPool with autoscaling enabled and a ScaleUpPolicy.
// maxReplicas of 0 means no cap (set pool.Spec.MaxReplicas manually if needed).
func makeScaleUpPool(name string, replicas int32, maxReplicas *int32, scaleUp *agentsv1alpha1.PoolScaleUpPolicy) *agentsv1alpha1.SandboxPool {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:    replicas,
			MaxReplicas: maxReplicas,
			Autoscaling: &agentsv1alpha1.PoolAutoscalingSpec{
				Enabled:       true,
				ScaleUpPolicy: scaleUp,
			},
		},
	}
	return pool
}

func makeScaleUpPolicy(mode agentsv1alpha1.PoolScaleUpMode, cooldownSec, idleThresholdSec int32) *agentsv1alpha1.PoolScaleUpPolicy {
	return &agentsv1alpha1.PoolScaleUpPolicy{
		Mode:                 mode,
		CooldownSeconds:      cooldownSec,
		IdleThresholdSeconds: idleThresholdSec,
	}
}

// setIdleZeroSince is a test helper that sets pool.Status.IdleZeroSince to
// time.Now() minus ageDuration, simulating a pool that has had 0 idle pods
// for the specified duration.
func setIdleZeroSince(pool *agentsv1alpha1.SandboxPool, ageDuration time.Duration) {
	t := metav1.NewTime(time.Now().Add(-ageDuration))
	pool.Status.IdleZeroSince = &t
}

// ── TestReconcileScaleUp ──────────────────────────────────────────────────────

func TestReconcileScaleUp_Disabled(t *testing.T) {
	// autoscaling == nil → should be a no-op
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec:       agentsv1alpha1.SandboxPoolSpec{Replicas: 3},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas should be unchanged, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_AtMaxReplicas(t *testing.T) {
	// replicas == maxReplicas → already at ceiling, no scale-up
	pool := makeScaleUpPool("pool-a", 5, i32ptr(5),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue at maxReplicas, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("spec.replicas should not change at ceiling, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_CooldownNotPassed(t *testing.T) {
	pool := makeScaleUpPool("pool-a", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 60, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	// Last scale-up was 20s ago; cooldown is 60s
	recent := metav1.NewTime(time.Now().Add(-20 * time.Second))
	pool.Status.LastScaleUpTime = &recent
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue for the remaining cooldown (~40s ± jitter)
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter during cooldown, got 0")
	}
	if result.RequeueAfter > 55*time.Second {
		t.Errorf("RequeueAfter looks too large: %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas must not change during cooldown, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_RecentScaleDown_Skips(t *testing.T) {
	// LastScaleDownTime within cooldown → soft guard prevents scale-up
	pool := makeScaleUpPool("pool-a", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 60, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	recent := metav1.NewTime(time.Now().Add(-10 * time.Second))
	pool.Status.LastScaleDownTime = &recent
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter (soft guard) when recent scale-down, got 0")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas must not change due to recent scale-down guard, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_NoIdleZeroSince(t *testing.T) {
	// IdleZeroSince == nil → pool has idle pods, no scale-up needed
	pool := makeScaleUpPool("pool-a", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	// Do NOT set IdleZeroSince
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue when IdleZeroSince is nil, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas should be unchanged, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_IdleZeroThresholdZero(t *testing.T) {
	// IdleThresholdSeconds == 0 → proactive scale-up disabled
	pool := makeScaleUpPool("pool-a", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 0))
	setIdleZeroSince(pool, 5*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue when idleThreshold=0, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas should not change when threshold=0, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_IdleZeroNotLongEnough(t *testing.T) {
	// IdleZeroSince set 10s ago; threshold is 60s → not yet triggered
	pool := makeScaleUpPool("pool-a", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 60))
	setIdleZeroSince(pool, 10*time.Second)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should requeue for ~50s
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter when idle threshold not yet reached, got 0")
	}
	if result.RequeueAfter > 55*time.Second {
		t.Errorf("RequeueAfter looks too large: %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("spec.replicas should not change yet, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_Conservative(t *testing.T) {
	// Conservative mode: +1 per scale-up
	pool := makeScaleUpPool("pool-a", 4, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter (cooldown) after scale-up, got 0")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 5 {
		t.Errorf("Conservative: expected replicas=5 (4+1), got %d", updated.Spec.Replicas)
	}
	if updated.Status.LastScaleUpTime == nil {
		t.Error("expected LastScaleUpTime to be set after scale-up")
	}
}

func TestReconcileScaleUp_Default(t *testing.T) {
	// Default mode: +max(1, ceil(4/2)) = +2
	pool := makeScaleUpPool("pool-a", 4, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	_, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 6 {
		t.Errorf("Default: expected replicas=6 (4+ceil(4/2)=4+2), got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_Aggressive(t *testing.T) {
	// Aggressive mode: current*2 = 4*2 = 8
	pool := makeScaleUpPool("pool-a", 4, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	_, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 8 {
		t.Errorf("Aggressive: expected replicas=8 (4*2), got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_AggressiveFromZero(t *testing.T) {
	// Aggressive mode from 0: should go to 1 (not 0*2=0)
	pool := makeScaleUpPool("pool-a", 0, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	_, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 1 {
		t.Errorf("Aggressive from 0: expected replicas=1, got %d", updated.Spec.Replicas)
	}
}

func TestReconcileScaleUp_CappedByMaxReplicas(t *testing.T) {
	// Aggressive would scale to 4*2=8 but maxReplicas=6
	pool := makeScaleUpPool("pool-a", 4, i32ptr(6),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	setIdleZeroSince(pool, 2*time.Minute)
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	_, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 6 {
		t.Errorf("expected replicas capped at maxReplicas=6, got %d", updated.Spec.Replicas)
	}
}

// ── TestComputeScaleUpTarget ──────────────────────────────────────────────────

func TestComputeScaleUpTarget_Conservative(t *testing.T) {
	pool := makeScaleUpPool("p", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 30))
	if got := computeScaleUpTarget(pool, 0); got != 4 {
		t.Errorf("Conservative 3→expected 4, got %d", got)
	}
}

func TestComputeScaleUpTarget_Default_Even(t *testing.T) {
	pool := makeScaleUpPool("p", 4, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	// step = max(1, ceil(4/2)) = 2 → target = 6
	if got := computeScaleUpTarget(pool, 0); got != 6 {
		t.Errorf("Default 4→expected 6, got %d", got)
	}
}

func TestComputeScaleUpTarget_Default_Odd(t *testing.T) {
	pool := makeScaleUpPool("p", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	// step = max(1, ceil(3/2)) = ceil(1.5) = 2 → target = 5
	if got := computeScaleUpTarget(pool, 0); got != 5 {
		t.Errorf("Default 3→expected 5, got %d", got)
	}
}

func TestComputeScaleUpTarget_Default_One(t *testing.T) {
	pool := makeScaleUpPool("p", 1, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeDefault, 30, 30))
	// step = max(1, ceil(1/2)) = max(1, 1) = 1 → target = 2
	if got := computeScaleUpTarget(pool, 0); got != 2 {
		t.Errorf("Default 1→expected 2, got %d", got)
	}
}

func TestComputeScaleUpTarget_Aggressive(t *testing.T) {
	pool := makeScaleUpPool("p", 3, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	// 3*2 = 6
	if got := computeScaleUpTarget(pool, 0); got != 6 {
		t.Errorf("Aggressive 3→expected 6, got %d", got)
	}
}

func TestComputeScaleUpTarget_Aggressive_Zero(t *testing.T) {
	pool := makeScaleUpPool("p", 0, nil,
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	// 0 → 1 (special case)
	if got := computeScaleUpTarget(pool, 0); got != 1 {
		t.Errorf("Aggressive 0→expected 1, got %d", got)
	}
}

func TestComputeScaleUpTarget_CappedByMax(t *testing.T) {
	pool := makeScaleUpPool("p", 4, i32ptr(5),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeAggressive, 30, 30))
	// 4*2 = 8 but maxReplicas=5
	if got := computeScaleUpTarget(pool, 5); got != 5 {
		t.Errorf("expected cap at maxReplicas=5, got %d", got)
	}
}

func TestComputeScaleUpTarget_DefaultMode_NilPolicy(t *testing.T) {
	// No ScaleUpPolicy → falls back to Default mode
	pool := makeScaleUpPool("p", 2, nil, nil)
	// step = max(1, ceil(2/2)) = 1 → target = 3
	if got := computeScaleUpTarget(pool, 0); got != 3 {
		t.Errorf("Default (nil policy) 2→expected 3, got %d", got)
	}
}

// ── TestSyncAutoscaling_ScaleUpSkipsScaleDown ─────────────────────────────────

// TestSyncAutoscaling_ScaleUpSkipsScaleDown verifies that when both scale-up
// and scale-down conditions are met in the same reconcile cycle, scale-up wins
// and scale-down is skipped.
//
// Scenario: pool has replicas=3, no idle pods right now (IdleZeroSince is set).
// Scale-up threshold is met → scale-up fires (3→4).
// Because scale-up changed replicas, syncAutoscaling must skip the scale-down
// path entirely in this cycle.
func TestSyncAutoscaling_ScaleUpSkipsScaleDown(t *testing.T) {
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:    3,
			MaxReplicas: i32ptr(10),
			Autoscaling: &agentsv1alpha1.PoolAutoscalingSpec{
				Enabled: true,
				ScaleUpPolicy: &agentsv1alpha1.PoolScaleUpPolicy{
					Mode:                 agentsv1alpha1.PoolScaleUpModeConservative,
					CooldownSeconds:      0,
					IdleThresholdSeconds: 30,
				},
				ScaleDownPolicy: &agentsv1alpha1.PoolScaleDownPolicy{
					IdleTimeoutSeconds:      60,
					StabilizationSeconds:    0,
					ProtectionWindowSeconds: 10,
				},
			},
		},
	}
	// IdleZeroSince 2 minutes ago — scale-up threshold (30s) is met.
	setIdleZeroSince(pool, 2*time.Minute)

	// No idle pods right now (consistent with IdleZeroSince being set).
	// Even though idle pods are empty, scale-down conditions would normally
	// short-circuit (no idle pods → no scale-down). The test verifies that the
	// scale-up path runs and does NOT get overridden by a scale-down decision.
	var idlePods []corev1.Pod

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	_, err := r.syncAutoscaling(context.Background(), pool, idlePods, 0)
	if err != nil {
		t.Fatalf("syncAutoscaling: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	// Conservative scale-up fired: 3 → 4
	if updated.Spec.Replicas != 4 {
		t.Errorf("expected scale-up to 4, got %d", updated.Spec.Replicas)
	}
	if updated.Status.LastScaleUpTime == nil {
		t.Error("expected LastScaleUpTime to be set after scale-up")
	}
}

// TestSyncAutoscaling_ScaleDownNotCalledAfterScaleUp verifies the syncAutoscaling
// early-return guard: when reconcileScaleUp changes spec.replicas, the function
// returns immediately without calling reconcileScaleDown in the same cycle.
//
// Note: scale-up requires IdleZeroSince to be set (no idle pods), while
// scale-down requires idle pods — these conditions are mutually exclusive in
// steady state, because syncIdleZeroSince clears IdleZeroSince when idle pods
// are non-empty. This test validates the guard by calling reconcileScaleUp
// directly on a pool where scale-up will fire, then checking that a second call
// to syncAutoscaling with the updated pool (now has scale-up cooldown) and
// ── TestReconcileScaleUp – createPending annotation (Condition 1) ─────────────

// setScaleUpPendingAnnotation is a test helper that writes the
// PoolScaleUpPendingAnnotationKey annotation onto pool with the given age,
// simulating an annotation written by the scheduler.
func setScaleUpPendingAnnotation(pool *agentsv1alpha1.SandboxPool, age time.Duration) {
	if pool.Annotations == nil {
		pool.Annotations = map[string]string{}
	}
	pool.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey] =
		time.Now().Add(-age).UTC().Format(time.RFC3339)
}

// TestReconcileScaleUp_CreatePendingTrigger: a fresh PoolScaleUpPendingAnnotationKey
// annotation triggers an immediate scale-up even when IdleZeroSince is nil.
// After the scale-up the annotation must be cleared and LastScaleUpTime set.
func TestReconcileScaleUp_CreatePendingTrigger(t *testing.T) {
	pool := makeScaleUpPool("pool-cp", 2, i32ptr(10),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 60))
	// IdleZeroSince is NOT set – pool may still have idle pods; only annotation matters.
	setScaleUpPendingAnnotation(pool, 5*time.Second) // fresh annotation (5s old)

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return a cooldown requeue after scale-up.
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter (cooldown) after scale-up via createPending")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	// Conservative +1: 2 → 3.
	if updated.Spec.Replicas != 3 {
		t.Errorf("expected replicas=3 after createPending scale-up, got %d", updated.Spec.Replicas)
	}
	// Annotation must be cleared.
	if _, ok := updated.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey]; ok {
		t.Error("PoolScaleUpPendingAnnotationKey must be cleared after scale-up")
	}
	// LastScaleUpTime must be set.
	if updated.Status.LastScaleUpTime == nil {
		t.Error("expected LastScaleUpTime to be set after scale-up")
	}
}

// TestReconcileScaleUp_CreatePendingBypassesIdleThreshold: annotation bypasses the
// idleThresholdSeconds wait even when IdleZeroSince has been set for a short time.
func TestReconcileScaleUp_CreatePendingBypassesIdleThreshold(t *testing.T) {
	// idleThresholdSeconds=60; without the annotation we'd have to wait 60s.
	pool := makeScaleUpPool("pool-cp2", 3, i32ptr(10),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 60))
	// IdleZeroSince set 5s ago — far short of the 60s idle threshold.
	setIdleZeroSince(pool, 5*time.Second)
	// Fresh annotation should bypass the threshold.
	setScaleUpPendingAnnotation(pool, 2*time.Second)

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter (cooldown) after immediate scale-up")
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	// Conservative +1: 3 → 4.
	if updated.Spec.Replicas != 4 {
		t.Errorf("expected replicas=4 (annotation bypass), got %d", updated.Spec.Replicas)
	}
}

// TestReconcileScaleUp_StaleAnnotationIgnored: an annotation older than
// max(2×cooldown, 2min) must not trigger a scale-up.
func TestReconcileScaleUp_StaleAnnotationIgnored(t *testing.T) {
	// cooldown=30s → maxAge = max(2×30s, 2min) = 2min; annotation is 10min old.
	pool := makeScaleUpPool("pool-stale", 2, i32ptr(10),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 60))
	// IdleZeroSince NOT set — proactive trigger also won't fire.
	setScaleUpPendingAnnotation(pool, 10*time.Minute) // stale

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Neither condition triggers → no requeue, no scale-up.
	if result.RequeueAfter != 0 {
		t.Errorf("stale annotation must not cause requeue, got %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 2 {
		t.Errorf("stale annotation must not change replicas, got %d", updated.Spec.Replicas)
	}
}

// TestReconcileScaleUp_CooldownBlocksCreatePending: a fresh createPending annotation
// is present but the cooldown window has not elapsed → scale-up must be blocked and
// a requeue scheduled for when the cooldown expires.
func TestReconcileScaleUp_CooldownBlocksCreatePending(t *testing.T) {
	// cooldown=30s; LastScaleUpTime=10s ago → 20s remaining.
	pool := makeScaleUpPool("pool-cd", 2, i32ptr(10),
		makeScaleUpPolicy(agentsv1alpha1.PoolScaleUpModeConservative, 30, 60))
	recent := metav1.NewTime(time.Now().Add(-10 * time.Second))
	pool.Status.LastScaleUpTime = &recent
	setScaleUpPendingAnnotation(pool, 2*time.Second) // fresh annotation

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	result, err := r.reconcileScaleUp(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Cooldown not elapsed → requeue for ~20s.
	if result.RequeueAfter == 0 {
		t.Error("expected RequeueAfter while in cooldown window, got 0")
	}
	if result.RequeueAfter > 25*time.Second {
		t.Errorf("RequeueAfter too large during cooldown: %v", result.RequeueAfter)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 2 {
		t.Errorf("replicas must not change during cooldown, got %d", updated.Spec.Replicas)
	}
}

// idle pods does NOT also decrement replicas in the same cycle.
func TestSyncAutoscaling_ScaleDownNotCalledAfterScaleUp(t *testing.T) {
	// Step 1: call syncAutoscaling with 0 idle pods + IdleZeroSince set.
	// Scale-up should fire (2→3). syncIdleZeroSince is a no-op (idleCount=0,
	// IdleZeroSince already set). Scale-down is skipped because replicas > prevReplicas.
	pool := &agentsv1alpha1.SandboxPool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool-b", Namespace: "default"},
		Spec: agentsv1alpha1.SandboxPoolSpec{
			Replicas:    2,
			MaxReplicas: i32ptr(10),
			Autoscaling: &agentsv1alpha1.PoolAutoscalingSpec{
				Enabled: true,
				ScaleUpPolicy: &agentsv1alpha1.PoolScaleUpPolicy{
					Mode:                 agentsv1alpha1.PoolScaleUpModeConservative,
					CooldownSeconds:      0,
					IdleThresholdSeconds: 10,
				},
				ScaleDownPolicy: &agentsv1alpha1.PoolScaleDownPolicy{
					IdleTimeoutSeconds:      60,
					StabilizationSeconds:    0,
					ProtectionWindowSeconds: 10,
				},
			},
		},
	}
	setIdleZeroSince(pool, 2*time.Minute)

	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	// Call syncAutoscaling with 0 idle pods — scale-up fires, scale-down is skipped.
	_, err := r.syncAutoscaling(context.Background(), pool, nil, 0)
	if err != nil {
		t.Fatalf("syncAutoscaling: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 3 {
		t.Errorf("expected scale-up to 3, got %d", updated.Spec.Replicas)
	}
	if updated.Status.LastScaleUpTime == nil {
		t.Error("expected LastScaleUpTime set")
	}
	if updated.Status.LastScaleDownTime != nil {
		t.Errorf("LastScaleDownTime must NOT be set when scale-down was skipped, got %v", updated.Status.LastScaleDownTime)
	}
}

// TestReconcileScaleDown_RunningFloor verifies that spec.replicas is never reduced
// below the current running count, even when idle pods have expired.
func TestReconcileScaleDown_RunningFloor(t *testing.T) {
	// Pool has replicas=2, minReplicas=0, one idle expired pod, one running sandbox.
	// The running floor should prevent a scale-down to 1 (which equals running count).
	pool := makePool("pool-a", 2, i32ptr(0), makeScaleDownPolicy(60, 0))
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli}

	idlePods := []corev1.Pod{makeIdlePodWithAge("pod-1", 10*time.Minute)}
	_, err := r.reconcileScaleDown(context.Background(), pool, idlePods, 1 /* runningReplicas */)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	// replicas=2, running=1 → lowerBound=max(0,1)=1; replicas(2) > lowerBound(1) → scale-down fires → replicas=1, not 0.
	if updated.Spec.Replicas != 1 {
		t.Errorf("expected spec.replicas=1 (one scale-down allowed), got %d", updated.Spec.Replicas)
	}

	// Second call: now replicas=1 equals the running floor (1) → must not scale further.
	if _, err := r.reconcileScaleDown(context.Background(), updated, idlePods, 1); err != nil {
		t.Fatalf("second call: %v", err)
	}
	updated2 := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated2); err != nil {
		t.Fatalf("get pool after 2nd call: %v", err)
	}
	if updated2.Spec.Replicas != 1 {
		t.Errorf("spec.replicas must not drop below running count (1), got %d", updated2.Spec.Replicas)
	}
}

// TestSyncAutoscaling_CorrectOvershootReplicas verifies that syncAutoscaling patches
// spec.replicas back up when it has fallen below the running count (overshoot correction).
func TestSyncAutoscaling_CorrectOvershootReplicas(t *testing.T) {
	// Simulate a race: autoscaler previously set spec.replicas=0, but there is
	// still one running sandbox (pod was claimed between the idle check and the patch).
	pool := makePool("pool-a", 0, i32ptr(0), makeScaleDownPolicy(60, 0))
	pool.Spec.Autoscaling = &agentsv1alpha1.PoolAutoscalingSpec{
		Enabled: true,
		ScaleDownPolicy: &agentsv1alpha1.PoolScaleDownPolicy{
			IdleTimeoutSeconds:      60,
			StabilizationSeconds:    0,
			ProtectionWindowSeconds: 10,
		},
	}
	cli := newTestClientBuilder(t).WithObjects(pool).Build()
	r := &SandboxPoolReconciler{Client: cli, Recorder: events.NewFakeRecorder(10)}

	// No idle pods, one running sandbox → correction should fire immediately.
	_, err := r.syncAutoscaling(context.Background(), pool, nil /* idlePods */, 1 /* runningReplicas */)
	if err != nil {
		t.Fatalf("syncAutoscaling: %v", err)
	}

	updated := &agentsv1alpha1.SandboxPool{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}, updated); err != nil {
		t.Fatalf("get pool: %v", err)
	}
	if updated.Spec.Replicas != 1 {
		t.Errorf("expected spec.replicas corrected to 1, got %d", updated.Spec.Replicas)
	}
}
