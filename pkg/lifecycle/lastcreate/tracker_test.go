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

package lastcreate

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := agentsv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return s
}

const (
	testNS   = "ns"
	testPool = "p"
)

// newPool returns a minimal SandboxPool fixture in the test namespace.
// Inlined as a helper rather than a const so future tests can override
// labels/annotations on the returned object before seeding the fake
// client.
func newPool() *agentsv1alpha1.SandboxPool {
	return &agentsv1alpha1.SandboxPool{ObjectMeta: metav1.ObjectMeta{Namespace: testNS, Name: testPool}}
}

// ---------- Bump / Get ----------

func TestBump_AndGet(t *testing.T) {
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tr := NewTracker(nil, 0)
	tr.now = func() time.Time { return now }

	if _, ok := tr.Get("ns", "p"); ok {
		t.Fatal("expected (zero, false) before any Bump")
	}
	tr.Bump("ns", "p")
	got, ok := tr.Get("ns", "p")
	if !ok || !got.Equal(now) {
		t.Errorf("Get = (%v, %v), want (%v, true)", got, ok, now)
	}
}

func TestBump_NilReceiver(t *testing.T) {
	var tr *Tracker
	tr.Bump("ns", "p") // must not panic
	if _, ok := tr.Get("ns", "p"); ok {
		t.Error("nil tracker should return false")
	}
}

func TestBump_RejectsEmptyKeys(t *testing.T) {
	tr := NewTracker(nil, 0)
	tr.Bump("", "p")
	tr.Bump("ns", "")
	if got, ok := tr.Get("", "p"); ok {
		t.Errorf("Get(empty ns) = (%v, true)", got)
	}
}

func TestBump_LatestWins(t *testing.T) {
	t1 := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(5 * time.Second)
	tr := NewTracker(nil, 0)

	tr.now = func() time.Time { return t1 }
	tr.Bump("ns", "p")
	tr.now = func() time.Time { return t2 }
	tr.Bump("ns", "p")

	got, _ := tr.Get("ns", "p")
	if !got.Equal(t2) {
		t.Errorf("expected latest %v, got %v", t2, got)
	}
}

func TestBump_ConcurrentSafe(t *testing.T) {
	tr := NewTracker(nil, 0)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range 50 {
				tr.Bump("ns", "p")
				_, _ = tr.Get("ns", "p")
				_ = n + j
			}
		}(i)
	}
	wg.Wait()
}

// ---------- flushOnce ----------

func TestFlushOnce_WritesAnnotation(t *testing.T) {
	scheme := newTestScheme(t)
	pool := newPool()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tr := NewTracker(c, time.Hour)
	tr.now = func() time.Time { return now }
	tr.Bump("ns", "p")

	tr.flushOnce(context.Background())

	got := &agentsv1alpha1.SandboxPool{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey] != now.Format(time.RFC3339) {
		t.Errorf("annotation = %q, want %q", got.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey], now.Format(time.RFC3339))
	}
}

func TestFlushOnce_SkipsCleanEntries(t *testing.T) {
	scheme := newTestScheme(t)
	pool := newPool()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tr := NewTracker(c, time.Hour)
	tr.now = func() time.Time { return now }

	tr.Bump("ns", "p")
	tr.flushOnce(context.Background()) // first flush
	// Spy resourceVersion baseline.
	rv := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, rv)
	baseline := rv.ResourceVersion

	// No new Bump → should be no-op.
	tr.flushOnce(context.Background())
	after := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, after)
	if after.ResourceVersion != baseline {
		t.Errorf("second flushOnce wrote the Pool unnecessarily (RV %q -> %q)", baseline, after.ResourceVersion)
	}
}

func TestFlushOnce_AdvancesAfterReBump(t *testing.T) {
	scheme := newTestScheme(t)
	pool := newPool()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	tr := NewTracker(c, time.Hour)

	t1 := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return t1 }
	tr.Bump("ns", "p")
	tr.flushOnce(context.Background())

	t2 := t1.Add(2 * time.Minute)
	tr.now = func() time.Time { return t2 }
	tr.Bump("ns", "p")
	tr.flushOnce(context.Background())

	after := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, after)
	if want := t2.Format(time.RFC3339); after.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey] != want {
		t.Errorf("annotation = %q, want %q", after.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey], want)
	}
}

func TestFlushOnce_PoolNotFound_DropsEntry(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build() // no pools seeded
	tr := NewTracker(c, time.Hour)
	tr.Bump("ns", "ghost")

	tr.flushOnce(context.Background())

	// Entry should be evicted so future flushes don't retry the ghost.
	tr.mu.Lock()
	_, present := tr.perPool["ns/ghost"]
	tr.mu.Unlock()
	if present {
		t.Errorf("expected ghost entry evicted on NotFound, still present")
	}
}

func TestFlushOnce_DoesNotOverwriteFresherAnnotation(t *testing.T) {
	scheme := newTestScheme(t)
	pool := newPool()
	fresher := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	pool.Annotations = map[string]string{
		agentsv1alpha1.LastSandboxCreateTimeAnnotationKey: fresher.Format(time.RFC3339),
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	tr := NewTracker(c, time.Hour)

	older := fresher.Add(-time.Hour)
	tr.now = func() time.Time { return older }
	tr.Bump("ns", "p")

	tr.flushOnce(context.Background())

	got := &agentsv1alpha1.SandboxPool{}
	_ = c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, got)
	if v := got.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey]; v != fresher.Format(time.RFC3339) {
		t.Errorf("annotation overwritten to %q, expected to stay %q", v, fresher.Format(time.RFC3339))
	}
}

// ---------- Start lifecycle ----------

func TestStart_NilGuards(t *testing.T) {
	if err := (*Tracker)(nil).Start(context.Background()); err == nil {
		t.Error("expected error from nil tracker")
	}
	if err := (&Tracker{}).Start(context.Background()); err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestStart_FlushesUntilCancel(t *testing.T) {
	scheme := newTestScheme(t)
	pool := newPool()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()

	tr := NewTracker(c, 5*time.Millisecond)
	tr.Bump("ns", "p")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tr.Start(ctx) }()

	// Spin until the annotation lands or we time out.
	deadline := time.Now().Add(2 * time.Second)
	var seen atomic.Bool
	for time.Now().Before(deadline) {
		got := &agentsv1alpha1.SandboxPool{}
		_ = c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "p"}, got)
		if got.Annotations[agentsv1alpha1.LastSandboxCreateTimeAnnotationKey] != "" {
			seen.Store(true)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Start did not exit after cancel within 1s")
	}
	if !seen.Load() {
		t.Error("Start never wrote the annotation")
	}
}
