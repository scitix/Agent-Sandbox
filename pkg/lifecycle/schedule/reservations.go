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

package schedule

import (
	"sync"
	"time"
)

// reservations is a TTL-bounded set of pod names representing pods that the
// scheduler has handed to a CAS goroutine (or just successfully claimed) and
// therefore must not be paired with another request again until the informer
// cache has observed the phase transition.
//
// The TTL is the primary defence against informer cache staleness: once a CAS
// succeeds, the watch event may take 50-100ms (P99 sub-second) to propagate
// back into the List cache. During that window ListIdlePodsForPool will still
// report the pod as Idle; reservations prevents us from re-selecting it.
//
// Concurrency: all methods are safe for concurrent use. The map is intentionally
// small (bounded by pool idle-pod count within the TTL window) so a single
// mutex is cheaper than sync.Map + its iteration quirks.
type reservations struct {
	mu  sync.Mutex
	m   map[string]time.Time // key: pod.Name, value: expiresAt
	ttl time.Duration
	now func() time.Time // injectable for tests
}

// newReservations returns a reservations set with the given TTL. A nil clock
// defaults to time.Now.
func newReservations(ttl time.Duration, now func() time.Time) *reservations {
	if now == nil {
		now = time.Now
	}
	return &reservations{
		m:   make(map[string]time.Time),
		ttl: ttl,
		now: now,
	}
}

// reserve records name with expiry = now() + ttl. If name is already present
// the expiry is refreshed (i.e. the clock is reset).
func (r *reservations) reserve(name string) {
	if name == "" {
		return
	}
	r.mu.Lock()
	r.m[name] = r.now().Add(r.ttl)
	r.mu.Unlock()
}

// release deletes name immediately, regardless of its expiry.
func (r *reservations) release(name string) {
	if name == "" {
		return
	}
	r.mu.Lock()
	delete(r.m, name)
	r.mu.Unlock()
}

// isReserved reports whether name has a non-expired reservation.
// Expired entries are treated as absent but are not removed here; sweep() is
// responsible for GC so that hot paths stay lock-short.
func (r *reservations) isReserved(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.m[name]
	if !ok {
		return false
	}
	return r.now().Before(exp)
}

// sweep removes expired entries and returns the number removed.
func (r *reservations) sweep() int {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for name, exp := range r.m {
		if !now.Before(exp) {
			delete(r.m, name)
			n++
		}
	}
	return n
}

// size returns the number of entries (including expired ones that sweep has
// not yet removed). Intended for metrics / diagnostics.
func (r *reservations) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}
