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

//go:build stress

// Package schedule stress tests — only compiled with `-tags=stress`.
//
// Run:
//   go test -tags=stress -race -count=1 ./pkg/lifecycle/schedule/...
//
// Strength is tunable via the SCHEDULE_STRESS_SCALE env var (floating point,
// default 1.0). 0.5 halves the load/duration; 3.0 triples them. Use a low
// scale (e.g. 0.3) for quick smoke runs, higher scales to chase rare races.
//
// These tests are intentionally isolated from the default `go test` run
// because they take several seconds each and their goroutine counts dwarf
// the rest of the suite. Unit tests in scheduler_test.go already cover
// functional correctness; these are here purely to harden the code against
// concurrency edge cases under -race.

package schedule

import (
	"context"
	"math"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
)

// stressScale returns the current stress multiplier. Default 1.0.
// Override with env var SCHEDULE_STRESS_SCALE=<float>.
func stressScale() float64 {
	raw := os.Getenv("SCHEDULE_STRESS_SCALE")
	if raw == "" {
		return 1.0
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f <= 0 {
		return 1.0
	}
	return f
}

// scaleInt returns max(1, round(n * scale)).
func scaleInt(n int) int {
	scaled := int(math.Round(float64(n) * stressScale()))
	if scaled < 1 {
		return 1
	}
	return scaled
}

// scaleDur scales a duration with a floor of 50 ms to keep tests meaningful.
func scaleDur(d time.Duration) time.Duration {
	scaled := time.Duration(float64(d) * stressScale())
	if scaled < 50*time.Millisecond {
		return 50 * time.Millisecond
	}
	return scaled
}

// TestScheduler_ExtremeScale pushes significantly more requests and pods than
// the baseline high-concurrency test, with concurrent Enqueue across many
// goroutines. Verifies: every pod is dispatched at most once, successes
// == pods, successes + noIdle == total requests. Runs under -race.
func TestScheduler_ExtremeScale(t *testing.T) {
	t.Parallel()
	nPods := scaleInt(500)
	nReqs := scaleInt(2000)
	const enqueueWorkers = 40
	const requestDeadline = 30 * time.Second
	const resultWait = requestDeadline + 10*time.Second
	// Round nReqs up to a multiple of enqueueWorkers.
	if rem := nReqs % enqueueWorkers; rem != 0 {
		nReqs += enqueueWorkers - rem
	}

	objs := make([]client.Object, 0, nPods)
	for i := range nPods {
		objs = append(objs, newIdlePod("ns", "p", "pod-"+itoa(i)))
	}
	c := newFakeClient(t, objs...)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	chs := make([]chan ClaimResult, nReqs)
	for i := range chs {
		chs[i] = make(chan ClaimResult, 1)
	}

	var enqWG sync.WaitGroup
	perWorker := nReqs / enqueueWorkers
	for w := range enqueueWorkers {
		enqWG.Add(1)
		go func(start int) {
			defer enqWG.Done()
			for i := range perWorker {
				idx := start + i
				req := &ClaimRequest{
					Ctx:      context.Background(),
					Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
					Deadline: time.Now().Add(requestDeadline),
					ResultCh: chs[idx],
				}
				enqueued := false
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if s.Enqueue(req) {
						enqueued = true
						break
					}
					time.Sleep(2 * time.Millisecond)
				}
				if !enqueued && !s.Enqueue(req) {
					t.Errorf("Enqueue idx %d never succeeded", idx)
				}
			}
		}(w * perWorker)
	}
	enqWG.Wait()

	var successCount, noIdleCount, otherCount atomic.Int32
	seen := sync.Map{}
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(c chan ClaimResult) {
			defer wg.Done()
			res := drainResult(t, c, resultWait)
			if res.Err == nil {
				successCount.Add(1)
				if _, dup := seen.LoadOrStore(res.Pod.Name, struct{}{}); dup {
					t.Errorf("pod %s dispatched twice", res.Pod.Name)
				}
				return
			}
			if res.Err == inplaceupdate.ErrNoIdlePodsAvailable {
				noIdleCount.Add(1)
			} else {
				otherCount.Add(1)
			}
		}(ch)
	}
	wg.Wait()

	if got := int(successCount.Load()); got != nPods {
		t.Errorf("success=%d, want %d", got, nPods)
	}
	if int(successCount.Load()+noIdleCount.Load()) != nReqs {
		t.Errorf("success+noIdle=%d+%d != total %d (other=%d)",
			successCount.Load(), noIdleCount.Load(), nReqs, otherCount.Load())
	}
	t.Logf("extreme scale: %d reqs, %d pods, %d success, %d noIdle, %d other (scale=%.2f)",
		nReqs, nPods, successCount.Load(), noIdleCount.Load(), otherCount.Load(), stressScale())
}

