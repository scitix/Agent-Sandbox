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
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
)

// runningInfoKey is a comparable struct used as a map key to track which
// sandbox-running-info metric label-sets are currently active.
type runningInfoKey struct {
	namespace string
	pool      string
	pod       string
	sandboxID string
	team      string
	user      string
}

// calculatePodStatus computes the current SandboxPoolStatus from a pod slice.
// It also maintains the SandboxRunningInfo gauge: entries that were Running in
// the previous reconciliation but are no longer Running are explicitly deleted
// to prevent metric leaks when pods vanish between cycles.
func (r *SandboxPoolReconciler) calculatePodStatus(poolKey string, pods []corev1.Pod, sandboxPool *agentsv1alpha1.SandboxPool) agentsv1alpha1.SandboxPoolStatus {
	var idle, running, starting, stopping, failed int32
	var unavailableIdle int32

	// Collect the set of Running info-gauge label-sets for this reconciliation.
	currentRunning := make(map[runningInfoKey]prometheus.Labels)

	for i := range pods {
		pod := &pods[i]
		phase := inplaceupdate.GetSandboxPhase(pod)

		// Build the info-gauge entry for any pod that carries a sandbox-id,
		// regardless of phase — we need the label set to clean up stale entries.
		if sandboxID := pod.Labels[agentsv1alpha1.SandboxIDLabelKey]; sandboxID != "" {
			infoLabels := prometheus.Labels{
				"namespace":  pod.Namespace,
				"pool":       pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
				"pod":        pod.Name,
				"sandbox_id": sandboxID,
				"team":       pod.Labels[agentsv1alpha1.LabelTeam],
				"user":       pod.Labels[agentsv1alpha1.LabelUser],
			}
			if phase == agentsv1alpha1.SandboxPhaseRunning {
				key := runningInfoKey{
					namespace: pod.Namespace,
					pool:      pod.Labels[agentsv1alpha1.SandboxPoolLabelKey],
					pod:       pod.Name,
					sandboxID: sandboxID,
					team:      pod.Labels[agentsv1alpha1.LabelTeam],
					user:      pod.Labels[agentsv1alpha1.LabelUser],
				}
				currentRunning[key] = infoLabels
			}
		}

		switch phase {
		case agentsv1alpha1.SandboxPhaseIdle:
			idle++
			if isIdlePodUnavailable(pod) {
				unavailableIdle++
			}
		case agentsv1alpha1.SandboxPhaseRunning:
			running++
		case agentsv1alpha1.SandboxPhaseStarting:
			starting++
		case agentsv1alpha1.SandboxPhaseStopping:
			stopping++
		case agentsv1alpha1.SandboxPhaseFailed:
			failed++
		default:
			// Treat unknown phases as idle for safety
			idle++
			if isIdlePodUnavailable(pod) {
				unavailableIdle++
			}
		}
	}

	// Diff against the previous snapshot: delete entries that are no longer Running.
	r.runningInfoMu.Lock()
	if prev := r.runningInfoPrev[poolKey]; prev != nil {
		for key, labels := range prev {
			if _, stillRunning := currentRunning[key]; !stillRunning {
				pkgmetrics.SandboxRunningInfo.Delete(labels)
			}
		}
	}
	// Set all current Running entries.
	for _, labels := range currentRunning {
		pkgmetrics.SandboxRunningInfo.With(labels).Set(1)
	}
	if r.runningInfoPrev == nil {
		r.runningInfoPrev = make(map[string]map[runningInfoKey]prometheus.Labels)
	}
	r.runningInfoPrev[poolKey] = currentRunning
	r.runningInfoMu.Unlock()

	desiredReplicas := sandboxPool.Spec.Replicas
	currentReplicas := idle + running + starting + stopping + failed

	poolPhase := calculatePoolPhase(desiredReplicas, currentReplicas, unavailableIdle, failed)
	conditions := buildPoolConditions(
		sandboxPool.Status.Conditions,
		desiredReplicas, currentReplicas,
		idle, running, starting, stopping, failed,
		unavailableIdle,
	)

	return agentsv1alpha1.SandboxPoolStatus{
		Phase:                   poolPhase,
		IdleReplicas:            idle,
		UnavailableIdleReplicas: unavailableIdle,
		RunningReplicas:         running,
		StartingReplicas:        starting,
		StoppingReplicas:        stopping,
		FailedReplicas:          failed,
		Conditions:              conditions,
	}
}

