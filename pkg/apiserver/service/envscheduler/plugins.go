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
//   - Priority dominates everything else (weight 1000). A
//     priority-0 Pool always wins over a priority-100 Pool unless the
//     IdleReady delta exceeds 10 (1 idle pod ~= 10 priority points)
//     AND the priority gap is within 10. In practice this means
//     priority is hard ordering, with a small efficiency override.
//
//   - IdleReady at 10/pod is the second-strongest signal: a Pool with
//     ready warm Pods is preferred to one that would have to scale up.
//
//   - Headroom at 5/replica is the third: among Pools with no idle
//     ready, prefer the one that still has room to grow.
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
	weightIdleReady          int64 = 10
	weightHeadroom           int64 = 5
	weightQueueLength        int64 = -2
	weightSaturationCooldown int64 = 1
	saturationPenalty        int64 = -1_000_000
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
	// At-or-above cap. Keep iff there is an idle ready pod to dispatch.
	return c.Snap.IdleReady > 0
}

// PriorityScorer rewards lower member.priority — the canonical
// "preferred member" knob. Negated so that priority=0 → score 0 and
// priority=100 → score -100; higher (closer to 0) is better.
type PriorityScorer struct{}

func (PriorityScorer) Name() string                    { return "Priority" }
func (PriorityScorer) Score(c *CandidateContext) int64 { return -int64(c.Member.priority) }

// IdleReadyScorer rewards Pools whose scheduler reports warm idle
// pods immediately available for dispatch. Reading PoolScheduler.Snapshot()
// is sub-µs (atomic + channel-len reads).
type IdleReadyScorer struct{}

func (IdleReadyScorer) Name() string                    { return "IdleReady" }
func (IdleReadyScorer) Score(c *CandidateContext) int64 { return int64(c.Snap.IdleReady) }

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
// This is the plugin that addresses the user-visible scenario where
// one Pool sits at its MaxReplicas with 0 idle while another with the
// same priority can still scale: previously the router would tie-break
// arbitrarily; now the headroom contribution pushes the growable Pool
// above the maxed-out one.
type HeadroomScorer struct{}

func (HeadroomScorer) Name() string { return "Headroom" }
func (HeadroomScorer) Score(c *CandidateContext) int64 {
	cap, have := c.effectiveMax()
	if !have {
		return 0
	}
	hr := max(cap-c.DesiredReplicas, 0)
	return int64(hr)
}

// SaturationCooldownScorer applies a heavy negative score when the
// candidate's owning Pool autoscaler recently failed to scale up (the
// cluster told us "no capacity right now"). Replaces the previous
// hard fresh/stale tier split with a soft penalty: a Pool that's
// saturated for the next 60s gets ranked far below fresh peers, but
// when EVERY candidate is saturated it still gets routing love based
// on its other factors instead of all of them dropping to 0.
type SaturationCooldownScorer struct{}

func (SaturationCooldownScorer) Name() string { return "SaturationCooldown" }
func (SaturationCooldownScorer) Score(c *CandidateContext) int64 {
	if c.SaturatedUntil == nil || !c.SaturatedUntil.After(c.Now) {
		return 0
	}
	return saturationPenalty
}
