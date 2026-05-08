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
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// mkPod creates a minimal idle Pod with the given name and UID in the default
// namespace. The Idle phase label is set so informer-cache lookups succeed.
func mkPod(name, uid string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(uid),
			Labels: map[string]string{
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
		},
	}
}

// mkProtectedPod creates an Idle pod marked with the scale-down-protection annotation.
func mkProtectedPod(name, uid string) corev1.Pod {
	p := mkPod(name, uid)
	p.Annotations = map[string]string{
		agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey: time.Now().UTC().Format(time.RFC3339),
	}
	return p
}

// nn returns the NamespacedName for a pod created by mkPod.
func nn(name string) types.NamespacedName {
	return types.NamespacedName{Namespace: "default", Name: name}
}

// fakeClientWithPods builds a controller-runtime fake client pre-seeded with pods.
func fakeClientWithPods(t *testing.T, pods ...corev1.Pod) client.Client {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("GetFakeClientBuilderWithIndexers: %v", err)
	}
	for i := range pods {
		cb = cb.WithObjects(&pods[i])
	}
	return cb.Build()
}

// pop is a test helper that calls popUnreservedAndReserve with background ctx
// and the given client.
func pop(q *readyQueue, c client.Client, r *reservations) (corev1.Pod, bool, int) {
	return q.popUnreservedAndReserve(context.Background(), c, r)
}

// ─── core behaviour ──────────────────────────────────────────────────────────

func TestReadyQueue_FIFOOrder(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{mkPod("a", "1"), mkPod("b", "2"), mkPod("c", "3")}
	c := fakeClientWithPods(t, pods...)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	q.appendFiltered(pods, r)
	for _, want := range []string{"a", "b", "c"} {
		p, ok, _ := pop(q, c, r)
		if !ok {
			t.Fatalf("expected %s, got empty", want)
		}
		if p.Name != want {
			t.Fatalf("want %s, got %s", want, p.Name)
		}
	}
	if _, ok, _ := pop(q, c, r); ok {
		t.Fatal("expected empty queue")
	}
}

func TestReadyQueue_DedupByName(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{mkPod("a", "1"), mkPod("b", "2")}
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	admitted, _ := q.appendFiltered([]corev1.Pod{mkPod("a", "1")}, r)
	if admitted != 1 {
		t.Fatalf("first append admitted=%d, want 1", admitted)
	}
	admitted, _ = q.appendFiltered(pods, r)
	if admitted != 1 {
		t.Fatalf("second append admitted=%d, want 1 (a is dup)", admitted)
	}
	if q.len() != 2 {
		t.Fatalf("queue len=%d, want 2", q.len())
	}
}

func TestReadyQueue_SkipScaleDownProtected(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{
		mkPod("a", "1"),
		mkProtectedPod("b", "2"),
		mkPod("c", "3"),
	}
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	admitted, skipped := q.appendFiltered(pods, r)
	if admitted != 2 {
		t.Fatalf("admitted=%d, want 2", admitted)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d, want 1", skipped)
	}
	if q.contains(nn("b")) {
		t.Fatal("protected pod should not be in queue")
	}
}

func TestReadyQueue_SkipAlreadyReserved(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{mkPod("a", "1"), mkPod("b", "2")}
	c := fakeClientWithPods(t, pods...)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	r.reserve("a")
	admitted, _ := q.appendFiltered(pods, r)
	if admitted != 1 {
		t.Fatalf("admitted=%d, want 1 (a is already reserved)", admitted)
	}
	p, ok, _ := pop(q, c, r)
	if !ok || p.Name != "b" {
		t.Fatalf("expected b, got %v (ok=%v)", p.Name, ok)
	}
}

func TestReadyQueue_PopSkipsReservedHead(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{mkPod("a", "1"), mkPod("b", "2"), mkPod("c", "3")}
	c := fakeClientWithPods(t, pods...)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	q.appendFiltered(pods, r)
	r.reserve("a")
	r.reserve("b")

	p, ok, _ := pop(q, c, r)
	if !ok || p.Name != "c" {
		t.Fatalf("expected c, got %v (ok=%v)", p.Name, ok)
	}
	if !r.isReserved("c") {
		t.Fatal("c should be reserved after pop")
	}
	if q.len() != 0 {
		t.Fatalf("all three popped/skipped, queue should be empty; len=%d", q.len())
	}
}

func TestReadyQueue_PopAtomicReserve(t *testing.T) {
	t.Parallel()
	pods := []corev1.Pod{mkPod("a", "1")}
	c := fakeClientWithPods(t, pods...)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	q.appendFiltered(pods, r)
	p, ok, _ := pop(q, c, r)
	if !ok || p.Name != "a" {
		t.Fatalf("pop failed: %v ok=%v", p, ok)
	}
	if !r.isReserved("a") {
		t.Fatal("pop must reserve the pod atomically")
	}
}