// statusEquals checks if the current status equals the new status.
// Selector, LabelSelector and PhaseSelectors are excluded from comparison since
// they are stable (derived from the pool name) and do not need to trigger updates.
func (r *SandboxPoolReconciler) statusEquals(sandboxPool *agentsv1alpha1.SandboxPool, newStatus agentsv1alpha1.SandboxPoolStatus) bool {
	old := sandboxPool.Status
	if old.Phase != newStatus.Phase {
		return false
	}
	if old.IdleReplicas != newStatus.IdleReplicas {
		return false
	}
	if old.UnavailableIdleReplicas != newStatus.UnavailableIdleReplicas {
		return false
	}
	if old.RunningReplicas != newStatus.RunningReplicas {
		return false
	}
	if old.StartingReplicas != newStatus.StartingReplicas {
		return false
	}
	if old.StoppingReplicas != newStatus.StoppingReplicas {
		return false
	}
	if old.FailedReplicas != newStatus.FailedReplicas {
		return false
	}
	// Compare autoscaler status timestamps.
	if !timePointerEqual(old.LastScaleUpTime, newStatus.LastScaleUpTime) {
		return false
	}
	if !timePointerEqual(old.LastScaleDownTime, newStatus.LastScaleDownTime) {
		return false
	}
	if !timePointerEqual(old.IdleZeroSince, newStatus.IdleZeroSince) {
		return false
	}
	return conditionsEqual(old.Conditions, newStatus.Conditions)
}

func (r *SandboxPoolReconciler) markPoolTerminating(ctx context.Context, sandboxPool *agentsv1alpha1.SandboxPool) error {
	if sandboxPool.Status.Phase == agentsv1alpha1.SandboxPoolPhaseTerminating {
		return nil
	}
	sandboxPool.Status.Phase = agentsv1alpha1.SandboxPoolPhaseTerminating
	return r.Status().Update(ctx, sandboxPool)
}

// timePointerEqual returns true when both pointers are nil, or both are non-nil
// and represent the same instant (compared at second granularity to avoid
// spurious updates from sub-second rounding differences).
func timePointerEqual(a, b *metav1.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.UTC().Equal(b.UTC())
}

// conditionsEqual compares two Condition slices for semantic equality (order-insensitive).
// LastTransitionTime is intentionally excluded: it is managed by apimeta.SetStatusCondition
// and only changes when Status changes — which is already captured by comparing Status/Reason/Message.
func conditionsEqual(a, b []metav1.Condition) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]metav1.Condition, len(a))
	for _, c := range a {
		am[c.Type] = c
	}
	for _, cb := range b {
		ca, ok := am[cb.Type]
		if !ok {
			return false
		}
		if ca.Status != cb.Status {
			return false
		}
		if ca.Reason != cb.Reason {
			return false
		}
		if ca.Message != cb.Message {
			return false
		}
		if ca.ObservedGeneration != cb.ObservedGeneration {
			return false
		}
	}
	return true
}

// isIdlePodUnavailable reports whether an idle-phase Pod is unavailable (PodReady != True).
// This covers: Pending (scheduling, image pull), CrashLoopBackOff, ErrImagePull, etc.
// Pods in non-idle phases are never considered unavailable by this function.
func isIdlePodUnavailable(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status != corev1.ConditionTrue
		}
	}
	// PodReady condition absent → pod is not yet ready
	return true
}

