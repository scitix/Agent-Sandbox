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

// Package armnotify carries the arming verdict for a claimed sandbox from the
// post-start hook runner to the create request waiting on it.
//
// Both live in the same process — cmd/sandbox runs the controller manager and
// the API server together — so this is a channel handoff, not a distributed
// problem. The alternative was to record the verdict on the Pod and have the
// create path watch for it, which spends one API-server write per sandbox,
// fanned out to every Pod informer in the cluster, to move a fact between two
// goroutines that already share an address space.
//
// The registry is deliberately not a cache: an entry exists only while someone
// is waiting for it, and a verdict with no waiter is dropped. Nothing here is
// state, so nothing here needs cleaning up on restart.
package armnotify

import (
	"errors"
	"sync"
)

// errDisplaced fails a waiter that a later Wait for the same sandbox replaced.
var errDisplaced = errors.New("superseded by another claim of the same sandbox")

// Registry routes arming verdicts to the goroutine waiting for them.
type Registry struct {
	mu      sync.Mutex
	waiters map[string]chan error
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{waiters: make(map[string]chan error)}
}

// Wait registers interest in a sandbox's arming verdict and returns the channel
// it will arrive on, plus a cancel function the caller must defer.
//
// Register before the work can start, not after: a verdict that arrives while
// no waiter is registered is dropped, and the caller would then wait for one
// that has already been delivered.
//
// A second Wait for the same sandbox replaces the first, and the displaced
// waiter is failed rather than left hanging: one request claims one sandbox, so
// this cannot happen for a live claim, but a waiter that can never be notified
// would block for its whole deadline if it ever did.
func (r *Registry) Wait(sandboxID string) (<-chan error, func()) {
	if r == nil || sandboxID == "" {
		return nil, func() {}
	}
	// Buffered so Notify never blocks on a caller that has already given up.
	ch := make(chan error, 1)

	r.mu.Lock()
	if prev, ok := r.waiters[sandboxID]; ok {
		select {
		case prev <- errDisplaced:
		default:
		}
	}
	r.waiters[sandboxID] = ch
	r.mu.Unlock()

	return ch, func() {
		r.mu.Lock()
		if cur, ok := r.waiters[sandboxID]; ok && cur == ch {
			delete(r.waiters, sandboxID)
		}
		r.mu.Unlock()
	}
}

// Notify delivers a verdict. err nil means the sandbox is armed.
//
// A verdict for a sandbox nobody is waiting on is dropped, which is the normal
// case for the re-arm that follows a configuration change: that work has no
// requester.
func (r *Registry) Notify(sandboxID string, err error) {
	if r == nil || sandboxID == "" {
		return
	}
	r.mu.Lock()
	ch, ok := r.waiters[sandboxID]
	if ok {
		delete(r.waiters, sandboxID)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

// Waiting reports how many sandboxes currently have someone waiting on them.
// Used by tests and by the metric that would reveal a leak.
func (r *Registry) Waiting() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.waiters)
}
