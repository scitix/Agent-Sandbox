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
	"reflect"
	"testing"
	"time"

	"k8s.io/utils/ptr"
)

// rankNames is a readability shim — extract the ranked Pool names so
// tests assert ordering without re-typing the field path.
func rankNames(in []CandidateContext) []string {
	out := make([]string, len(in))
	for i, c := range in {
		out[i] = c.Member.poolName
	}
	return out
}

// alwaysFilter / neverFilter / fixedScorer are tiny stub plugins used
// to exercise the framework's filter + score pipeline without coupling
// to the production plugin set.
type neverFilter struct{}

func (neverFilter) Name() string                    { return "never" }
func (neverFilter) Filter(c *CandidateContext) bool { return false }

type fixedScorer struct {
	name  string
	score int64
}

func (f fixedScorer) Name() string                    { return f.name }
func (f fixedScorer) Score(c *CandidateContext) int64 { return f.score }

// nameScorer scores via a lookup so different candidates get different
// contributions without us building a closure plugin per case.
type nameScorer struct{ by map[string]int64 }

func (nameScorer) Name() string { return "byName" }
func (n nameScorer) Score(c *CandidateContext) int64 {
	return n.by[c.Member.poolName]
}

// ---------- Rank() basics ----------

func TestFramework_Rank_EmptyInput(t *testing.T) {
	f := newDefaultFramework()
	ranked, hadFresh := f.Rank(nil)
	if ranked != nil || hadFresh {
		t.Errorf("empty input must return (nil, false); got (%v, %v)", ranked, hadFresh)
	}
}

func TestFramework_Rank_SortsHighestScoreFirst(t *testing.T) {
	f := NewFramework().Score(nameScorer{by: map[string]int64{"a": 10, "b": 50, "c": 30}}, 1)
	ranked, hadFresh := f.Rank([]CandidateContext{
		candidate{name: "a"}.build(),
		candidate{name: "b"}.build(),
		candidate{name: "c"}.build(),
	})
	if !hadFresh {
		t.Error("no filters → all candidates are fresh")
	}
	if got := rankNames(ranked); !reflect.DeepEqual(got, []string{"b", "c", "a"}) {
		t.Errorf("ranked = %v, want [b c a]", got)
	}
}

func TestFramework_Rank_NameTieBreaker(t *testing.T) {
	f := NewFramework().Score(fixedScorer{name: "zero", score: 0}, 1)
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "zzz"}.build(),
		candidate{name: "aaa"}.build(),
		candidate{name: "mmm"}.build(),
	})
	if got := rankNames(ranked); !reflect.DeepEqual(got, []string{"aaa", "mmm", "zzz"}) {
		t.Errorf("ranked = %v, want alphabetic tie-break", got)
	}
}

func TestFramework_Rank_WeightMultiplies(t *testing.T) {
	// Score-A returns 1 for "a", -1 for "b"; weight 10 → ±10.
	// Score-B returns -2 for "a", 0 for "b"; weight 1 → -2/0.
	// Totals: a = 10 - 2 = 8; b = -10. a wins.
	f := NewFramework().
		Score(nameScorer{by: map[string]int64{"a": 1, "b": -1}}, 10).
		Score(nameScorer{by: map[string]int64{"a": -2, "b": 0}}, 1)
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "a"}.build(),
		candidate{name: "b"}.build(),
	})
	if got := rankNames(ranked); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("ranked = %v, want [a b]", got)
	}
}

func TestFramework_Rank_FilterDropsCandidates(t *testing.T) {
	// Filter drops "b" specifically; survivors are ranked alphabetically.
	bdropFilter := filterFn(func(c *CandidateContext) bool { return c.Member.poolName != "b" })
	f := NewFramework().Filter(bdropFilter)
	ranked, hadFresh := f.Rank([]CandidateContext{
		candidate{name: "a"}.build(),
		candidate{name: "b"}.build(),
		candidate{name: "c"}.build(),
	})
	if !hadFresh {
		t.Error("hadFresh should be true when at least one candidate survives")
	}
	if got := rankNames(ranked); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("ranked = %v, want [a c]", got)
	}
}

func TestFramework_Rank_AllFilteredOutFallsBack(t *testing.T) {
	// neverFilter rejects everything → framework still ranks the
	// original list (last-ditch dispatch) and reports hadFresh=false.
	f := NewFramework().
		Filter(neverFilter{}).
		Score(nameScorer{by: map[string]int64{"a": 1, "b": 2}}, 1)
	ranked, hadFresh := f.Rank([]CandidateContext{
		candidate{name: "a"}.build(),
		candidate{name: "b"}.build(),
	})
	if hadFresh {
		t.Error("hadFresh must be false when every candidate is filtered")
	}
	if got := rankNames(ranked); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("ranked = %v, want [b a] (fallback ranking)", got)
	}
}

// ---------- Default framework integration ----------

