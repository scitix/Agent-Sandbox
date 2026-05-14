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
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

const (
	// minAutoscalerRequeueAfter is the lower bound for RequeueAfter returned by the
	// autoscaler, preventing overly aggressive requeue storms.
	minAutoscalerRequeueAfter = 5 * time.Second
)

// syncAutoscaling is the entry point for the PoolAutoscaler.
//
// It is called from reconcilePods after pod status has been calculated. It
// receives a read-only slice of the current idle pods and may:
//   - Patch pool.spec.replicas upward (scale-up decision)
//   - Patch pool.spec.replicas downward (scale-down decision)
//   - Patch pool.status fields (IdleZeroSince, LastScaleUpTime, LastScaleDownTime)
//
// Contract:
//   - NEVER creates or deletes Pods
//   - NEVER modifies the pods slice
//   - Only patches spec.replicas and status fields on pool
//
// Returns a suggested ctrl.Result{RequeueAfter: ...} so the reconciler can
// schedule the next autoscaler check at the right time.
func (r *SandboxPoolReconciler) syncAutoscaling(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	idlePods []corev1.Pod,
	runningReplicas int32,
) (ctrl.Result, error) {
	// Always maintain IdleZeroSince regardless of autoscaling.enabled, so the
	// timestamp is ready for scale-up detection.
	if err := r.syncIdleZeroSince(ctx, pool, int32(len(idlePods))); err != nil {
		return ctrl.Result{}, err
	}

	if pool.Spec.Autoscaling == nil || !pool.Spec.Autoscaling.Enabled {
		return ctrl.Result{}, nil
	}

	// Invariant guard: spec.replicas must never drop below the number of running
	// sandboxes. If a previous scale-down decision over-shot (e.g. a sandbox was
	// claimed between the idle-pod check and the patch), correct it immediately
	// before any further autoscaler logic runs.
	if pool.Spec.Replicas < runningReplicas {
		base := pool.DeepCopy()
		pool.Spec.Replicas = runningReplicas
		if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
		klog.InfoS("Autoscaler: corrected spec.replicas to match running sandboxes",
			"pool", pool.Namespace+"/"+pool.Name,
			"correctedReplicas", runningReplicas)
		if r.Recorder != nil {
			r.Recorder.Eventf(pool, nil, corev1.EventTypeWarning, "AutoscalerCorrection", "ReplicasCorrection",
				"Autoscaler corrected spec.replicas from %d to %d to match running sandboxes",
				base.Spec.Replicas, runningReplicas)
		}
	}

	// Scale-up is evaluated first. If we actually scaled up this cycle, skip the
	// scale-down check to avoid contradictory decisions in the same reconcile.
	prevReplicas := pool.Spec.Replicas
	upResult, err := r.reconcileScaleUp(ctx, pool)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pool.Spec.Replicas > prevReplicas {
		// A scale-up happened — return immediately and skip scale-down.
		return upResult, nil
	}

	downResult, err := r.reconcileScaleDown(ctx, pool, idlePods, runningReplicas)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return the smallest non-zero RequeueAfter so neither check is starved.
	return minNonZeroResult(upResult, downResult), nil
}

// syncIdleZeroSince maintains the Status.IdleZeroSince timestamp:
//   - Sets it to now when idleCount drops to 0 (and it was not already set).
//   - Clears it when idleCount returns above 0.
//
// This timestamp is used by future Scale-Up logic to detect how long the pool
// has had no idle pods.
func (r *SandboxPoolReconciler) syncIdleZeroSince(ctx context.Context, pool *agentsv1alpha1.SandboxPool, idleCount int32) error {
	if idleCount == 0 && pool.Status.IdleZeroSince == nil {
		now := metav1.Now()
		base := pool.DeepCopy()
		pool.Status.IdleZeroSince = &now
		if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
			return err
		}
		klog.V(4).InfoS("Autoscaler: set IdleZeroSince", "pool", pool.Namespace+"/"+pool.Name, "time", now)
		return nil
	}

	if idleCount > 0 && pool.Status.IdleZeroSince != nil {
		base := pool.DeepCopy()
		pool.Status.IdleZeroSince = nil
		if err := r.Status().Patch(ctx, pool, client.MergeFrom(base)); err != nil {
			return err
		}
		klog.V(4).InfoS("Autoscaler: cleared IdleZeroSince", "pool", pool.Namespace+"/"+pool.Name)
	}

	return nil
}

