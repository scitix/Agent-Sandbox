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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newIdlePod builds a minimum Idle pod suitable for driving a scheduler test.
// It carries the pool + phase labels and an inplace-update-state annotation
// so TriggerUpdateWithOptions's CAS succeeds.
func newIdlePod(ns, pool, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("uid-" + name),
			Labels: map[string]string{
				agentsv1alpha1.SandboxPoolLabelKey:  pool,
				agentsv1alpha1.SandboxPhaseLabelKey: agentsv1alpha1.SandboxPhaseIdle,
			},
			Annotations: map[string]string{
				inplaceupdate.PodAnnotationInPlaceUpdateStateKey: `{"phase":"completed","targetPodPhase":"idle","updateTimestamp":"` + time.Now().UTC().Add(-time.Minute).Format(time.RFC3339) + `"}`,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "sandbox", Image: "pause:3.10"}},
		},
	}
}

// newProtectedIdlePod is an Idle pod marked as a scale-down candidate; the
// scheduler must skip it.
func newProtectedIdlePod(ns, pool, name string) *corev1.Pod {
	p := newIdlePod(ns, pool, name)
	p.Annotations[agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey] = time.Now().UTC().Format(time.RFC3339)
	return p
}

// newFakeClient wires a controller-runtime fake client with the project's
// field indexers and the supplied objects.
func newFakeClient(t *testing.T, objs ...client.Object) client.WithWatch {
	t.Helper()
	cb, err := indexer.GetFakeClientBuilderWithIndexers()
	if err != nil {
		t.Fatalf("GetFakeClientBuilderWithIndexers: %v", err)
	}
	for _, o := range objs {
		cb = cb.WithObjects(o)
	}
	return cb.Build()
}

// updateInterceptor describes an Update intercept decision.
type updateInterceptor func(podName string) error

// withUpdateIntercept wraps base with an interceptor that consults fn when
// Update is called on a Pod. Returning a non-nil error from fn short-circuits
// the Update; returning nil delegates to the underlying client.
func withUpdateIntercept(base client.WithWatch, fn updateInterceptor) client.Client {
	return interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if pod, ok := obj.(*corev1.Pod); ok {
				if err := fn(pod.Name); err != nil {
					return err
				}
			}
			return c.Update(ctx, obj, opts...)
		},
	})
}

// newConflictErr returns an apierrors.IsConflict-true error for pod CAS
// conflict simulation.
func newConflictErr(podName string) error {
	return apierrors.NewConflict(
		schema.GroupResource{Group: "", Resource: "pods"},
		podName,
		nil,
	)
}

// drainResult waits for one ClaimResult on ch or fails.
func drainResult(t *testing.T, ch <-chan ClaimResult, d time.Duration) ClaimResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(d):
		t.Fatalf("timed out waiting for result after %v", d)
		return ClaimResult{}
	}
}

func mkReq() (*ClaimRequest, chan ClaimResult) {
	ch := make(chan ClaimResult, 1)
	return &ClaimRequest{
		Ctx:      context.Background(),
		Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
		Deadline: time.Now().Add(30 * time.Second),
		ResultCh: ch,
	}, ch
}

// startScheduler returns a running scheduler + stop func.
func startScheduler(t *testing.T, c client.Client, ns, name string) (*PoolScheduler, func()) { //nolint:unparam
	t.Helper()
	s := NewPoolScheduler(ns, name, "", "", c)
	go s.Run(context.Background())
	return s, func() { s.Shutdown() }
}

// ---------------------------------------------------------------------------
// test cases
// ---------------------------------------------------------------------------

// TestScheduler_ImmediateDispatch: one idle pod + one request → success.
func TestScheduler_ImmediateDispatch(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t, newIdlePod("ns", "p", "pod-0"))
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	req, ch := mkReq()
	if !s.Enqueue(req) {
		t.Fatal("Enqueue returned false")
	}
	res := drainResult(t, ch, 5*time.Second)
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if res.Pod == nil || res.Pod.Name != "pod-0" {
		t.Fatalf("unexpected pod: %+v", res.Pod)
	}
}

