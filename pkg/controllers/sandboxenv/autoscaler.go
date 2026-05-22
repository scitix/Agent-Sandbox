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
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

const (
	// minAutoscalerRequeueAfter is the lower bound on RequeueAfter, preventing
	// overly aggressive re-evaluations.
	minAutoscalerRequeueAfter = 5 * time.Second

	defaultScaleUpCooldownSeconds       = 30
	defaultScaleUpIdleThresholdSeconds  = 30
	defaultScaleDownIdleTimeoutSeconds  = 300
	defaultScaleDownStabilizationSecond = 60
)

// syncAutoscaling runs the Env-level autoscaler against the single-member
// Phase 1 model: it reads the member SandboxPool's status, decides scale-up
// or scale-down, and patches Pool.spec.replicas.
//
// Returns a non-zero RequeueAfter when the next decision depends on a wall
// clock (cooldown, idle threshold, stabilization).
func (r *SandboxEnvReconciler) syncAutoscaling(ctx context.Context, env *agentsv1alpha1.SandboxEnv) (ctrl.Result, error) {
	group := autoscalingGroup(env, defaultScalingGroup)
	if group == nil {
		// No autoscaling group configured — nothing to do.
		return ctrl.Result{}, nil
	}

	localSpec, ok := findLocalClusterSpec(env, r.LocalClusterID)
	if !ok || len(localSpec.Members) == 0 {
		// Adoption hasn't populated the local segment yet.
		return ctrl.Result{}, nil
	}
	// Phase 1 invariant: exactly one member per Env.
	member := localSpec.Members[0]

	pool := &agentsv1alpha1.SandboxPool{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: env.Namespace, Name: member.Name}, pool); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Always maintain IdleZeroSince so a delayed Enabled=true switch can act
	// immediately.
	if err := r.syncIdleZeroSince(ctx, env, pool.Status.IdleReplicas); err != nil {
		return ctrl.Result{}, err
	}

	if env.Spec.Autoscaling == nil || !env.Spec.Autoscaling.Enabled {
		return ctrl.Result{}, nil
	}

	// Scale-up is evaluated first; on hit we skip scale-down this cycle.
	prevReplicas := pool.Spec.Replicas
	upResult, err := r.reconcileScaleUp(ctx, env, pool, group)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pool.Spec.Replicas > prevReplicas {
		return upResult, nil
	}

	downResult, err := r.reconcileScaleDown(ctx, env, pool, group)
	if err != nil {
		return ctrl.Result{}, err
	}
	return minNonZeroResult(upResult, downResult), nil
}

// syncIdleZeroSince mirrors the SandboxPool autoscaler's bookkeeping for the
// Env's local cluster status segment. The timestamp drives the proactive
// scale-up condition (idleThresholdSeconds since idle=0).
func (r *SandboxEnvReconciler) syncIdleZeroSince(ctx context.Context, env *agentsv1alpha1.SandboxEnv, idleCount int32) error {
	dirty := false
	if idleCount == 0 {
		var existing *metav1.Time
		mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
			existing = local.IdleZeroSince
		})
		if existing == nil {
			now := metav1.Now()
			mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
				local.IdleZeroSince = &now
			})
			dirty = true
		}
	} else {
		var existing *metav1.Time
		mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
			existing = local.IdleZeroSince
		})
		if existing != nil {
			mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
				local.IdleZeroSince = nil
			})
			dirty = true
		}
	}
	if !dirty {
		return nil
	}
	base := env.DeepCopy()
	// Reload the latest status before patching so we don't stomp concurrent
	// updates from syncStatus.
	if err := r.Status().Patch(ctx, env, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return nil // next reconcile picks it up
		}
		return err
	}
	return nil
}

