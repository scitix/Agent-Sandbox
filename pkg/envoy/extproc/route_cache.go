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

package extproc

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"
)

// RouteEntry is one cached sandboxID -> (ns, pod_name) mapping. It carries
// neither Phase nor PodIP: those are read live from the Pod informer cache at
// request time so the entry never goes stale across Pod lifecycle transitions
// (Starting -> Running -> Stopping).
type RouteEntry struct {
	Namespace string
	PodName   string
	ExpiresAt time.Time
}

// RouteCache is a lock-free (on the read path) cache of sandboxID → RouteEntry.
// It is written by the Controller via gRPC PushRoute and read by the Envoy
// ExtProc router on every proxied request. Writes are rare (once per sandbox
// Create) and reads are hot (every HTTP request through the gateway), so the
// cache uses a copy-on-write map snapshot:
//
//   - Get: single atomic Load of a *map pointer, then a map lookup.
//   - Put/Delete: acquire the writer mutex, clone the map, mutate the clone,
//     atomic Store the new pointer.
//
// Lazy expiry on Get returns (_, false) for stale entries without mutating the
// map. A background GC sweeper compacts the map periodically so its size
// remains bounded when many sandboxes are created and released.
//
// The cache is a latency optimization, NOT a source of truth. The Informer
// fallback in K8sSandboxRouter always reconciles correctness.
type RouteCache struct {
	snapshot atomic.Pointer[map[string]RouteEntry]
	mu       sync.Mutex // serialises writers only
	ttl      time.Duration
}

// NewRouteCache returns a RouteCache with the given per-entry TTL. A TTL of
// 1 minute is appropriate for the push-on-Create use case: the Informer
// normally catches up well within that window and takes over.
func NewRouteCache(ttl time.Duration) *RouteCache {
	c := &RouteCache{ttl: ttl}
	empty := map[string]RouteEntry{}
	c.snapshot.Store(&empty)
	return c
}

// Get returns the entry for sandboxID and whether it is present and fresh.
// Expired entries are treated as misses (no map mutation happens on the read
// path — the sweeper reclaims them asynchronously).
func (c *RouteCache) Get(sandboxID string) (RouteEntry, bool) {
	if sandboxID == "" {
		return RouteEntry{}, false
	}
	m := *c.snapshot.Load()
	e, ok := m[sandboxID]
	if !ok {
		return RouteEntry{}, false
	}
	if time.Now().After(e.ExpiresAt) {
		return RouteEntry{}, false
	}
	return e, true
}

// Put inserts or overwrites the entry for sandboxID. ExpiresAt is set to
// time.Now().Add(ttl), so repeated Puts refresh the TTL. The caller should
// supply PodIP/Phase/Namespace/PodName; ExpiresAt in the argument is ignored.
func (c *RouteCache) Put(sandboxID string, e RouteEntry) {
	if sandboxID == "" {
		return
	}
	e.ExpiresAt = time.Now().Add(c.ttl)

	c.mu.Lock()
	defer c.mu.Unlock()
	cur := *c.snapshot.Load()
	next := make(map[string]RouteEntry, len(cur)+1)
	maps.Copy(next, cur)
	next[sandboxID] = e
	c.snapshot.Store(&next)
}

// Delete removes the entry for sandboxID. No-op if absent.
func (c *RouteCache) Delete(sandboxID string) {
	if sandboxID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := *c.snapshot.Load()
	if _, ok := cur[sandboxID]; !ok {
		return
	}
	next := make(map[string]RouteEntry, len(cur))
	for k, v := range cur {
		if k == sandboxID {
			continue
		}
		next[k] = v
	}
	c.snapshot.Store(&next)
}

// Len returns the current entry count. Useful for metrics and tests.
func (c *RouteCache) Len() int {
	return len(*c.snapshot.Load())
}

// StartGC launches a background goroutine that periodically drops expired
// entries. It is safe to call multiple times; each call starts a new goroutine
// that lives until ctx is cancelled. A single call per cache is the norm.
func (c *RouteCache) StartGC(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sweep()
			}
		}
	}()
}

// sweep drops expired entries with a single COW swap. Cheap when nothing has
// expired — returns without touching the snapshot.
func (c *RouteCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := *c.snapshot.Load()
	now := time.Now()
	expired := 0
	for _, e := range cur {
		if now.After(e.ExpiresAt) {
			expired++
		}
	}
	if expired == 0 {
		return
	}
	next := make(map[string]RouteEntry, len(cur)-expired)
	for k, v := range cur {
		if now.After(v.ExpiresAt) {
			continue
		}
		next[k] = v
	}
	c.snapshot.Store(&next)
	klog.V(4).InfoS("RouteCache sweep", "dropped", expired, "remaining", len(next))
}
