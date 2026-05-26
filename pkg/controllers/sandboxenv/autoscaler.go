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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/framework/plugins"
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
	defaultSaturationCooldownSeconds    = 60
)

// syncAutoscaling runs the Env-level autoscaler.
//
// MVP scope (single ScalingGroup): the autoscaler operates on the local
// cluster segment's members as one cohort. The chosen group config is
// `env.Spec.Autoscaling.Groups[0]` (or empty defaults) — multi-group
// schema is preserved for future per-resource-shape scaling but only one
// active group is consulted today.
//
// Scale-up may PATCH multiple member Pools in a single reconcile cycle:
// the autoscaler iterates members by scaleUpPriority and consumes the
// computed delta across them, using the plugin admission probe to converge
// on the largest scheduler-acceptable target for each.
//
// Returns a non-zero RequeueAfter when the next decision depends on a wall
// clock (cooldown, idle threshold, stabilization).
func (r *SandboxEnvReconciler) syncAutoscaling(ctx context.Context, env *agentsv1alpha1.SandboxEnv) (ctrl.Result, error) {
	localSpec, ok := findLocalClusterSpec(env, r.LocalClusterID)
	if !ok || len(localSpec.Members) == 0 {
		return ctrl.Result{}, nil
	}

	// Load every member Pool once up front — IdleZeroSince needs the
	// aggregate idle count across all members, and reconcileScaleUp iterates
	// over them.
	members, err := r.loadMemberPools(ctx, env.Namespace, localSpec.Members)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(members) == 0 {
		return ctrl.Result{}, nil
	}

	aggIdle := int32(0)
	for _, m := range members {
		aggIdle += m.pool.Status.IdleReplicas
	}

	// Always maintain IdleZeroSince so a delayed Enabled=true switch can act
	// immediately.
	if err := r.syncIdleZeroSince(ctx, env, aggIdle); err != nil {
		return ctrl.Result{}, err
	}

	group := pickAutoscalingGroup(env)
	if group == nil {
		// Autoscaling is per-group: when no group declares Enabled=true
		// the autoscaler stays dormant on this cycle. IdleZeroSince
		// bookkeeping above still runs so a delayed Enabled toggle can
		// act immediately.
		return ctrl.Result{}, nil
	}

	// Scale-up is evaluated first; if any Pool was actually grown, skip
	// scale-down this cycle.
	scaledUp, upResult, err := r.reconcileScaleUp(ctx, env, members, group, aggIdle)
	if err != nil {
		return ctrl.Result{}, err
	}
	if scaledUp {
		return upResult, nil
	}

	// Scale-down still single-member-driven for the MVP. We pick the first
	// member as the candidate (Phase 1 adoption is 1:1). Multi-member
	// scale-down will arrive when multi-member scale-up has bedded in.
	downResult, err := r.reconcileScaleDown(ctx, env, members[0].pool, group)
	if err != nil {
		return ctrl.Result{}, err
	}
	return minNonZeroResult(upResult, downResult), nil
}

// memberWithPool pairs a member spec entry with its live Pool object.
type memberWithPool struct {
	spec agentsv1alpha1.EnvClusterMember
	pool *agentsv1alpha1.SandboxPool
}

// loadMemberPools fetches the SandboxPool for each member name. Members
// whose Pool is missing are skipped (typical during adopter churn).
func (r *SandboxEnvReconciler) loadMemberPools(ctx context.Context, ns string, members []agentsv1alpha1.EnvClusterMember) ([]memberWithPool, error) {
	out := make([]memberWithPool, 0, len(members))
	for _, m := range members {
		pool := &agentsv1alpha1.SandboxPool{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: m.Name}, pool); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, memberWithPool{spec: m, pool: pool})
	}
	return out, nil
}