// reconcileScaleUp mirrors the SandboxPool autoscaler's scale-up path,
// adapted to read configuration from Env.Spec.Autoscaling.Groups[0] and to
// patch the member Pool's spec.replicas.
func (r *SandboxEnvReconciler) reconcileScaleUp(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	pool *agentsv1alpha1.SandboxPool,
	group *agentsv1alpha1.EnvAutoscalingGroup,
) (ctrl.Result, error) {
	maxR := effectiveMaxReplicas(group)
	if maxR > 0 && pool.Spec.Replicas >= maxR {
		return ctrl.Result{}, nil
	}

	cooldown, idleThresholdSec := scaleUpPolicyOrDefault(group)

	localStatus := findLocalClusterStatus(env, r.LocalClusterID)

	if localStatus.LastScaleUpTime != nil {
		elapsed := time.Since(localStatus.LastScaleUpTime.Time)
		if elapsed < cooldown {
			return ctrl.Result{RequeueAfter: clampRequeue(cooldown - elapsed)}, nil
		}
	}
	if localStatus.LastScaleDownTime != nil {
		elapsed := time.Since(localStatus.LastScaleDownTime.Time)
		if elapsed < cooldown {
			return ctrl.Result{RequeueAfter: clampRequeue(cooldown - elapsed)}, nil
		}
	}

	// Reactive trigger: Pool-level PoolScaleUpPendingAnnotationKey written by
	// PoolScheduler when a request couldn't dispatch. Env-level pending is
	// reserved for Phase 2 (when EnvScheduler may itself hold the queue).
	pendingTrigger := isPendingScaleUpAnnotationFresh(pool, cooldown)

	idleThreshold := time.Duration(idleThresholdSec) * time.Second
	idleZeroTrigger := idleThreshold > 0 &&
		localStatus.IdleZeroSince != nil &&
		time.Since(localStatus.IdleZeroSince.Time) >= idleThreshold

	if !pendingTrigger && !idleZeroTrigger {
		if idleThreshold > 0 && localStatus.IdleZeroSince != nil {
			d := time.Since(localStatus.IdleZeroSince.Time)
			return ctrl.Result{RequeueAfter: clampRequeue(idleThreshold - d)}, nil
		}
		return ctrl.Result{}, nil
	}

	trigger := "idleZero"
	if pendingTrigger {
		trigger = "createPending"
	}

	newReplicas := computeScaleUpTarget(pool, group, maxR)
	if newReplicas <= pool.Spec.Replicas {
		return ctrl.Result{}, nil
	}

	base := pool.DeepCopy()
	pool.Spec.Replicas = newReplicas
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	r.emitEvent(env, "ScaleUp", "AutoscalerScaleUp",
		"increased %s/%s replicas from %d to %d (trigger: %s)",
		pool.Namespace, pool.Name, base.Spec.Replicas, newReplicas, trigger)

	// Clear the Pool-side pending annotation when we acted on it.
	if pendingTrigger {
		base3 := pool.DeepCopy()
		delete(pool.Annotations, agentsv1alpha1.PoolScaleUpPendingAnnotationKey)
		if pErr := r.Patch(ctx, pool, client.MergeFrom(base3)); pErr != nil {
			klog.V(4).ErrorS(pErr, "clear scale-up-pending annotation failed (non-fatal)",
				"pool", pool.Namespace+"/"+pool.Name)
		}
	}

	// Update LastScaleUpTime on the Env's local segment.
	now := metav1.Now()
	baseEnv := env.DeepCopy()
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
		local.LastScaleUpTime = &now
	})
	if err := r.Status().Patch(ctx, env, client.MergeFrom(baseEnv)); err != nil && !apierrors.IsConflict(err) {
		klog.ErrorS(err, "failed to update Env.status.lastScaleUpTime (non-fatal)",
			"env", env.Namespace+"/"+env.Name)
	}

	klog.InfoS("Env autoscaler: increased Pool spec.replicas",
		"env", env.Namespace+"/"+env.Name,
		"pool", pool.Namespace+"/"+pool.Name,
		"oldReplicas", base.Spec.Replicas, "newReplicas", newReplicas, "trigger", trigger)

	return ctrl.Result{RequeueAfter: clampRequeue(cooldown)}, nil
}