func TestReadyQueue_EmptyPop(t *testing.T) {
	t.Parallel()
	c := fakeClientWithPods(t)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	if _, ok, _ := pop(q, c, r); ok {
		t.Fatal("empty queue pop should fail")
	}
}

// ─── stale-pod (scale-down) behaviour ────────────────────────────────────────

// TestReadyQueue_StalePodsDiscardedOnPop verifies that pods deleted from the
// informer cache after admission are silently discarded when popped, and
// discarded count is reported correctly.
func TestReadyQueue_StalePodsDiscardedOnPop(t *testing.T) {
	t.Parallel()
	// Seed client with only b and d — a and c are "deleted" (not in cache).
	b := mkPod("b", "2")
	d := mkPod("d", "4")
	c := fakeClientWithPods(t, b, d)

	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	// Admit [a, b, c, d] — pretend all were Idle when refreshReady ran.
	q.appendFiltered([]corev1.Pod{mkPod("a", "1"), b, mkPod("c", "3"), d}, r)
	if q.len() != 4 {
		t.Fatalf("queue len=%d, want 4", q.len())
	}

	// Pop: a is gone → discarded; b is live → returned; next call: c is gone →
	// discarded; d is live → returned.
	p1, ok1, disc1 := pop(q, c, r)
	if !ok1 || p1.Name != "b" {
		t.Fatalf("first pop: want b ok=true, got %q ok=%v", p1.Name, ok1)
	}
	if disc1 != 1 {
		t.Fatalf("first pop discarded=%d, want 1 (a was deleted)", disc1)
	}

	p2, ok2, disc2 := pop(q, c, r)
	if !ok2 || p2.Name != "d" {
		t.Fatalf("second pop: want d ok=true, got %q ok=%v", p2.Name, ok2)
	}
	if disc2 != 1 {
		t.Fatalf("second pop discarded=%d, want 1 (c was deleted)", disc2)
	}

	_, ok3, _ := pop(q, c, r)
	if ok3 {
		t.Fatal("third pop should return empty")
	}
}

// TestReadyQueue_StalePodsPreserveFIFO verifies that valid pods maintain their
// relative FIFO order even when interspersed deleted pods are skipped.
func TestReadyQueue_StalePodsPreserveFIFO(t *testing.T) {
	t.Parallel()
	live := []corev1.Pod{mkPod("p1", "1"), mkPod("p3", "3"), mkPod("p5", "5")}
	c := fakeClientWithPods(t, live...)
	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	// Admit all 5 pods; p2 and p4 are absent from the client (deleted).
	all := []corev1.Pod{
		mkPod("p1", "1"), mkPod("p2", "2"), mkPod("p3", "3"),
		mkPod("p4", "4"), mkPod("p5", "5"),
	}
	q.appendFiltered(all, r)

	order := []string{"p1", "p3", "p5"}
	for _, want := range order {
		p, ok, _ := pop(q, c, r)
		if !ok {
			t.Fatalf("expected %s, got empty", want)
		}
		if p.Name != want {
			t.Fatalf("FIFO violated: want %s, got %s", want, p.Name)
		}
	}
}

// TestReadyQueue_PhaseMismatchDiscarded verifies that a pod whose phase label
// changed to non-Idle after admission is silently discarded.
func TestReadyQueue_PhaseMismatchDiscarded(t *testing.T) {
	t.Parallel()
	// Pod is in the cache but no longer Idle (e.g. Running).
	running := mkPod("r", "1")
	running.Labels[agentsv1alpha1.SandboxPhaseLabelKey] = agentsv1alpha1.SandboxPhaseRunning
	idle := mkPod("i", "2")
	c := fakeClientWithPods(t, running, idle)

	q := newReadyQueue()
	r := newReservations(time.Second, nil)
	q.appendFiltered([]corev1.Pod{mkPod("r", "1"), idle}, r)

	p, ok, discarded := pop(q, c, r)
	if !ok || p.Name != "i" {
		t.Fatalf("expected i ok=true, got %q ok=%v", p.Name, ok)
	}
	if discarded != 1 {
		t.Fatalf("discarded=%d, want 1 (r had wrong phase)", discarded)
	}
}

// ─── concurrency ─────────────────────────────────────────────────────────────

