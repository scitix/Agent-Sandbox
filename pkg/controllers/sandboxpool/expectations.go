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

package sandboxpool

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// expectationsTTL is the maximum age of an expectation before it is treated as
// satisfied. This safety valve prevents a permanently-stuck expectation (due to
// dropped pod events or controller restart) from blocking scaling forever.
// Matches the upstream Kubernetes ReplicaSet controller default.
const expectationsTTL = 5 * time.Minute

// poolExpectation holds the count of pending pod creates and deletes for one
// SandboxPool. Counters are decremented as pod events are observed via the
// custom Watch handler, and are clamped to zero (never negative).
type poolExpectation struct {
	adds      int64
	dels      int64
	timestamp time.Time
}

// PoolExpectations tracks in-flight pod creation and deletion counts per
// SandboxPool to prevent the informer-cache lag from causing oscillation.
//
// When a scale-up creates N pods, the reconciler records N pending creations
// before issuing r.Create calls. Subsequent Reconcile calls skip the scaling
// decision until all N Pod Add events have been observed (decrementing the
// counter to zero), or the 5-minute TTL expires as a safety valve.
//
// All methods are safe for concurrent use.
type PoolExpectations struct {
	mu    sync.Mutex
	items map[types.NamespacedName]*poolExpectation
}

// NewPoolExpectations returns an initialized PoolExpectations.
func NewPoolExpectations() *PoolExpectations {
	return &PoolExpectations{
		items: make(map[types.NamespacedName]*poolExpectation),
	}
}

// ExpectCreations records n pending pod creations for the given pool. The
// timestamp is refreshed so the TTL clock starts from this scale decision.
// Any previous pending-creation count is replaced; pending deletions are
// left unchanged.
func (e *PoolExpectations) ExpectCreations(key types.NamespacedName, n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	exp := e.getOrCreate(key)
	exp.adds = int64(n)
	exp.timestamp = time.Now()
}

// ExpectDeletions records n pending pod deletions for the given pool.
// Any previous pending-deletion count is replaced; pending creations are
// left unchanged.
func (e *PoolExpectations) ExpectDeletions(key types.NamespacedName, n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	exp := e.getOrCreate(key)
	exp.dels = int64(n)
	exp.timestamp = time.Now()
}

// CreationObserved decrements the pending-creation counter by 1 (clamped to
// zero). Called from the Pod Add event handler when a new pod belonging to
// the pool is observed in the informer.
func (e *PoolExpectations) CreationObserved(key types.NamespacedName) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if exp, ok := e.items[key]; ok && exp.adds > 0 {
		exp.adds--
	}
}

// DeletionObserved decrements the pending-deletion counter by 1 (clamped to
// zero). Called from the Pod Delete event handler.
func (e *PoolExpectations) DeletionObserved(key types.NamespacedName) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if exp, ok := e.items[key]; ok && exp.dels > 0 {
		exp.dels--
	}
}

// Satisfied reports whether all pending operations for the pool have been
// observed. Returns true when:
//   - no entry exists for the key (first reconcile, or after controller restart)
//   - both counters are zero
//   - the entry is older than expectationsTTL (safety valve)
//
// A stale (TTL-expired) entry is deleted as a side-effect.
func (e *PoolExpectations) Satisfied(key types.NamespacedName) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	exp, ok := e.items[key]
	if !ok {
		return true
	}
	if time.Since(exp.timestamp) >= expectationsTTL {
		delete(e.items, key)
		return true
	}
	return exp.adds == 0 && exp.dels == 0
}

// DeleteExpectations removes the entry for the given pool. Called when the
// pool is deleted so the map does not grow unboundedly.
func (e *PoolExpectations) DeleteExpectations(key types.NamespacedName) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.items, key)
}

// getOrCreate returns the existing entry or inserts a new zero-value one.
// Must be called with e.mu held.
func (e *PoolExpectations) getOrCreate(key types.NamespacedName) *poolExpectation {
	if exp, ok := e.items[key]; ok {
		return exp
	}
	exp := &poolExpectation{}
	e.items[key] = exp
	return exp
}
