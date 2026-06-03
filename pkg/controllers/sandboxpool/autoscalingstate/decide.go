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
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Decide is the pure-function entry point of the Pool autoscaler.
// Given a fully-loaded Snapshot it computes whatever bookkeeping +
// spec writes the autoscaler wants for this reconcile cycle and
// stages them on mut. No K8s I/O happens here — that is Commit's job.
//
// Decision flow:
//
//  1. Idle-zero bookkeeping. Maintains
//     Pool.Status.AutoScaling.IdleZeroSince — set when idleReplicas
//     hits 0, cleared when it climbs back above 0. The timestamp
//     drives the proactive scale-up trigger; the same bookkeeping
//     runs even when autoscaling is disabled so the field doesn't
//     stay stale forever after a toggle.
//
//  2. Scale-up evaluation. Runs only when autoscaling is enabled.
//     Returns early (without trying scale-down) when it actually
//     committed a scale-up — same-cycle scale-down on a Pool we just
//     decided to grow is never desirable.
//
//  3. Scale-down evaluation. Only runs when no scale-up happened
//     this cycle. Honours the group MinReplicas aggregate floor,
//     scale-down stabilization, and the per-Pod idleTimeoutSeconds
//     gate.
//
// All cooldown / quiet-window / idle-threshold semantics come from
// the group's PoolScaleUpPolicy / PoolScaleDownPolicy, which the
// CRD's kubebuilder defaults guarantee are populated.
func Decide(snap *Snapshot, mut *Mutator) {
	if snap == nil || mut == nil {
		return
	}
	updateIdleZeroSince(snap, mut)

	if !snap.IsAutoscalingEnabled() {
		klog.V(3).InfoS("autoscaler: autoscaling disabled — skipping scale-up/down",
			"pool", poolKeyFor(snap),
			"hasGroup", snap.Group != nil,
			"groupEnabled", snap.Group != nil && snap.Group.Enabled,
		)
		return
	}
	// Self-heal invariant: spec.replicas must never sit below the live
	// RunningReplicas count. Running pods are claimed by active Sandboxes
	// and are scale-down-protected; a target below them is a phantom
	// decision the Pool reconciler can't act on. Raise the floor and
	// defer further scale-up / scale-down to the next cycle so we see
	// the corrected state.
	if reconcileRunningFloor(snap, mut) {
		return
	}
	if scaledUp := evaluateScaleUp(snap, mut); scaledUp {
		// A scale-up cut an in-flight scale-down session short. Close it
		// silently: the session did not finish on its own terms, so a
		// "completed, removed N" event would misreport. The in-process
		// session is cleared; the Started event already emitted stands as
		// the record that a drain began.
		if snap.ScaleDownSession.Active {
			mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownAborted})
		}
		return
	}
	evaluateScaleDown(snap, mut)
}

// reconcileRunningFloor raises Pool.Spec.Replicas to Status.RunningReplicas
// when the persisted target has slipped below the claimed-pod count.
// Returns true when a write was staged.
//
// This corrects the bug where an earlier scale-down (or a stale snapshot)
// dropped spec.replicas below the actual Running pod count, leaving the
// Pool reconciler unable to delete the excess (Running pods are
// protected) and Env.Status.totalDesired permanently misrepresenting
// cluster capacity.
func reconcileRunningFloor(snap *Snapshot, mut *Mutator) bool {
	if snap.Pool == nil {
		return false
	}
	running := snap.Pool.Status.RunningReplicas
	current := snap.Pool.Spec.Replicas
	if running <= current {
		return false
	}
	klog.V(2).InfoS("autoscaler: raising spec.replicas to RunningReplicas floor",
		"pool", poolKeyFor(snap),
		"current", current,
		"running", running,
	)
	mut.SetTargetReplicas(running)
	return true
}

// poolKeyFor is a small helper to keep log lines compact.
func poolKeyFor(snap *Snapshot) string {
	if snap == nil || snap.Pool == nil {
		return ""
	}
	return snap.Pool.Namespace + "/" + snap.Pool.Name
}