func TestReadyQueue_Concurrent(t *testing.T) {
	t.Parallel()
	const totalPods = 500
	pods := make([]corev1.Pod, totalPods)
	for i := range pods {
		pods[i] = mkPod(fmt.Sprintf("p%d", i), fmt.Sprintf("u%d", i))
	}
	c := fakeClientWithPods(t, pods...)
	q := newReadyQueue()
	r := newReservations(50*time.Millisecond, nil)
	q.appendFiltered(pods, r)

	var popped atomic.Int64
	seen := sync.Map{}
	var wg sync.WaitGroup
	const workers = 16
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				p, ok, _ := pop(q, c, r)
				if !ok {
					return
				}
				if _, dup := seen.LoadOrStore(p.Name, struct{}{}); dup {
					t.Errorf("pod %s popped twice", p.Name)
					return
				}
				popped.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := popped.Load(); got != totalPods {
		t.Fatalf("popped=%d, want %d", got, totalPods)
	}
}

// TestReadyQueue_ProducerConsumer exercises concurrent admission + dispatch.
func TestReadyQueue_ProducerConsumer(t *testing.T) {
	t.Parallel()
	const (
		producers      = 8
		consumers      = 16
		batchesPerProd = 200
		batchSize      = 16
		uniqueKeySpace = producers * batchesPerProd * batchSize
	)

	// Pre-build all pods for the fake client.
	allPods := make([]corev1.Pod, uniqueKeySpace)
	for i := range allPods {
		allPods[i] = mkPod(fmt.Sprintf("n-%d", i), fmt.Sprintf("u-%d", i))
	}
	c := fakeClientWithPods(t, allPods...)
	q := newReadyQueue()
	r := newReservations(500*time.Millisecond, nil)

	admitted := make([]atomic.Bool, uniqueKeySpace)
	var poppedCount atomic.Int64
	poppedSet := sync.Map{}

	var wgProd, wgCons sync.WaitGroup
	done := make(chan struct{})

	for p := range producers {
		wgProd.Add(1)
		go func(pid int) {
			defer wgProd.Done()
			for b := range batchesPerProd {
				batch := make([]corev1.Pod, 0, batchSize)
				for k := range batchSize {
					uidIdx := (pid*batchesPerProd+b)*batchSize + k
					name := fmt.Sprintf("n-%d", uidIdx)
					uid := fmt.Sprintf("u-%d", uidIdx)
					batch = append(batch, mkPod(name, uid))
					admitted[uidIdx].Store(true)
				}
				q.appendFiltered(batch, r)
			}
		}(p)
	}

	for range consumers {
		wgCons.Go(func() {
			for {
				p, ok, _ := pop(q, c, r)
				if !ok {
					select {
					case <-done:
						if p2, ok2, _ := pop(q, c, r); ok2 {
							if _, dup := poppedSet.LoadOrStore(p2.Name, struct{}{}); dup {
								t.Errorf("pod %s popped twice", p2.Name)
							}
							poppedCount.Add(1)
							continue
						}
						return
					default:
						continue
					}
				}
				if _, dup := poppedSet.LoadOrStore(p.Name, struct{}{}); dup {
					t.Errorf("pod %s popped twice", p.Name)
				}
				poppedCount.Add(1)
				r.release(p.Name)
			}
		})
	}

	wgProd.Wait()
	for range 50 {
		if q.len() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(done)
	wgCons.Wait()

	poppedSet.Range(func(key, _ any) bool {
		name := key.(string)
		var idx int
		_, err := fmt.Sscanf(name, "n-%d", &idx)
		if err != nil || idx < 0 || idx >= uniqueKeySpace {
			t.Errorf("popped unknown name %q", name)
			return true
		}
		if !admitted[idx].Load() {
			t.Errorf("popped %s was never admitted", name)
		}
		return true
	})
	if got := poppedCount.Load(); got == 0 {
		t.Fatal("no pods popped")
	}
	t.Logf("producer/consumer: admitted %d pods, popped %d", uniqueKeySpace, poppedCount.Load())
}

// TestReadyQueue_HeavyDedup pushes 10k pods with only 1k unique names across
// many producer goroutines; the queue must accept each name exactly once.
func TestReadyQueue_HeavyDedup(t *testing.T) {
	t.Parallel()
	const unique = 1_000
	pods := make([]corev1.Pod, unique)
	for i := range pods {
		pods[i] = mkPod(fmt.Sprintf("n%d", i), fmt.Sprintf("u%d", i))
	}
	q := newReadyQueue()
	r := newReservations(time.Second, nil)

	const workers = 32
	const batches = 10
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range batches {
				batch := make([]corev1.Pod, unique)
				for i := range batch {
					batch[i] = mkPod(fmt.Sprintf("n%d", i), fmt.Sprintf("u%d", i))
				}
				q.appendFiltered(batch, r)
			}
		})
	}
	wg.Wait()
	if got := q.len(); got != unique {
		t.Fatalf("queue len=%d, want %d (dedup failed)", got, unique)
	}
}