// pickAutoscalingGroup returns the autoscaling group the Reconciler operates
// on this cycle. MVP semantics: pick the first Enabled=true group in
// env.Spec.Autoscaling.Groups; falls back to nil when none is enabled (which
// short-circuits the caller). Multi-group will route per-member based on
// member.ScalingGroup once multi-resource Envs land.
func pickAutoscalingGroup(env *agentsv1alpha1.SandboxEnv) *agentsv1alpha1.EnvAutoscalingGroup {
	if env == nil || env.Spec.Autoscaling == nil {
		return nil
	}
	for i := range env.Spec.Autoscaling.Groups {
		if env.Spec.Autoscaling.Groups[i].Enabled {
			return &env.Spec.Autoscaling.Groups[i]
		}
	}
	return nil
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

// reconcileScaleUp evaluates and acts on the Env-level scale-up decision.
//
// Decision flow (per cycle):
//
//  1. Honour the group's cooldown and recent scale-down stabilisation.
//
//  2. Determine whether at least one trigger fires:
//     - Reactive: any member Pool carries a fresh PoolScaleUpPendingAnnotation
//     (PoolScheduler's "queue empty + pending requests" doorbell).
//     - Proactive: aggregate idle has been zero for IdleThresholdSeconds.
//
//  3. Compute a group-wide delta = computeScaleUpDelta(aggregateDesired,
//     groupMaxReplicas, mode).
//
//  4. Sort members by (scaleUpPriority asc, name asc) and iterate. For each
//     member, skip if SaturatedUntil is in the future. Otherwise run
//     ProbeAcceptedReplicas to find the largest count the plugin admits in
//     [current+1, candidate]. PATCH the result, decrement delta, move on.
//
//  5. Record per-member attempt results into Env.Status.ObservedMembers so
//     the router can deprioritise saturated members and a kubectl-describe
//     surfaces InsufficientResources / InternalError messages directly.
//
// Returns (scaledUp, requeueResult, err). scaledUp is true when ANY member
// actually had its replicas patched up (full or partial); the caller skips
// scale-down for the cycle. requeueResult.RequeueAfter is the suggested
// next-tick.
func (r *SandboxEnvReconciler) reconcileScaleUp(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	members []memberWithPool,
	group *agentsv1alpha1.EnvAutoscalingGroup,
	aggIdle int32,
) (bool, ctrl.Result, error) {
	cooldown, idleThresholdSec, saturationCooldown := scaleUpPolicyOrDefault(group)

	if requeue, ok := scaleUpCooldownActive(env, r.LocalClusterID, cooldown); ok {
		return false, requeue, nil
	}

	trigger, requeue, fired := evaluateScaleUpTriggers(env, members, r.LocalClusterID, cooldown, idleThresholdSec)
	if !fired {
		return false, requeue, nil
	}

	maxR := effectiveMaxReplicas(group)
	aggDesired := int32(0)
	for _, m := range members {
		aggDesired += m.pool.Spec.Replicas
	}
	if maxR > 0 && aggDesired >= maxR {
		return false, ctrl.Result{}, nil
	}
	deltaTotal := computeScaleUpDelta(aggDesired, group, maxR)
	if deltaTotal <= 0 {
		return false, ctrl.Result{}, nil
	}

	// Sort: scaleUpPriority ascending (lower = try first), then name to break
	// ties deterministically.
	sorted := make([]memberWithPool, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool {
		iPri := sorted[i].spec.Config.EffectiveScaleUpPriority()
		jPri := sorted[j].spec.Config.EffectiveScaleUpPriority()
		if iPri != jPri {
			return iPri < jPri
		}
		return sorted[i].spec.Name < sorted[j].spec.Name
	})
	_ = aggIdle // future: per-member idle weighting

	observedByName := buildObservedMemberMap(env, r.LocalClusterID)

	now := time.Now()

	deltaRemaining := deltaTotal
	statusUpdates := []memberStatusUpdate{}
	scaledUp := false
	for _, m := range sorted {
		if deltaRemaining <= 0 {
			break
		}
		om := observedByName[m.spec.Name]
		if om.SaturatedUntil != nil && om.SaturatedUntil.After(now) {
			continue
		}
		attempt := r.attemptMemberScaleUp(ctx, env, m, group, deltaRemaining, aggDesired, maxR, now, saturationCooldown, trigger)
		if attempt.err != nil {
			return scaledUp, ctrl.Result{}, attempt.err
		}
		if attempt.update.name != "" {
			statusUpdates = append(statusUpdates, attempt.update)
		}
		deltaRemaining -= attempt.consumed
		aggDesired += attempt.consumed
		if attempt.consumed > 0 {
			scaledUp = true
		}
	}

	// Persist status updates + LastScaleUpTime on the Env.
	if scaledUp || len(statusUpdates) > 0 {
		if err := r.writeScaleUpStatus(ctx, env, statusUpdates, scaledUp); err != nil {
			klog.ErrorS(err, "failed to update Env.status after scale-up attempt (non-fatal)",
				"env", env.Namespace+"/"+env.Name)
		}
	}

	if !scaledUp && deltaRemaining > 0 {
		klog.V(2).InfoS("Env autoscaler: scale-up requested but no member admitted any delta",
			"env", env.Namespace+"/"+env.Name, "deltaRequested", deltaTotal)
	}

	return scaledUp, ctrl.Result{RequeueAfter: clampRequeue(cooldown)}, nil
}

// scaleUpCooldownActive reports whether the LastScaleUp/LastScaleDown
// timestamps still gate scale-up. When active, the caller should requeue
// after the residual time.
func scaleUpCooldownActive(env *agentsv1alpha1.SandboxEnv, localClusterID string, cooldown time.Duration) (ctrl.Result, bool) {
	localStatus := findLocalClusterStatus(env, localClusterID)
	for _, ts := range []*metav1.Time{localStatus.LastScaleUpTime, localStatus.LastScaleDownTime} {
		if ts == nil {
			continue
		}
		if elapsed := time.Since(ts.Time); elapsed < cooldown {
			return ctrl.Result{RequeueAfter: clampRequeue(cooldown - elapsed)}, true
		}
	}
	return ctrl.Result{}, false
}

// evaluateScaleUpTriggers checks the reactive (annotation) and proactive
// (idle-zero) signals. Returns the trigger label (for events / logs), an
// optional requeue suggestion, and whether any trigger fired.
//
// When no trigger fires but the idle-zero timer is mid-flight, returns a
// non-zero requeue so we wake up exactly when the threshold elapses.
func evaluateScaleUpTriggers(env *agentsv1alpha1.SandboxEnv, members []memberWithPool, localClusterID string, cooldown time.Duration, idleThresholdSec int32) (trigger string, requeue ctrl.Result, fired bool) {
	for _, m := range members {
		if isPendingScaleUpAnnotationFresh(m.pool, cooldown) {
			return "createPending", ctrl.Result{}, true
		}
	}
	idleThreshold := time.Duration(idleThresholdSec) * time.Second
	localStatus := findLocalClusterStatus(env, localClusterID)
	if idleThreshold > 0 && localStatus.IdleZeroSince != nil {
		idleZero := time.Since(localStatus.IdleZeroSince.Time)
		if idleZero >= idleThreshold {
			return "idleZero", ctrl.Result{}, true
		}
		return "", ctrl.Result{RequeueAfter: clampRequeue(idleThreshold - idleZero)}, false
	}
	return "", ctrl.Result{}, false
}

// buildObservedMemberMap indexes the local cluster's ObservedMembers by
// member name so per-attempt saturation checks are O(1).
func buildObservedMemberMap(env *agentsv1alpha1.SandboxEnv, localClusterID string) map[string]agentsv1alpha1.EnvObservedMember {
	out := map[string]agentsv1alpha1.EnvObservedMember{}
	for _, c := range env.Status.Clusters {
		if c.ClusterID != localClusterID {
			continue
		}
		for _, om := range c.ObservedMembers {
			out[om.Name] = om
		}
	}
	return out
}

// memberAttempt captures a single member's contribution to the cycle: how
// many replicas it accepted (consumed), what status update to record, and
// any error that aborts the whole reconcile.
type memberAttempt struct {
	consumed int32              // how many replicas were absorbed by this member's PATCH
	update   memberStatusUpdate // empty when the member was skipped (e.g. saturated headroom)
	err      error              // non-nil aborts the reconcile cycle
}

// attemptMemberScaleUp tries to PATCH a single member's spec.replicas up by
// some portion of deltaRemaining. Honours member.MaxReplicas and the
// group-level maxR ceiling, probes the plugin chain via
// ProbeAcceptedReplicas, and translates the probe result into a memberAttempt.
func (r *SandboxEnvReconciler) attemptMemberScaleUp(
	ctx context.Context,
	env *agentsv1alpha1.SandboxEnv,
	m memberWithPool,
	_ *agentsv1alpha1.EnvAutoscalingGroup,
	deltaRemaining, aggDesired, maxR int32,
	now time.Time,
	saturationCooldown time.Duration,
	trigger string,
) memberAttempt {
	current := m.pool.Spec.Replicas
	candidate := current + deltaRemaining
	if m.spec.Config.MaxReplicas != nil && *m.spec.Config.MaxReplicas > 0 && candidate > *m.spec.Config.MaxReplicas {
		candidate = *m.spec.Config.MaxReplicas
	}
	if maxR > 0 {
		if h := maxR - aggDesired + current; candidate > h {
			candidate = h
		}
	}
	if candidate <= current {
		return memberAttempt{}
	}

	res := plugins.ProbeAcceptedReplicas(ctx, r.PluginManager, m.pool, nil, current, candidate)
	switch res.Kind {
	case plugins.ProbeOK:
		if err := r.patchPoolReplicas(ctx, m.pool, res.Accepted); err != nil {
			return memberAttempt{err: err}
		}
		r.clearPoolScaleUpPending(ctx, m.pool)
		r.emitEvent(env, "ScaleUp", "AutoscalerScaleUp",
			"increased %s/%s replicas from %d to %d (trigger: %s)",
			m.pool.Namespace, m.pool.Name, current, res.Accepted, trigger)
		return memberAttempt{
			consumed: res.Accepted - current,
			update: memberStatusUpdate{
				name:           m.spec.Name,
				attemptResult:  "Success",
				clearSaturated: true,
			},
		}
	case plugins.ProbeInsufficientResources:
		consumed := int32(0)
		if res.Accepted > current {
			if err := r.patchPoolReplicas(ctx, m.pool, res.Accepted); err != nil {
				return memberAttempt{err: err}
			}
			r.clearPoolScaleUpPending(ctx, m.pool)
			r.emitEvent(env, "ScaleUpPartial", "AutoscalerScaleUpPartial",
				"partially increased %s/%s replicas from %d to %d (scheduler cap)",
				m.pool.Namespace, m.pool.Name, current, res.Accepted)
			consumed = res.Accepted - current
		}
		until := metav1.NewTime(now.Add(saturationCooldown))
		return memberAttempt{
			consumed: consumed,
			update: memberStatusUpdate{
				name:           m.spec.Name,
				attemptResult:  "InsufficientResources",
				saturatedUntil: &until,
				message:        truncErr(res.Err),
			},
		}
	case plugins.ProbeInvalidSpec:
		until := metav1.NewTime(now.Add(saturationCooldown))
		return memberAttempt{
			update: memberStatusUpdate{
				name:           m.spec.Name,
				attemptResult:  "InvalidSpec",
				saturatedUntil: &until,
				message:        truncErr(res.Err),
			},
		}
	default: // ProbeInternalError — retry next reconcile, don't mark saturated.
		return memberAttempt{
			update: memberStatusUpdate{
				name:          m.spec.Name,
				attemptResult: "InternalError",
				message:       truncErr(res.Err),
			},
		}
	}
}

// memberStatusUpdate accumulates the result of one member's scale-up
// attempt within a reconcile cycle. Applied to Env.Status in a single
// batched patch by writeScaleUpStatus.
type memberStatusUpdate struct {
	name           string
	attemptResult  string       // matches EnvObservedMember.LastScaleUpAttemptResult values
	saturatedUntil *metav1.Time // nil unless saturating
	clearSaturated bool         // when true, force SaturatedUntil → nil
	message        string       // verbatim short error description
}

// writeScaleUpStatus persists per-member attempt results plus LastScaleUpTime
// to Env.Status. Single Patch through the Status subresource.
func (r *SandboxEnvReconciler) writeScaleUpStatus(ctx context.Context, env *agentsv1alpha1.SandboxEnv, updates []memberStatusUpdate, anyScaledUp bool) error {
	base := env.DeepCopy()
	now := metav1.Now()
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
		if anyScaledUp {
			local.LastScaleUpTime = &now
		}
		for _, u := range updates {
			applyMemberAttemptResult(local, u)
		}
	})
	if err := r.Status().Patch(ctx, env, client.MergeFrom(base)); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

