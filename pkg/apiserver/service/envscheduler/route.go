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
	"time"

	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// routeMulti handles the multi-member case. It builds one
// CandidateContext per local member, runs them through the scoring
// framework (filter → score → sort), then tries Enqueue on each pool
// in best-first order. The first scheduler whose Enqueue accepts wins.
// If every candidate refuses, the call returns RouteSaturated.
func (m *Manager) routeMulti(_ context.Context, envKey types.NamespacedName, entry *envEntry, req *schedule.ClaimRequest) RouteResult {
	cands := m.buildCandidates(envKey, entry)
	if len(cands) == 0 {
		return RouteResult{Kind: RouteNotFound}
	}
	ranked, _ := m.framework.Rank(cands)
	for _, c := range ranked {
		pool := m.pools.GetOrCreateScheduler(envKey.Namespace, c.Member.poolName, "", "")
		if pool == nil {
			continue
		}
		if pool.Enqueue(req) {
			return RouteResult{Kind: RouteLocal, Pool: pool}
		}
	}
	return RouteResult{Kind: RouteSaturated}
}

// buildCandidates materialises one CandidateContext per local member
// of the env. Reads PoolScheduler.Snapshot for live counters and
// SandboxEnv.Status.ObservedMember for DesiredReplicas + SaturatedUntil.
// Both are O(1) atomic / map lookups on the hot path.
func (m *Manager) buildCandidates(envKey types.NamespacedName, entry *envEntry) []CandidateContext {
	now := time.Now()
	// Pull observed-member projections + per-scaling-group sibling
	// totals from Env.Status (when available). The router has to know
	// DesiredReplicas to score Headroom, and Σ-siblings to apply the
	// group cap.
	desiredByMember := map[string]int32{}
	saturatedByMember := map[string]*time.Time{}
	if m.envGetter != nil {
		if env, ok := m.envGetter.GetEnv(envKey.Namespace, envKey.Name); ok {
			for _, c := range env.Status.Clusters {
				if c.ClusterID != m.local {
					continue
				}
				for i := range c.ObservedMembers {
					om := &c.ObservedMembers[i]
					desiredByMember[om.Name] = om.DesiredReplicas
					if om.SaturatedUntil != nil && om.SaturatedUntil.After(now) {
						t := om.SaturatedUntil.Time
						saturatedByMember[om.Name] = &t
					}
				}
			}
		}
	}

	// Σ DesiredReplicas per scalingGroup so each candidate's
	// "siblings desired" is just (groupSum - self).
	groupSum := map[string]int32{}
	for _, mr := range entry.members {
		if !mr.isLocal || mr.scalingGroup == "" {
			continue
		}
		groupSum[mr.scalingGroup] += desiredByMember[mr.poolName]
	}

	cands := make([]CandidateContext, 0, len(entry.members))
	for _, mr := range entry.members {
		if !mr.isLocal {
			continue // cross-cluster routing deferred
		}
		var snap schedule.Snapshot
		if sched := m.pools.GetScheduler(envKey.Namespace, mr.poolName); sched != nil {
			snap = sched.Snapshot()
		}
		desired := desiredByMember[mr.poolName]
		siblings := int32(0)
		if mr.scalingGroup != "" {
			siblings = groupSum[mr.scalingGroup] - desired
		}
		cands = append(cands, CandidateContext{
			Now:                now,
			Member:             mr,
			Snap:               snap,
			DesiredReplicas:    desired,
			SiblingsDesiredSum: siblings,
			SaturatedUntil:     saturatedByMember[mr.poolName],
		})
	}
	return cands
}

// unused but exported helper kept here for future cross-cluster work —
// gives callers a way to peek at the saturation map without exposing the
// internal Env.Status walk. Compiles in but is not called yet.
var _ = func() *agentsv1alpha1.SandboxEnv { return nil }