// reconcileScaleDown mirrors the SandboxPool autoscaler's scale-down path.
// Phase 1 single-member; runningReplicas comes from Pool.status, idle pod
// ages are read via a List of idle Pods (same approach the legacy autoscaler
// uses internally).
func (r *SandboxEnvReconciler) reconcileScaleDown(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	pool *agentsv1alpha1.SandboxPool,
	group *agentsv1alpha1.EnvAutoscalingGroup,
) (ctrl.Result, error) {
	idleTimeoutSec, stabilizationSec := scaleDownPolicyOrDefault(group)
	idleTimeout := time.Duration(idleTimeoutSec) * time.Second
	stabilization := time.Duration(stabilizationSec) * time.Second

	if pool.Status.IdleReplicas == 0 {
		return ctrl.Result{}, nil
	}

	lowerBound := max(pool.Status.RunningReplicas, effectiveMinReplicas(group))
	if pool.Spec.Replicas <= lowerBound {
		return ctrl.Result{}, nil
	}

	localStatus := findLocalClusterStatus(env, r.LocalClusterID)
	if localStatus.LastScaleDownTime != nil {
		elapsed := time.Since(localStatus.LastScaleDownTime.Time)
		if elapsed < stabilization {
			return ctrl.Result{RequeueAfter: clampRequeue(stabilization - elapsed)}, nil
		}
	}

	idleSince, err := r.oldestIdlePodSince(ctx, pool)
	if err != nil {
		return ctrl.Result{}, err
	}
	if idleSince == nil {
		return ctrl.Result{}, nil
	}

	idleDuration := time.Since(*idleSince)
	if idleDuration < idleTimeout {
		return ctrl.Result{RequeueAfter: clampRequeue(idleTimeout - idleDuration)}, nil
	}

	newReplicas := pool.Spec.Replicas - 1
	base := pool.DeepCopy()
	pool.Spec.Replicas = newReplicas
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	r.emitEvent(env, "ScaleDown", "AutoscalerScaleDown",
		"decreased %s/%s replicas from %d to %d (oldestIdleDuration: %s)",
		pool.Namespace, pool.Name, newReplicas+1, newReplicas, idleDuration.Round(time.Second))

	now := metav1.Now()
	baseEnv := env.DeepCopy()
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
		local.LastScaleDownTime = &now
	})
	if err := r.Status().Patch(ctx, env, client.MergeFrom(baseEnv)); err != nil && !apierrors.IsConflict(err) {
		klog.ErrorS(err, "failed to update Env.status.lastScaleDownTime (non-fatal)",
			"env", env.Namespace+"/"+env.Name)
	}

	klog.InfoS("Env autoscaler: decreased Pool spec.replicas",
		"env", env.Namespace+"/"+env.Name,
		"pool", pool.Namespace+"/"+pool.Name,
		"oldReplicas", newReplicas+1, "newReplicas", newReplicas,
		"oldestIdleDuration", idleDuration.Round(time.Second))

	return ctrl.Result{RequeueAfter: clampRequeue(stabilization)}, nil
}

// oldestIdlePodSince returns the earliest "idle phase since" timestamp across
// the member Pool's idle pods, or nil when none can be resolved.
func (r *SandboxEnvReconciler) oldestIdlePodSince(ctx context.Context, pool *agentsv1alpha1.SandboxPool) (*time.Time, error) {
	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(pool.Namespace),
		client.MatchingLabels{
			agentsv1alpha1.SandboxPoolLabelKey:  pool.Name,
			agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
		},
	); err != nil {
		return nil, err
	}
	var oldest *time.Time
	for i := range podList.Items {
		since, ok, err := inplaceupdate.GetPodPhaseSince(&podList.Items[i], agentsv1alpha1.SandboxPhaseIdle)
		if err != nil || !ok || since.IsZero() {
			continue
		}
		if oldest == nil || since.Before(*oldest) {
			t := since
			oldest = &t
		}
	}
	return oldest, nil
}

// autoscalingGroup looks up a group by name. Phase 1 always uses
// defaultScalingGroup; the function is general for forward compatibility.
func autoscalingGroup(env *agentsv1alpha1.SandboxEnv, name string) *agentsv1alpha1.EnvAutoscalingGroup {
	if env == nil || env.Spec.Autoscaling == nil {
		return nil
	}
	for i := range env.Spec.Autoscaling.Groups {
		if env.Spec.Autoscaling.Groups[i].Name == name {
			return &env.Spec.Autoscaling.Groups[i]
		}
	}
	return nil
}