// calculatePoolPhase derives the overall pool phase from replica counts (pure function).
// Priority (high → low):
//  1. desired==0 && current==0 → Pending
//  2. current < desired         → ScalingUp
//  3. current > desired         → ScalingDown
//  4. unavailableIdle>0 || failed>0 → Degraded
//  5. otherwise                 → Ready
func calculatePoolPhase(desired, current, unavailableIdle, failed int32) agentsv1alpha1.SandboxPoolPhase {
	switch {
	case desired == 0 && current == 0:
		return agentsv1alpha1.SandboxPoolPhasePending
	case current < desired:
		return agentsv1alpha1.SandboxPoolPhaseScalingUp
	case current > desired:
		return agentsv1alpha1.SandboxPoolPhaseScalingDown
	case unavailableIdle > 0 || failed > 0:
		return agentsv1alpha1.SandboxPoolPhaseDegraded
	default:
		return agentsv1alpha1.SandboxPoolPhaseReady
	}
}

// buildPoolConditions constructs the three standard SandboxPool Conditions:
// Available, Scaling, and Degraded. It takes the existing conditions so that
// apimeta.SetStatusCondition can preserve LastTransitionTime when Status is unchanged.
func buildPoolConditions(
	existing []metav1.Condition,
	desired, current int32,
	idle, running, starting, stopping, failed int32,
	unavailableIdle int32,
) []metav1.Condition {
	conditions := make([]metav1.Condition, len(existing))
	copy(conditions, existing)

	availableIdle := max(idle-unavailableIdle, 0)

	// ── Available ────────────────────────────────────────────────────────────
	var availCond metav1.Condition
	availCond.Type = agentsv1alpha1.SandboxPoolConditionAvailable
	switch {
	case current < desired:
		availCond.Status = metav1.ConditionFalse
		availCond.Reason = agentsv1alpha1.SandboxPoolReasonScalingUp
		availCond.Message = fmt.Sprintf("scaling up: %d/%d replicas ready", current, desired)
	case availableIdle > 0:
		availCond.Status = metav1.ConditionTrue
		availCond.Reason = agentsv1alpha1.SandboxPoolReasonIdlePodsAvailable
		availCond.Message = fmt.Sprintf("%d idle pod(s) available", availableIdle)
	default:
		availCond.Status = metav1.ConditionFalse
		availCond.Reason = agentsv1alpha1.SandboxPoolReasonNoIdlePodsAvailable
		if idle > 0 {
			availCond.Message = fmt.Sprintf("all %d idle pod(s) are unavailable (NotReady)", idle)
		} else {
			availCond.Message = fmt.Sprintf("no idle pods: %d running, %d starting, %d stopping", running, starting, stopping)
		}
	}
	apimeta.SetStatusCondition(&conditions, availCond)

	// ── Scaling ──────────────────────────────────────────────────────────────
	var scalingCond metav1.Condition
	scalingCond.Type = agentsv1alpha1.SandboxPoolConditionScaling
	switch {
	case current < desired:
		scalingCond.Status = metav1.ConditionTrue
		scalingCond.Reason = agentsv1alpha1.SandboxPoolReasonScalingUp
		scalingCond.Message = fmt.Sprintf("scaling up from %d to %d replicas", current, desired)
	case current > desired:
		scalingCond.Status = metav1.ConditionTrue
		scalingCond.Reason = agentsv1alpha1.SandboxPoolReasonScalingDown
		scalingCond.Message = fmt.Sprintf("scaling down from %d to %d: waiting for %d running pod(s) to release", current, desired, running)
	default:
		scalingCond.Status = metav1.ConditionFalse
		scalingCond.Reason = agentsv1alpha1.SandboxPoolReasonReplicasReady
		scalingCond.Message = fmt.Sprintf("all %d replicas are up-to-date", desired)
	}
	apimeta.SetStatusCondition(&conditions, scalingCond)

	// ── Degraded ─────────────────────────────────────────────────────────────
	var degradedCond metav1.Condition
	degradedCond.Type = agentsv1alpha1.SandboxPoolConditionDegraded
	switch {
	case unavailableIdle > 0 && failed > 0:
		degradedCond.Status = metav1.ConditionTrue
		degradedCond.Reason = agentsv1alpha1.SandboxPoolReasonUnhealthyAndFailed
		degradedCond.Message = fmt.Sprintf("%d unavailable idle pod(s) (NotReady), %d failed pod(s)", unavailableIdle, failed)
	case unavailableIdle > 0:
		degradedCond.Status = metav1.ConditionTrue
		degradedCond.Reason = agentsv1alpha1.SandboxPoolReasonUnhealthyIdlePods
		degradedCond.Message = fmt.Sprintf("%d/%d idle pod(s) are unavailable (NotReady)", unavailableIdle, idle)
	case failed > 0:
		degradedCond.Status = metav1.ConditionTrue
		degradedCond.Reason = agentsv1alpha1.SandboxPoolReasonFailedPodsPresent
		degradedCond.Message = fmt.Sprintf("%d pod(s) in failed state", failed)
	default:
		degradedCond.Status = metav1.ConditionFalse
		degradedCond.Reason = agentsv1alpha1.SandboxPoolReasonAllPodsHealthy
		degradedCond.Message = "all pods are healthy"
	}
	apimeta.SetStatusCondition(&conditions, degradedCond)

	return conditions
}

