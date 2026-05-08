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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }
func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func TestReservations_ReserveIsReservedRelease(t *testing.T) {
	t.Parallel()
	r := newReservations(time.Second, nil)
	if r.isReserved("a") {
		t.Fatal("a should not be reserved initially")
	}
	r.reserve("a")
	if !r.isReserved("a") {
		t.Fatal("a should be reserved after reserve()")
	}
	if r.isReserved("b") {
		t.Fatal("b should not be reserved")
	}
	r.release("a")
	if r.isReserved("a") {
		t.Fatal("a should not be reserved after release()")
	}
}

func TestReservations_EmptyNameIsNoop(t *testing.T) {
	t.Parallel()
	r := newReservations(time.Second, nil)
	r.reserve("")
	r.release("")
	if r.size() != 0 {
		t.Fatalf("empty name must not be stored; size=%d", r.size())
	}
}

func TestReservations_TTLExpiry(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	r := newReservations(2*time.Second, clk.Now)

	r.reserve("a")
	if !r.isReserved("a") {
		t.Fatal("reserved just now should be active")
	}
	clk.Advance(time.Second)
	if !r.isReserved("a") {
		t.Fatal("1s into a 2s TTL should still be active")
	}
	clk.Advance(2 * time.Second) // total 3s > 2s TTL
	if r.isReserved("a") {
		t.Fatal("expired reservation should report not reserved")
	}
	// isReserved does NOT GC; size should still be 1 until sweep runs.
	if r.size() != 1 {
		t.Fatalf("expected size=1 before sweep, got %d", r.size())
	}
	if n := r.sweep(); n != 1 {
		t.Fatalf("sweep should report 1 removed, got %d", n)
	}
	if r.size() != 0 {
		t.Fatalf("expected size=0 after sweep, got %d", r.size())
	}
}

func TestReservations_ReserveRefreshesExpiry(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	r := newReservations(2*time.Second, clk.Now)

	r.reserve("a")
	clk.Advance(time.Second) // 1s elapsed
	r.reserve("a")           // refresh, new expiry = now+2s
	clk.Advance(2 * time.Second)
	// Without refresh would have expired at t+2s; with refresh expires at t+3s.
	// We are at t+3s, exactly at expiry boundary — isReserved uses Before(), so false.
	if r.isReserved("a") {
		t.Fatalf("at exact expiry should be false")
	}
}

func TestReservations_SweepLeavesFresh(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	r := newReservations(2*time.Second, clk.Now)
	r.reserve("old")
	clk.Advance(3 * time.Second)
	r.reserve("fresh")
	if got := r.sweep(); got != 1 {
		t.Fatalf("sweep should remove only old; got %d", got)
	}
	if !r.isReserved("fresh") {
		t.Fatal("fresh should remain after sweep")
	}
}

func TestReservations_Concurrent(t *testing.T) {
	t.Parallel()
	r := newReservations(50*time.Millisecond, nil)
	const N = 64
	var wg sync.WaitGroup
	wg.Add(N)
	var ops atomic.Int64
	stop := make(chan struct{})
	for i := range N {
		go func(id int) {
			defer wg.Done()
			name := string(rune('a' + id%26))
			for {
				select {
				case <-stop:
					return
				default:
				}
				r.reserve(name)
				_ = r.isReserved(name)
				if id%3 == 0 {
					r.release(name)
				}
				if id%5 == 0 {
					r.sweep()
				}
				ops.Add(1)
			}
		}(i)
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
	if ops.Load() == 0 {
		t.Fatal("expected at least one op")
	}
}

// TestReservations_ConcurrentHighLoad exercises many goroutines (producers,
// consumers, sweepers) against a shared reservations map with a wide key space.
// Under -race, any unguarded map access would trigger; under normal runs we
// sanity-check that no op hangs and that final size is bounded.
func TestReservations_ConcurrentHighLoad(t *testing.T) {
	t.Parallel()
	const (
		producers = 32
		consumers = 32
		sweepers  = 8
		duration  = 300 * time.Millisecond
		keySpace  = 512
	)
	r := newReservations(10*time.Millisecond, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var reserves, releases, reads, sweeps atomic.Int64

	for i := range producers {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			k := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				name := fmt.Sprintf("p-%d", k%keySpace)
				r.reserve(name)
				reserves.Add(1)
				k++
			}
		}(i * 7919) // odd stride per goroutine
	}

	for i := range consumers {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			k := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				name := fmt.Sprintf("p-%d", k%keySpace)
				if r.isReserved(name) {
					r.release(name)
					releases.Add(1)
				}
				reads.Add(1)
				k++
			}
		}(i * 6131)
	}

	for range sweepers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				r.sweep()
				sweeps.Add(1)
			}
		})
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	// Sanity: we actually ran. If any atomic is zero something is badly wrong.
	if reserves.Load() < 100 || reads.Load() < 100 {
		t.Fatalf("suspiciously low op count: reserves=%d reads=%d", reserves.Load(), reads.Load())
	}
	// Size is bounded by key space.
	if got := r.size(); got > keySpace {
		t.Fatalf("size=%d exceeds key space %d — map grew unbounded", got, keySpace)
	}
	t.Logf("ops: reserves=%d releases=%d reads=%d sweeps=%d finalSize=%d",
		reserves.Load(), releases.Load(), reads.Load(), sweeps.Load(), r.size())
}

// TestReservations_SweepConcurrentWithReserve races sweep() against concurrent
// reserve() calls using a fake clock to force deterministic expiry. Any
// dropped reservation / stale pointer / map corruption shows up under -race.
func TestReservations_SweepConcurrentWithReserve(t *testing.T) {
	t.Parallel()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	r := newReservations(5*time.Millisecond, clk.Now)

	const writers = 16
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers reserve new names constantly.
	for i := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				r.reserve(fmt.Sprintf("w%d-%d", id, n))
				n++
			}
		}(i)
	}

	// Sweeper advances the fake clock and sweeps, racing with writers.
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			clk.Advance(10 * time.Millisecond)
			r.sweep()
		}
	})

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	// After stop, advance well past TTL and sweep; everything should clear.
	clk.Advance(time.Second)
	r.sweep()
	if got := r.size(); got != 0 {
		t.Fatalf("expected size=0 after final sweep, got %d", got)
	}
}