// applyMemberAttemptResult upserts the per-member fields driven by a probe
// attempt outcome. The ObservedMember entry must already exist (syncStatus
// is responsible for creating it on the first observation).
func applyMemberAttemptResult(local *agentsv1alpha1.EnvClusterStatus, u memberStatusUpdate) {
	idx := -1
	for i := range local.ObservedMembers {
		if local.ObservedMembers[i].Name == u.name {
			idx = i
			break
		}
	}
	if idx < 0 {
		// syncStatus hasn't observed this member yet; create a minimal entry
		// so the autoscaler's signals aren't lost. State will be reconciled
		// on the next syncStatus pass.
		local.ObservedMembers = append(local.ObservedMembers, agentsv1alpha1.EnvObservedMember{Name: u.name})
		idx = len(local.ObservedMembers) - 1
	}
	om := &local.ObservedMembers[idx]
	om.LastScaleUpAttemptResult = u.attemptResult
	om.ScaleUpErrorMessage = u.message
	switch {
	case u.clearSaturated:
		om.SaturatedUntil = nil
	case u.saturatedUntil != nil:
		om.SaturatedUntil = u.saturatedUntil
	}
}

// patchPoolReplicas writes spec.replicas onto pool, retrying on conflict.
// Mutates pool in place so the caller can use the updated value for
// aggregate accounting within the same reconcile cycle.
func (r *SandboxEnvReconciler) patchPoolReplicas(ctx context.Context, pool *agentsv1alpha1.SandboxPool, target int32) error {
	base := pool.DeepCopy()
	pool.Spec.Replicas = target
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		// Restore the in-memory value so subsequent iteration sees the
		// previous replicas count rather than the failed target.
		pool.Spec.Replicas = base.Spec.Replicas
		return err
	}
	klog.InfoS("Env autoscaler: patched Pool spec.replicas",
		"pool", pool.Namespace+"/"+pool.Name,
		"oldReplicas", base.Spec.Replicas, "newReplicas", target)
	return nil
}

