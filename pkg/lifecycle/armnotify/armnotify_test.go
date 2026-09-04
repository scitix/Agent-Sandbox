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

package armnotify

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNotifyReachesTheWaiter(t *testing.T) {
	r := New()
	ch, cancel := r.Wait("sb1")
	defer cancel()

	go r.Notify("sb1", nil)

	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("verdict never arrived")
	}
}

func TestNotifyCarriesTheFailure(t *testing.T) {
	r := New()
	ch, cancel := r.Wait("sb1")
	defer cancel()

	want := errors.New("runtime never answered")
	go r.Notify("sb1", want)

	if got := <-ch; !errors.Is(got, want) {
		t.Fatalf("expected the arming error to reach the caller, got %v", got)
	}
}

// The re-arm that follows a configuration change has no requester. Dropping its
// verdict must not block the goroutine reporting it.
func TestNotifyWithNoWaiterIsDropped(t *testing.T) {
	r := New()
	done := make(chan struct{})
	go func() {
		r.Notify("nobody-is-waiting", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked with no waiter")
	}
}

// A caller that gives up must not leave an entry behind, or the registry grows
// by one per abandoned request.
func TestCancelRemovesTheWaiter(t *testing.T) {
	r := New()
	_, cancel := r.Wait("sb1")
	if r.Waiting() != 1 {
		t.Fatalf("expected 1 waiter, got %d", r.Waiting())
	}
	cancel()
	if r.Waiting() != 0 {
		t.Fatalf("cancel must remove the waiter, got %d", r.Waiting())
	}
	// And a verdict arriving afterwards is harmless.
	r.Notify("sb1", nil)
}

// Notify must not block when the caller has stopped reading — the buffered
// channel is what guarantees the arming goroutine always makes progress.
func TestNotifyDoesNotBlockOnAnAbandonedWaiter(t *testing.T) {
	r := New()
	_, _ = r.Wait("sb1")

	done := make(chan struct{})
	go func() {
		r.Notify("sb1", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on an abandoned waiter")
	}
}

func TestDisplacedWaiterIsFailedNotLeftHanging(t *testing.T) {
	r := New()
	first, cancelFirst := r.Wait("sb1")
	defer cancelFirst()
	_, cancelSecond := r.Wait("sb1")
	defer cancelSecond()

	select {
	case err := <-first:
		if err == nil {
			t.Fatal("a displaced waiter must be failed, not succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("displaced waiter was left hanging")
	}
}

func TestConcurrentWaitAndNotify(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := range 50 {
		id := fmt.Sprintf("sb-%d", i)
		wg.Add(2)
		go func() { defer wg.Done(); ch, cancel := r.Wait(id); defer cancel(); <-ch }()
		go func() { defer wg.Done(); time.Sleep(time.Millisecond); r.Notify(id, nil) }()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent wait/notify deadlocked")
	}
}

func TestNilRegistryIsSafe(t *testing.T) {
	var r *Registry
	ch, cancel := r.Wait("sb1")
	if ch != nil {
		t.Fatal("a nil registry has nothing to wait on")
	}
	cancel()
	r.Notify("sb1", nil)
	if r.Waiting() != 0 {
		t.Fatal("nil registry must report no waiters")
	}
}
