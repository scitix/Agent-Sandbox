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

// Package envscheduler implements the in-process Env-level request router.
//
// Two concerns live here:
//
//   - Resolve: turn a Sandbox.Create `template` string into the routing
//     target — Env, local Pool (explicit clusterID==local), remote Pool
//     (clusterID != local), or NotFound.
//
//   - Route: when the target is an Env, pick the right member SandboxPool
//     and hand the ClaimRequest to its PoolScheduler.Enqueue. The router
//     is stateless and lock-light: a single RWMutex guards the env table;
//     the request hot path is one map lookup + one PoolScheduler.Enqueue.
//
// The Env autoscaler (`pkg/controllers/sandboxenv/`) is responsible for
// deciding when and how much to scale; this package never patches
// Pool.Spec.Replicas.
package envscheduler

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/schedule"
)

// SchedulerLookup is the contract this package needs from the apiserver's
// per-pool scheduler registry (sandbox_service). It's an interface to keep
// the dependency direction one-way (apiserver/service → envscheduler) and
// to make unit testing trivial via a fake.
type SchedulerLookup interface {
	// GetScheduler returns the running PoolScheduler for the named pool or
	// nil when none has been created yet. Read-only — used for Snapshot.
	GetScheduler(ns, poolName string) *schedule.PoolScheduler
	// GetOrCreateScheduler returns the running PoolScheduler for the named
	// pool, starting one on first use. Used on the dispatch path.
	GetOrCreateScheduler(ns, poolName, team, user, env string) *schedule.PoolScheduler
}

// EnvGetter is the read-only handle the router uses to look at the current
// SandboxEnv.Status (specifically ObservedMember.SaturatedUntil) when
// ranking candidate members. Backed by a controller-runtime informer cache
// in production; tests inject a fake map.
type EnvGetter interface {
	// GetEnv returns the cached SandboxEnv, or (nil, false) when absent.
	// The returned pointer MUST NOT be mutated — callers DeepCopy if
	// they need to retain or modify it.
	GetEnv(ns, name string) (*agentsv1alpha1.SandboxEnv, bool)
}

// FederationView is the read-only slice of the cross-cluster capacity
// registry the router needs to decide whether a create should be served
// locally or forwarded to a same-named Env in another cluster. Backed by
// federation.Registry in production; nil disables cross-cluster placement.
type FederationView interface {
	// LocalIdle is the local cluster's idle capacity for the Env across
	// members matching the scaling group (empty group = all members).
	LocalIdle(namespace, env, group string) int32
	// LocalCanGrow reports whether any local member in the group could scale
	// up (autoscaling on and not at ceiling), letting the router keep a create
	// local instead of spilling to a foreign cluster that can also only scale.
	LocalCanGrow(namespace, env, group string) bool
	// BestForeignMember is the cluster ID and member pool of the best
	// schedulable member in another cluster for the Env and scaling group —
	// idle Pods preferred over scale-up room; ok is false when none can serve.
	// The returned idle is the chosen member's idle count (0 when chosen via
	// scale-up headroom).
	BestForeignMember(namespace, env, group string) (clusterID, memberPool string, idle int32, ok bool)
}

// Manager owns the env → route-table mapping. There is one Manager per
// apiserver process. Methods are safe for concurrent use; the request hot
// path takes only the read lock.
type Manager struct {
	mu        sync.RWMutex
	envs      map[types.NamespacedName]*envEntry
	pools     SchedulerLookup
	envGetter EnvGetter
	local     string
	framework *Framework
	fed       FederationView
}

// envEntry is the cached router-relevant projection of a SandboxEnv spec.
// Recomputed on every OnEnvUpsert from the freshest SandboxEnv we've seen
// (informer event or reconciler-driven fallback).
type envEntry struct {
	key     types.NamespacedName
	members []memberRef
}

type memberRef struct {
	clusterID       string
	poolName        string
	isLocal         bool
	priority        int32
	scaleUpPriority int32
	scalingGroup    string

	// memberMaxReplicas is the per-member ceiling (Config.MaxReplicas).
	// nil = unbounded at the member level — only the group ceiling
	// applies. Used by Headroom and MaxedOut plugins to compute the
	// effective per-Pool growth window without re-walking the spec.
	memberMaxReplicas *int32

	// groupMaxReplicas is the autoscaling group ceiling for the group
	// named by scalingGroup. Denormalised at OnEnvUpsert time so the
	// scheduling hot path never has to scan env.Spec.Autoscaling.
	// nil = autoscaling not configured for this group / member has no
	// group.
	groupMaxReplicas *int32
}

// ResolveKind classifies how a `template` string resolved.
type ResolveKind int

const (
	// ResolveEnv means the template was a bare name matching a known Env.
	// Caller proceeds to Manager.Route.
	ResolveEnv ResolveKind = iota
	// ResolveLocalPool means the template explicitly named a Pool in the
	// local cluster via "<localID>::poolName". Caller goes straight to
	// PoolScheduler — Env layer is bypassed.
	ResolveLocalPool
	// ResolveCrossCluster means the template explicitly named a Pool in
	// a remote cluster via "<remoteID>::poolName". Caller hands off to
	// the cross-cluster forwarder.
	ResolveCrossCluster
	// ResolveNotFound means the bare-name template didn't match any Env.
	// MVP semantics — there is no Pool fallback because Phase 1 adoption
	// guarantees every SandboxPool has a same-named SandboxEnv.
	ResolveNotFound
)

// ResolveResult carries the outcome of Manager.Resolve.
type ResolveResult struct {
	Kind      ResolveKind
	EnvKey    types.NamespacedName // populated when Kind == ResolveEnv
	PoolName  string               // populated when Kind in {ResolveLocalPool, ResolveCrossCluster, ResolveNotFound}
	ClusterID string               // populated when Kind == ResolveCrossCluster
}

// RouteKind classifies how a Route call dispatched.
type RouteKind int

const (
	// RouteLocal means the ClaimRequest was enqueued into a local PoolScheduler.
	RouteLocal RouteKind = iota
	// RouteSaturated means every candidate member's Enqueue refused (reqCh
	// full). Caller surfaces TooManyRequests / backpressure to the client.
	RouteSaturated
	// RouteNotFound means the Env exists but has no eligible members.
	// Typical reason: adopter race — Env created, member list still empty.
	RouteNotFound
)

// RouteResult carries the outcome of Manager.Route.
type RouteResult struct {
	Kind RouteKind
	Pool *schedule.PoolScheduler // populated only on RouteLocal
}
