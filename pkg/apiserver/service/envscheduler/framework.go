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
	"sort"
	"time"

	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// CandidateContext is the per-Pool snapshot fed to every Filter / Score
// plugin during a single Rank() pass. Built once in route.go and treated
// as read-only by plugins so the framework can call any plugin set in
// any order without worrying about side effects.
type CandidateContext struct {
	// Now is the time the routing decision is being made. Pinned at
	// build time so every plugin sees a consistent clock for cooldown /
	// freshness comparisons.
	Now time.Time

	// Member carries the cached router-relevant projection of the
	// SandboxEnv member spec. Includes priority + scalingGroup +
	// per-member MaxReplicas + the owning group's MaxReplicas.
	Member memberRef

	// Snap is the live PoolScheduler counters at decision time. May be
	// the zero Snapshot when no scheduler has been started for the Pool
	// yet (cold path) — plugins must tolerate that.
	Snap schedule.Snapshot

	// DesiredReplicas mirrors SandboxPool.Spec.Replicas via
	// Env.Status.ObservedMember.DesiredReplicas. Used by Headroom and
	// MaxedOut plugins. 0 when the env status hasn't been populated yet.
	DesiredReplicas int32

	// SiblingsDesiredSum is Σ DesiredReplicas of every OTHER member in
	// the same scalingGroup. Precomputed once per Rank() so each
	// Headroom call is O(1).
	SiblingsDesiredSum int32

	// SaturatedUntil mirrors Env.Status.ObservedMember.SaturatedUntil
	// when set and in the future. nil otherwise. SaturationCooldown
	// plugin consults this to push hot Pools down the ranking.
	SaturatedUntil *time.Time
}

// FilterPlugin is a hard predicate: returning false excludes the
// candidate from the primary tier. When EVERY candidate is filtered
// out, Rank falls back to ranking the unfiltered original list — a
// loaded Pool is still preferable to a 503.
type FilterPlugin interface {
	Name() string
	Filter(c *CandidateContext) bool
}

// ScorePlugin is an additive contribution: higher = better. Plugins
// do not normalize; the framework multiplies by the registered weight
// and sums across plugins to produce the final ranking key.
type ScorePlugin interface {
	Name() string
	Score(c *CandidateContext) int64
}

type weightedScorer struct {
	plugin ScorePlugin
	weight int64
}

// Framework holds a registered filter + score plugin set. Construct
// once per Manager via newDefaultFramework(); plugins are stateless
// so the same instance is safe for concurrent use across requests.
type Framework struct {
	filters []FilterPlugin
	scorers []weightedScorer
}

// NewFramework returns an empty Framework. Use the chainable Filter /
// Score helpers (or newDefaultFramework()) to populate it.
func NewFramework() *Framework { return &Framework{} }

// Filter appends a filter plugin to the framework's filter chain.
// Order is preserved; the first filter to return false wins.
func (f *Framework) Filter(p FilterPlugin) *Framework {
	f.filters = append(f.filters, p)
	return f
}

// Score appends a score plugin with the given weight. Weights are
// applied multiplicatively, so a plugin returning {0, 1, 2} with
// weight 100 contributes {0, 100, 200} to the final score.
func (f *Framework) Score(p ScorePlugin, weight int64) *Framework {
	f.scorers = append(f.scorers, weightedScorer{plugin: p, weight: weight})
	return f
}

// Rank applies the filter chain and then sorts the survivors by total
// score, descending. Ties are broken alphabetically by pool name so
// the result is deterministic.
//
// When every candidate is filtered out, the original list is ranked
// instead — the second return value is false in that case so callers
// can surface "we routed to a saturated Pool" in telemetry.
func (f *Framework) Rank(cands []CandidateContext) (ranked []CandidateContext, hadFresh bool) {
	if len(cands) == 0 {
		return nil, false
	}

	fresh := make([]CandidateContext, 0, len(cands))
	for i := range cands {
		if f.accept(&cands[i]) {
			fresh = append(fresh, cands[i])
		}
	}
	tier := fresh
	hadFresh = len(fresh) > 0
	if !hadFresh {
		// Fallback tier: route to the best of the rejected set rather
		// than failing the request. The caller can observe hadFresh=false
		// for telemetry / backpressure if needed.
		tier = append([]CandidateContext(nil), cands...)
	}
	return f.sortByScore(tier), hadFresh
}

// accept returns true iff every filter accepts the candidate.
func (f *Framework) accept(c *CandidateContext) bool {
	for _, p := range f.filters {
		if !p.Filter(c) {
			return false
		}
	}
	return true
}

// sortByScore returns a new slice ranked best-first using the
// registered scorer set + name tie-break.
func (f *Framework) sortByScore(in []CandidateContext) []CandidateContext {
	type ranked struct {
		c     CandidateContext
		score int64
	}
	scored := make([]ranked, len(in))
	for i := range in {
		var total int64
		for _, s := range f.scorers {
			total += s.plugin.Score(&in[i]) * s.weight
		}
		scored[i] = ranked{c: in[i], score: total}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].c.Member.poolName < scored[j].c.Member.poolName
	})
	out := make([]CandidateContext, len(scored))
	for i := range scored {
		out[i] = scored[i].c
	}
	return out
}

// effectiveMax returns the tightest MaxReplicas cap that applies to
// this candidate: the lesser of memberMaxReplicas and (groupMax minus
// other-siblings' DesiredReplicas). Returns (max, true) when at least
// one cap is configured; (0, false) when neither is set.
//
// Lives on CandidateContext rather than as a plugin helper so the
// MaxedOut filter and the Headroom scorer share one implementation.
func (c *CandidateContext) effectiveMax() (int32, bool) {
	var have bool
	var cap int32
	if c.Member.memberMaxReplicas != nil {
		cap = *c.Member.memberMaxReplicas
		have = true
	}
	if c.Member.groupMaxReplicas != nil {
		// Group headroom for THIS pool = groupMax - other siblings' desired.
		groupHeadroom := max(*c.Member.groupMaxReplicas-c.SiblingsDesiredSum, 0)
		if !have || groupHeadroom < cap {
			cap = groupHeadroom
			have = true
		}
	}
	return cap, have
}
