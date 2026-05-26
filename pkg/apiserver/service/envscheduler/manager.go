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
	"github.com/scitix/agent-sandbox/pkg/utils/cluster"
)

// New constructs a Manager. localClusterID identifies the cluster this
// apiserver runs in; the empty string disables cross-cluster classification
// (every explicit "X::pool" reference is treated as remote).
//
// pools is required — Route depends on it. envGetter may be nil; in that
// case the router cannot consult SandboxEnv.Status (e.g. SaturatedUntil) and
// will fall back to the spec-only ordering.
func New(localClusterID string, pools SchedulerLookup, envGetter EnvGetter) *Manager {
	return &Manager{
		envs:      make(map[types.NamespacedName]*envEntry),
		pools:     pools,
		envGetter: envGetter,
		local:     localClusterID,
	}
}

// Resolve maps a Sandbox.Create `template` (formerly known as PoolName) to
// the routing target. See ResolveKind for the four outcomes. The result is
// computed under a single RLock + one map lookup; safe to call from the
// hot path.
//
// Resolve rules:
//   - "<localID>::poolName" — ResolveLocalPool (bypass Env routing)
//   - "<remoteID>::poolName" — ResolveCrossCluster
//   - "bareName" matching an Env — ResolveEnv
//   - "bareName" with no Env — ResolveNotFound (MVP: no Pool fallback)
func (m *Manager) Resolve(ns, raw string) ResolveResult {
	parsed := cluster.ParsePoolRef(raw)
	switch {
	case parsed.ClusterID != "" && parsed.ClusterID == m.local:
		return ResolveResult{Kind: ResolveLocalPool, PoolName: parsed.PoolName}
	case parsed.ClusterID != "":
		return ResolveResult{Kind: ResolveCrossCluster, ClusterID: parsed.ClusterID, PoolName: parsed.PoolName}
	}
	if parsed.PoolName == "" {
		return ResolveResult{Kind: ResolveNotFound}
	}
	key := types.NamespacedName{Namespace: ns, Name: parsed.PoolName}
	m.mu.RLock()
	_, ok := m.envs[key]
	m.mu.RUnlock()
	if !ok {
		return ResolveResult{Kind: ResolveNotFound, PoolName: parsed.PoolName}
	}
	return ResolveResult{Kind: ResolveEnv, EnvKey: key}
}

// Route dispatches req to one of envKey's member PoolSchedulers.
//
// Single-member fast path (the common Phase 1 shape): no Snapshot read, no
// sort, no Env.Status check — just look up the lone local member's
// PoolScheduler and Enqueue.
//
// Multi-member ranking is in route.go (Step 6).
func (m *Manager) Route(ctx context.Context, envKey types.NamespacedName, req *schedule.ClaimRequest) RouteResult {
	m.mu.RLock()
	entry, ok := m.envs[envKey]
	m.mu.RUnlock()
	if !ok || len(entry.members) == 0 {
		return RouteResult{Kind: RouteNotFound}
	}

	// Single local member — fast path. Multi-member ranking lives in routeMulti.
	if len(entry.members) == 1 && entry.members[0].isLocal {
		m0 := entry.members[0]
		pool := m.pools.GetOrCreateScheduler(envKey.Namespace, m0.poolName, "", "")
		if pool == nil {
			return RouteResult{Kind: RouteNotFound}
		}
		if pool.Enqueue(req) {
			return RouteResult{Kind: RouteLocal, Pool: pool}
		}
		return RouteResult{Kind: RouteSaturated}
	}

	return m.routeMulti(ctx, envKey, entry, req)
}

// OnEnvUpsert refreshes the cached entry for env. Called from the SandboxEnv
// informer's Add/Update handler in production and from the Reconciler at
// the end of Reconcile as a belt-and-braces fallback. Idempotent.
//
// env may be nil — treated as a no-op.
func (m *Manager) OnEnvUpsert(env *agentsv1alpha1.SandboxEnv) {
	if env == nil {
		return
	}
	entry := m.buildEntry(env)
	key := types.NamespacedName{Namespace: env.Namespace, Name: env.Name}
	m.mu.Lock()
	m.envs[key] = entry
	m.mu.Unlock()
}

// OnEnvDelete drops the cached entry. Safe to call for unknown keys.
func (m *Manager) OnEnvDelete(key types.NamespacedName) {
	m.mu.Lock()
	delete(m.envs, key)
	m.mu.Unlock()
}

// buildEntry projects a SandboxEnv into the router's cached shape.
// Currently only the local cluster's segment is materialised; cross-cluster
// member promotion is a future extension point (see plan §12).
func (m *Manager) buildEntry(env *agentsv1alpha1.SandboxEnv) *envEntry {
	key := types.NamespacedName{Namespace: env.Namespace, Name: env.Name}
	entry := &envEntry{key: key}
	for _, c := range env.Spec.Clusters {
		isLocal := c.ClusterID == m.local
		for _, sm := range c.Members {
			entry.members = append(entry.members, memberRef{
				clusterID:       c.ClusterID,
				poolName:        sm.Name,
				isLocal:         isLocal,
				priority:        sm.Config.Priority,
				scaleUpPriority: sm.Config.EffectiveScaleUpPriority(),
				scalingGroup:    sm.Config.ScalingGroup,
			})
		}
	}
	return entry
}

// SelectPool picks the best local member of envKey to receive an incoming
// Sandbox.Create request, returning its bare pool name (no cluster prefix).
//
// Selection rules match routeMulti's ranking: priority ascending, then
// IdleReady descending, then QueueLen ascending, with deterministic name
// tie-break. Saturated members are deferred to a fallback tier; when at
// least one fresh member exists the fallback tier is not consulted.
//
// Returns "" when the env is unknown or has no eligible local members.
//
// Unlike Route, SelectPool does NOT Enqueue — the caller is responsible
// for that downstream (typically after fetching the chosen Pool, resolving
// container images, etc.). This split lets the same selection logic run
// at Create's entry so the rest of the function sees a concrete pool
// name in input.PoolName.
func (m *Manager) SelectPool(envKey types.NamespacedName) string {
	m.mu.RLock()
	entry, ok := m.envs[envKey]
	m.mu.RUnlock()
	if !ok || len(entry.members) == 0 {
		return ""
	}

	// Single-member fast path.
	if len(entry.members) == 1 && entry.members[0].isLocal {
		return entry.members[0].poolName
	}

	saturated := map[string]bool{}
	if m.envGetter != nil {
		if env, ok := m.envGetter.GetEnv(envKey.Namespace, envKey.Name); ok {
			now := time.Now()
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
			continue
		}
		if saturated[c.poolName] {
			stale = append(stale, c)
		} else {
			fresh = append(fresh, c)
		}
	}
	for _, tier := range [][]memberRef{fresh, stale} {
		if len(tier) == 0 {
			continue
		}
		ranked := m.rankCandidates(envKey.Namespace, tier)
		return ranked[0].poolName
	}
	return ""
}

// Snapshot returns a list of the env keys currently known to the Manager,
// useful for diagnostics. Order is unspecified.
func (m *Manager) Snapshot() []types.NamespacedName {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]types.NamespacedName, 0, len(m.envs))
	for k := range m.envs {
		out = append(out, k)
	}
	return out
}
