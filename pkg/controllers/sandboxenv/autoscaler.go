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
	"fmt"
	"math"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/apiserver/domain"
	"github.com/scitix/agent-sandbox/pkg/apiserver/service/envcommon"
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

// syncAutoscaling runs the Env-level autoscaler over every enabled
// scaling group declared on env.Spec.Autoscaling.Groups.
//
// Per-group flow (within one reconcile cycle, executed in the order the
// groups are declared on the Env):
//  1. Pick the subset of local members whose Config.ScalingGroup matches.
//  2. Refresh the group's IdleZeroSince based on its own aggregate idle.
//  3. Evaluate scale-up using the group's policy + state; PATCH
//     Member.Spec.Replicas on the Env for any member that wins delta. If
//     any member scaled up, skip scale-down for this group this cycle.
//  4. Otherwise evaluate scale-down on the first member of the subset
//     (Phase 1 single-member scale-down).
//
// All bookkeeping (LastScaleUpTime, LastScaleDownTime, IdleZeroSince) is
// per-group, so unrelated groups never block one another's cooldown,
// stabilization, or idle-zero proactive triggers.
//
// All groups are evaluated in one reconcile pass; the controller does NOT
// spawn a goroutine per group — that would conflict with the
// controller-runtime single-reconcile-per-Env contract and require
// independent caches / leader election. Different Envs already reconcile
// in parallel via MaxConcurrentReconciles.
//
// Returns a non-zero RequeueAfter when any group's next decision depends
// on a wall clock.
func (r *SandboxEnvReconciler) syncAutoscaling(ctx context.Context, env *agentsv1alpha1.SandboxEnv) (ctrl.Result, error) {
	localSpec, ok := findLocalClusterSpec(env, r.LocalClusterID)
	if !ok || len(localSpec.Members) == 0 {
		return ctrl.Result{}, nil
	}

	// Load each member's view (Env-side desired + Pool-side observed) once
	// up front so per-group iteration doesn't re-fetch.
	views, err := r.loadMemberViews(ctx, env.Namespace, localSpec.Members)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(views) == 0 {
		return ctrl.Result{}, nil
	}

	if env.Spec.Autoscaling == nil || len(env.Spec.Autoscaling.Groups) == 0 {
		return ctrl.Result{}, nil
	}

	byGroup := groupViewsByScalingGroup(views)
	var combined ctrl.Result
	for i := range env.Spec.Autoscaling.Groups {
		group := &env.Spec.Autoscaling.Groups[i]
		if !group.Enabled {
			continue
		}
		members := byGroup[group.Name]
		if len(members) == 0 {
			// Group is declared + enabled but no member references it. This
			// is a config error the user should fix; we log and skip rather
			// than fail the reconcile.
			klog.V(2).InfoS("Env autoscaler: enabled scaling group has no members",
				"env", env.Namespace+"/"+env.Name, "group", group.Name)
			continue
		}

		// Per-group idle aggregation drives the per-group proactive trigger.
		groupIdle := int32(0)
		for _, m := range members {
			groupIdle += m.pool.Status.IdleReplicas
		}
		if err := r.syncIdleZeroSince(ctx, env, group.Name, groupIdle); err != nil {
			return ctrl.Result{}, err
		}

		scaledUp, upResult, err := r.reconcileScaleUp(ctx, env, members, group, groupIdle)
		if err != nil {
			return ctrl.Result{}, err
		}
		combined = minNonZeroResult(combined, upResult)
		if scaledUp {
			// Don't run scale-down for the same group this cycle; other
			// groups still get their turn below.
			continue
		}

		downResult, err := r.reconcileScaleDown(ctx, env, members[0], group)
		if err != nil {
			return ctrl.Result{}, err
		}
		combined = minNonZeroResult(combined, downResult)
	}
	return combined, nil
}

// memberView pairs an Env-side member entry with a snapshot of its live
// SandboxPool. The struct draws a clear contract between desired and
// observed state:
//
//   - member: the EnvClusterMember from env.Spec.Clusters[].Members[]. This
//     is the SoLE source of truth for "desired" — identity (Name),
//     desired replica count (Spec.Replicas), user/admission knobs (Config:
//     MaxReplicas, ScalingGroup, priorities, …). Reading "desired" from
//     anywhere else is wrong.
//   - pool: the live SandboxPool CR. Read pool.Status (idle / running
//     counts, etc.), pool.Annotations (the PoolScaleUpPending doorbell),
//     and pass it to plugin probes / pod-listing helpers that need a
//     *SandboxPool. NEVER read pool.Spec — it lags member.Spec until the
//     Env Reconciler's drift loop runs, so a same-cycle read is racy.
type memberView struct {
	member agentsv1alpha1.EnvClusterMember
	pool   *agentsv1alpha1.SandboxPool
}

