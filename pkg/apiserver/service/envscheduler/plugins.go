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

package envscheduler

// The default framework weight set. These are the dials that decide
// "is one factor strictly more important than another, or do they
// trade off". The numbers below encode the policy:
//
//   - Priority dominates everything else (weight 1000). A priority-0
//     Pool always wins over a priority-100 Pool unless the idle delta
//     exceeds 10 (1 idle pod == 10 priority points). In practice this
//     means priority is hard ordering, with a small efficiency override.
//
//   - Idle at 100/pod is the second-strongest signal, and it must stay
//     strictly above Headroom's ceiling: a warm Pod serves the request
//     now, whereas headroom is only a promise that a Pod could be
//     started. Scoring raw headroom against raw idle inverts that — a
//     Pool with a four-figure ceiling would outrank a Pool holding warm
//     Pods by two orders of magnitude, sending every request to the
//     biggest Pool instead of the ready one.
//
//   - Headroom is clamped at headroomScoreCap replicas before weighting,
//     so its total contribution tops out below a single idle Pod. It
//     orders Pools that have no warm capacity, and nothing more: past a
//     handful of spare replicas, "can grow" is not meaningfully better.
//
//   - QueueLength at -2/req is a mild penalty for already-backed-up
//     Pools — better to spread load when other factors tie.
//
//   - SaturationCooldown applies a -1,000,000 penalty. Has to outweigh
//     even a maximally large priority delta (≈100 × weightPriority =
//     100,000) so a saturated low-priority Pool yields to a fresh
//     high-priority sibling. When EVERY candidate is saturated the
//     penalty contributes equally and the other factors decide the
//     ordering — so the all-saturated fallback case still ranks Pools
//     by priority rather than collapsing them to a tie.
const (
	weightPriority           int64 = 1000
	weightIdleReady          int64 = 100
	weightHeadroom           int64 = 5
	weightQueueLength        int64 = -2
	weightSaturationCooldown int64 = 1
	saturationPenalty        int64 = -1_000_000

	// headroomScoreCap bounds the Headroom scorer's input so its weighted
	// contribution (cap × weightHeadroom = 50) stays below one idle Pod
	// (weightIdleReady = 100).
	headroomScoreCap int32 = 10
)

// newDefaultFramework wires up the production filter + score plugin
// set. Kept here so manager.go doesn't have to know the wiring details
// and tests can construct alternate frameworks against the same
// CandidateContext shape.
func newDefaultFramework() *Framework {
	return NewFramework().
		Filter(MaxedOutFilter{}).
		Score(PriorityScorer{}, weightPriority).
		Score(IdleReadyScorer{}, weightIdleReady).
		Score(HeadroomScorer{}, weightHeadroom).
		Score(QueueLengthScorer{}, weightQueueLength).
		Score(SaturationCooldownScorer{}, weightSaturationCooldown)
}

// MaxedOutFilter rejects Pools that have hit their effective cap AND
// have no idle ready. Such a Pool cannot accept the request without
// queuing forever (no idle, no growth headroom), so we'd rather route
// elsewhere. When every candidate is rejected the framework falls back
// to the rejected set so we still hand the request to the least-bad
// option rather than 503.
type MaxedOutFilter struct{}

func (MaxedOutFilter) Name() string { return "MaxedOut" }
func (MaxedOutFilter) Filter(c *CandidateContext) bool {
	cap, have := c.effectiveMax()
	if !have {
		// No cap configured → never maxed out.
		return true
	}
	if c.DesiredReplicas < cap {
		// Still has growth room.
		return true
	}
	// At-or-above cap. Keep iff there is an idle pod to dispatch.
	return c.effectiveIdle() > 0
}

// PriorityScorer rewards lower member.priority — the canonical
// "preferred member" knob. Negated so that priority=0 → score 0 and
// priority=100 → score -100; higher (closer to 0) is better.
type PriorityScorer struct{}

func (PriorityScorer) Name() string                    { return "Priority" }
func (PriorityScorer) Score(c *CandidateContext) int64 { return -int64(c.Member.priority) }

// IdleReadyScorer rewards Pools that have warm idle pods immediately
// available for dispatch. Reads the in-process PoolScheduler snapshot
// (sub-µs: atomic + channel-len reads) and falls back to the Env-observed
// idle count when this process holds no scheduler for the Pool — see
// CandidateContext.effectiveIdle.
type IdleReadyScorer struct{}

func (IdleReadyScorer) Name() string                    { return "IdleReady" }
func (IdleReadyScorer) Score(c *CandidateContext) int64 { return int64(c.effectiveIdle()) }

// QueueLengthScorer penalises Pools that already have a queue
// building up: even if they have headroom, piling more requests on a
// loaded Pool worsens latency. The weight is small so this only
// breaks ties between otherwise-equivalent Pools.
type QueueLengthScorer struct{}

func (QueueLengthScorer) Name() string                    { return "QueueLength" }
func (QueueLengthScorer) Score(c *CandidateContext) int64 { return int64(c.Snap.QueueLen) }

// HeadroomScorer rewards Pools that can still grow. "Headroom" is the
// number of replicas this Pool can add before hitting either its
// per-member cap or the group ceiling. When no cap is configured it
// scores 0 (no preference signal).
//
// This is the plugin that addresses the scenario where one Pool sits at
// its MaxReplicas with 0 idle while another with the same priority can
// still scale: the headroom contribution pushes the growable Pool above
// the maxed-out one.
//
// The raw count is clamped to headroomScoreCap first. Headroom is a
// promise of future capacity, so it must break ties between Pools that
// have no warm Pods without ever outweighing a Pool that does — and an
// unclamped four-figure ceiling would do exactly that.
type HeadroomScorer struct{}

func (HeadroomScorer) Name() string { return "Headroom" }
func (HeadroomScorer) Score(c *CandidateContext) int64 {
	cap, have := c.effectiveMax()
	if !have {
		return 0
	}
	hr := min(max(cap-c.DesiredReplicas, 0), headroomScoreCap)
	return int64(hr)
}

// SaturationCooldownScorer applies a heavy negative score when the
// candidate's owning Pool autoscaler recently failed to scale up (the
// cluster told us "no capacity right now"). A Pool that is saturated for
// the next 60s gets ranked far below fresh peers, but when EVERY
// candidate is saturated the penalty applies equally and the other
// factors still decide the ordering.
//
// Saturation means the Pool cannot *grow*, not that it cannot *serve*.
// A Pool holding warm Pods dispatches them instantly no matter what the
// last scale-up probe said, so it is exempt — penalising it hands the
// request to a sibling that may have nothing to offer at all.
type SaturationCooldownScorer struct{}

func (SaturationCooldownScorer) Name() string { return "SaturationCooldown" }
func (SaturationCooldownScorer) Score(c *CandidateContext) int64 {
	if c.SaturatedUntil == nil || !c.SaturatedUntil.After(c.Now) {
		return 0
	}
	if c.effectiveIdle() > 0 {
		return 0
	}
	return saturationPenalty
}