// buildPhaseSelectors returns pre-computed label selector strings for each pod phase.
func buildPhaseSelectors(poolName string) map[string]string {
	base := agentsv1alpha1.SandboxPoolLabelKey + "=" + poolName
	phases := []string{
		agentsv1alpha1.SandboxPhaseIdle,
		agentsv1alpha1.SandboxPhaseRunning,
		agentsv1alpha1.SandboxPhaseStarting,
		agentsv1alpha1.SandboxPhaseStopping,
		agentsv1alpha1.SandboxPhaseFailed,
	}
	result := map[string]string{"all": base}
	for _, phase := range phases {
		result[phase] = base + "," + agentsv1alpha1.SandboxPhaseLabelKey + "=" + phase
	}
	return result
}

// emitPhaseTransitionEvent sends a Kubernetes Event when the pool phase changes.
// Only fires on transitions (old != new) to avoid flooding the event stream.
func (r *SandboxPoolReconciler) emitPhaseTransitionEvent(
	pool *agentsv1alpha1.SandboxPool,
	oldPhase, newPhase agentsv1alpha1.SandboxPoolPhase,
	status agentsv1alpha1.SandboxPoolStatus,
) {
	if r.Recorder == nil || oldPhase == newPhase {
		return
	}
	switch newPhase {
	case agentsv1alpha1.SandboxPoolPhaseScalingUp:
		r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, string(agentsv1alpha1.SandboxPoolPhaseScalingUp), "ScaleUp",
			"Scaling up pool from %d to %d replicas",
			status.IdleReplicas+status.RunningReplicas+status.StartingReplicas+status.StoppingReplicas+status.FailedReplicas,
			pool.Spec.Replicas)
	case agentsv1alpha1.SandboxPoolPhaseScalingDown:
		r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, string(agentsv1alpha1.SandboxPoolPhaseScalingDown), "ScaleDown",
			"Scaling down pool from %d to %d: waiting for %d running pod(s) to release",
			status.IdleReplicas+status.RunningReplicas+status.StartingReplicas+status.StoppingReplicas+status.FailedReplicas,
			pool.Spec.Replicas, status.RunningReplicas)
	case agentsv1alpha1.SandboxPoolPhaseReady:
		if oldPhase == agentsv1alpha1.SandboxPoolPhaseDegraded {
			r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, "PoolRecovered", "Recover",
				"Pool reached desired state: %d replicas, %d idle", pool.Spec.Replicas, status.IdleReplicas)
		} else {
			r.Recorder.Eventf(pool, nil, corev1.EventTypeNormal, "PoolReady", "Sync",
				"Pool reached desired state: %d replicas, %d idle", pool.Spec.Replicas, status.IdleReplicas)
		}
	case agentsv1alpha1.SandboxPoolPhaseDegraded:
		if status.FailedReplicas > 0 {
			r.Recorder.Eventf(pool, nil, corev1.EventTypeWarning, string(agentsv1alpha1.SandboxPoolPhaseDegraded), "Degraded",
				"%d pod(s) failed, creating replacements", status.FailedReplicas)
		}
		// UnavailableIdleReplicas (NotReady) transitions are intentionally not
		// emitted as events — NotReady is transient during image pulls and pod
		// restarts, and emitting an event on every flip would flood the event stream.
	}
}