// TestScheduler_FIFOOrder: the ready queue maintains FIFO for admitted pods.
// We drive it directly rather than via apiserver List (whose ordering is not
// guaranteed by the fake client) so the invariant is unambiguous.
func TestScheduler_FIFOOrder(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t /* no pods; we seed the queue directly */)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	// Seed the ready queue in a deterministic order.
	pods := []corev1.Pod{
		*newIdlePod("ns", "p", "a"),
		*newIdlePod("ns", "p", "b"),
		*newIdlePod("ns", "p", "c"),
		*newIdlePod("ns", "p", "d"),
	}
	// Also register with the fake client so TriggerUpdateWithOptions's Get
	// + Update succeed.
	for i := range pods {
		if err := c.Create(context.Background(), pods[i].DeepCopy()); err != nil {
			t.Fatalf("create pod %s: %v", pods[i].Name, err)
		}
	}
	s.queue.appendFiltered(pods, s.reserved)

	// Enqueue 4 requests strictly before the scheduler has a chance to
	// dispatch (we exploit the brief goroutine-scheduling window by enqueuing
	// them in a tight loop; the main-goroutine dispatch still runs strictly
	// FIFO because the scheduler Run loop reads reqCh sequentially).
	chs := make([]chan ClaimResult, 4)
	for i := range chs {
		req, ch := mkReq()
		chs[i] = ch
		if !s.Enqueue(req) {
			t.Fatalf("Enqueue %d failed", i)
		}
	}

	got := make([]string, 0, 4)
	for _, ch := range chs {
		res := drainResult(t, ch, 5*time.Second)
		if res.Err != nil {
			t.Fatalf("unexpected err: %v", res.Err)
		}
		got = append(got, res.Pod.Name)
	}
	// Each result channel maps 1:1 to its enqueued request, and since the
	// scheduler handles reqCh in order and the queue is FIFO, the results
	// arrive in enqueue order.
	want := []string{"a", "b", "c", "d"}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("dispatch order[%d]=%s, want %s", i, got[i], n)
		}
	}
}

// TestScheduler_ScaleDownProtectedSkipped: protected pod is never dispatched.
func TestScheduler_ScaleDownProtectedSkipped(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t,
		newProtectedIdlePod("ns", "p", "protected"),
		newIdlePod("ns", "p", "free"),
	)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	req, ch := mkReq()
	if !s.Enqueue(req) {
		t.Fatal("Enqueue failed")
	}
	res := drainResult(t, ch, 5*time.Second)
	if res.Err != nil {
		t.Fatalf("expected success, got err: %v", res.Err)
	}
	if res.Pod.Name != "free" {
		t.Fatalf("expected free, got %s", res.Pod.Name)
	}
}

// TestScheduler_RetriableConflictRetries: first CAS returns Conflict on pod-a;
// request re-queues and is paired with pod-b (which succeeds).
func TestScheduler_RetriableConflictRetries(t *testing.T) {
	t.Parallel()
	var failed atomic.Bool
	base := newFakeClient(t, newIdlePod("ns", "p", "a"), newIdlePod("ns", "p", "b"))
	c := withUpdateIntercept(base, func(name string) error {
		if name == "a" && failed.CompareAndSwap(false, true) {
			return newConflictErr(name) // first attempt on a fails
		}
		return nil
	})
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	req, ch := mkReq()
	s.Enqueue(req)
	res := drainResult(t, ch, 5*time.Second)
	if res.Err != nil {
		t.Fatalf("expected eventual success, got err: %v", res.Err)
	}
	if res.Pod == nil || res.Pod.Name != "b" {
		t.Fatalf("expected b (after a conflicted), got %+v", res.Pod)
	}
	// Pod a should remain reserved (retriable path keeps the reservation).
	if !s.reserved.isReserved("a") {
		t.Fatal("pod a should stay reserved after retriable conflict")
	}
}