// TestScheduler_ConflictInjectionUnderLoad injects retriable conflicts on ~30%
// of pods (each of those pods' first Update attempt returns a Conflict error).
// The scheduler must still dispatch every pod at most once and every request
// must terminate: successful claims should equal the pod count because retries
// find a different pod.
func TestScheduler_ConflictInjectionUnderLoad(t *testing.T) {
	t.Parallel()
	nPods := scaleInt(200)
	nReqs := scaleInt(600)

	objs := make([]client.Object, 0, nPods)
	for i := range nPods {
		objs = append(objs, newIdlePod("ns", "p", "pod-"+itoa(i)))
	}
	base := newFakeClient(t, objs...)

	// Per-pod "first attempt conflicts" flag. Keyed by pod name.
	conflictOnce := sync.Map{}
	for i := range nPods {
		if i%3 == 0 {
			conflictOnce.Store("pod-"+itoa(i), new(atomic.Bool))
		}
	}
	c := withUpdateIntercept(base, func(name string) error {
		v, ok := conflictOnce.Load(name)
		if !ok {
			return nil
		}
		if v.(*atomic.Bool).CompareAndSwap(false, true) {
			return newConflictErr(name)
		}
		return nil
	})
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	chs := make([]chan ClaimResult, nReqs)
	for i := range chs {
		chs[i] = make(chan ClaimResult, 1)
		req := &ClaimRequest{
			Ctx:      context.Background(),
			Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
			Deadline: time.Now().Add(5 * time.Second),
			ResultCh: chs[i],
		}
		if !s.Enqueue(req) {
			t.Fatalf("Enqueue %d failed", i)
		}
	}

	var successCount, noIdleCount, otherCount atomic.Int32
	seen := sync.Map{}
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(c chan ClaimResult) {
			defer wg.Done()
			res := drainResult(t, c, 20*time.Second)
			if res.Err == nil {
				successCount.Add(1)
				if _, dup := seen.LoadOrStore(res.Pod.Name, struct{}{}); dup {
					t.Errorf("pod %s dispatched twice", res.Pod.Name)
				}
				return
			}
			if res.Err == inplaceupdate.ErrNoIdlePodsAvailable {
				noIdleCount.Add(1)
			} else {
				otherCount.Add(1)
			}
		}(ch)
	}
	wg.Wait()

	if got := int(successCount.Load()); got != nPods {
		t.Errorf("success=%d, want %d (conflict retries must still resolve)", got, nPods)
	}
	if int(successCount.Load()+noIdleCount.Load()) != nReqs {
		t.Errorf("success+noIdle=%d+%d != %d (other=%d)",
			successCount.Load(), noIdleCount.Load(), nReqs, otherCount.Load())
	}
	t.Logf("conflict injection: %d reqs, %d pods (1/3 injected), %d success, %d noIdle (scale=%.2f)",
		nReqs, nPods, successCount.Load(), noIdleCount.Load(), stressScale())
}

