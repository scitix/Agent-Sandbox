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

// Package federation holds the Worker-side soft-state view of cross-cluster
// SandboxEnv capacity. Same-named SandboxEnvs living in different clusters are
// independent objects; the federation shares only their per-member runtime
// capacity so a Worker's Env router can pin a create to an exact member pool
// in another cluster (forwarding "<cluster>::<pool>"). Env spec is never
// synced — management stays local to each cluster.
//
// The registry is pure soft state: every record has an observation time and is
// aged out on a TTL, so a cluster that stops reporting simply disappears. No
// explicit delete is ever needed.
package federation

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// headroomUnbounded is the Capacity value meaning "autoscaling enabled with no
// finite ceiling" — the member can grow without a computable limit.
const headroomUnbounded int32 = -1

// Capacity is one cluster's runtime capacity for a single member pool of a
// same-named SandboxEnv. The (ClusterID, Namespace, EnvName, MemberPool) tuple
// is the record's identity; ScalingGroup lets the router honour a requested
// group.
type Capacity struct {
	ClusterID    string
	Namespace    string
	EnvName      string
	MemberPool   string
	ScalingGroup string
	Idle         int32
	Running      int32
	Pending      int32
	Desired      int32
	// AutoscalingEnabled reports whether this member's scaling group has the
	// autoscaler on in its owning cluster. Because each cluster scales
	// independently, this is a per-member, per-cluster fact; it gates whether
	// Capacity below is meaningful.
	AutoscalingEnabled bool
	// Capacity is the member's remaining scale-up headroom on its owning
	// cluster, meaningful only when AutoscalingEnabled: -1 = enabled but
	// unbounded (no finite ceiling), 0 = enabled but at ceiling (cannot grow
	// now), >0 = enabled with this much room. When AutoscalingEnabled is
	// false it is 0 and ignored.
	Capacity int32
	// SaturatedFor is the remaining saturation window; zero means not saturated.
	SaturatedFor time.Duration
	// ObservedAt is when the sample was taken at its source cluster. The
	// consumer derives it from the wire's relative age so TTL expiry is
	// measured from the real observation instant, immune to clock skew.
	ObservedAt time.Time
}

func key(clusterID, namespace, env, memberPool string) string {
	return strings.Join([]string{clusterID, namespace, env, memberPool}, "\x00")
}

// matchesGroup reports whether a record belongs to the requested scaling group.
// An empty request matches every member (no group constraint).
func matchesGroup(c Capacity, group string) bool {
	return group == "" || c.ScalingGroup == group
}

// CanGrow reports whether the member could accept a new sandbox by scaling
// up: autoscaling is on and it is not already at its ceiling (Capacity != 0).
// Capacity == -1 (unbounded) and Capacity > 0 both count as room.
func (c Capacity) CanGrow() bool {
	return c.AutoscalingEnabled && c.Capacity != 0
}

// Schedulable reports whether a create routed here would be served without an
// open-ended wait: either an idle Pod is ready now, or the member can scale up
// to make one. A member with no idle and no scale-up room is excluded — routing
// there would park the request until it times out.
func (c Capacity) Schedulable() bool {
	return c.Idle > 0 || c.CanGrow()
}

// Registry is the Worker's in-memory federation store. It is safe for
// concurrent use. The clock is injectable so tests exercise TTL expiry without
// sleeping.
type Registry struct {
	localClusterID string
	ttl            time.Duration

	mu    sync.RWMutex
	items map[string]Capacity
	now   func() time.Time
}

// NewRegistry builds a Registry that ignores its own cluster's records when
// answering foreign-capacity queries and ages every record out after ttl.
func NewRegistry(localClusterID string, ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Registry{
		localClusterID: localClusterID,
		ttl:            ttl,
		items:          make(map[string]Capacity),
		now:            time.Now,
	}
}

// SetClock overrides the time source. Test-only.
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// Upsert records a batch of capacity samples, replacing any previous value for
// the same member.
func (r *Registry) Upsert(items []Capacity) {
	if len(items) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range items {
		r.items[key(it.ClusterID, it.Namespace, it.EnvName, it.MemberPool)] = it
	}
}

// fresh reports whether c is within the TTL relative to now.
func (r *Registry) fresh(c Capacity, now time.Time) bool {
	return now.Sub(c.ObservedAt) <= r.ttl
}

// LocalIdle sums this cluster's idle capacity for the Env across members that
// match the requested scaling group (empty group = all members).
func (r *Registry) LocalIdle(namespace, env, group string) int32 {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := int32(0)
	for _, c := range r.items {
		if c.ClusterID != r.localClusterID || c.Namespace != namespace || c.EnvName != env {
			continue
		}
		if !matchesGroup(c, group) || !r.fresh(c, now) {
			continue
		}
		total += c.Idle
	}
	return total
}

