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

	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
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
		framework: newDefaultFramework(),
	}
}

// WithFramework swaps the scoring framework. Returns the receiver for
// chaining. Intended for tests that want to assert against a custom
// plugin set without re-wiring the whole Manager.
func (m *Manager) WithFramework(f *Framework) *Manager {
	m.framework = f
	return m
}

// Resolve maps a parsed Sandbox.Create template to the routing target. See
// ResolveKind for the four outcomes. The result is computed under a single
// RLock + one map lookup; safe to call from the hot path.
//
// The caller passes the already-split reference: clusterID is the cluster
// prefix the user supplied verbatim (empty when the user gave a bare name —
// callers MUST NOT substitute a default, since an empty clusterID is what
// distinguishes a bare Env-name lookup from an explicit pool reference), and
// poolOrEnvName is the bare name with any image override already stripped —
// interpreted as a Pool name when clusterID is set, or an Env name when it is
// not.
//
// Resolve rules:
//   - clusterID == localID                 — ResolveLocalPool (bypass Env routing)
//   - clusterID set but not local          — ResolveCrossCluster
//   - bare name matching an Env            — ResolveEnv
//   - bare name with no Env                — ResolveNotFound (no Pool fallback)
func (m *Manager) Resolve(ns, clusterID, poolOrEnvName string) ResolveResult {
	switch {
	case clusterID != "" && clusterID == m.local:
		return ResolveResult{Kind: ResolveLocalPool, PoolName: poolOrEnvName}
	case clusterID != "":
		return ResolveResult{Kind: ResolveCrossCluster, ClusterID: clusterID, PoolName: poolOrEnvName}
	}
	if poolOrEnvName == "" {
		return ResolveResult{Kind: ResolveNotFound}
	}
	key := types.NamespacedName{Namespace: ns, Name: poolOrEnvName}
	m.mu.RLock()
	_, ok := m.envs[key]
	m.mu.RUnlock()
	if !ok {
		return ResolveResult{Kind: ResolveNotFound, PoolName: poolOrEnvName}
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
		pool := m.pools.GetOrCreateScheduler(envKey.Namespace, m0.poolName, "", "", envKey.Name)
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
	// Denormalise group MaxReplicas keyed by name so members can pick up
	// their owning group's ceiling without re-walking the autoscaling
	// spec on the request hot path.
	groupMax := map[string]*int32{}
	if env.Spec.Autoscaling != nil {
		for i := range env.Spec.Autoscaling.Groups {
			g := &env.Spec.Autoscaling.Groups[i]
			if g.MaxReplicas != nil {
				v := *g.MaxReplicas
				groupMax[g.Name] = &v
			}
		}
	}
	for _, c := range env.Spec.Clusters {
		isLocal := c.ClusterID == m.local
		for _, sm := range c.Members {
			var mMax *int32
			if sm.Config.MaxReplicas != nil {
				v := *sm.Config.MaxReplicas
				mMax = &v
			}
			entry.members = append(entry.members, memberRef{
				clusterID:         c.ClusterID,
				poolName:          sm.Name,
				isLocal:           isLocal,
				priority:          sm.Config.Priority,
				scaleUpPriority:   sm.Config.EffectiveScaleUpPriority(),
				scalingGroup:      sm.Config.ScalingGroup,
				memberMaxReplicas: mMax,
				groupMaxReplicas:  groupMax[sm.Config.ScalingGroup],
			})
		}
	}
	return entry
}

// SelectPool picks the best local member of envKey to receive an incoming
// Sandbox.Create request, returning its bare pool name (no cluster prefix).
//
// Selection runs the same filter + score framework used by routeMulti, so
// the API-time Create path sees the same ranking the dispatch path would
// apply. Returns "" when the env is unknown or has no eligible local
// members.
//
// Unlike Route, SelectPool does NOT Enqueue — the caller is responsible
// for that downstream (typically after fetching the chosen Pool, resolving
// container images, etc.).
func (m *Manager) SelectPool(envKey types.NamespacedName) string {
	m.mu.RLock()
	entry, ok := m.envs[envKey]
	m.mu.RUnlock()
	if !ok || len(entry.members) == 0 {
		return ""
	}
	// Single-member fast path: no scoring needed.
	if len(entry.members) == 1 && entry.members[0].isLocal {
		return entry.members[0].poolName
	}
	cands := m.buildCandidates(envKey, entry)
	if len(cands) == 0 {
		return ""
	}
	ranked, _ := m.framework.Rank(cands)
	if len(ranked) == 0 {
		return ""
	}
	return ranked[0].Member.poolName
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