// loadMemberViews fetches the SandboxPool for each member name. Members
// whose Pool is missing are skipped (typical during adopter churn or when
// the Reconciler hasn't materialised the Pool yet).
func (r *SandboxEnvReconciler) loadMemberViews(ctx context.Context, ns string, members []agentsv1alpha1.EnvClusterMember) ([]memberView, error) {
	out := make([]memberView, 0, len(members))
	for _, m := range members {
		pool := &agentsv1alpha1.SandboxPool{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: m.Name}, pool); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, memberView{member: m, pool: pool})
	}
	return out, nil
}

// groupViewsByScalingGroup partitions members by their Config.ScalingGroup.
// Members with empty ScalingGroup are excluded — autoscaling only operates
// on members opted into a group.
func groupViewsByScalingGroup(views []memberView) map[string][]memberView {
	out := map[string][]memberView{}
	for _, v := range views {
		g := v.member.Config.ScalingGroup
		if g == "" {
			continue
		}
		out[g] = append(out[g], v)
	}
	return out
}

// syncIdleZeroSince mirrors the SandboxPool autoscaler's bookkeeping for the
// Env's local cluster status segment. The timestamp drives the proactive
// scale-up condition (idleThresholdSeconds since idle=0).
func (r *SandboxEnvReconciler) syncIdleZeroSince(ctx context.Context, env *agentsv1alpha1.SandboxEnv, groupName string, idleCount int32) error {
	existing := findScalingGroupStatus(env, groupName).IdleZeroSince
	if (idleCount == 0) == (existing != nil) {
		// Already in the desired state (zero+set, or non-zero+cleared).
		return nil
	}
	base := env.DeepCopy()
	if idleCount == 0 {
		now := metav1.Now()
		mutateScalingGroupStatus(env, groupName, func(g *agentsv1alpha1.EnvScalingGroupStatus) {
			g.IdleZeroSince = &now
		})
	} else {
		mutateScalingGroupStatus(env, groupName, func(g *agentsv1alpha1.EnvScalingGroupStatus) {
			g.IdleZeroSince = nil
		})
	}
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
	members []memberView,
	group *agentsv1alpha1.EnvAutoscalingGroup,
	aggIdle int32,
) (bool, ctrl.Result, error) {
	cooldown, idleThresholdSec, saturationCooldown := scaleUpPolicyOrDefault(group)

	if requeue, ok := scaleUpCooldownActive(env, group.Name, cooldown); ok {
		return false, requeue, nil
	}

	trigger, requeue, fired := evaluateScaleUpTriggers(env, members, group.Name, cooldown, idleThresholdSec)
	if !fired {
		return false, requeue, nil
	}

	maxR := effectiveMaxReplicas(group)
	aggDesired := int32(0)
	for _, m := range members {
		// Member.Spec.Replicas is the canonical desired count; pool's
		// Spec.Replicas may temporarily trail it during convergence.
		aggDesired += m.member.Spec.Replicas
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
	sorted := make([]memberView, len(members))
	copy(sorted, members)
	sort.SliceStable(sorted, func(i, j int) bool {
		iPri := sorted[i].member.Config.EffectiveScaleUpPriority()
		jPri := sorted[j].member.Config.EffectiveScaleUpPriority()
		if iPri != jPri {
			return iPri < jPri
		}
		return sorted[i].member.Name < sorted[j].member.Name
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
		om := observedByName[m.member.Name]
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
		if err := r.writeScaleUpStatus(ctx, env, group.Name, statusUpdates, scaledUp); err != nil {
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

// scaleUpCooldownActive reports whether the group's LastScaleUpTime /
// LastScaleDownTime still gate scale-up. When active, the caller should
// requeue after the residual time.
func scaleUpCooldownActive(env *agentsv1alpha1.SandboxEnv, groupName string, cooldown time.Duration) (ctrl.Result, bool) {
	g := findScalingGroupStatus(env, groupName)
	for _, ts := range []*metav1.Time{g.LastScaleUpTime, g.LastScaleDownTime} {
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
// (idle-zero) signals for a single scaling group. Returns the trigger
// label (for events / logs), an optional requeue suggestion, and whether
// any trigger fired.
//
// When no trigger fires but the idle-zero timer is mid-flight, returns a
// non-zero requeue so we wake up exactly when the threshold elapses.
func evaluateScaleUpTriggers(env *agentsv1alpha1.SandboxEnv, members []memberView, groupName string, cooldown time.Duration, idleThresholdSec int32) (trigger string, requeue ctrl.Result, fired bool) {
	for _, m := range members {
		if isPendingScaleUpAnnotationFresh(m.pool, cooldown) {
			return "createPending", ctrl.Result{}, true
		}
	}
	idleThreshold := time.Duration(idleThresholdSec) * time.Second
	groupStatus := findScalingGroupStatus(env, groupName)
	if idleThreshold > 0 && groupStatus.IdleZeroSince != nil {
		idleZero := time.Since(groupStatus.IdleZeroSince.Time)
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
	m memberView,
	_ *agentsv1alpha1.EnvAutoscalingGroup,
	deltaRemaining, aggDesired, maxR int32,
	now time.Time,
	saturationCooldown time.Duration,
	trigger string,
) memberAttempt {
	// Source of truth for "current desired" is Member.Spec.Replicas on
	// the Env — this is what API writes and what the Reconciler converges
	// the live Pool to. The live Pool's Spec.Replicas may lag the Env if a
	// reconcile is still in flight, so we read from Member to keep the
	// autoscaler's decisions self-consistent across cycles.
	current := m.member.Spec.Replicas
	candidate := current + deltaRemaining
	if m.member.Config.MaxReplicas != nil && *m.member.Config.MaxReplicas > 0 && candidate > *m.member.Config.MaxReplicas {
		candidate = *m.member.Config.MaxReplicas
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
		if err := r.patchMemberSpecReplicas(ctx, env, m, res.Accepted); err != nil {
			return memberAttempt{err: err}
		}
		r.clearPoolScaleUpPending(ctx, m.pool)
		r.emitEvent(env, "ScaleUp", "AutoscalerScaleUp",
			"increased %s/%s replicas from %d to %d (trigger: %s)",
			m.pool.Namespace, m.pool.Name, current, res.Accepted, trigger)
		return memberAttempt{
			consumed: res.Accepted - current,
			update: memberStatusUpdate{
				name:           m.member.Name,
				attemptResult:  "Success",
				clearSaturated: true,
			},
		}
	case plugins.ProbeInsufficientResources:
		consumed := int32(0)
		if res.Accepted > current {
			if err := r.patchMemberSpecReplicas(ctx, env, m, res.Accepted); err != nil {
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
				name:           m.member.Name,
				attemptResult:  "InsufficientResources",
				saturatedUntil: &until,
				message:        truncErr(res.Err),
			},
		}
	case plugins.ProbeInvalidSpec:
		until := metav1.NewTime(now.Add(saturationCooldown))
		return memberAttempt{
			update: memberStatusUpdate{
				name:           m.member.Name,
				attemptResult:  "InvalidSpec",
				saturatedUntil: &until,
				message:        truncErr(res.Err),
			},
		}
	default: // ProbeInternalError — retry next reconcile, don't mark saturated.
		return memberAttempt{
			update: memberStatusUpdate{
				name:          m.member.Name,
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

// writeScaleUpStatus persists per-member attempt results (on the local
// cluster status segment) plus the group's LastScaleUpTime. Single Patch
// through the Status subresource.
func (r *SandboxEnvReconciler) writeScaleUpStatus(ctx context.Context, env *agentsv1alpha1.SandboxEnv, groupName string, updates []memberStatusUpdate, anyScaledUp bool) error {
	base := env.DeepCopy()
	now := metav1.Now()
	if anyScaledUp {
		mutateScalingGroupStatus(env, groupName, func(g *agentsv1alpha1.EnvScalingGroupStatus) {
			g.LastScaleUpTime = &now
		})
	}
	mutateLocalClusterStatus(env, r.LocalClusterID, func(local *agentsv1alpha1.EnvClusterStatus) {
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

// patchMemberSpecReplicas writes the desired replicas count onto the
// matching EnvClusterMember.Spec.Replicas via a retry-on-conflict patch on
// the SandboxEnv CR. The Env Reconciler is the sole writer of the live
// Pool's Spec.Replicas — it picks up the new value on its next reconcile
// pass and propagates it.
//
// The supplied m.member.Spec.Replicas is updated in place so subsequent
// iterations of the scale-up loop see the new aggregate within the same
// reconcile cycle. The live Pool (m.pool) is left untouched: anyone
// reading pool.Spec is doing it wrong (see memberView's contract).
func (r *SandboxEnvReconciler) patchMemberSpecReplicas(ctx context.Context, env *agentsv1alpha1.SandboxEnv, m memberView, target int32) error {
	key := types.NamespacedName{Namespace: env.Namespace, Name: env.Name}
	oldReplicas := m.member.Spec.Replicas
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &agentsv1alpha1.SandboxEnv{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		base := current.DeepCopy()
		ms := envcommon.LocalClusterMembers(&current.Spec, r.LocalClusterID)
		idx := -1
		for i := range ms {
			if ms[i].Name == m.member.Name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("member %q vanished from env %s/%s", m.member.Name, env.Namespace, env.Name)
		}
		ms[idx].Spec.Replicas = target
		envcommon.SetLocalClusterMembers(&current.Spec, r.LocalClusterID, ms)
		return r.Patch(ctx, current, client.MergeFrom(base))
	}); err != nil {
		return err
	}

	// NOTE: m is a value-copy here so mutating m.member.Spec.Replicas would
	// not propagate to the loop in reconcileScaleUp. Aggregate accounting
	// across loop iterations is handled via aggDesired += attempt.consumed
	// at the call site; each member is only visited once per cycle, so we
	// don't need to keep m in sync. The live Pool's spec catches up when
	// the Env Reconciler runs (it observed the Env change above and will
	// be requeued).

	klog.InfoS("Env autoscaler: patched Member.Spec.Replicas",
		"env", env.Namespace+"/"+env.Name,
		"member", m.member.Name,
		"oldReplicas", oldReplicas, "newReplicas", target)
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
	m memberView,
	group *agentsv1alpha1.EnvAutoscalingGroup,
) (ctrl.Result, error) {
	pool := m.pool
	idleTimeoutSec, stabilizationSec := scaleDownPolicyOrDefault(group)
	idleTimeout := time.Duration(idleTimeoutSec) * time.Second
	stabilization := time.Duration(stabilizationSec) * time.Second

	if pool.Status.IdleReplicas == 0 {
		return ctrl.Result{}, nil
	}

	current := m.member.Spec.Replicas
	lowerBound := max(pool.Status.RunningReplicas, effectiveMinReplicas(group))
	if current <= lowerBound {
		return ctrl.Result{}, nil
	}

	groupStatus := findScalingGroupStatus(env, group.Name)
	if groupStatus.LastScaleDownTime != nil {
		elapsed := time.Since(groupStatus.LastScaleDownTime.Time)
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

	newReplicas := current - 1
	if err := r.patchMemberSpecReplicas(ctx, env, m, newReplicas); err != nil {
		return ctrl.Result{}, err
	}

	r.emitEvent(env, "ScaleDown", "AutoscalerScaleDown",
		"decreased %s/%s replicas from %d to %d (oldestIdleDuration: %s)",
		pool.Namespace, pool.Name, current, newReplicas, idleDuration.Round(time.Second))

	now := metav1.Now()
	baseEnv := env.DeepCopy()
	mutateScalingGroupStatus(env, group.Name, func(g *agentsv1alpha1.EnvScalingGroupStatus) {
		g.LastScaleDownTime = &now
	})
	if err := r.Status().Patch(ctx, env, client.MergeFrom(baseEnv)); err != nil && !apierrors.IsConflict(err) {
		klog.ErrorS(err, "failed to update scalingGroup.lastScaleDownTime (non-fatal)",
			"env", env.Namespace+"/"+env.Name, "group", group.Name)
	}

	klog.InfoS("Env autoscaler: decreased Member.Spec.Replicas",
		"env", env.Namespace+"/"+env.Name,
		"member", m.member.Name,
		"oldReplicas", current, "newReplicas", newReplicas,
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