// reconcileScaleDown decides whether to decrease spec.replicas by one.
//
// The decision is made purely based on pool-level status and the provided idle
// pod list. Pod deletion itself is handled by the regular reconcilePods
// scale-down path, which also enforces the two-phase protection window.
//
// runningReplicas is used as a hard floor: spec.replicas will never be reduced
// below max(effectiveMinReplicas, runningReplicas), ensuring the autoscaler does
// not under-provision while sandboxes are still active.
func (r *SandboxPoolReconciler) reconcileScaleDown(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
	idlePods []corev1.Pod,
	runningReplicas int32,
) (ctrl.Result, error) {
	if pool.Spec.Autoscaling == nil || !pool.Spec.Autoscaling.Enabled {
		return ctrl.Result{}, nil
	}

	idleTimeoutSec, stabilizationSec, _ := scaleDownPolicyOrDefault(pool)
	idleTimeout := time.Duration(idleTimeoutSec) * time.Second
	stabilization := time.Duration(stabilizationSec) * time.Second

	// Condition 1: there must be idle pods to consider for removal.
	if len(idlePods) == 0 {
		return ctrl.Result{}, nil
	}

	// Condition 2: replicas must be above the effective lower bound, which is
	// the greater of the configured minReplicas and the current running count.
	// This prevents the autoscaler from reducing spec.replicas below the number
	// of active sandboxes.
	lowerBound := max(effectiveMinReplicas(pool), runningReplicas)
	if pool.Spec.Replicas <= lowerBound {
		return ctrl.Result{}, nil
	}

	// Condition 3: stabilization window since last scale-down.
	if pool.Status.LastScaleDownTime != nil {
		elapsed := time.Since(pool.Status.LastScaleDownTime.Time)
		if elapsed < stabilization {
			remaining := maxDuration(stabilization-elapsed, minAutoscalerRequeueAfter)
			klog.V(4).InfoS("Autoscaler: scale-down stabilization not passed",
				"pool", pool.Namespace+"/"+pool.Name,
				"elapsed", elapsed.Round(time.Second),
				"stabilization", stabilization,
				"requeue", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	// Condition 4: the oldest idle pod must have been idle long enough.
	since := oldestIdleSince(idlePods)
	if since == nil {
		return ctrl.Result{}, nil
	}

	idleDuration := time.Since(*since)
	if idleDuration < idleTimeout {
		remaining := maxDuration(idleTimeout-idleDuration, minAutoscalerRequeueAfter)
		klog.V(4).InfoS("Autoscaler: oldest idle pod not yet expired",
			"pool", pool.Namespace+"/"+pool.Name,
			"idleDuration", idleDuration.Round(time.Second),
			"idleTimeout", idleTimeout,
			"requeue", remaining)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// All conditions met: decrease spec.replicas by 1.
	newReplicas := pool.Spec.Replicas - 1
	base := pool.DeepCopy()
	pool.Spec.Replicas = newReplicas
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	if r.Recorder != nil {
		r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, "AutoscalerScaleDown", "ScaleDown",
			"Autoscaler decreased replicas from %d to %d (oldestIdleDuration: %s)",
			newReplicas+1, newReplicas, idleDuration.Round(time.Second))
	}

	// Update LastScaleDownTime in status.
	now := metav1.Now()
	base2 := pool.DeepCopy()
	pool.Status.LastScaleDownTime = &now
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base2)); err != nil {
		// Non-fatal: the scale-down patch already happened. Log and continue.
		klog.ErrorS(err, "Autoscaler: failed to update LastScaleDownTime (non-fatal)",
			"pool", pool.Namespace+"/"+pool.Name)
	}

	klog.InfoS("Autoscaler: decreased spec.replicas (scale-down decision)",
		"pool", pool.Namespace+"/"+pool.Name,
		"oldReplicas", newReplicas+1,
		"newReplicas", newReplicas,
		"oldestIdleDuration", idleDuration.Round(time.Second))

	// After a scale-down, wait for the stabilization window before considering another.
	return ctrl.Result{RequeueAfter: maxDuration(stabilization, minAutoscalerRequeueAfter)}, nil
}

// oldestIdleSince returns the time the longest-idle Pod entered the Idle phase,
// or nil if the slice is empty or no phase-since timestamp can be resolved.
func oldestIdleSince(pods []corev1.Pod) *time.Time {
	var oldest *time.Time
	for i := range pods {
		since, ok, err := inplaceupdate.GetPodPhaseSince(&pods[i], agentsv1alpha1.SandboxPhaseIdle)
		if err != nil || !ok || since.IsZero() {
			continue
		}
		if oldest == nil || since.Before(*oldest) {
			t := since // copy
			oldest = &t
		}
	}
	return oldest
}

// effectiveMinReplicas returns the lower bound from spec.minReplicas, or 0 if unset.
func effectiveMinReplicas(pool *agentsv1alpha1.SandboxPool) int32 {
	if pool.Spec.MinReplicas != nil {
		return *pool.Spec.MinReplicas
	}
	return 0
}

// scaleDownPolicyOrDefault returns the scale-down policy values, falling back to
// Proposal-defined defaults when individual fields are negative or the policy is nil.
// A zero value is valid and means "disabled" for stabilizationSeconds.
func scaleDownPolicyOrDefault(pool *agentsv1alpha1.SandboxPool) (idleTimeoutSeconds, stabilizationSeconds, protectionWindowSeconds int32) {
	const (
		defaultIdleTimeoutSeconds      = 300
		defaultStabilizationSeconds    = 60
		defaultProtectionWindowSeconds = 10
	)
	p := pool.Spec.Autoscaling.ScaleDownPolicy
	if p == nil {
		return defaultIdleTimeoutSeconds, defaultStabilizationSeconds, defaultProtectionWindowSeconds
	}

	idleTimeoutSeconds = p.IdleTimeoutSeconds
	if idleTimeoutSeconds <= 0 {
		idleTimeoutSeconds = defaultIdleTimeoutSeconds
	}

	// StabilizationSeconds=0 is valid and means "no stabilization window".
	stabilizationSeconds = p.StabilizationSeconds
	if stabilizationSeconds < 0 {
		stabilizationSeconds = defaultStabilizationSeconds
	}

	protectionWindowSeconds = p.ProtectionWindowSeconds
	if protectionWindowSeconds <= 0 {
		protectionWindowSeconds = defaultProtectionWindowSeconds
	}

	return idleTimeoutSeconds, stabilizationSeconds, protectionWindowSeconds
}

// maxDuration returns the larger of two durations.
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// minNonZeroResult returns the ctrl.Result with the smallest non-zero RequeueAfter.
// If only one has a non-zero value, that one is returned. If both are zero, zero is returned.
func minNonZeroResult(a, b ctrl.Result) ctrl.Result {
	switch {
	case a.RequeueAfter > 0 && b.RequeueAfter > 0:
		if a.RequeueAfter < b.RequeueAfter {
			return a
		}
		return b
	case a.RequeueAfter > 0:
		return a
	default:
		return b
	}
}

// reconcileScaleUp decides whether to increase spec.replicas based on how long
// the pool has had zero idle pods (IdleZeroSince).
//
// Trigger conditions (all must hold):
//  1. autoscaling.enabled == true
//  2. spec.replicas < maxReplicas (or maxReplicas is unset)
//  3. Neither the scale-up cooldown nor a recent scale-down is blocking
//  4. idleThresholdSeconds > 0 and IdleZeroSince has been set for long enough
func (r *SandboxPoolReconciler) reconcileScaleUp(
	ctx context.Context,
	pool *agentsv1alpha1.SandboxPool,
) (ctrl.Result, error) {
	if pool.Spec.Autoscaling == nil || !pool.Spec.Autoscaling.Enabled {
		return ctrl.Result{}, nil
	}

	maxReplicas := effectiveMaxReplicas(pool)
	if maxReplicas > 0 && pool.Spec.Replicas >= maxReplicas {
		// Already at the ceiling — nothing to do.
		return ctrl.Result{}, nil
	}

	cooldownSec, idleThresholdSec := scaleUpPolicyOrDefault(pool)
	cooldown := time.Duration(cooldownSec) * time.Second

	// Condition: scale-up cooldown window.
	if pool.Status.LastScaleUpTime != nil {
		elapsed := time.Since(pool.Status.LastScaleUpTime.Time)
		if elapsed < cooldown {
			remaining := maxDuration(cooldown-elapsed, minAutoscalerRequeueAfter)
			klog.V(4).InfoS("Autoscaler: scale-up cooldown not elapsed",
				"pool", pool.Namespace+"/"+pool.Name,
				"elapsed", elapsed.Round(time.Second), "cooldown", cooldown,
				"requeue", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	// Soft guard: skip scale-up if we recently scaled down (prevents thrashing).
	if pool.Status.LastScaleDownTime != nil {
		elapsed := time.Since(pool.Status.LastScaleDownTime.Time)
		if elapsed < cooldown {
			remaining := maxDuration(cooldown-elapsed, minAutoscalerRequeueAfter)
			klog.V(4).InfoS("Autoscaler: skip scale-up due to recent scale-down",
				"pool", pool.Namespace+"/"+pool.Name,
				"elapsed", elapsed.Round(time.Second), "cooldown", cooldown,
				"requeue", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	// Condition 1 (reactive): a Create request signalled scale-up by writing the
	// PoolScaleUpPendingAnnotationKey annotation.  This bypass the idleThreshold
	// wait but still respects the cooldown window — we've already returned if the
	// cooldown has not elapsed.
	pendingTrigger := isPendingScaleUpAnnotationFresh(pool, cooldown)

	// Condition 2 (proactive): idle=0 has persisted for idleThresholdSeconds.
	idleThreshold := time.Duration(idleThresholdSec) * time.Second
	idleZeroTrigger := idleThreshold > 0 &&
		pool.Status.IdleZeroSince != nil &&
		time.Since(pool.Status.IdleZeroSince.Time) >= idleThreshold

	if !pendingTrigger && !idleZeroTrigger {
		// Neither condition is satisfied.  If the proactive threshold is configured
		// and IdleZeroSince is set, schedule a requeue when the threshold will fire.
		if idleThreshold > 0 && pool.Status.IdleZeroSince != nil {
			idleZeroDuration := time.Since(pool.Status.IdleZeroSince.Time)
			remaining := maxDuration(idleThreshold-idleZeroDuration, minAutoscalerRequeueAfter)
			klog.V(4).InfoS("Autoscaler: idle=0 threshold not yet reached",
				"pool", pool.Namespace+"/"+pool.Name,
				"idleZeroDuration", idleZeroDuration.Round(time.Second),
				"threshold", idleThreshold,
				"requeue", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		return ctrl.Result{}, nil
	}

	trigger := "idleZero"
	if pendingTrigger {
		trigger = "createPending"
	}

	// Compute target replicas according to the configured mode.
	newReplicas := computeScaleUpTarget(pool, maxReplicas)
	if newReplicas <= pool.Spec.Replicas {
		return ctrl.Result{}, nil
	}

	// Patch spec.replicas upward.
	base := pool.DeepCopy()
	pool.Spec.Replicas = newReplicas
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	if r.Recorder != nil {
		r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, "AutoscalerScaleUp", "ScaleUp",
			"Autoscaler increased replicas from %d to %d (trigger: %s)",
			base.Spec.Replicas, newReplicas, trigger)
	}

	// If the reactive trigger fired, clear the annotation now that we have acted.
	if pendingTrigger {
		base3 := pool.DeepCopy()
		delete(pool.Annotations, agentsv1alpha1.PoolScaleUpPendingAnnotationKey)
		if pErr := r.Patch(ctx, pool, client.MergeFrom(base3)); pErr != nil {
			// Non-fatal: a stale annotation will be ignored by the freshness check.
			klog.V(4).ErrorS(pErr, "Autoscaler: failed to clear scale-up-pending annotation (non-fatal)",
				"pool", pool.Namespace+"/"+pool.Name)
		}
	}

	// Update LastScaleUpTime in status (non-fatal if this fails).
	now := metav1.Now()
	base2 := pool.DeepCopy()
	pool.Status.LastScaleUpTime = &now
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(base2)); err != nil {
		klog.ErrorS(err, "Autoscaler: failed to update LastScaleUpTime (non-fatal)",
			"pool", pool.Namespace+"/"+pool.Name)
	}

	klog.InfoS("Autoscaler: increased spec.replicas (scale-up decision)",
		"pool", pool.Namespace+"/"+pool.Name,
		"oldReplicas", base.Spec.Replicas, "newReplicas", newReplicas,
		"trigger", trigger)

	return ctrl.Result{RequeueAfter: maxDuration(cooldown, minAutoscalerRequeueAfter)}, nil
}

// computeScaleUpTarget returns the new target replica count for a scale-up
// decision, capped at maxReplicas (0 means no cap).
func computeScaleUpTarget(pool *agentsv1alpha1.SandboxPool, maxReplicas int32) int32 {
	current := pool.Spec.Replicas
	mode := agentsv1alpha1.PoolScaleUpModeDefault
	if pool.Spec.Autoscaling.ScaleUpPolicy != nil && pool.Spec.Autoscaling.ScaleUpPolicy.Mode != "" {
		mode = pool.Spec.Autoscaling.ScaleUpPolicy.Mode
	}

	var target int32
	switch mode {
	case agentsv1alpha1.PoolScaleUpModeConservative:
		target = current + 1
	case agentsv1alpha1.PoolScaleUpModeAggressive:
		if current == 0 {
			target = 1
		} else {
			target = current * 2
		}
	default: // Default
		step := max(int32(1), int32(math.Ceil(float64(current)/2.0)))
		target = current + step
	}

	if maxReplicas > 0 && target > maxReplicas {
		target = maxReplicas
	}
	return target
}

// scaleUpPolicyOrDefault returns cooldownSeconds and idleThresholdSeconds
// from the pool's ScaleUpPolicy, applying defaults when fields are unset or negative.
func scaleUpPolicyOrDefault(pool *agentsv1alpha1.SandboxPool) (cooldownSeconds, idleThresholdSeconds int32) {
	const (
		defaultCooldownSeconds      = 30
		defaultIdleThresholdSeconds = 30
	)
	p := pool.Spec.Autoscaling.ScaleUpPolicy
	if p == nil {
		return defaultCooldownSeconds, defaultIdleThresholdSeconds
	}
	cooldownSeconds = p.CooldownSeconds
	if cooldownSeconds < 0 {
		cooldownSeconds = defaultCooldownSeconds
	}
	idleThresholdSeconds = p.IdleThresholdSeconds
	if idleThresholdSeconds < 0 {
		idleThresholdSeconds = defaultIdleThresholdSeconds
	}
	return cooldownSeconds, idleThresholdSeconds
}

// effectiveMaxReplicas returns the MaxReplicas upper bound (0 means unlimited).
func effectiveMaxReplicas(pool *agentsv1alpha1.SandboxPool) int32 {
	if pool.Spec.MaxReplicas != nil {
		return *pool.Spec.MaxReplicas
	}
	return 0
}

// isPendingScaleUpAnnotationFresh returns true when the pool has a
// PoolScaleUpPendingAnnotationKey annotation that is newer than maxAge.
// The maxAge is computed as max(2×cooldown, 2 minutes) to avoid acting on
// stale annotation leftovers from a previous scale-up event.
func isPendingScaleUpAnnotationFresh(pool *agentsv1alpha1.SandboxPool, cooldown time.Duration) bool {
	ts, ok := pool.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey]
	if !ok || ts == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	maxAge := maxDuration(cooldown*2, 2*time.Minute)
	return time.Since(t) <= maxAge
}