// BestForeignMember returns the cluster ID and member pool of the best
// schedulable member in another cluster for the Env and requested scaling
// group. A member is schedulable when it can serve without an open-ended wait:
// an idle Pod ready now, or autoscaling room to make one (see Schedulable).
// Members that are neither (no idle, cannot grow) are excluded — routing there
// would park the request until it times out.
//
// Ranking prefers immediacy: any member with idle > 0 outranks any pure
// scale-up member (an idle Pod serves instantly; a scale-up incurs a cold
// start). Within the idle tier, more idle wins; within the scale-up tier, more
// headroom wins (unbounded ranks highest). Ties break on (clusterID,
// memberPool) for determinism.
//
// The returned idle is the chosen member's idle count — 0 when it was chosen
// via scale-up headroom. ok is false when no foreign member is schedulable.
func (r *Registry) BestForeignMember(namespace, env, group string) (clusterID, memberPool string, idle int32, ok bool) {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best Capacity
	for _, c := range r.items {
		if c.ClusterID == r.localClusterID || c.Namespace != namespace || c.EnvName != env {
			continue
		}
		if !matchesGroup(c, group) || !r.fresh(c, now) || !c.Schedulable() {
			continue
		}
		if !ok || foreignBetter(c, best) {
			best, ok = c, true
		}
	}
	if !ok {
		return "", "", 0, false
	}
	return best.ClusterID, best.MemberPool, best.Idle, true
}

// foreignBetter reports whether routing candidate a is preferable to b. Both
// are assumed schedulable. See BestForeignMember for the ranking rationale.
func foreignBetter(a, b Capacity) bool {
	at, bt := scheduleTier(a), scheduleTier(b)
	if at != bt {
		return at < bt // lower tier (0 = immediate) is better
	}
	if at == tierImmediate {
		if a.Idle != b.Idle {
			return a.Idle > b.Idle
		}
	} else {
		if ah, bh := headroomRank(a.Capacity), headroomRank(b.Capacity); ah != bh {
			return ah > bh
		}
	}
	if a.ClusterID != b.ClusterID {
		return a.ClusterID < b.ClusterID
	}
	return a.MemberPool < b.MemberPool
}

const (
	tierImmediate = 0 // has an idle Pod ready now
	tierScaleUp   = 1 // no idle, but can autoscale
)

func scheduleTier(c Capacity) int {
	if c.Idle > 0 {
		return tierImmediate
	}
	return tierScaleUp
}

// headroomRank orders scale-up headroom with unbounded (-1) ranking highest.
func headroomRank(h int32) int64 {
	if h == headroomUnbounded {
		return math.MaxInt64
	}
	return int64(h)
}

// LocalCanGrow reports whether any local member of the Env in the requested
// group could scale up (autoscaling on and not at ceiling). Lets the router
// keep a create local — letting the local autoscaler react — instead of
// spilling to a foreign cluster that can also only scale up.
func (r *Registry) LocalCanGrow(namespace, env, group string) bool {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.items {
		if c.ClusterID != r.localClusterID || c.Namespace != namespace || c.EnvName != env {
			continue
		}
		if !matchesGroup(c, group) || !r.fresh(c, now) {
			continue
		}
		if c.CanGrow() {
			return true
		}
	}
	return false
}

// ForeignMembers returns every non-expired member record for the Env that
// belongs to a cluster other than the local one, sorted by (cluster, pool).
// Used to mirror the cross-cluster view into SandboxEnv.status.clusters so it
// is visible via kubectl.
func (r *Registry) ForeignMembers(namespace, env string) []Capacity {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Capacity
	for _, c := range r.items {
		if c.ClusterID == r.localClusterID || c.Namespace != namespace || c.EnvName != env {
			continue
		}
		if r.fresh(c, now) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterID != out[j].ClusterID {
			return out[i].ClusterID < out[j].ClusterID
		}
		return out[i].MemberPool < out[j].MemberPool
	})
	return out
}

// Snapshot returns every non-expired record (local and foreign) for
// observability and debugging.
func (r *Registry) Snapshot() []Capacity {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Capacity, 0, len(r.items))
	for _, c := range r.items {
		if r.fresh(c, now) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterID != out[j].ClusterID {
			return out[i].ClusterID < out[j].ClusterID
		}
		if out[i].EnvName != out[j].EnvName {
			return out[i].EnvName < out[j].EnvName
		}
		return out[i].MemberPool < out[j].MemberPool
	})
	return out
}