// updateIdleZeroSince syncs Pool.Status.AutoScaling.IdleZeroSince with
// the current idle count: set on the 1 → 0 transition (or first
// observation at 0), cleared on 0 → 1+.
//
// We stage the write on the mutator only when the persisted value
// actually disagrees with the desired one, so a long-running zero
// window doesn't generate a no-op status patch every reconcile.
func updateIdleZeroSince(snap *Snapshot, mut *Mutator) {
	idle := poolIdleReplicas(snap)
	cur := currentIdleZeroSince(snap)

	switch {
	case idle == 0 && cur == nil:
		now := metav1.NewTime(snap.Now)
		mut.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
			s.IdleZeroSince = &now
		})
	case idle > 0 && cur != nil:
		mut.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
			s.IdleZeroSince = nil
		})
	}
}

// evaluateScaleUp runs the scale-up decision pipeline. Returns true
// iff it staged a ScaleUpAttempt (the actual probe + spec write are
// performed in Mutator.Commit). "Returned true" is the signal to skip
// scale-down for this cycle — even when the probe later rejects the
// growth, we don't want same-cycle shrink on top.
//
// The order is deliberately:
//  1. Honour the dual cooldown (success cooldown + saturation cooldown).
//  2. Identify which trigger (reactive / proactive) fires.
//  3. Yield to a higher-priority sibling that also wants to grow.
//  4. Compute the target replicas, clamped by member.MaxReplicas
//     and group.MaxReplicas (group aggregate ceiling).
//  5. Stage a ScaleUpAttempt; Mutator.Commit consults the Prober and
//     writes status + spec based on the probe outcome.
func evaluateScaleUp(snap *Snapshot, mut *Mutator) bool {
	key := poolKeyFor(snap)
	policy := snap.Group.ScaleUpPolicy

	if scaleUpCooldownActive(snap, policy) {
		klog.V(2).InfoS("autoscaler: scale-up gated by cooldown",
			"pool", key,
			"lastScaleUpTime", asStatus(snap).LastScaleUpTime,
			"lastScaleUpAttemptTime", asStatus(snap).LastScaleUpAttemptTime,
			"lastScaleUpAttemptResult", asStatus(snap).LastScaleUpAttemptResult,
			"cooldownSeconds", policy.CooldownSeconds,
			"saturationCooldownSeconds", policy.SaturationCooldownSeconds,
		)
		return false
	}

	trigger, ok := pickScaleUpTrigger(snap, policy)
	if !ok {
		klog.V(2).InfoS("autoscaler: scale-up not triggered",
			"pool", key,
			"reactiveDemand", snap.IsReactiveDemand(),
			"idleZeroSince", asStatus(snap).IdleZeroSince,
			"idleThresholdSeconds", policy.IdleThresholdSeconds,
			"idleZeroQuietWindowSeconds", policy.IdleZeroQuietWindowSeconds,
			"lastCreateAt", snap.LastCreateAt,
			"now", snap.Now,
		)
		return false
	}

	if shouldYieldToHigherPriority(snap) {
		klog.V(2).InfoS("autoscaler: scale-up yielded to higher-priority sibling",
			"pool", key,
			"trigger", trigger,
			"selfPriority", snap.MemberConfig.EffectiveScaleUpPriority(),
		)
		return false
	}

	current := snap.Pool.Spec.Replicas
	target := computeScaleUpTarget(snap, policy, current)
	if target <= current {
		var groupMax any = "<unset>"
		if snap.Group.MaxReplicas != nil {
			groupMax = *snap.Group.MaxReplicas
		}
		klog.V(2).InfoS("autoscaler: scale-up target not above current (likely clamped by group/member cap)",
			"pool", key,
			"current", current,
			"target", target,
			"groupMaxReplicas", groupMax,
			"groupDesiredTotal", snap.GroupDesiredTotal(),
		)
		return false
	}

	klog.V(2).InfoS("autoscaler: staging scale-up attempt",
		"pool", key,
		"trigger", trigger,
		"current", current,
		"target", target,
	)
	mut.ScaleUpAttempt(current, target)
	return true
}