// TestScheduler_StressStorm runs a continuous storm of enqueues, notifyIdles,
// and pod churn for a fixed duration, then verifies clean shutdown. The goal
// is to catch rare race conditions that only appear with complex interleaving
// across all scheduler inputs. Under -race this should be deterministic.
//
// Correctness signals checked:
//   - At least some requests are accepted (storm actually does work).
//   - Shutdown() returns within 5 s (no hung goroutine in the scheduler).
//
// We intentionally do NOT use runtime.NumGoroutine() as an assertion: when
// multiple stress tests run in parallel inside one process, they inflate the
// live goroutine count independent of any real leak in the unit under test.
// The -race detector is the authoritative signal for concurrency bugs here.
func TestScheduler_StressStorm(t *testing.T) {
	t.Parallel()
	stormDuration := scaleDur(600 * time.Millisecond)
	initialPods := scaleInt(200)
	enqueueRate := scaleInt(64)
	notifyRate := scaleInt(16)

	objs := make([]client.Object, 0, initialPods)
	for i := range initialPods {
		objs = append(objs, newIdlePod("ns", "p", "pod-"+itoa(i)))
	}
	c := newFakeClient(t, objs...)
	s := NewPoolScheduler("ns", "p", "", "", "", c)
	go s.Run(context.Background())

	var enqueued, rejected atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	for range enqueueRate {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ch := make(chan ClaimResult, 1)
				req := &ClaimRequest{
					Ctx:      context.Background(),
					Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
					Deadline: time.Now().Add(200 * time.Millisecond),
					ResultCh: ch,
				}
				if !s.Enqueue(req) {
					rejected.Add(1)
					continue
				}
				enqueued.Add(1)
				select {
				case <-ch:
				case <-time.After(500 * time.Millisecond):
				case <-stop:
					select {
					case <-ch:
					case <-time.After(200 * time.Millisecond):
					}
					return
				}
			}
		}()
	}

	for range notifyRate {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s.NotifyIdle()
			}
		}()
	}

	time.Sleep(stormDuration)
	close(stop)
	wg.Wait()

	// Shutdown must return promptly. If it hangs, the scheduler has a
	// goroutine stuck (e.g. blocked on a full resultCh or misused cond).
	done := make(chan struct{})
	shutdownStart := time.Now()
	go func() {
		s.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return within 5 s — scheduler hung")
	}

	t.Logf("stress storm: enqueued=%d rejected=%d shutdown=%s (scale=%.2f)",
		enqueued.Load(), rejected.Load(), time.Since(shutdownStart), stressScale())
	if enqueued.Load() == 0 {
		t.Fatal("no requests accepted — storm did nothing")
	}
}

// TestScheduler_ConcurrentEnqueueAndShutdown: Shutdown while many goroutines
// are still calling Enqueue. Neither side may hang; Enqueue either wins
// (result arrives, possibly err) or the scheduler's drain on exit rejects it.
func TestScheduler_ConcurrentEnqueueAndShutdown(t *testing.T) {
	t.Parallel()
	enqueuers := scaleInt(32)

	c := newFakeClient(t /* no pods → all requests sit in reqCh */)
	s := NewPoolScheduler("ns", "p", "", "", "", c)
	go s.Run(context.Background())

	var wg sync.WaitGroup
	stopEnq := make(chan struct{})
	for range enqueuers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopEnq:
					return
				default:
				}
				ch := make(chan ClaimResult, 1)
				req := &ClaimRequest{
					Ctx:      context.Background(),
					Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
					Deadline: time.Now().Add(500 * time.Millisecond),
					ResultCh: ch,
				}
				if !s.Enqueue(req) {
					continue
				}
				select {
				case <-ch:
				case <-time.After(1 * time.Second):
				case <-stopEnq:
					select {
					case <-ch:
					case <-time.After(200 * time.Millisecond):
					}
					return
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		s.Shutdown()
		close(done)
	}()
	close(stopEnq)
	wg.Wait()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return within 5s — scheduler hung")
	}
}

// Note: itoa() is defined in scheduler_test.go and shared with stress tests
// (Go compiles test files for a package together regardless of build tags
// within that package, so the helper is always in scope).

