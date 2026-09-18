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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/activity"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// activityPod builds a Running pod carrying a sandbox, optionally with the
// lifecycle annotations the flusher reads.
func activityPod(name, sandboxID string, annotations map[string]string) *corev1.Pod {
	merged := map[string]string{}
	maps.Copy(merged, annotations)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Labels:      map[string]string{agentsv1alpha1.SandboxIDLabelKey: sandboxID, agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseRunning},
			Annotations: merged,
		},
		Status: corev1.PodStatus{PodIP: testPodIP},
	}
}

type flusherFixture struct {
	flusher *ActivityFlusher
	tracker *ActivityTracker
	k8s     client.Client
	clock   *fakeClock
	now     time.Time
}

// fakeClock is shared with the flusher so a test can move time forward.
type fakeClock struct{ t time.Time }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newFlusherFixture wires a tracker + fake client and pins the flusher's clock.
func newFlusherFixture(t *testing.T, cfg ActivityFlusherConfig, pods ...*corev1.Pod) *flusherFixture {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	for _, p := range pods {
		cb = cb.WithObjects(p)
	}
	k8s := cb.Build()

	tracker := NewActivityTracker()
	f := NewActivityFlusher(tracker, k8s, cfg)
	clock := &fakeClock{t: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	f.now = func() time.Time { return clock.t }

	return &flusherFixture{flusher: f, tracker: tracker, k8s: k8s, clock: clock, now: clock.t}
}

func (fx *flusherFixture) annotation(t *testing.T, sandboxID string) string { //nolint:unparam // the lookup is by id; today's fixtures happen to use one
	t.Helper()
	pod, err := indexer.GetPodBySandboxID(context.Background(), fx.k8s, sandboxID)
	if err != nil {
		t.Fatalf("lookup %s: %v", sandboxID, err)
	}
	return pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]
}

// ---------------------------------------------------------------------------
// shouldRefresh: the write rule
// ---------------------------------------------------------------------------

