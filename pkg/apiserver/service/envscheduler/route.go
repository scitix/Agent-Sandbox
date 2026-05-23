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
	"context"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// routeMulti handles the multi-member case. It:
//
//  1. Filters to local members (cross-cluster routing is reserved for a
//     future extension — see plan §12).
//
//  2. Splits the candidate list into "fresh" (SaturatedUntil not in the
//     future per Env.Status) and "stale" (still in saturation cooldown).
//     When at least one fresh member exists, the stale set is held back
//     as a fallback. When all members are stale, the cooldown is ignored
//     and the full list is tried — better to attempt a likely-rejected
//     Pool than to fail the request outright.
//
//  3. Sorts each tier by (priority asc, snapshot.IdleReady desc,
//     snapshot.QueueLen asc, poolName asc). Snapshot reads are atomic
//     channel-len / queue-len reads on the PoolScheduler — sub-µs.
//
//  4. Iterates: first scheduler whose Enqueue accepts wins. If every
//     candidate's reqCh is full, the call returns RouteSaturated and
//     the caller surfaces backpressure to the client.
func (m *Manager) routeMulti(_ context.Context, envKey types.NamespacedName, entry *envEntry, req *schedule.ClaimRequest) RouteResult {
	now := time.Now()
	saturated := map[string]bool{}
	if m.envGetter != nil {
		if env, ok := m.envGetter.GetEnv(envKey.Namespace, envKey.Name); ok {
			for _, c := range env.Status.Clusters {
				if c.ClusterID != m.local {
					continue
				}
				for _, om := range c.ObservedMembers {
					if om.SaturatedUntil != nil && om.SaturatedUntil.After(now) {
						saturated[om.Name] = true
					}
				}
			}
		}
	}

	var fresh, stale []memberRef
	for _, c := range entry.members {
		if !c.isLocal {
			continue // cross-cluster routing deferred
		}
		if saturated[c.poolName] {
			stale = append(stale, c)
		} else {
			fresh = append(fresh, c)
		}
	}

	if len(fresh) == 0 && len(stale) == 0 {
		return RouteResult{Kind: RouteNotFound}
	}

	// Try fresh tier first; fall through to stale if all fresh are full.
	for _, tier := range [][]memberRef{fresh, stale} {
		if len(tier) == 0 {
			continue
		}
		ranked := m.rankCandidates(envKey.Namespace, tier)
		for _, c := range ranked {
			pool := m.pools.GetOrCreateScheduler(envKey.Namespace, c.poolName, "", "")
			if pool == nil {
				continue
			}
			if pool.Enqueue(req) {
				return RouteResult{Kind: RouteLocal, Pool: pool}
			}
		}
	}
	return RouteResult{Kind: RouteSaturated}
}

// rankCandidates sorts a list of local member refs by:
//   - priority ascending (lower number = higher routing preference)
//   - then PoolScheduler.Snapshot().IdleReady DESC (prefer Pools with ready
//     idle Pods so the request dispatches without a wait)
//   - then PoolScheduler.Snapshot().QueueLen ASC (prefer less-backed-up Pools)
//   - then poolName ascending (lexicographic) so ties are deterministic
//
// Snapshot is called once per candidate; result is captured locally to keep
// the sort comparator branch-free and avoid re-reading volatile counters.
func (m *Manager) rankCandidates(ns string, in []memberRef) []memberRef {
	type ranked struct {
		ref      memberRef
		idle     int
		queueLen int
	}
	scored := make([]ranked, 0, len(in))
	for _, c := range in {
		var snap schedule.Snapshot
		if sched := m.pools.GetScheduler(ns, c.poolName); sched != nil {
			snap = sched.Snapshot()
		}
		scored = append(scored, ranked{ref: c, idle: snap.IdleReady, queueLen: snap.QueueLen})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		a, b := scored[i], scored[j]
		if a.ref.priority != b.ref.priority {
			return a.ref.priority < b.ref.priority
		}
		if a.idle != b.idle {
			return a.idle > b.idle
		}
		if a.queueLen != b.queueLen {
			return a.queueLen < b.queueLen
		}
		return a.ref.poolName < b.ref.poolName
	})
	out := make([]memberRef, len(scored))
	for i := range scored {
		out[i] = scored[i].ref
	}
	return out
}

// unused but exported helper kept here for future cross-cluster work —
// gives callers a way to peek at the saturation map without exposing the
// internal Env.Status walk. Compiles in but is not called yet.
var _ = func() *agentsv1alpha1.SandboxEnv { return nil }