// TestDefaultFramework_HeadroomBeatsPriorityTie addresses the scenario
// the user called out: two same-priority Pools, neither has idle, but
// one is maxed out while the other has growth room. The fresh-headroom
// Pool must win.
func TestDefaultFramework_HeadroomBeatsPriorityTie(t *testing.T) {
	f := newDefaultFramework()
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "maxed", priority: 0, memberMax: ptr.To(int32(2)), desired: 2, idle: 0}.build(),
		candidate{name: "growable", priority: 0, memberMax: ptr.To(int32(10)), desired: 2, idle: 0}.build(),
	})
	// "maxed" hits MaxedOutFilter (desired==cap, idle==0) → filtered
	// out of the fresh tier. "growable" survives and ranks first.
	if got := rankNames(ranked); got[0] != "growable" {
		t.Errorf("ranked = %v, want growable first", got)
	}
}

// TestDefaultFramework_FreshBeatsSaturatedPriority confirms the
// saturation penalty outweighs a 100-point priority delta — a saturated
// priority-0 Pool yields to a fresh priority-100 sibling.
func TestDefaultFramework_FreshBeatsSaturatedPriority(t *testing.T) {
	future := time.Now().Add(time.Hour)
	f := newDefaultFramework()
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "saturated-pri0", priority: 0, saturated: &future}.build(),
		candidate{name: "fresh-pri100", priority: 100}.build(),
	})
	if got := rankNames(ranked); got[0] != "fresh-pri100" {
		t.Errorf("ranked = %v, want fresh-pri100 first", got)
	}
}

// TestDefaultFramework_AllSaturatedRanksByPriority: when every member
// is saturated the penalty cancels out in relative terms; priority
// then decides.
func TestDefaultFramework_AllSaturatedRanksByPriority(t *testing.T) {
	future := time.Now().Add(time.Hour)
	f := newDefaultFramework()
	ranked, hadFresh := f.Rank([]CandidateContext{
		candidate{name: "low-pri", priority: 100, saturated: &future}.build(),
		candidate{name: "high-pri", priority: 0, saturated: &future}.build(),
	})
	// SaturationCooldown is a Score plugin, not a Filter — both
	// candidates survive the filter chain. hadFresh stays true; the
	// penalty contributes equally and priority decides the order.
	if !hadFresh {
		t.Error("saturation is a scorer, not a filter — survivors should still count as fresh")
	}
	if got := rankNames(ranked); got[0] != "high-pri" {
		t.Errorf("ranked = %v, want high-pri first (priority 0 beats 100)", got)
	}
}

// TestDefaultFramework_AllMaxedOutFallsBack: when every candidate is
// rejected by MaxedOutFilter, hadFresh flips false and the framework
// still returns a ranking from the rejected set.
func TestDefaultFramework_AllMaxedOutFallsBack(t *testing.T) {
	f := newDefaultFramework()
	ranked, hadFresh := f.Rank([]CandidateContext{
		candidate{name: "a", priority: 100, memberMax: ptr.To(int32(2)), desired: 2, idle: 0}.build(),
		candidate{name: "b", priority: 0, memberMax: ptr.To(int32(2)), desired: 2, idle: 0}.build(),
	})
	if hadFresh {
		t.Error("hadFresh must be false when every candidate is filtered out")
	}
	if got := rankNames(ranked); got[0] != "b" {
		t.Errorf("ranked = %v, want b first (priority 0 beats 100 in fallback tier)", got)
	}
}

// TestDefaultFramework_IdleReadyOverridesQueue: two members at the
// same priority, one has idle ready, the other has shorter queue.
// IdleReady is weighted high enough to dominate QueueLength.
func TestDefaultFramework_IdleReadyOverridesQueue(t *testing.T) {
	f := newDefaultFramework()
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "idle-but-busy", priority: 0, idle: 1, queue: 5}.build(),
		candidate{name: "no-idle-clear", priority: 0, idle: 0, queue: 0}.build(),
	})
	if got := rankNames(ranked); got[0] != "idle-but-busy" {
		t.Errorf("ranked = %v, want idle-but-busy first", got)
	}
}

// TestDefaultFramework_GroupHeadroomShared confirms group-level cap is
// shared across siblings: a Pool whose siblings have already consumed
// most of the group cap should score lower headroom than a sibling
// with more local room.
func TestDefaultFramework_GroupHeadroomShared(t *testing.T) {
	groupMax := ptr.To(int32(10))
	f := newDefaultFramework()
	// "left": siblings=8, so group headroom = 2.
	// "right": siblings=2, so group headroom = 8.
	// Same priority, no idle, no saturation → headroom decides.
	ranked, _ := f.Rank([]CandidateContext{
		candidate{name: "left", priority: 0, groupMax: groupMax, siblings: 8, desired: 0}.build(),
		candidate{name: "right", priority: 0, groupMax: groupMax, siblings: 2, desired: 0}.build(),
	})
	if got := rankNames(ranked); got[0] != "right" {
		t.Errorf("ranked = %v, want right first (more headroom)", got)
	}
}

// ---------- filterFn helper ----------

type filterFn func(*CandidateContext) bool

func (filterFn) Name() string                       { return "fn" }
func (fn filterFn) Filter(c *CandidateContext) bool { return fn(c) }
