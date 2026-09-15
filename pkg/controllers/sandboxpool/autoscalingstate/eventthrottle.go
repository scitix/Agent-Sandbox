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

package autoscalingstate

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// repeatEventInterval is how long an identical autoscaler event stays
// suppressed for one Pool. Sized against the scale-up cooldown (30s by
// default): a Pool re-deciding the same growth every cooldown window would
// otherwise post two API-server writes a minute, per Pool, indefinitely.
//
// Five minutes keeps a genuinely stuck Pool visible in `kubectl describe`
// without it dominating the namespace's event budget.
const repeatEventInterval = 5 * time.Minute

// throttleEntryTTL evicts Pools that stopped emitting, so the map does not
// grow with every Pool that ever existed. Generously longer than
// repeatEventInterval: evicting early only costs one extra event.
const throttleEntryTTL = 30 * time.Minute

// EventThrottle suppresses repeats of an identical autoscaler event for the
// same Pool.
//
// The autoscaler is edge-triggered in intent but level-triggered in fact: it
// re-evaluates every reconcile and, whenever its decision cannot take effect,
// reaches the same conclusion again on the next cooldown window. Each of those
// conclusions used to post an Event, so a Pool that could not grow — no
// cluster headroom, a misrouted write, a hard ceiling — produced an unbounded
// stream of identical records, every one a write against the API server.
//
// Deduplication is on the rendered message, not just the reason: "increased
// replicas from 1 to 6" repeating is noise, while "from 6 to 9" is news and
// must always get through. A changed message resets the window, so a Pool that
// is actually progressing reports every step at full fidelity.
//
// The throttle deliberately does not touch status writes or the decision
// itself — only whether the Event is posted. Suppressing an event never
// suppresses an action.
//
// All methods are safe for concurrent use.
type EventThrottle struct {
	mu    sync.Mutex
	items map[types.NamespacedName]*throttleEntry
}

type throttleEntry struct {
	// lastMessage is the most recently emitted message for this Pool, keyed
	// per reason so a scale-up and a scale-down event do not evict each other.
	lastMessage map[string]string
	// lastEmitAt is when that message was last allowed through, per reason.
	lastEmitAt map[string]time.Time
	// updatedAt drives the inactivity TTL.
	updatedAt time.Time
}

// NewEventThrottle returns an initialized EventThrottle.
func NewEventThrottle() *EventThrottle {
	return &EventThrottle{items: make(map[types.NamespacedName]*throttleEntry)}
}

// Allow reports whether the (reason, message) event for pool may be posted
// now, and records the decision when it returns true.
//
// It returns true when the message differs from the last one emitted under
// that reason, or when repeatEventInterval has elapsed since the last
// identical one. A nil throttle allows everything, so callers that have not
// wired one (tests, embedders) keep the previous behaviour.
func (t *EventThrottle) Allow(pool types.NamespacedName, reason, message string, now time.Time) bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evictExpiredLocked(now)

	e, ok := t.items[pool]
	if !ok {
		e = &throttleEntry{
			lastMessage: map[string]string{},
			lastEmitAt:  map[string]time.Time{},
		}
		t.items[pool] = e
	}
	e.updatedAt = now

	if prev, seen := e.lastMessage[reason]; seen && prev == message {
		if at := e.lastEmitAt[reason]; !at.IsZero() && now.Sub(at) < repeatEventInterval {
			return false
		}
	}
	e.lastMessage[reason] = message
	e.lastEmitAt[reason] = now
	return true
}

// Forget drops a Pool's throttle state. Called when a Pool is deleted so a
// recreated Pool of the same name starts with a clean slate.
func (t *EventThrottle) Forget(pool types.NamespacedName) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.items, pool)
}

// evictExpiredLocked drops entries untouched for longer than
// throttleEntryTTL. Caller must hold the mutex.
func (t *EventThrottle) evictExpiredLocked(now time.Time) {
	for k, v := range t.items {
		if now.Sub(v.updatedAt) > throttleEntryTTL {
			delete(t.items, k)
		}
	}
}