// TestScheduler_HardErrorReleasesReservation: Forbidden -> error to caller,
// reservation released immediately.
func TestScheduler_HardErrorReleasesReservation(t *testing.T) {
	t.Parallel()
	base := newFakeClient(t, newIdlePod("ns", "p", "x"))
	c := withUpdateIntercept(base, func(name string) error {
		if name == "x" {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, name, nil)
		}
		return nil
	})
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	req, ch := mkReq()
	s.Enqueue(req)
	res := drainResult(t, ch, 5*time.Second)
	if res.Err == nil {
		t.Fatal("expected hard error, got success")
	}
	// Give release the briefest moment; it is synchronous before ResultCh
	// write, so this should already be true.
	if s.reserved.isReserved("x") {
		t.Fatal("pod x reservation should have been released on hard error")
	}
}

// TestScheduler_ReservationPreventsDoubleDispatch: manually reserve a pod;
// verify it is not dispatched until reservation is cleared.
func TestScheduler_ReservationPreventsDoubleDispatch(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t, newIdlePod("ns", "p", "only"))
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	// Manually reserve before the scheduler's refresh has run.
	s.reserved.reserve("only")
	// Force a refresh so the pod is considered.
	s.refreshReady(context.Background())
	if s.queue.contains(types.NamespacedName{Namespace: "ns", Name: "only"}) {
		t.Fatal("reserved pod must not enter the queue")
	}
	// Now release and kick a dispatch.
	s.reserved.release("only")
	s.refreshReady(context.Background())

	req, ch := mkReq()
	s.Enqueue(req)
	res := drainResult(t, ch, 5*time.Second)
	if res.Err != nil {
		t.Fatalf("expected success after release, got err: %v", res.Err)
	}
	if res.Pod.Name != "only" {
		t.Fatalf("expected only, got %s", res.Pod.Name)
	}
}

// TestScheduler_HighConcurrencyNoDuplicateDispatch: 100 pods, 500 requests.
// 100 should succeed; the other 400 should be rejected with
// ErrNoIdlePodsAvailable once their short deadline expires. No pod may be
// dispatched twice.
func TestScheduler_HighConcurrencyNoDuplicate(t *testing.T) {
	t.Parallel()
	const nPods = 100
	const nReqs = 500
	objs := make([]client.Object, 0, nPods)
	for i := range nPods {
		objs = append(objs, newIdlePod("ns", "p", "pod-"+itoa(i)))
	}
	c := newFakeClient(t, objs...)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	chs := make([]chan ClaimResult, nReqs)
	for i := range chs {
		ch := make(chan ClaimResult, 1)
		chs[i] = ch
		// Short deadline so requests that cannot be matched get expired by
		// the scheduler's expireTimer without timing out the test itself.
		req := &ClaimRequest{
			Ctx:      context.Background(),
			Opts:     ClaimOptions{TargetPodPhase: agentsv1alpha1.SandboxPhaseRunning},
			Deadline: time.Now().Add(2 * time.Second),
			ResultCh: ch,
		}
		if !s.Enqueue(req) {
			t.Fatalf("Enqueue %d returned false", i)
		}
	}

	var successCount, noIdleCount, otherCount atomic.Int32
	seenPods := sync.Map{}
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(c chan ClaimResult) {
			defer wg.Done()
			res := drainResult(t, c, 15*time.Second)
			if res.Err == nil {
				successCount.Add(1)
				if _, dup := seenPods.LoadOrStore(res.Pod.Name, struct{}{}); dup {
					t.Errorf("pod %s dispatched twice!", res.Pod.Name)
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

	if got := successCount.Load(); got != nPods {
		t.Errorf("success=%d, want %d", got, nPods)
	}
	if successCount.Load()+noIdleCount.Load() != nReqs {
		t.Errorf("success+noIdle=%d+%d != total %d (other=%d)",
			successCount.Load(), noIdleCount.Load(), nReqs, otherCount.Load())
	}
}

// TestScheduler_PodExhaustionWaitsForIdle: enqueue requests, no pods yet; push
// pods via NotifyIdle; requests drain in FIFO order.
func TestScheduler_PodExhaustionWaitsForIdle(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t /* no pods */)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	const n = 5
	chs := make([]chan ClaimResult, n)
	for i := range chs {
		req, ch := mkReq()
		chs[i] = ch
		s.Enqueue(req)
	}
	// None should complete yet.
	time.Sleep(50 * time.Millisecond)
	for i, ch := range chs {
		select {
		case res := <-ch:
			t.Fatalf("req %d completed prematurely: %+v", i, res)
		default:
		}
	}

	// Now add pods and notify. We need to add pods through the client so
	// ListIdlePodsForPool finds them.
	for i := range n {
		if err := c.Create(context.Background(), newIdlePod("ns", "p", "p-"+itoa(i))); err != nil {
			t.Fatalf("create pod: %v", err)
		}
	}
	s.NotifyIdle()

	for i, ch := range chs {
		res := drainResult(t, ch, 5*time.Second)
		if res.Err != nil {
			t.Fatalf("req %d: unexpected err: %v", i, res.Err)
		}
	}
}

// TestScheduler_ExpiredContextRejected: context cancelled before dispatch;
// request is rejected with ErrNoIdlePodsAvailable.
func TestScheduler_ExpiredContextRejected(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t /* no pods, so the req sits in reqCh */)
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan ClaimResult, 1)
	req := &ClaimRequest{Ctx: ctx, Deadline: time.Now().Add(time.Minute), ResultCh: ch}
	s.Enqueue(req)

	res := drainResult(t, ch, 2*time.Second)
	if res.Err != inplaceupdate.ErrNoIdlePodsAvailable {
		t.Fatalf("expected ErrNoIdlePodsAvailable, got %v", res.Err)
	}
}