func TestShouldRefresh(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	const id = "sandbox-1"
	const timeout = 30 * time.Minute // above the crossover, so δ is the base
	delta := activity.RefreshIntervalFor(id, timeout)

	cases := []struct {
		name       string
		lastSeen   time.Time
		annotation time.Time
		timeout    time.Duration
		want       bool
	}{
		{
			name:       "no idle timeout → nothing will ever be reclaimed, so nothing to refresh",
			lastSeen:   now,
			annotation: now.Add(-time.Hour),
			timeout:    0,
			want:       false,
		},
		{
			name:       "never observed activity",
			lastSeen:   time.Time{},
			annotation: now.Add(-time.Hour),
			timeout:    timeout,
			want:       false,
		},
		{
			name:       "another replica already wrote something newer",
			lastSeen:   now.Add(-2 * time.Minute),
			annotation: now.Add(-time.Minute),
			timeout:    timeout,
			want:       false,
		},
		{
			name:       "annotation is still fresh — wait, this is what collapses N replicas to ~1 write",
			lastSeen:   now,
			annotation: now.Add(-delta / 2),
			timeout:    timeout,
			want:       false,
		},
		{
			name:       "annotation has just gone stale and we hold newer activity",
			lastSeen:   now,
			annotation: now.Add(-delta - time.Second),
			timeout:    timeout,
			want:       true,
		},
		{
			name:       "annotation missing entirely",
			lastSeen:   now,
			annotation: time.Time{},
			timeout:    timeout,
			want:       true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRefresh(now, id, tc.lastSeen, tc.annotation, tc.timeout); got != tc.want {
				t.Errorf("shouldRefresh() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A short idle timeout must shrink the interval, otherwise the annotation goes
// stale between refreshes and the controller releases a sandbox in use.
func TestShouldRefreshHonoursShortIdleTimeouts(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	const id = "sandbox-1"
	short := time.Minute // δ = 20s

	if got := shouldRefresh(now, id, now, now.Add(-30*time.Second), short); !got {
		t.Error("a 1m idle timeout with a 30s-stale annotation must refresh (δ = T/3 = 20s)")
	}
	if got := shouldRefresh(now, id, now, now.Add(-10*time.Second), short); got {
		t.Error("10s is inside the 20s interval; refreshing would be a wasted write")
	}
}

// ---------------------------------------------------------------------------
// flushOnce: end to end through the fake client
// ---------------------------------------------------------------------------

func TestFlusherAdvancesTheAnnotation(t *testing.T) {
	seen := time.Date(2026, 9, 17, 11, 59, 0, 0, time.UTC)
	pod := activityPod("pod-1", "sb1", map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
		agentsv1alpha1.SandboxLastActiveAnnotationKey:  seen.Add(-time.Hour).Format(time.RFC3339),
	})
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	fx.tracker.Touch("sb1")
	// Touch stamps its own clock; drive the value through the tracker's own
	// state so the test asserts on a known timestamp.
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = seen
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	if got, want := fx.annotation(t, "sb1"), seen.Format(time.RFC3339); got != want {
		t.Errorf("last-active = %q, want %q", got, want)
	}
}

func TestFlusherDoesNotRollBackANewerAnnotation(t *testing.T) {
	fresh := time.Date(2026, 9, 17, 11, 59, 30, 0, time.UTC)
	pod := activityPod("pod-1", "sb1", map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
		agentsv1alpha1.SandboxLastActiveAnnotationKey:  fresh.Format(time.RFC3339),
	})
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	// This replica saw the sandbox two minutes ago; another replica has since
	// written something newer. Writing ours would move the annotation backwards.
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fresh.Add(-2 * time.Minute)
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	if got, want := fx.annotation(t, "sb1"), fresh.Format(time.RFC3339); got != want {
		t.Errorf("last-active = %q, want the newer value %q to survive", got, want)
	}
}

func TestFlusherSkipsSandboxesWithoutATimeout(t *testing.T) {
	pod := activityPod("pod-1", "sb1", nil) // no idle-timeout annotation
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fx.now
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	if got := fx.annotation(t, "sb1"); got != "" {
		t.Errorf("wrote last-active=%q for a sandbox that can never be reclaimed by idleness", got)
	}
}

func TestFlusherSkipsPodsThatAreNotRunning(t *testing.T) {
	pod := activityPod("pod-1", "sb1", map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
	})
	// Stopping pods keep their sandbox-id label on purpose (so in-flight traffic
	// is not cut off), but the controller only ever considers Running ones.
	pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseStopping
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fx.now
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	if got := fx.annotation(t, "sb1"); got != "" {
		t.Errorf("wrote last-active=%q to a Stopping pod", got)
	}
}

func TestFlusherIgnoresSandboxesWithNoPod(t *testing.T) {
	fx := newFlusherFixture(t, ActivityFlusherConfig{})
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["gone"] = fx.now
	fx.tracker.mu.Unlock()

	// Must not panic or block: a released sandbox is the normal case here.
	fx.flusher.flushOnce(context.Background())
}

// ---------------------------------------------------------------------------
// Deferred writes: the queue and its bound
// ---------------------------------------------------------------------------

// When a rate limit is configured, work that does not fit stays queued and the
// age ratio reports how much of the controller's slack it is eating. Losing
// that signal would mean silently releasing sandboxes in use.
func TestFlusherQueuesWhenRateLimitedAndReportsAge(t *testing.T) {
	stale := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	pods := make([]*corev1.Pod, 0, 10)
	for i := range 10 {
		id := "sb" + string(rune('a'+i))
		pods = append(pods, activityPod("pod-"+id, id, map[string]string{
			agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
			agentsv1alpha1.SandboxLastActiveAnnotationKey:  stale.Format(time.RFC3339),
		}))
	}
	fx := newFlusherFixture(t, ActivityFlusherConfig{RateLimitPerSecond: 3}, pods...)
	fx.tracker.mu.Lock()
	for i := range 10 {
		fx.tracker.lastActive["sb"+string(rune('a'+i))] = fx.now.Add(-time.Minute)
	}
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	fx.flusher.mu.Lock()
	pending := len(fx.flusher.pending)
	fx.flusher.mu.Unlock()
	if pending != 7 {
		t.Errorf("pending = %d, want 7 (10 due, 3 allowed by the limiter)", pending)
	}

	// The next tick should not stall on the same tokens: a fresh second refills.
	fx.clock.advance(time.Second)
	fx.flusher.flushOnce(context.Background())
	fx.flusher.mu.Lock()
	pending = len(fx.flusher.pending)
	fx.flusher.mu.Unlock()
	if pending != 4 {
		t.Errorf("pending after the second tick = %d, want 4", pending)
	}
}

func TestFlusherDropsBeyondMaxPending(t *testing.T) {
	pods := make([]*corev1.Pod, 0, 5)
	for i := range 5 {
		id := "sb" + string(rune('a'+i))
		pods = append(pods, activityPod("pod-"+id, id, map[string]string{
			agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
			agentsv1alpha1.SandboxLastActiveAnnotationKey:  time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
		}))
	}
	fx := newFlusherFixture(t, ActivityFlusherConfig{RateLimitPerSecond: 1, MaxPending: 2}, pods...)
	fx.tracker.mu.Lock()
	for i := range 5 {
		fx.tracker.lastActive["sb"+string(rune('a'+i))] = fx.now.Add(-time.Minute)
	}
	fx.tracker.mu.Unlock()

	fx.flusher.flushOnce(context.Background())

	fx.flusher.mu.Lock()
	pending := len(fx.flusher.pending)
	fx.flusher.mu.Unlock()
	if pending > 2 {
		t.Errorf("pending = %d, must stay within MaxPending=2", pending)
	}
}

// A tick walks every tracked sandbox, and each lookup deep-copies a Pod out of
// the informer cache. With thousands of sandboxes that is a lot of copying for
// an answer that cannot have changed inside the refresh interval, so evaluation
// is throttled to the same cadence as the write.
func TestFlusherThrottlesPodLookups(t *testing.T) {
	pod := activityPod("pod-1", "sb1", map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
		agentsv1alpha1.SandboxLastActiveAnnotationKey:  time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fx.now
	fx.tracker.mu.Unlock()

	// First tick evaluates and writes.
	fx.flusher.flushOnce(context.Background())
	if got, want := fx.annotation(t, "sb1"), fx.now.Format(time.RFC3339); got != want {
		t.Fatalf("last-active = %q, want %q", got, want)
	}

	fx.flusher.mu.Lock()
	next := fx.flusher.states["sb1"].nextCheck
	fx.flusher.mu.Unlock()
	if !next.After(fx.now) {
		t.Fatalf("nextCheck = %v, want a future deadline", next)
	}

	// A tick inside that window must not re-evaluate — observable as the
	// deadline staying put rather than being pushed forward.
	fx.clock.advance(time.Second)
	fx.flusher.flushOnce(context.Background())
	fx.flusher.mu.Lock()
	after := fx.flusher.states["sb1"].nextCheck
	fx.flusher.mu.Unlock()
	if !after.Equal(next) {
		t.Errorf("nextCheck moved from %v to %v inside the throttle window", next, after)
	}

	// Once the window passes it evaluates again. Advance past the jittered
	// maximum, not the base interval — this sandbox's draw can be up to 25%
	// longer.
	fx.clock.advance(activity.MaxRefreshInterval(time.Hour))
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fx.clock.t
	fx.tracker.mu.Unlock()
	fx.flusher.flushOnce(context.Background())
	if got, want := fx.annotation(t, "sb1"), fx.clock.t.Format(time.RFC3339); got != want {
		t.Errorf("after the window, last-active = %q, want %q", got, want)
	}
}

// Bookkeeping must not outlive the sandboxes it describes.
func TestFlusherPrunesStateForVanishedSandboxes(t *testing.T) {
	pod := activityPod("pod-1", "sb1", map[string]string{
		agentsv1alpha1.SandboxIdleTimeoutAnnotationKey: "600",
		agentsv1alpha1.SandboxLastActiveAnnotationKey:  time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	fx := newFlusherFixture(t, ActivityFlusherConfig{}, pod)
	fx.tracker.mu.Lock()
	fx.tracker.lastActive["sb1"] = fx.now
	fx.tracker.mu.Unlock()
	fx.flusher.flushOnce(context.Background())

	fx.tracker.mu.Lock()
	delete(fx.tracker.lastActive, "sb1")
	fx.tracker.mu.Unlock()
	fx.flusher.flushOnce(context.Background())

	fx.flusher.mu.Lock()
	_, present := fx.flusher.states["sb1"]
	fx.flusher.mu.Unlock()
	if present {
		t.Error("state for a sandbox the tracker no longer holds was retained")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func TestParseSecondsAnnotation(t *testing.T) {
	cases := map[string]time.Duration{
		"":       0,
		"0":      0,
		"-5":     0,
		"abc":    0,
		"600":    600 * time.Second,
		"1":      time.Second,
		" 600 ":  0, // the claim never writes whitespace; treat it as malformed
		"999999": 999999 * time.Second,
	}
	for raw, want := range cases {
		if got := parseSecondsAnnotation(raw); got != want {
			t.Errorf("parseSecondsAnnotation(%q) = %v, want %v", raw, got, want)
		}
	}
}
