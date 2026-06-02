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

// scaleDownSessionTTL bounds how long a session entry survives without a
// committed decrement. It measures *inactivity*: every step refreshes the
// clock, so a long multi-step drain (e.g. 49 → 0 at one step per
// stabilization window) is not evicted mid-flight. The TTL only fires when
// no decrement lands for this long, which means the in-process gate would
// otherwise pin a session open forever after dropped events or a partial
// reconcile. Mirrors the safety-valve role of expectationsTTL on the Pod
// path.
const scaleDownSessionTTL = 10 * time.Minute

// scaleDownSession is the in-process record of one Pool's ongoing
// scale-down. It exists to make two decisions immune to informer-cache
// lag, which the persisted Pool.Status.AutoScaling.LastScaleDownTime
// cannot guarantee because the status patch is not read back before the
// next watch-triggered reconcile:
//
//   - The stabilization window between two consecutive decrements
//     (lastDecisionAt), so two near-simultaneous reconciles on the same
//     stale Snapshot cannot both decrement and both emit an event.
//   - Whether a scale-down lifecycle is currently open (active), so the
//     decision logic can emit one Started / Completed pair per session
//     instead of one event per replica removed.
type scaleDownSession struct {
	// lastDecisionAt is the time of the last committed decrement. The
	// stabilization gate compares Snapshot.Now against this in addition
	// to the persisted status timestamp.
	lastDecisionAt time.Time
	// active reports whether a Started event has fired without a matching
	// Completed/Aborted yet.
	active bool
	// startReplicas is Pool.Spec.Replicas captured when the session began
	// — the "from" reported in the Completed event.
	startReplicas int32
	// startAt is the wall time the session began, used to report duration.
	startAt time.Time
	// lastStuckAt rate-limits AutoscalerScaleDownStuck warnings.
	lastStuckAt time.Time
	// updatedAt drives the inactivity TTL.
	updatedAt time.Time
}

// ScaleDownTracker holds the per-Pool scale-down sessions. A single
// instance is shared between the Loader (which reads a View into each
// Snapshot) and the reconciler (which Applies transitions after a
// successful Commit). All methods are safe for concurrent use; the same
// Pool key is never reconciled concurrently, so the mutex only guards
// cross-Pool map access.
type ScaleDownTracker struct {
	mu    sync.Mutex
	items map[types.NamespacedName]*scaleDownSession
}

// NewScaleDownTracker returns an initialized ScaleDownTracker.
func NewScaleDownTracker() *ScaleDownTracker {
	return &ScaleDownTracker{items: make(map[types.NamespacedName]*scaleDownSession)}
}

// ScaleDownSessionView is the immutable projection of a session handed to
// the decision logic via the Snapshot. The zero value means "no session"
// (no entry, or expired): Active is false and every timestamp is zero.
type ScaleDownSessionView struct {
	LastDecisionAt time.Time
	StartAt        time.Time
	LastStuckAt    time.Time
	Active         bool
	StartReplicas  int32
}

// View returns the current session projection for key. An entry older than
// scaleDownSessionTTL (no decrement observed for that long) is treated as
// stale, deleted, and reported as the zero value.
func (t *ScaleDownTracker) View(key types.NamespacedName, now time.Time) ScaleDownSessionView {
	if t == nil {
		return ScaleDownSessionView{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.items[key]
	if !ok {
		return ScaleDownSessionView{}
	}
	if now.Sub(s.updatedAt) >= scaleDownSessionTTL {
		delete(t.items, key)
		return ScaleDownSessionView{}
	}
	return ScaleDownSessionView{
		LastDecisionAt: s.lastDecisionAt,
		StartAt:        s.startAt,
		LastStuckAt:    s.lastStuckAt,
		Active:         s.active,
		StartReplicas:  s.startReplicas,
	}
}

// ScaleDownTransitionKind enumerates the session state changes the
// decision logic can request. The reconciler applies the recorded
// transition to the tracker only after the cycle's writes commit, keeping
// the in-process gate from ever running ahead of a persisted decrement.
type ScaleDownTransitionKind int

const (
	// ScaleDownNoTransition is the zero value: the cycle touched no
	// session state.
	ScaleDownNoTransition ScaleDownTransitionKind = iota
	// ScaleDownStarted opens a new session on the first decrement.
	ScaleDownStarted
	// ScaleDownStepped records a subsequent decrement within an open
	// session (no event emitted).
	ScaleDownStepped
	// ScaleDownCompleted closes a session that ended naturally (nothing
	// left to remove).
	ScaleDownCompleted
	// ScaleDownAborted closes a session cut short by scale-up or reactive
	// demand (no Completed event).
	ScaleDownAborted
	// ScaleDownStuckMarked records that a Stuck warning fired this cycle;
	// the session stays open.
	ScaleDownStuckMarked
)

// ScaleDownTransition is the session change a Decide cycle wants applied
// after Commit. StartReplicas is meaningful only for ScaleDownStarted.
type ScaleDownTransition struct {
	Kind          ScaleDownTransitionKind
	StartReplicas int32
}

// Apply mutates the session for key according to tr. It is called from the
// reconciler after a successful Commit, using the same Snapshot.Now that
// the cycle's gate read so lastDecisionAt lines up exactly with the next
// reconcile's comparison base.
func (t *ScaleDownTracker) Apply(key types.NamespacedName, tr ScaleDownTransition, now time.Time) {
	if t == nil || tr.Kind == ScaleDownNoTransition {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch tr.Kind {
	case ScaleDownStarted:
		t.items[key] = &scaleDownSession{
			lastDecisionAt: now,
			active:         true,
			startReplicas:  tr.StartReplicas,
			startAt:        now,
			updatedAt:      now,
		}
	case ScaleDownStepped:
		s := t.getOrCreate(key)
		s.lastDecisionAt = now
		s.updatedAt = now
	case ScaleDownCompleted, ScaleDownAborted:
		delete(t.items, key)
	case ScaleDownStuckMarked:
		s := t.getOrCreate(key)
		s.lastStuckAt = now
		s.updatedAt = now
	}
}

// DeleteSession drops the entry for key. Called when the Pool is deleted
// so the map does not grow unboundedly.
func (t *ScaleDownTracker) DeleteSession(key types.NamespacedName) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.items, key)
}

// getOrCreate returns the existing entry or inserts a zero-value one. Must
// be called with t.mu held.
func (t *ScaleDownTracker) getOrCreate(key types.NamespacedName) *scaleDownSession {
	if s, ok := t.items[key]; ok {
		return s
	}
	s := &scaleDownSession{}
	t.items[key] = s
	return s
}