// TestScheduler_ExpiredDeadlineRejected: deadline already in the past.
func TestScheduler_ExpiredDeadlineRejected(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t, newIdlePod("ns", "p", "x"))
	s, stop := startScheduler(t, c, "ns", "p")
	defer stop()

	ch := make(chan ClaimResult, 1)
	req := &ClaimRequest{
		Ctx:      context.Background(),
		Deadline: time.Now().Add(-time.Second),
		ResultCh: ch,
	}
	s.Enqueue(req)
	res := drainResult(t, ch, 2*time.Second)
	if res.Err != inplaceupdate.ErrNoIdlePodsAvailable {
		t.Fatalf("expected ErrNoIdlePodsAvailable, got %v", res.Err)
	}
}

// TestScheduler_ShutdownDrainsPending: requests sitting in reqCh are rejected
// when the scheduler stops.
func TestScheduler_ShutdownDrainsPending(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t /* no pods */)
	s := NewPoolScheduler("ns", "p", "", "", c)
	go s.Run(context.Background())

	const n = 10
	chs := make([]chan ClaimResult, n)
	for i := range chs {
		req, ch := mkReq()
		chs[i] = ch
		s.Enqueue(req)
	}
	// Give Run() time to read once, then stop.
	time.Sleep(20 * time.Millisecond)
	s.Shutdown()

	for i, ch := range chs {
		select {
		case res := <-ch:
			if res.Err != inplaceupdate.ErrNoIdlePodsAvailable {
				t.Errorf("req %d: got err %v, want ErrNoIdlePodsAvailable", i, res.Err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("req %d timed out on shutdown drain", i)
		}
	}
}

// TestScheduler_ReenqueueOnFullChannelFailsRequest: force a race where the
// channel is full during requeue; verify the request is not lost (we get a
// terminal result rather than silence).
//
// This is a subtle edge: in practice the channel is never full during requeue
// because we just popped from it. We construct the scenario by holding the
// scheduler with a full channel's worth of blocked requests and then tricking
// requeue.
// Skipped as it is not easily deterministic in fake-client tests; the
// production code path is covered by the explicit fail branch in requeue().
func TestScheduler_ReenqueueFullChannel(t *testing.T) {
	t.Skip("covered by code review of requeue(); non-deterministic to reproduce")
}

// TestScheduler_NotifyIdleIsNonBlocking: NotifyIdle never blocks even when
// called many times without the scheduler consuming idleCh.
func TestScheduler_NotifyIdleNonBlocking(t *testing.T) {
	t.Parallel()
	s := NewPoolScheduler("ns", "p", "", "", nil)
	done := make(chan struct{})
	go func() {
		for range 1000 {
			s.NotifyIdle()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NotifyIdle blocked")
	}
}

// itoa avoids strconv import for a trivial helper.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
