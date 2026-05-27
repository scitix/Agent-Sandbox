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

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

// Values for PoolAutoScalingStatus.LastScaleUpAttemptResult. Kept as
// constants so callers and tests can compare without typo-prone string
// literals.
const (
	ScaleUpAttemptResultSuccess               = "Success"
	ScaleUpAttemptResultInsufficientResources = "InsufficientResources"
	ScaleUpAttemptResultInvalidSpec           = "InvalidSpec"
	ScaleUpAttemptResultInternalError         = "InternalError"
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
		return
	}
	if scaledUp := evaluateScaleUp(snap, mut); scaledUp {
		return
	}
	evaluateScaleDown(snap, mut)
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
// iff it committed a SetTargetReplicas (i.e. the Pool's desired
// replicas actually grew this cycle).
//
// The order is deliberately:
//  1. Honour scale-up cooldown.
//  2. Identify which trigger (reactive / proactive) fires.
//  3. Yield to a higher-priority sibling that also wants to grow.
//  4. Compute the target replicas, clamped by member.MaxReplicas
//     and group.MaxReplicas (group aggregate ceiling).
//  5. Commit the spec write + LastScaleUpTime status update.
func evaluateScaleUp(snap *Snapshot, mut *Mutator) bool {
	policy := snap.Group.ScaleUpPolicy
	if scaleUpCooldownActive(snap, policy) {
		return false
	}

	trigger, ok := pickScaleUpTrigger(snap, policy)
	if !ok {
		return false
	}

	if shouldYieldToHigherPriority(snap) {
		return false
	}

	current := snap.Pool.Spec.Replicas
	target := computeScaleUpTarget(snap, policy, current)
	if target <= current {
		return false
	}

	mut.SetTargetReplicas(target)
	now := metav1.NewTime(snap.Now)
	mut.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		s.LastScaleUpTime = &now
		s.LastScaleUpAttemptResult = ScaleUpAttemptResultSuccess
		s.ScaleUpErrorMessage = ""
	})
	mut.EmitEvent(corev1.EventTypeNormal, "ScaleUp", "AutoscalerScaleUp",
		"increased %s/%s replicas from %d to %d (trigger: %s)",
		snap.Pool.Namespace, snap.Pool.Name, current, target, trigger)
	return true
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

// scaleUpCooldownActive reports whether LastScaleUpTime sits inside
// the policy's cooldown window. Honouring this gate is what stops
// the cache-race "double scale-up" the §0.2 incident report
// documents.
func scaleUpCooldownActive(snap *Snapshot, policy agentsv1alpha1.PoolScaleUpPolicy) bool {
	if policy.CooldownSeconds <= 0 {
		return false
	}
	if snap.Pool.Status.AutoScaling == nil || snap.Pool.Status.AutoScaling.LastScaleUpTime == nil {
		return false
	}
	elapsed := snap.Now.Sub(snap.Pool.Status.AutoScaling.LastScaleUpTime.Time)
	return elapsed < time.Duration(policy.CooldownSeconds)*time.Second
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

// evaluateScaleDown runs the scale-down decision pipeline. Decrements
// Pool.Spec.Replicas by exactly one when every gate clears. Multi-step
// scale-down is intentionally not done — gives the next reconcile a
// chance to observe the new state before deciding to go further.
func evaluateScaleDown(snap *Snapshot, mut *Mutator) {
	if snap.IsReactiveDemand() {
		return
	}
	policy := snap.Group.ScaleDownPolicy
	if scaleDownStabilizationActive(snap, policy) {
		return
	}
	current := snap.Pool.Spec.Replicas
	if current <= 0 {
		return
	}
	if !groupMinReplicasHeadroomAvailable(snap, current) {
		return
	}
	if !oldestIdleEligible(snap, policy) {
		return
	}

	target := current - 1
	mut.SetTargetReplicas(target)
	now := metav1.NewTime(snap.Now)
	mut.PatchStatus(func(s *agentsv1alpha1.PoolAutoScalingStatus) {
		s.LastScaleDownTime = &now
	})
	oldest, _ := snap.OldestIdleAge()
	mut.EmitEvent(corev1.EventTypeNormal, "ScaleDown", "AutoscalerScaleDown",
		"decreased %s/%s replicas from %d to %d (oldestIdleDuration: %s)",
		snap.Pool.Namespace, snap.Pool.Name, current, target, oldest.Round(time.Second))
}

// scaleDownStabilizationActive enforces a minimum gap between two
// consecutive scale-downs on the same Pool.
func scaleDownStabilizationActive(snap *Snapshot, policy agentsv1alpha1.PoolScaleDownPolicy) bool {
	if policy.StabilizationSeconds <= 0 {
		return false
	}
	if snap.Pool.Status.AutoScaling == nil || snap.Pool.Status.AutoScaling.LastScaleDownTime == nil {
		return false
	}
	elapsed := snap.Now.Sub(snap.Pool.Status.AutoScaling.LastScaleDownTime.Time)
	return elapsed < time.Duration(policy.StabilizationSeconds)*time.Second
}

// groupMinReplicasHeadroomAvailable returns true when the group's
// aggregate desired (minus 1 for our planned decrement) is still at
// or above MinReplicas. The min is a group-level invariant; per-Pool
// min would conflate group policy with individual Pool state.
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