// asStatus returns the Pool.Status.AutoScaling block or an empty
// placeholder so log statements can dereference without nil checks.
func asStatus(snap *Snapshot) *agentsv1alpha1.PoolAutoScalingStatus {
	if snap == nil || snap.Pool == nil || snap.Pool.Status.AutoScaling == nil {
		return &agentsv1alpha1.PoolAutoScalingStatus{}
	}
	return snap.Pool.Status.AutoScaling
}

// pickScaleUpTrigger selects which scale-up trigger fires this cycle,
// or returns ok=false when none does.
//
// Priority:
//  1. Reactive — `QueueLen > 0 && IdleReady == 0`. Bypasses every
//     other gate; a real waiter is the strongest possible signal.
//  2. Proactive idleZero — `idleReplicas==0` has persisted for at
//     least IdleThresholdSeconds AND a Sandbox.Create has been
//     observed within IdleZeroQuietWindowSeconds. The quiet window
//     prevents "warm pool churn" when no user is actually around.
func pickScaleUpTrigger(snap *Snapshot, policy agentsv1alpha1.PoolScaleUpPolicy) (string, bool) {
	if snap.IsReactiveDemand() {
		return "reactiveDemand", true
	}
	if policy.IdleThresholdSeconds <= 0 {
		return "", false
	}
	cur := currentIdleZeroSince(snap)
	if cur == nil {
		return "", false
	}
	if snap.Now.Sub(cur.Time) < time.Duration(policy.IdleThresholdSeconds)*time.Second {
		return "", false
	}
	if policy.IdleZeroQuietWindowSeconds > 0 {
		if snap.LastCreateAt == nil {
			return "", false
		}
		if snap.Now.Sub(*snap.LastCreateAt) > time.Duration(policy.IdleZeroQuietWindowSeconds)*time.Second {
			return "", false
		}
	}
	return "idleZero", true
}

// scaleUpCooldownActive reports whether the Pool is inside either of
// two cooldown windows that block further scale-up attempts:
//
//   - success cooldown: LastScaleUpTime + CooldownSeconds. Prevents
//     two successful scale-ups firing closer together than the
//     configured cadence, and is the gate that catches cache-race
//     "double scale-up" symptoms.
//
//   - saturation cooldown: LastScaleUpAttemptTime + SaturationCooldownSeconds,
//     applied only when the last attempt's result was non-Enough
//     (Insufficient / JustRight / Failed). Prevents the autoscaler
//     from re-probing the plugin chain immediately after the cluster
//     told us "no headroom" or "spec invalid".
//
// Either window active → cooldown active.
func scaleUpCooldownActive(snap *Snapshot, policy agentsv1alpha1.PoolScaleUpPolicy) bool {
	s := snap.Pool.Status.AutoScaling
	if s == nil {
		return false
	}
	if policy.CooldownSeconds > 0 && s.LastScaleUpTime != nil {
		if snap.Now.Sub(s.LastScaleUpTime.Time) < time.Duration(policy.CooldownSeconds)*time.Second {
			return true
		}
	}
	if policy.SaturationCooldownSeconds > 0 && s.LastScaleUpAttemptTime != nil && isSaturatingResult(s.LastScaleUpAttemptResult) {
		if snap.Now.Sub(s.LastScaleUpAttemptTime.Time) < time.Duration(policy.SaturationCooldownSeconds)*time.Second {
			return true
		}
	}
	return false
}

// isSaturatingResult reports whether the given result implies the Pool
// is saturated for cooldown purposes — i.e. the last attempt told us
// "the cluster cannot fit more" (or "this spec is wrong"), so retrying
// soon is wasteful. PoolScaleUpAttemptEnough and empty (never tried)
// do NOT saturate.
func isSaturatingResult(r agentsv1alpha1.PoolScaleUpAttemptResult) bool {
	switch r {
	case agentsv1alpha1.PoolScaleUpAttemptInsufficient,
		agentsv1alpha1.PoolScaleUpAttemptJustRight,
		agentsv1alpha1.PoolScaleUpAttemptFailed:
		return true
	default:
		return false
	}
}