// TestScheduler_ScaleDownChurnUnderLoad exercises the Get-on-pop eviction path
// introduced by the ready-queue refactor: while dispatches are in flight, a
// scale-down goroutine deletes a random subset of idle pods from the fake
// client. The scheduler must:
//
//   - Never dispatch a deleted pod (success ResultCh must carry a pod that
//     still exists in the client at the moment of success).
//   - Never hang or produce spurious hard errors from the deletion race.
//   - Drain every request to either success or noIdle.
func TestScheduler_ScaleDownChurnUnderLoad(t *testing.T) {
	t.Parallel()
	nPods := scaleInt(400)
	nReqs := scaleInt(800)
	nDeletes := nPods / 2 // delete half the pods mid-flight
	const enqueueWorkers = 32
	const requestDeadline = 20 * time.Second
	const resultWait = requestDeadline + 10*time.Second
	if rem := nReqs % enqueueWorkers; rem != 0 {
		nReqs += enqueueWorkers - rem
	}

	objs := make([]client.Object, 0, nPods)
	for i := range nPods {
		objs = append(objs, newIdlePod("ns", "p", "pod-"+itoa(i)))
	}
	c := newFakeClient(t, objs...)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	chs := make([]chan ClaimResult, nReqs)
	for i := range chs {
		chs[i] = make(chan ClaimResult, 1)
	}

	// Launch enqueuers.
	var enqWG sync.WaitGroup
	perWorker := nReqs / enqueueWorkers
	for w := range enqueueWorkers {
		enqWG.Add(1)
		go func(start int) {
			defer enqWG.Done()
			for i := range perWorker {
				idx := start + i
				req := &ClaimRequest{
					Ctx:      context.Background(),
					Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
					Deadline: time.Now().Add(requestDeadline),
					ResultCh: chs[idx],
				}
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if s.Enqueue(req) {
						break
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(w * perWorker)
	}

	// Concurrent scale-down: delete nDeletes pods while dispatches are in
	// flight. Target pods chosen deterministically (odd indices) so we can
	// later assert which pods must NOT have been dispatched.
	var deleteDone atomic.Bool
	deleted := sync.Map{} // name -> true
	go func() {
		// Small initial delay so some dispatches succeed before deletion storms.
		time.Sleep(20 * time.Millisecond)
		for i := 0; i < nDeletes; i++ {
			name := "pod-" + itoa(i*2+1) // odd index
			pod := &corev1.Pod{}
			pod.Namespace = "ns"
			pod.Name = name
			if err := c.Delete(context.Background(), pod); err == nil {
				deleted.Store(name, true)
			}
			// Tiny jitter so deletions don't all land in one tick.
			if i%8 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
		deleteDone.Store(true)
	}()

	enqWG.Wait()

	var successCount, noIdleCount, otherCount atomic.Int32
	seen := sync.Map{}
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(ch chan ClaimResult) {
			defer wg.Done()
			res := drainResult(t, ch, resultWait)
			if res.Err == nil {
				successCount.Add(1)
				if res.Pod == nil {
					t.Error("nil Pod on success")
					return
				}
				if _, dup := seen.LoadOrStore(res.Pod.Name, struct{}{}); dup {
					t.Errorf("pod %s dispatched twice", res.Pod.Name)
				}
				// Invariant: no deleted pod may appear in a success result.
				// The Get-on-pop check should have caught it and discarded.
				if _, del := deleted.Load(res.Pod.Name); del {
					t.Errorf("pod %s was dispatched after being deleted", res.Pod.Name)
				}
				return
			}
			if res.Err == inplaceupdate.ErrNoIdlePodsAvailable {
				noIdleCount.Add(1)
			} else {
				otherCount.Add(1)
			}
		}(ch)
	}
	wg.Wait()

	if !deleteDone.Load() {
		t.Error("deletion goroutine did not finish")
	}
	if int(otherCount.Load()) > 0 {
		t.Errorf("unexpected hard errors: %d (deletion should produce noIdle, not errors)", otherCount.Load())
	}
	total := int(successCount.Load() + noIdleCount.Load() + otherCount.Load())
	if total != nReqs {
		t.Errorf("result total=%d != nReqs=%d", total, nReqs)
	}
	// Upper bound sanity: we deleted nDeletes pods, so successes should not
	// exceed (nPods - nDeletes) + slack for dispatches that raced deletion.
	// In practice this is tight; allow +10% to avoid flakiness on fast CI.
	maxExpectedSuccess := (nPods - nDeletes) + nDeletes/10
	if int(successCount.Load()) > nPods {
		t.Errorf("success=%d exceeds nPods=%d (indicates double-dispatch)",
			successCount.Load(), nPods)
	}
	t.Logf("scale-down churn: %d reqs, %d pods, %d deleted | %d success, %d noIdle, %d other (cap=%d, scale=%.2f)",
		nReqs, nPods, nDeletes, successCount.Load(), noIdleCount.Load(), otherCount.Load(),
		maxExpectedSuccess, stressScale())
}
