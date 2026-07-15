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
	"sort"
	"strings"
	"sync"
	"time"
)

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
	// Capacity is how many more sandboxes this member could admit; -1 means
	// unknown / unbounded.
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

// BestForeignMember returns the cluster ID and member pool of the member in
// another cluster with the most idle capacity for the Env and requested
// scaling group. ok is false when no foreign member has a fresh record with
// idle > 0. Ties break on (clusterID, memberPool) for determinism.
func (r *Registry) BestForeignMember(namespace, env, group string) (clusterID, memberPool string, idle int32, ok bool) {
	now := r.now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.items {
		if c.ClusterID == r.localClusterID || c.Namespace != namespace || c.EnvName != env {
			continue
		}
		if !matchesGroup(c, group) || !r.fresh(c, now) || c.Idle <= 0 {
			continue
		}
		better := c.Idle > idle ||
			(c.Idle == idle && (!ok ||
				c.ClusterID < clusterID ||
				(c.ClusterID == clusterID && c.MemberPool < memberPool)))
		if better {
			clusterID, memberPool, idle, ok = c.ClusterID, c.MemberPool, c.Idle, true
		}
	}
	return clusterID, memberPool, idle, ok
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