// shouldYieldToHigherPriority returns true when at least one sibling
// Pool in the same scaling group has a strictly lower
// EffectiveScaleUpPriority (i.e. is preferred to grow first) AND
// also looks like it wants to scale up right now. This keeps the
// per-Pool decisions from inverting the user's stated priority
// ordering when two Pools simultaneously hit their triggers.
//
// The "looks like wants to scale up" probe is a coarse-grained
// approximation: we only check what we already have in the Snapshot
// — sibling's IdleZeroSince + idle count + scaling policy. We don't
// re-read each sibling's PoolScheduler queue length (too expensive
// per cycle, and reactive demand on a sibling does not necessarily
// override our own reactive demand).
func shouldYieldToHigherPriority(snap *Snapshot) bool {
	if snap.MemberConfig == nil {
		return false
	}
	selfPriority := snap.MemberConfig.EffectiveScaleUpPriority()
	for _, sib := range snap.SiblingPools {
		if sib.Name == snap.Pool.Name {
			continue
		}
		sibCfg := findMemberConfig(snap.Env, sib.Name)
		if sibCfg == nil {
			continue
		}
		if sibCfg.EffectiveScaleUpPriority() >= selfPriority {
			continue
		}
		// A higher-priority sibling exists. Yield if it ALSO has a
		// fired trigger — otherwise we'd starve ourselves over an
		// idle sibling.
		if siblingIsRipeToScaleUp(snap, sib) {
			return true
		}
	}
	return false
}

// siblingIsRipeToScaleUp is the cheap, read-only proxy for "this
// sibling's autoscaler would also act this cycle". Same gating
// (idle==0 + threshold elapsed) as our own proactive trigger;
// reactive sibling demand is not checked here — there's no shared
// fact in the Snapshot for it.
func siblingIsRipeToScaleUp(snap *Snapshot, sib *agentsv1alpha1.SandboxPool) bool {
	if sib.Status.IdleReplicas > 0 {
		return false
	}
	if sib.Status.AutoScaling == nil || sib.Status.AutoScaling.IdleZeroSince == nil {
		return false
	}
	threshold := time.Duration(snap.Group.ScaleUpPolicy.IdleThresholdSeconds) * time.Second
	if threshold <= 0 {
		return false
	}
	return snap.Now.Sub(sib.Status.AutoScaling.IdleZeroSince.Time) >= threshold
}

// computeScaleUpTarget computes the desired post-scale-up Pool
// replicas. Steps:
//  1. Base growth from `current` using the policy's mode.
//  2. Clamp to member.MaxReplicas (if set).
//  3. Clamp to the group's MaxReplicas aggregate ceiling — we treat
//     the group total as the ceiling and let this Pool absorb the
//     remaining headroom.
//
// Returns current when no growth is possible.
func computeScaleUpTarget(snap *Snapshot, policy agentsv1alpha1.PoolScaleUpPolicy, current int32) int32 {
	target := applyScaleUpMode(policy.Mode, current)

	if snap.MemberConfig != nil && snap.MemberConfig.MaxReplicas != nil {
		if mm := *snap.MemberConfig.MaxReplicas; mm > 0 && target > mm {
			target = mm
		}
	}
	if snap.Group.MaxReplicas != nil {
		groupCeiling := *snap.Group.MaxReplicas
		// Headroom = ceiling minus everyone else's desired.
		otherDesired := snap.GroupDesiredTotal() - current
		headroom := groupCeiling - otherDesired
		if target > headroom {
			target = headroom
		}
	}
	if target < current {
		// All clamps below current → no growth.
		return current
	}
	// Invariant guard: never grow by more than max(current, 1) in
	// a single step. Default/Aggressive modes already satisfy this
	// for cur>=2; this defends against future mode additions.
	delta := target - current
	cap := max(current, 1)
	if delta > cap {
		target = current + cap
	}
	return target
}

