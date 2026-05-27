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

import (
	"testing"
	"time"

	"k8s.io/utils/ptr"

	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// candidate is a builder shorthand. Tests instantiate this and call
// .build() so each test reads as a wide-table row instead of pasting
// the same boilerplate field-by-field.
type candidate struct {
	name      string
	priority  int32
	memberMax *int32
	groupMax  *int32
	desired   int32
	siblings  int32
	idle      int
	queue     int
	saturated *time.Time
}

func (b candidate) build() CandidateContext {
	return CandidateContext{
		Now: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
		Member: memberRef{
			poolName:          b.name,
			isLocal:           true,
			priority:          b.priority,
			memberMaxReplicas: b.memberMax,
			groupMaxReplicas:  b.groupMax,
			scalingGroup:      "g1",
		},
		Snap:               schedule.Snapshot{IdleReady: b.idle, QueueLen: b.queue},
		DesiredReplicas:    b.desired,
		SiblingsDesiredSum: b.siblings,
		SaturatedUntil:     b.saturated,
	}
}

// ---------- Effective max ----------

func TestEffectiveMax_MemberCapOnly(t *testing.T) {
	c := candidate{memberMax: ptr.To(int32(5))}.build()
	cap, ok := c.effectiveMax()
	if !ok || cap != 5 {
		t.Errorf("cap = %d (ok=%v); want 5, true", cap, ok)
	}
}

func TestEffectiveMax_GroupCapMinusSiblings(t *testing.T) {
	c := candidate{groupMax: ptr.To(int32(10)), siblings: 3}.build()
	cap, ok := c.effectiveMax()
	if !ok || cap != 7 {
		t.Errorf("cap = %d (ok=%v); want 7, true", cap, ok)
	}
}

func TestEffectiveMax_GroupOverbookedClampsToZero(t *testing.T) {
	// Sum of siblings already exceeds group max — headroom must clamp
	// to 0 rather than going negative.
	c := candidate{groupMax: ptr.To(int32(5)), siblings: 8}.build()
	cap, ok := c.effectiveMax()
	if !ok || cap != 0 {
		t.Errorf("cap = %d (ok=%v); want 0, true", cap, ok)
	}
}

func TestEffectiveMax_TightestWins(t *testing.T) {
	// member cap 4, group headroom 7 → effective = 4.
	c := candidate{memberMax: ptr.To(int32(4)), groupMax: ptr.To(int32(10)), siblings: 3}.build()
	cap, _ := c.effectiveMax()
	if cap != 4 {
		t.Errorf("cap = %d, want 4 (member tighter than group)", cap)
	}
	// member cap 9, group headroom 7 → effective = 7.
	c2 := candidate{memberMax: ptr.To(int32(9)), groupMax: ptr.To(int32(10)), siblings: 3}.build()
	cap2, _ := c2.effectiveMax()
	if cap2 != 7 {
		t.Errorf("cap = %d, want 7 (group tighter than member)", cap2)
	}
}

func TestEffectiveMax_None(t *testing.T) {
	c := candidate{}.build()
	if _, ok := c.effectiveMax(); ok {
		t.Error("expected effectiveMax to report no cap configured")
	}
}

// ---------- MaxedOutFilter ----------

func TestMaxedOutFilter_KeepsWhenNoCap(t *testing.T) {
	c := candidate{desired: 100}.build()
	if !(MaxedOutFilter{}).Filter(&c) {
		t.Error("no cap configured must always accept")
	}
}

func TestMaxedOutFilter_KeepsBelowCap(t *testing.T) {
	c := candidate{memberMax: ptr.To(int32(5)), desired: 3}.build()
	if !(MaxedOutFilter{}).Filter(&c) {
		t.Error("desired < cap must accept")
	}
}

func TestMaxedOutFilter_RejectsAtCapWithNoIdle(t *testing.T) {
	c := candidate{memberMax: ptr.To(int32(5)), desired: 5, idle: 0}.build()
	if (MaxedOutFilter{}).Filter(&c) {
		t.Error("at cap with no idle must reject")
	}
}

func TestMaxedOutFilter_KeepsAtCapWhenIdleReady(t *testing.T) {
	// Pool sits at cap but has an idle Pod waiting — dispatching to it
	// doesn't require growth. Must accept.
	c := candidate{memberMax: ptr.To(int32(5)), desired: 5, idle: 1}.build()
	if !(MaxedOutFilter{}).Filter(&c) {
		t.Error("at cap with idle ready must accept (no growth needed)")
	}
}

// ---------- PriorityScorer ----------

func TestPriorityScorer_LowerNumberHigherScore(t *testing.T) {
	low := candidate{priority: 0}.build()
	high := candidate{priority: 100}.build()
	if (PriorityScorer{}).Score(&low) <= (PriorityScorer{}).Score(&high) {
		t.Errorf("priority=0 should outrank priority=100")
	}
}

// ---------- IdleReadyScorer ----------

func TestIdleReadyScorer_MoreIdleHigherScore(t *testing.T) {
	a := candidate{idle: 3}.build()
	b := candidate{idle: 0}.build()
	if (IdleReadyScorer{}).Score(&a) <= (IdleReadyScorer{}).Score(&b) {
		t.Errorf("idle=3 should outrank idle=0")
	}
}

// ---------- QueueLengthScorer ----------

func TestQueueLengthScorer_ShorterQueueHigherScore(t *testing.T) {
	short := candidate{queue: 1}.build()
	long := candidate{queue: 10}.build()
	// Score itself is positive (returns queue), but the registered
	// weight is negative — so it's the weighted contribution that
	// negates. The raw score is monotonic in queue; here we just sanity-
	// check the sign of the unweighted output.
	if (QueueLengthScorer{}).Score(&short) >= (QueueLengthScorer{}).Score(&long) {
		t.Errorf("raw QueueLength score should grow with queue (raw); weight applies sign")
	}
}

// ---------- HeadroomScorer ----------

func TestHeadroomScorer_NoCapReturnsZero(t *testing.T) {
	c := candidate{desired: 3}.build()
	if got := (HeadroomScorer{}).Score(&c); got != 0 {
		t.Errorf("no cap → score 0, got %d", got)
	}
}

func TestHeadroomScorer_RewardsMoreRoom(t *testing.T) {
	roomy := candidate{memberMax: ptr.To(int32(10)), desired: 2}.build()   // hr 8
	cramped := candidate{memberMax: ptr.To(int32(10)), desired: 8}.build() // hr 2
	if (HeadroomScorer{}).Score(&roomy) <= (HeadroomScorer{}).Score(&cramped) {
		t.Errorf("more headroom should score higher")
	}
}

func TestHeadroomScorer_NegativeClampsToZero(t *testing.T) {
	// desired exceeded the cap somehow (e.g. user shrank max under
	// existing replicas). Headroom must report 0, not negative.
	c := candidate{memberMax: ptr.To(int32(5)), desired: 10}.build()
	if got := (HeadroomScorer{}).Score(&c); got != 0 {
		t.Errorf("over-cap headroom must clamp to 0, got %d", got)
	}
}

// ---------- SaturationCooldownScorer ----------

func TestSaturationCooldownScorer_NoCooldownZero(t *testing.T) {
	c := candidate{}.build()
	if (SaturationCooldownScorer{}).Score(&c) != 0 {
		t.Error("no cooldown → score 0")
	}
}

func TestSaturationCooldownScorer_PastCooldownIgnored(t *testing.T) {
	past := time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)
	c := candidate{saturated: &past}.build()
	if (SaturationCooldownScorer{}).Score(&c) != 0 {
		t.Error("expired cooldown must score 0")
	}
}

func TestSaturationCooldownScorer_FutureCooldownPenalises(t *testing.T) {
	future := time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC)
	c := candidate{saturated: &future}.build()
	if got := (SaturationCooldownScorer{}).Score(&c); got != saturationPenalty {
		t.Errorf("future cooldown score = %d, want %d", got, saturationPenalty)
	}
}
