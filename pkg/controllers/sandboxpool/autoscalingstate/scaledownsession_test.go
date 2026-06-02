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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

var testKey = types.NamespacedName{Namespace: "ns", Name: "pool"}

func TestScaleDownTracker_EmptyView(t *testing.T) {
	tr := NewScaleDownTracker()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	v := tr.View(testKey, now)
	if v.Active || !v.LastDecisionAt.IsZero() {
		t.Errorf("empty view should be zero, got %+v", v)
	}
}

func TestScaleDownTracker_StartedThenView(t *testing.T) {
	tr := NewScaleDownTracker()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 5}, now)
	v := tr.View(testKey, now)
	if !v.Active || v.StartReplicas != 5 || !v.LastDecisionAt.Equal(now) || !v.StartAt.Equal(now) {
		t.Errorf("started view = %+v", v)
	}
}

func TestScaleDownTracker_SteppedAdvancesAndRefreshes(t *testing.T) {
	tr := NewScaleDownTracker()
	t0 := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 5}, t0)

	t1 := t0.Add(70 * time.Second)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStepped}, t1)
	v := tr.View(testKey, t1)
	if !v.Active || !v.LastDecisionAt.Equal(t1) {
		t.Errorf("stepped should advance lastDecisionAt to t1, got %+v", v)
	}
	if v.StartReplicas != 5 || !v.StartAt.Equal(t0) {
		t.Errorf("stepped must preserve session origin, got %+v", v)
	}
	// updatedAt was refreshed to t1, so a view well past t1 but within the
	// TTL of t1 still survives — exercised by the TTL test below.
}

func TestScaleDownTracker_CompletedAndAbortedDeleteEntry(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	for _, kind := range []ScaleDownTransitionKind{ScaleDownCompleted, ScaleDownAborted} {
		tr := NewScaleDownTracker()
		tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 3}, now)
		tr.Apply(testKey, ScaleDownTransition{Kind: kind}, now)
		if v := tr.View(testKey, now); v.Active {
			t.Errorf("kind %v should clear the session, got %+v", kind, v)
		}
	}
}

func TestScaleDownTracker_StuckMarkedSetsTimestamp(t *testing.T) {
	tr := NewScaleDownTracker()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 5}, now)
	stuckAt := now.Add(time.Minute)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStuckMarked}, stuckAt)
	v := tr.View(testKey, stuckAt)
	if !v.Active || !v.LastStuckAt.Equal(stuckAt) {
		t.Errorf("stuck mark should set LastStuckAt, got %+v", v)
	}
}

func TestScaleDownTracker_TTLExpiry(t *testing.T) {
	tr := NewScaleDownTracker()
	t0 := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 5}, t0)

	// Just inside the TTL: still present.
	if v := tr.View(testKey, t0.Add(scaleDownSessionTTL-time.Second)); !v.Active {
		t.Error("session should survive within TTL")
	}
	// Past the inactivity TTL: evicted and reported zero.
	if v := tr.View(testKey, t0.Add(scaleDownSessionTTL+time.Second)); v.Active {
		t.Error("session should be evicted after the inactivity TTL")
	}
}

func TestScaleDownTracker_DeleteSession(t *testing.T) {
	tr := NewScaleDownTracker()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted, StartReplicas: 5}, now)
	tr.DeleteSession(testKey)
	if v := tr.View(testKey, now); v.Active {
		t.Error("DeleteSession should drop the entry")
	}
}

// NoTransition is a no-op and nil-safe.
func TestScaleDownTracker_NilAndNoTransition(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	var nilTr *ScaleDownTracker
	nilTr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownStarted}, now) // must not panic
	if v := nilTr.View(testKey, now); v.Active {
		t.Error("nil tracker view should be zero")
	}

	tr := NewScaleDownTracker()
	tr.Apply(testKey, ScaleDownTransition{Kind: ScaleDownNoTransition}, now)
	if v := tr.View(testKey, now); v.Active {
		t.Error("NoTransition must not create an entry")
	}
}
