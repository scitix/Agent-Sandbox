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
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRouteCache_PutGet(t *testing.T) {
	c := NewRouteCache(time.Minute)

	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "p"})
	got, ok := c.Get("sb1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Namespace != "ns" || got.PodName != "p" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if got.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt not set by Put")
	}
}

func TestRouteCache_GetMiss(t *testing.T) {
	c := NewRouteCache(time.Minute)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss")
	}
	if _, ok := c.Get(""); ok {
		t.Fatal("expected miss on empty id")
	}
}

func TestRouteCache_Overwrite(t *testing.T) {
	c := NewRouteCache(time.Minute)
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "pod-a"})
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "pod-b"})
	got, ok := c.Get("sb1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.PodName != "pod-b" {
		t.Fatalf("expected overwritten PodName, got %q", got.PodName)
	}
}

func TestRouteCache_TTLExpiry(t *testing.T) {
	c := NewRouteCache(20 * time.Millisecond)
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "p"})

	if _, ok := c.Get("sb1"); !ok {
		t.Fatal("expected fresh hit")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get("sb1"); ok {
		t.Fatal("expected expired miss")
	}
	// Entry should still be in the underlying map until sweep.
	if c.Len() != 1 {
		t.Fatalf("expected len=1 before sweep, got %d", c.Len())
	}
}

func TestRouteCache_Delete(t *testing.T) {
	c := NewRouteCache(time.Minute)
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "p"})
	c.Delete("sb1")
	if _, ok := c.Get("sb1"); ok {
		t.Fatal("expected miss after Delete")
	}
	// Idempotent.
	c.Delete("sb1")
	c.Delete("missing")
}

func TestRouteCache_Sweep(t *testing.T) {
	c := NewRouteCache(20 * time.Millisecond)
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "p"})
	c.Put("sb2", RouteEntry{Namespace: "ns", PodName: "p2"})
	if c.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", c.Len())
	}
	time.Sleep(40 * time.Millisecond)
	c.sweep()
	if c.Len() != 0 {
		t.Fatalf("expected 0 entries after sweep, got %d", c.Len())
	}
}

func TestRouteCache_StartGC(t *testing.T) {
	c := NewRouteCache(20 * time.Millisecond)
	c.Put("sb1", RouteEntry{Namespace: "ns", PodName: "p"})
	ctx := t.Context()
	c.StartGC(ctx, 10*time.Millisecond)

	// Wait longer than TTL + a couple of GC ticks.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if c.Len() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected GC to reclaim expired entries; Len=%d", c.Len())
}

// TestRouteCache_ConcurrentPutGet verifies lock-free reads don't race with
// writers. Run with -race to catch any regressions.
func TestRouteCache_ConcurrentPutGet(t *testing.T) {
	c := NewRouteCache(time.Minute)
	var wg sync.WaitGroup

	// Writers
	for w := range 4 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range 200 {
				id := fmt.Sprintf("sb-%d-%d", w, i)
				c.Put(id, RouteEntry{Namespace: "ns", PodName: "p"})
			}
		}(w)
	}
	// Readers
	for range 8 {
		wg.Go(func() {
			for i := range 2000 {
				_, _ = c.Get(fmt.Sprintf("sb-0-%d", i%200))
			}
		})
	}
	wg.Wait()
}