// applyScaleUpMode is the textbook mode-based growth function. Pure
// math, no policy knobs — see the field doc on PoolScaleUpMode.
func applyScaleUpMode(mode agentsv1alpha1.PoolScaleUpMode, current int32) int32 {
	if mode == "" {
		mode = agentsv1alpha1.PoolScaleUpModeDefault
	}
	switch mode {
	case agentsv1alpha1.PoolScaleUpModeConservative:
		return current + 1
	case agentsv1alpha1.PoolScaleUpModeAggressive:
		if current == 0 {
			return 1
		}
		return current * 2
	default: // PoolScaleUpModeDefault
		add := max(int32(math.Ceil(float64(current)/2.0)), 1)
		return current + add
	}
}

// stuckEventInterval rate-limits the AutoscalerScaleDownStuck warning so a
// session blocked for a long time by a hard floor (RunningReplicas or
// group MinReplicas) reports periodically rather than every reconcile. The
// per-step stabilization window is too short to reuse here — it governs
// decrement cadence, not how often to re-warn about a stall.
const stuckEventInterval = 5 * time.Minute

// evaluateScaleDown runs the scale-down decision pipeline. It decrements
// Pool.Spec.Replicas by exactly one when every gate clears — multi-step
// scale-down is intentionally not done, giving the next reconcile a chance
// to observe the new state before going further — and it drives a
// Started / Completed / Stuck event lifecycle (one pair per drain) instead
// of one event per replica removed.
//
// Each gate early-return below maps to exactly one session transition; the
// mapping must be preserved if the gate order is ever changed:
//
//	reactive demand        → Aborted (active session cut short by demand)
//	stabilization window   → no transition (transient wait; session stays open)
//	current <= 0           → Completed (drained to zero)
//	group MinReplicas floor→ Stuck (idle pods remain but a hard floor blocks)
//	no idle pod aged enough→ Completed (nothing left to remove — natural end)
//	RunningReplicas floor  → Stuck (idle pods remain but a hard floor blocks)
//	decrement staged       → Started (first step) or Stepped (subsequent, silent)
func evaluateScaleDown(snap *Snapshot, mut *Mutator) {
	key := poolKeyFor(snap)
	sess := snap.ScaleDownSession
	if snap.IsReactiveDemand() {
		klog.V(3).InfoS("autoscaler: scale-down skipped (reactive demand)",
			"pool", key,
			"queueLen", snap.PoolSchedSnap.QueueLen,
			"idleReady", snap.PoolSchedSnap.IdleReady,
		)
		if sess.Active {
			mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownAborted})
		}
		return
	}
	policy := snap.Group.ScaleDownPolicy
	if scaleDownStabilizationActive(snap, policy) {
		// Transient gate: the previous decrement is still inside its
		// stabilization window. The session, if open, stays open — do not
		// emit Completed or Stuck.
		klog.V(3).InfoS("autoscaler: scale-down gated by stabilization window",
			"pool", key,
			"lastScaleDownTime", asStatus(snap).LastScaleDownTime,
			"inProcessLastDecision", sess.LastDecisionAt,
			"stabilizationSeconds", policy.StabilizationSeconds,
		)
		return
	}
	current := snap.Pool.Spec.Replicas
	if current <= 0 {
		completeScaleDownSession(snap, mut)
		return
	}
	if !groupMinReplicasHeadroomAvailable(snap, current) {
		klog.V(3).InfoS("autoscaler: scale-down gated by group MinReplicas",
			"pool", key,
			"groupDesiredTotal", snap.GroupDesiredTotal(),
			"minReplicas", snap.Group.MinReplicas,
		)
		maybeEmitScaleDownStuck(snap, mut, current, "group MinReplicas")
		return
	}
	if !oldestIdleEligible(snap, policy) {
		// No idle Pod has aged past the timeout: there is genuinely
		// nothing more to remove right now. This is the natural end of a
		// drain, not a stall.
		klog.V(3).InfoS("autoscaler: scale-down skipped (no idle pod aged enough)",
			"pool", key,
			"idleTimeoutSeconds", policy.IdleTimeoutSeconds,
			"idlePodCount", len(snap.IdlePodAges),
		)
		completeScaleDownSession(snap, mut)
		return
	}

	target := current - 1
	// Per-member floor: the owning Env member may declare its own
	// MinReplicas (Config.MinReplicas), independent of the group's
	// aggregate MinReplicas. Never shrink this Pool below it.
	if memberMin := memberMinReplicasFloor(snap); target < memberMin {
		klog.V(3).InfoS("autoscaler: scale-down blocked by member MinReplicas floor",
			"pool", key, "wantTarget", target, "memberMinReplicas", memberMin)
		maybeEmitScaleDownStuck(snap, mut, current, "member MinReplicas")
		return
	}
	// Defensive: never lower the target below RunningReplicas. The
	// top-of-Decide self-heal usually keeps current >= running so this
	// branch is rarely reached, but keeping the invariant adjacent to
	// the scale-down site documents the rule for future edits.
	if running := snap.Pool.Status.RunningReplicas; target < running {
		klog.V(3).InfoS("autoscaler: scale-down blocked by RunningReplicas floor",
			"pool", key, "wantTarget", target, "running", running)
		maybeEmitScaleDownStuck(snap, mut, current, "RunningReplicas floor")
		return
	}
	klog.V(2).InfoS("autoscaler: scaling down",
		"pool", key,
		"current", current,
		"target", target,
		"sessionActive", sess.Active,
	)
	mut.SetTargetReplicas(target)
	now := metav1.NewTime(snap.Now)
	mut.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		s.LastScaleDownTime = &now
	})
	if sess.Active {
		// Subsequent step of an open session — decrement silently.
		mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownStepped})
		return
	}
	// First step — open a session and announce it once.
	mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: current})
	oldest, _ := snap.OldestIdleAge()
	mut.EmitEvent(corev1.EventTypeNormal, "ScaleDown", "AutoscalerScaleDownStarted",
		"started scale-down of %s/%s from %d replicas (oldestIdleDuration: %s)",
		snap.Pool.Namespace, snap.Pool.Name, current, oldest.Round(time.Second))
}