// clearPoolScaleUpPending strips the doorbell annotation when the
// autoscaler has acted on it. Best-effort.
func (r *SandboxEnvReconciler) clearPoolScaleUpPending(ctx context.Context, pool *agentsv1alpha1.SandboxPool) {
	if pool.Annotations[agentsv1alpha1.PoolScaleUpPendingAnnotationKey] == "" {
		return
	}
	base := pool.DeepCopy()
	delete(pool.Annotations, agentsv1alpha1.PoolScaleUpPendingAnnotationKey)
	if err := r.Patch(ctx, pool, client.MergeFrom(base)); err != nil {
		klog.V(4).ErrorS(err, "clear scale-up-pending annotation failed (non-fatal)",
			"pool", pool.Namespace+"/"+pool.Name)
	}
}

// truncErr produces a short single-line description suitable for a CRD
// status field. Returns "" when err is nil.
func truncErr(err *domain.AppError) string {
	if err == nil {
		return ""
	}
	const max = 240
	msg := err.Message
	if len(msg) > max {
		msg = msg[:max] + "…"
	}
	return msg
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

func scaleUpPolicyOrDefault(group *agentsv1alpha1.EnvAutoscalingGroup) (cooldown time.Duration, idleThresholdSeconds int32, saturationCooldown time.Duration) {
	cooldown = time.Duration(defaultScaleUpCooldownSeconds) * time.Second
	idleThresholdSeconds = defaultScaleUpIdleThresholdSeconds
	saturationCooldown = time.Duration(defaultSaturationCooldownSeconds) * time.Second
	if group == nil || group.ScaleUpPolicy == nil {
		return
	}
	if group.ScaleUpPolicy.CooldownSeconds > 0 {
		cooldown = time.Duration(group.ScaleUpPolicy.CooldownSeconds) * time.Second
	}
	if group.ScaleUpPolicy.IdleThresholdSeconds >= 0 {
		idleThresholdSeconds = group.ScaleUpPolicy.IdleThresholdSeconds
	}
	if group.ScaleUpPolicy.SaturationCooldownSeconds > 0 {
		saturationCooldown = time.Duration(group.ScaleUpPolicy.SaturationCooldownSeconds) * time.Second
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

// computeScaleUpDelta computes how many additional replicas the group as a
// whole wants this cycle, given the current aggregate desired count and the
// scale-up mode. Capped by group.MaxReplicas (when maxR > 0).
//
// The mode is applied to the *aggregate* not per-Pool so a multi-member
// Env's "Default" growth still amounts to +max(1, ceil(N/2)) where N is
// the sum of replicas — matching the user's intuition that the policy
// describes "how the Env grows", not "how each Pool grows".
func computeScaleUpDelta(aggregateDesired int32, group *agentsv1alpha1.EnvAutoscalingGroup, maxR int32) int32 {
	mode := agentsv1alpha1.PoolScaleUpModeDefault
	if group != nil && group.ScaleUpPolicy != nil && group.ScaleUpPolicy.Mode != "" {
		mode = group.ScaleUpPolicy.Mode
	}
	cur := aggregateDesired
	var target int32
	switch mode {
	case agentsv1alpha1.PoolScaleUpModeConservative:
		target = cur + 1
	case agentsv1alpha1.PoolScaleUpModeAggressive:
		if cur == 0 {
			target = 1
		} else {
			target = cur * 2
		}
	default: // Default mode: +max(1, ceil(cur/2))
		add := max(int32(math.Ceil(float64(cur)/2.0)), 1)
		target = cur + add
	}
	if maxR > 0 && target > maxR {
		target = maxR
	}
	if target < cur {
		return 0
	}
	return target - cur
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