func effectiveMaxReplicas(group *agentsv1alpha1.EnvAutoscalingGroup) int32 {
	if group == nil || group.MaxReplicas == nil {
		return 0
	}
	return *group.MaxReplicas
}

func effectiveMinReplicas(group *agentsv1alpha1.EnvAutoscalingGroup) int32 {
	if group == nil || group.MinReplicas == nil {
		return 0
	}
	return *group.MinReplicas
}

func scaleUpPolicyOrDefault(group *agentsv1alpha1.EnvAutoscalingGroup) (cooldown time.Duration, idleThresholdSeconds int32) {
	cooldown = time.Duration(defaultScaleUpCooldownSeconds) * time.Second
	idleThresholdSeconds = defaultScaleUpIdleThresholdSeconds
	if group == nil || group.ScaleUpPolicy == nil {
		return
	}
	if group.ScaleUpPolicy.CooldownSeconds > 0 {
		cooldown = time.Duration(group.ScaleUpPolicy.CooldownSeconds) * time.Second
	}
	if group.ScaleUpPolicy.IdleThresholdSeconds >= 0 {
		idleThresholdSeconds = group.ScaleUpPolicy.IdleThresholdSeconds
	}
	return
}

func scaleDownPolicyOrDefault(group *agentsv1alpha1.EnvAutoscalingGroup) (idleTimeoutSeconds, stabilizationSeconds int32) {
	idleTimeoutSeconds = defaultScaleDownIdleTimeoutSeconds
	stabilizationSeconds = defaultScaleDownStabilizationSecond
	if group == nil || group.ScaleDownPolicy == nil {
		return
	}
	if group.ScaleDownPolicy.IdleTimeoutSeconds > 0 {
		idleTimeoutSeconds = group.ScaleDownPolicy.IdleTimeoutSeconds
	}
	if group.ScaleDownPolicy.StabilizationSeconds >= 0 {
		stabilizationSeconds = group.ScaleDownPolicy.StabilizationSeconds
	}
	return
}

func computeScaleUpTarget(pool *agentsv1alpha1.SandboxPool, group *agentsv1alpha1.EnvAutoscalingGroup, maxR int32) int32 {
	mode := agentsv1alpha1.PoolScaleUpModeDefault
	if group != nil && group.ScaleUpPolicy != nil && group.ScaleUpPolicy.Mode != "" {
		mode = group.ScaleUpPolicy.Mode
	}
	cur := pool.Spec.Replicas
	switch mode {
	case agentsv1alpha1.PoolScaleUpModeConservative:
		target := cur + 1
		if maxR > 0 && target > maxR {
			target = maxR
		}
		return target
	case agentsv1alpha1.PoolScaleUpModeAggressive:
		target := cur * 2
		if cur == 0 {
			target = 1
		}
		if maxR > 0 && target > maxR {
			target = maxR
		}
		return target
	default: // Default mode: +max(1, ceil(cur/2))
		add := max(int32(math.Ceil(float64(cur)/2.0)), 1)
		target := cur + add
		if maxR > 0 && target > maxR {
			target = maxR
		}
		return target
	}
}

// isPendingScaleUpAnnotationFresh returns true when the Pool carries a
// PoolScaleUpPendingAnnotationKey whose timestamp is younger than the
// cooldown window. Stale annotations are ignored.
func isPendingScaleUpAnnotationFresh(pool *agentsv1alpha1.SandboxPool, cooldown time.Duration) bool {
	if pool == nil || pool.Annotations == nil {
		return false
	}
	raw, ok := pool.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey]
	if !ok || raw == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false
	}
	return time.Since(t) <= cooldown*2
}

// clampRequeue floors a requeue duration at minAutoscalerRequeueAfter so the
// controller doesn't busy-loop when a cooldown / stabilization window is
// nearly elapsed.
func clampRequeue(d time.Duration) time.Duration {
	if d > minAutoscalerRequeueAfter {
		return d
	}
	return minAutoscalerRequeueAfter
}

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