// completeScaleDownSession closes an open scale-down session and emits a
// single summary event. A no-op when no session is active (the common case
// where the Pool was already at its floor and never started draining).
func completeScaleDownSession(snap *Snapshot, mut *Mutator) {
	sess := snap.ScaleDownSession
	if !sess.Active {
		return
	}
	current := snap.Pool.Spec.Replicas
	removed := sess.StartReplicas - current
	mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownCompleted})
	mut.EmitEvent(corev1.EventTypeNormal, "ScaleDown", "AutoscalerScaleDownCompleted",
		"completed scale-down of %s/%s from %d to %d (removed %d replicas in %s)",
		snap.Pool.Namespace, snap.Pool.Name, sess.StartReplicas, current, removed,
		snap.Now.Sub(sess.StartAt).Round(time.Second))
}

// maybeEmitScaleDownStuck warns that an open session cannot make progress
// because a hard floor (RunningReplicas or group MinReplicas) blocks the
// decrement while idle Pods remain. Rate-limited to one warning per
// stuckEventInterval; the session stays open so it resumes automatically
// once the floor lifts. A no-op when no session is active.
func maybeEmitScaleDownStuck(snap *Snapshot, mut *Mutator, current int32, reason string) {
	sess := snap.ScaleDownSession
	if !sess.Active {
		return
	}
	if !sess.LastStuckAt.IsZero() && snap.Now.Sub(sess.LastStuckAt) < stuckEventInterval {
		return
	}
	mut.SetScaleDownTransition(ScaleDownTransition{Kind: ScaleDownStuckMarked})
	mut.EmitEvent(corev1.EventTypeWarning, "ScaleDown", "AutoscalerScaleDownStuck",
		"scale-down of %s/%s blocked at %d replicas by %s for over %s",
		snap.Pool.Namespace, snap.Pool.Name, current, reason, stuckEventInterval)
}

// scaleDownStabilizationActive enforces a minimum gap between two
// consecutive scale-downs on the same Pool. It gates on the more recent of
// two timestamps: the persisted Pool.Status.AutoScaling.LastScaleDownTime
// (survives process restart) and the in-process session's lastDecisionAt
// (immune to informer-cache lag). The status timestamp alone is not
// enough: it is not read back from the cache before the next
// watch-triggered reconcile, so two near-simultaneous reconciles on the
// same stale Snapshot would both pass the gate and both decrement.
func scaleDownStabilizationActive(snap *Snapshot, policy agentsv1alpha1.PoolScaleDownPolicy) bool {
	if policy.StabilizationSeconds <= 0 {
		return false
	}
	last := lastScaleDownReference(snap)
	if last.IsZero() {
		return false
	}
	elapsed := snap.Now.Sub(last)
	return elapsed < time.Duration(policy.StabilizationSeconds)*time.Second
}

// lastScaleDownReference returns the more recent of the persisted
// LastScaleDownTime and the in-process session's lastDecisionAt. Either
// may be zero (no prior scale-down recorded on that path).
func lastScaleDownReference(snap *Snapshot) time.Time {
	var t time.Time
	if s := snap.Pool.Status.AutoScaling; s != nil && s.LastScaleDownTime != nil {
		t = s.LastScaleDownTime.Time
	}
	if ip := snap.ScaleDownSession.LastDecisionAt; ip.After(t) {
		t = ip
	}
	return t
}

// groupMinReplicasHeadroomAvailable returns true when the group's
// aggregate desired (minus 1 for our planned decrement) is still at
// or above MinReplicas. The min is a group-level invariant; per-Pool
// min would conflate group policy with individual Pool state.
// memberMinReplicasFloor returns the per-member scale-down floor declared on
// the owning Env member (Config.MinReplicas). 0 when unset — in that case only
// the group aggregate MinReplicas applies.
func memberMinReplicasFloor(snap *Snapshot) int32 {
	if snap.MemberConfig != nil && snap.MemberConfig.MinReplicas != nil {
		if m := *snap.MemberConfig.MinReplicas; m > 0 {
			return m
		}
	}
	return 0
}

func groupMinReplicasHeadroomAvailable(snap *Snapshot, currentSelf int32) bool {
	minR := int32(0)
	if snap.Group.MinReplicas != nil {
		minR = *snap.Group.MinReplicas
	}
	aggregate := snap.GroupDesiredTotal()
	postShrink := aggregate - 1
	if postShrink < minR {
		return false
	}
	_ = currentSelf // currentSelf is implicit in aggregate; explicit signature is for readability at call sites.
	return true
}

// oldestIdleEligible returns true when at least one idle Pod has
// been sitting at idle for >= idleTimeoutSeconds. Pool reconciler
// downstream picks which specific Pod to evict (it already has the
// scale-down-protected two-phase flow).
func oldestIdleEligible(snap *Snapshot, policy agentsv1alpha1.PoolScaleDownPolicy) bool {
	if policy.IdleTimeoutSeconds <= 0 {
		// Treat 0 as "any idle pod is eligible immediately" — useful
		// for tests and aggressive shrinkage. Negative is impossible
		// thanks to the CRD's Minimum=0 validation.
		return len(snap.IdlePodAges) > 0
	}
	threshold := time.Duration(policy.IdleTimeoutSeconds) * time.Second
	oldest, ok := snap.OldestIdleAge()
	if !ok {
		return false
	}
	return oldest >= threshold
}

// poolIdleReplicas reads the live idle count out of the Pool's
// status. Defaults to 0 when the field has never been populated
// (freshly materialised Pool before its first reconcile).
func poolIdleReplicas(snap *Snapshot) int32 {
	if snap.Pool == nil {
		return 0
	}
	return snap.Pool.Status.IdleReplicas
}

// currentIdleZeroSince extracts the persisted IdleZeroSince
// timestamp from Pool.Status.AutoScaling, or nil when absent.
func currentIdleZeroSince(snap *Snapshot) *metav1.Time {
	if snap.Pool == nil || snap.Pool.Status.AutoScaling == nil {
		return nil
	}
	return snap.Pool.Status.AutoScaling.IdleZeroSince
}
