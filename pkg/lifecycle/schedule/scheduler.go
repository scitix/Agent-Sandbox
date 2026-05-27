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

// Package schedule implements the per-pool streaming claim scheduler.
//
// Streaming model (vs. the retired batch flight):
//
//   - A persistent ready-queue holds idle pods observed via informer cache.
//     notifyIdle() triggers an incremental refresh that appends newly-idle
//     pods in arrival order; no global sort is performed on the hot path.
//
//   - ClaimRequest arrivals pop one pod and spawn a goroutine that executes
//     inplaceupdate.TriggerUpdateWithOptions. The scheduler goroutine never
//     blocks on the apiserver round-trip, so new requests are handled as
//     fast as they arrive.
//
//   - A TTL-bounded reservation map (see reservations.go) prevents a pod from
//     being re-selected while its phase transition is still propagating from
//     apiserver → informer cache. This is the main defence against the
//     ErrUnexpectedPodPhase conflict observed under high QPS in the batch
//     implementation.
//
// See /home/ylli/.claude/plans/spicy-pondering-kahn.md for the design note.
package schedule

import (
	"context"
	"maps"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/lifecycle/inplaceupdate"
	pkgmetrics "github.com/scitix/agent-sandbox/pkg/metrics"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

const (
	// reqChanCap caps the number of pending claim requests per pool.
	// Enqueue returns false when the channel is full; callers should convert
	// that into HTTP 429. 8192 is generous: at the documented peak of ~65/s
	// per pool this is >100 seconds of headroom, which is longer than any
	// realistic wait-for-idle window, while the memory cost (~24 B per
	// ClaimRequest pointer + overhead) stays under ~300 KB per scheduler.
	reqChanCap = 8192

	// reservationTTL is how long a pod stays reserved after being handed to a
	// CAS goroutine. It must cover the informer cache sync latency window
	// (watch event propagation after c.Update: typically 50-100 ms, P99
	// sub-second) with comfortable margin, but must stay well below the time
	// a pod takes to legitimately return to Idle (Starting → Running → use →
	// Stopping → Idle, at minimum tens of seconds).
	reservationTTL = 2 * time.Second

	// refreshMinInterval throttles ListIdlePodsForPool + queue admission.
	// refreshReady is called on many wakeups; this avoids burning CPU on
	// back-to-back refreshes when the informer cache has not advanced.
	refreshMinInterval = 50 * time.Millisecond

	// idleNotifyDelay is the time we wait after NotifyIdle() before actually
	// calling refreshReady(). The controller calls NotifyIdleAvailable()
	// immediately after c.Update(pod, phase=idle) returns, but the informer
	// watch event that updates the cache typically arrives 50-100 ms later
	// (P99 < 300 ms). Firing refreshReady() too early returns a stale cache
	// view where the just-recycled pod is still "Stopping", so the scheduler
	// misses it and falls back to the 10-second pollTimer.
	// 200 ms covers P99 informer propagation with comfortable margin while
	// still being far faster than the 10-second poll fallback.
	idleNotifyDelay = 200 * time.Millisecond

	// pollInterval is the base retry period; it grows exponentially up to
	// pollIntervalMax when flights find no idle pods while requests are
	// pending, and resets on any successful dispatch.
	pollInterval    = 10 * time.Second
	pollIntervalMax = 5 * time.Minute

	// expireCheckInterval is how often expireTimer fires to evict expired
	// requests and sweep expired reservations, so short-deadline callers
	// don't wait up to pollInterval for a response.
	expireCheckInterval = 300 * time.Millisecond

	// lowWaterMark triggers a best-effort refreshReady when the queue drops
	// below this threshold during a request arrival, pre-fetching idle pods
	// before the queue empties entirely.
	lowWaterMark = 4

	// maxInflightCAS caps the number of TriggerUpdateWithOptions goroutines
	// running concurrently for one pool. Beyond this, new dispatches block
	// until a slot frees. Generous: apiserver can handle much more, but a
	// bound prevents pathological fan-out if something goes wrong upstream.
	maxInflightCAS = 128

	// statusWriteInterval bounds how often the throttled status writer can
	// patch SandboxPool.Status.PendingRequests. Picked to be slow enough
	// that 100 active pools generate well under 1 K patches/min total, but
	// quick enough that Dashboard observability lags by no more than a
	// handful of seconds.
	statusWriteInterval = 3 * time.Second

	// statusWriteRelativeDelta is the relative change in queue length that
	// triggers a patch within an interval. Below this threshold the writer
	// skips the patch — drift from the prior reported value stays bounded
	// by 20 % per cycle.
	statusWriteRelativeDelta = 0.2
)

// ClaimResult is the outcome of a single Enqueue/claim attempt.
type ClaimResult struct {
	Pod *corev1.Pod
	Err error
}

// Snapshot is a point-in-time view of a PoolScheduler's internal counters.
// All fields are read with atomic / channel-len semantics; calling Snapshot
// is safe from any goroutine and does not interact with the apiserver.
//
// EnvScheduler reads Snapshots on the request hot path to rank candidate
// member pools, so the cost must stay in the tens-of-nanoseconds range.
type Snapshot struct {
	// IdleReady is the number of idle Pods currently admitted to the ready
	// queue and not reserved — what would be popped if a request arrived now.
	IdleReady int
	// QueueLen is the total number of ClaimRequests this scheduler is
	// holding that have not yet reached a terminal result. Counts both
	// requests still in the producer→consumer channel AND requests
	// parked inside the scheduler goroutine waiting for an idle pod.
	// Sourced from an atomic counter so the value stays stable while a
	// request transiently moves between buffers (the autoscaler relies
	// on this for its reactive demand signal).
	QueueLen int
	// ReservedCount is the number of pods currently reserved (handed off to
	// a CAS goroutine but not yet observed back as Idle/Starting through the
	// informer cache).
	ReservedCount int
	// InflightCAS is the number of TriggerUpdateWithOptions goroutines
	// currently running. Useful for diagnosing apiserver back-pressure.
	InflightCAS int
	// LastDispatchAt is the wall-clock time of the most recent successful
	// dispatch (doCAS OK branch). Zero when no dispatch has ever succeeded.
	LastDispatchAt time.Time
}

// ClaimOptions carries the per-request options applied to the target pod when
// the CAS succeeds. Fields map 1:1 onto inplaceupdate.UpdateOptions.
type ClaimOptions struct {
	ContainerImages map[string]string
	Labels          map[string]string
	Annotations     map[string]string
	// TargetPodPhase defaults to SandboxPhaseRunning when empty.
	TargetPodPhase string
}

// ClaimRequest is one pending claim awaiting an idle pod.
type ClaimRequest struct {
	Ctx      context.Context
	Opts     ClaimOptions
	Deadline time.Time
	ResultCh chan<- ClaimResult
	// EnqueuedAt is set by Enqueue() and read by doCAS to record the
	// end-to-end dispatch latency via ScheduleDispatchLatencySeconds. Callers
	// may leave it zero; Enqueue will populate it if so.
	EnqueuedAt time.Time
}

// isExpired reports whether the request's deadline has passed or its context
// is cancelled.
func (r *ClaimRequest) isExpired() bool {
	if r.Ctx != nil && r.Ctx.Err() != nil {
		return true
	}
	return !r.Deadline.IsZero() && time.Now().After(r.Deadline)
}

// PoolScheduler is a per-pool background goroutine that streams claim requests
// to idle pods as they become available.
type PoolScheduler struct {
	poolNS   string
	poolName string
	team     string
	user     string

	k8sClient client.Client

	reqCh     chan *ClaimRequest
	triggerCh chan struct{}
	idleCh    chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}

	queue    *readyQueue
	reserved *reservations

	inflightCAS  chan struct{}
	lastRefresh  atomic.Int64 // unix nanos; 0 means never
	lastDispatch atomic.Int64 // unix nanos of most recent successful doCAS; 0 means never

	// pendingClaims tracks every in-flight ClaimRequest — incremented
	// when Enqueue admits one, decremented when a terminal result
	// (success / hard error / expired) lands on its ResultCh. The
	// retriable-CAS path does NOT decrement: the request is still
	// in flight, just re-circulating through reqCh.
	//
	// Exposed to external readers via Snapshot.QueueLen because
	// len(reqCh) alone is racy — a request transiently disappears
	// between the Run loop's pop and the no-pod park, which would
	// otherwise let the Pool autoscaler read 0 mid-bounce and
	// conclude "no reactive demand" when there genuinely is some.
	pendingClaims atomic.Int32
}

// Snapshot returns a point-in-time view of the scheduler's internal counters.
// See the Snapshot doc comment for field semantics. Safe to call from any
// goroutine; cost is O(1) atomic / channel-len reads.
func (s *PoolScheduler) Snapshot() Snapshot {
	var lastDispatch time.Time
	if v := s.lastDispatch.Load(); v != 0 {
		lastDispatch = time.Unix(0, v)
	}
	return Snapshot{
		IdleReady:      s.queue.len(),
		QueueLen:       int(s.pendingClaims.Load()),
		ReservedCount:  s.reserved.size(),
		InflightCAS:    len(s.inflightCAS),
		LastDispatchAt: lastDispatch,
	}
}

// NewPoolScheduler allocates a scheduler without starting its goroutine.
// team/user identify the owning pool for metrics labelling. k8sClient may
// be nil; in that case the background status writer that mirrors the
// in-memory queue length onto SandboxPool.Status.PendingRequests is
// skipped (useful for unit tests that exercise only the in-process
// dispatch path).
func NewPoolScheduler(ns, name, team, user string, k8sClient client.Client) *PoolScheduler {
	return &PoolScheduler{
		poolNS:      ns,
		poolName:    name,
		team:        team,
		user:        user,
		k8sClient:   k8sClient,
		reqCh:       make(chan *ClaimRequest, reqChanCap),
		triggerCh:   make(chan struct{}, 1),
		idleCh:      make(chan struct{}, 1),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		queue:       newReadyQueue(),
		reserved:    newReservations(reservationTTL, nil),
		inflightCAS: make(chan struct{}, maxInflightCAS),
	}
}

// Enqueue hands req to the scheduler. Returns false (and does NOT write to
// ResultCh) when the internal channel is full; the caller must translate that
// into backpressure.
func (s *PoolScheduler) Enqueue(req *ClaimRequest) bool {
	if req.EnqueuedAt.IsZero() {
		req.EnqueuedAt = time.Now()
	}
	select {
	case s.reqCh <- req:
		s.pendingClaims.Add(1)
		// Wake the scheduler goroutine so it dispatches without waiting for
		// the poll timer.
		select {
		case s.triggerCh <- struct{}{}:
		default:
		}
		return true
	default:
		return false
	}
}

// failRequest is the single chokepoint for sending a terminal result with
// an error back to the caller. It:
//
//   - emits a V(2) log line with the diagnostic context (reason, ctx.Err,
//     deadline) so debug sessions can correlate scheduler decisions
//     against caller-visible failures;
//   - decrements pendingClaims so Snapshot.QueueLen stays accurate;
//   - sends on req.ResultCh non-blocking — callers are expected to
//     provide a buffered channel of capacity ≥ 1 so the first send
//     always succeeds even after the caller goroutine returned (e.g.
//     HTTP client disconnect). On the rare full-channel case we drop
//     the send rather than wedge the scheduler goroutine.
func (s *PoolScheduler) failRequest(req *ClaimRequest, err error, reason string) {
	var ctxErr error
	if req.Ctx != nil {
		ctxErr = req.Ctx.Err()
	}
	klog.V(2).InfoS("schedule: failing claim request",
		"pool", s.poolNS+"/"+s.poolName,
		"reason", reason,
		"err", err,
		"ctxErr", ctxErr,
		"deadline", req.Deadline,
		"enqueuedAt", req.EnqueuedAt,
	)
	select {
	case req.ResultCh <- ClaimResult{Err: err}:
	default:
		klog.V(3).InfoS("schedule: result channel full while failing request — caller already drained or disconnected",
			"pool", s.poolNS+"/"+s.poolName, "reason", reason)
	}
	s.pendingClaims.Add(-1)
}

// NotifyIdle is called when a pod transitions Stopping → Idle for this pool.
// It schedules a delayed wake of the scheduler so the informer cache has time
// to reflect the apiserver write before refreshReady() runs its List call.
// The delay (idleNotifyDelay ≈ 200 ms) covers P99 informer propagation latency;
// without it refreshReady() fires against a stale cache that still reports the
// pod as Stopping and misses it, causing the scheduler to fall back to the
// 10-second pollTimer.
// Safe to call from any goroutine.
func (s *PoolScheduler) NotifyIdle() {
	go func() {
		select {
		case <-s.stopCh:
			return
		case <-time.After(idleNotifyDelay):
		}
		select {
		case s.idleCh <- struct{}{}:
		default:
		}
	}()
}

// Shutdown stops the scheduler goroutine and blocks until it has drained
// pending requests.
func (s *PoolScheduler) Shutdown() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	<-s.doneCh
}

// Run is the scheduler goroutine; call it exactly once per scheduler.
func (s *PoolScheduler) Run(ctx context.Context) {
	// `waiting` holds requests that have already been pulled off
	// reqCh but could not be paired with an idle pod yet. It is the
	// goroutine-local "parking lot" — only this goroutine ever reads
	// or writes it, so no synchronisation is needed. Parking here
	// instead of bouncing back into reqCh eliminates the hot-spin
	// where `case req := <-s.reqCh` would otherwise fire on every
	// iteration of the select while a request can't be served.
	var waiting []*ClaimRequest

	defer func() {
		// Shutdown: surface every still-pending request as a failure
		// so callers unblock. drainAll covers reqCh; the loop covers
		// waiting.
		for _, req := range waiting {
			s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "scheduler shutdown")
		}
		s.drainAll(inplaceupdate.ErrNoIdlePodsAvailable)
		s.updateGauges()
		close(s.doneCh)
	}()

	// Prime the queue on start so the first request does not pay the cost of
	// a full List+admit on its hot path.
	s.refreshReady(ctx)

	// Throttled mirror of QueueLen onto Pool.Status.PendingRequests. Runs in
	// its own goroutine because patches go through the apiserver and we
	// don't want them on the dispatch hot path. No-op when k8sClient is nil.
	go s.runStatusWriter(ctx)

	curPollInterval := pollInterval
	pollTimer := time.NewTimer(curPollInterval)
	defer pollTimer.Stop()
	expireTimer := time.NewTimer(expireCheckInterval)
	defer expireTimer.Stop()

	for {
		select {
		case <-s.stopCh:
			return

		case req := <-s.reqCh:
			waiting = s.handleRequest(ctx, req, waiting)

		case <-s.idleCh:
			// A Stopping → Idle transition completed; new pods are likely
			// visible. Refresh and try to drain the parking lot.
			s.refreshReady(ctx)
			waiting = s.dispatchFromWaiting(ctx, waiting)
			curPollInterval = pollInterval
			resetTimer(pollTimer, curPollInterval)

		case <-s.triggerCh:
			// A new request arrived (or a prior CAS reported retriable).
			// Drain the parking lot — the new pod that came back from a
			// retriable CAS may now be eligible after its reservation
			// TTL expires, and a fresh refreshReady already ran during
			// handleRequest if needed.
			waiting = s.dispatchFromWaiting(ctx, waiting)
			curPollInterval = pollInterval
			resetTimer(pollTimer, curPollInterval)

		case <-pollTimer.C:
			// Fallback: informer may have missed a wakeup. Refresh and
			// drain. Advance back-off only when nothing changed and
			// work still exists.
			s.refreshReady(ctx)
			before := len(waiting)
			waiting = s.dispatchFromWaiting(ctx, waiting)
			switch {
			case len(waiting) < before:
				curPollInterval = pollInterval
			case len(waiting) > 0 || s.hasPending():
				curPollInterval = nextPollInterval(curPollInterval)
				klog.V(5).InfoS("schedule: no idle pods, backing off",
					"pool", s.poolNS+"/"+s.poolName,
					"waiting", len(waiting),
					"nextInterval", curPollInterval)
			default:
				curPollInterval = pollInterval
			}
			resetTimer(pollTimer, curPollInterval)

		case <-expireTimer.C:
			waiting = s.filterExpiredWaiting(waiting)
			s.expireReqCh()
			if n := s.reserved.sweep(); n > 0 {
				pkgmetrics.ScheduleReservationTTLExpiredTotal.With(s.plabels()).Add(float64(n))
			}
			// Doubling the expire tick as a low-rate dispatch retry
			// is what keeps a parked request progressing when the
			// idleCh / triggerCh / pollTimer wake-ups don't fire
			// (refresh got throttled, a reservation TTL just freed
			// a pod, NotifyIdle was missed by the upstream, etc.).
			// expireCheckInterval = 300 ms is short enough to honour
			// sub-second wait SLOs and far slower than the spin the
			// previous design accidentally produced.
			if len(waiting) > 0 {
				s.refreshReady(ctx)
				waiting = s.dispatchFromWaiting(ctx, waiting)
			}
			s.updateGauges()
			expireTimer.Reset(expireCheckInterval)
		}
	}
}

// ---------------------------------------------------------------------------
// Request handling
// ---------------------------------------------------------------------------

// handleRequest processes one request fresh off reqCh. If an idle pod is
// available it spawns the doCAS goroutine immediately; otherwise the
// request is parked in `waiting` and returned for the caller (the Run
// loop) to keep track of. handleRequest never blocks on apiserver
// calls — refreshReady is throttled and async-friendly.
//
// The returned slice is `waiting` itself (possibly with one element
// appended); the caller MUST replace its local variable with the
// returned value, since append may realloc.
func (s *PoolScheduler) handleRequest(ctx context.Context, req *ClaimRequest, waiting []*ClaimRequest) []*ClaimRequest {
	if req.isExpired() {
		s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "expired before dispatch")
		return waiting
	}

	pod, ok, discarded := s.queue.popUnreservedAndReserve(ctx, s.k8sClient, s.reserved)
	if discarded > 0 {
		pkgmetrics.ScheduleReadyQueueEvictedTotal.With(s.plabels()).Add(float64(discarded))
	}
	if ok {
		go s.doCAS(req, pod)
		s.updateGauges()
		return waiting
	}

	// No pod — park the request. The autoscaler sees the pending
	// demand via pendingClaims (Snapshot.QueueLen); the next
	// idleCh / triggerCh / pollTimer event will run
	// dispatchFromWaiting to revisit.
	waiting = append(waiting, req)
	klog.V(4).InfoS("schedule: parked request waiting for idle pod",
		"pool", s.poolNS+"/"+s.poolName,
		"waiting", len(waiting),
		"idleReady", s.queue.len(),
	)

	// Best-effort refresh in case the ready queue is just behind the
	// informer cache. dispatchFromWaiting will retry if refresh
	// admitted something usable.
	if s.queue.len() < lowWaterMark {
		s.refreshReady(ctx)
		waiting = s.dispatchFromWaiting(ctx, waiting)
	}
	s.updateGauges()
	return waiting
}

// dispatchFromWaiting walks the parking lot in arrival order and pairs
// each request with an idle pod. As soon as the ready queue runs dry,
// the remaining requests are kept in place (FIFO order preserved).
// Expired requests are dropped with a failure result regardless of pod
// availability.
//
// The returned slice replaces the input — append may realloc, so the
// caller MUST use the returned value.
func (s *PoolScheduler) dispatchFromWaiting(ctx context.Context, waiting []*ClaimRequest) []*ClaimRequest {
	if len(waiting) == 0 {
		return waiting
	}
	// out reuses the underlying array — safe because every read from
	// `waiting[i]` happens before the corresponding write to
	// `out[len(out)]` (sequential single-goroutine traversal).
	out := waiting[:0]
	for i, req := range waiting {
		if req.isExpired() {
			s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "expired while waiting")
			continue
		}
		pod, ok, discarded := s.queue.popUnreservedAndReserve(ctx, s.k8sClient, s.reserved)
		if discarded > 0 {
			pkgmetrics.ScheduleReadyQueueEvictedTotal.With(s.plabels()).Add(float64(discarded))
		}
		if !ok {
			// No more pods — keep this request and everything after
			// it in their original order.
			out = append(out, waiting[i:]...)
			break
		}
		go s.doCAS(req, pod)
	}
	s.updateGauges()
	return out
}

// filterExpiredWaiting removes expired requests from the parking lot
// (called from the expireTimer tick). Keeps everything else in place.
func (s *PoolScheduler) filterExpiredWaiting(waiting []*ClaimRequest) []*ClaimRequest {
	if len(waiting) == 0 {
		return waiting
	}
	out := waiting[:0]
	for _, req := range waiting {
		if req.isExpired() {
			s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "expired in waiting (expireTick)")
			continue
		}
		out = append(out, req)
	}
	return out
}

// requeue returns req to reqCh for the doCAS retriable path: the pod
// turned out to be unusable (phase race / not-found / conflict) but
// the request itself is still valid and should be re-attempted by the
// next dispatch cycle. If reqCh is unexpectedly full the request is
// failed instead of dropped silently.
func (s *PoolScheduler) requeue(req *ClaimRequest) {
	select {
	case s.reqCh <- req:
	default:
		s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "reqCh full on requeue (retriable CAS)")
	}
}

// wake nudges the scheduler to run tryDispatchPending ASAP.
func (s *PoolScheduler) wake() {
	select {
	case s.triggerCh <- struct{}{}:
	default:
	}
}

// doCAS runs TriggerUpdateWithOptions for one request + pod pair. It is always
// invoked from a fresh goroutine so the scheduler loop does not block on the
// apiserver round-trip.
func (s *PoolScheduler) doCAS(req *ClaimRequest, pod corev1.Pod) {
	s.inflightCAS <- struct{}{}
	defer func() { <-s.inflightCAS }()

	// Re-check expiry after acquiring the inflight slot. A request may
	// have been valid when it was popped from the queue, but if the
	// caller's context was cancelled (HTTP disconnect) while we were
	// waiting for a slot we must not claim the pod — the result
	// channel may have no reader and the pod would be stranded in
	// Starting until startup-timeout fires.
	if req.isExpired() {
		// Release the reservation so a later refreshReady can re-admit
		// this pod for a different request. Fail the expired request
		// directly — there's no value in keeping it alive any longer.
		s.reserved.release(pod.Name)
		s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "expired after inflight slot acquire")
		pkgmetrics.ScheduleCASOutcomeTotal.With(outcomeLabels(s.plabels(), "expired")).Inc()
		return
	}

	// Dispatch latency = enqueue → CAS start. Captures queue, trigger, and
	// handoff slices; the apiserver RTT itself is recorded separately via
	// InplaceUpdateTotal inside TriggerUpdateWithOptions.
	if !req.EnqueuedAt.IsZero() {
		pkgmetrics.ScheduleDispatchLatencySeconds.With(s.plabels()).Observe(time.Since(req.EnqueuedAt).Seconds())
	}

	targetPhase := req.Opts.TargetPodPhase
	if targetPhase == "" {
		targetPhase = agentsv1alpha1.SandboxPhaseRunning
	}

	candidate := pod.DeepCopy()
	fresh, updateErr := inplaceupdate.TriggerUpdateWithOptions(req.Ctx, s.k8sClient, candidate, inplaceupdate.UpdateOptions{
		ContainerImages:             req.Opts.ContainerImages,
		Labels:                      req.Opts.Labels,
		Annotations:                 req.Opts.Annotations,
		TargetPodPhase:              targetPhase,
		UpdatePodPhase:              agentsv1alpha1.SandboxPhaseStarting,
		ExpectedCurrentSandboxPhase: agentsv1alpha1.SandboxPhaseIdle,
		RemoveAnnotations:           []string{agentsv1alpha1.SandboxScaleDownProtectedAnnotationKey},
		DisableRetry:                true,
	})

	labels := s.plabels()
	switch {
	case updateErr == nil:
		// Non-blocking send — caller's ResultCh is expected to have
		// buffer ≥ 1. If the caller already disconnected we still
		// surface the success outcome via metrics + counter.
		select {
		case req.ResultCh <- ClaimResult{Pod: fresh}:
		default:
			klog.V(3).InfoS("schedule: result channel full on success (caller drained or disconnected)",
				"pool", s.poolNS+"/"+s.poolName, "pod", pod.Name)
		}
		s.pendingClaims.Add(-1)
		s.lastDispatch.Store(time.Now().UnixNano())
		pkgmetrics.ScheduleCASOutcomeTotal.With(outcomeLabels(labels, "success")).Inc()
		// Reservation stays; TTL sweep evicts it after the informer catches up.

	case updateErr == inplaceupdate.ErrUnexpectedPodPhase || errors.IsConflict(updateErr) || errors.IsNotFound(updateErr):
		// Another actor beat us to the pod, or the pod was deleted
		// between our cache-backed pop and the CAS Update (common
		// during scale-down). Put the request back and keep the
		// reservation — the pod is no longer Idle / no longer exists,
		// so we must not re-admit it via a stale-cache refresh in
		// the next few iterations. pendingClaims stays unchanged
		// because the request is still in flight.
		klog.V(4).InfoS("schedule: CAS reported retriable conflict; requeueing claim",
			"pool", s.poolNS+"/"+s.poolName, "pod", pod.Name, "err", updateErr)
		s.requeue(req)
		s.wake()
		pkgmetrics.ScheduleCASOutcomeTotal.With(outcomeLabels(labels, "retriable")).Inc()

	default:
		// Hard error (Forbidden, transport error, etc.). Release the
		// reservation so a future refresh may try this pod again.
		s.reserved.release(pod.Name)
		s.failRequest(req, updateErr, "CAS hard error")
		pkgmetrics.ScheduleCASOutcomeTotal.With(outcomeLabels(labels, "hard")).Inc()
	}
}

// ---------------------------------------------------------------------------
// Ready-queue maintenance
// ---------------------------------------------------------------------------

// refreshReady lists the pool's idle pods from the informer cache and admits
// them into the queue. Throttled by refreshMinInterval so back-to-back wakeups
// don't burn CPU re-listing the same cache generation.
func (s *PoolScheduler) refreshReady(ctx context.Context) {
	now := time.Now().UnixNano()
	last := s.lastRefresh.Load()
	if now-last < int64(refreshMinInterval) {
		pkgmetrics.ScheduleRefreshTotal.With(outcomeLabels(s.plabels(), "throttled")).Inc()
		return
	}
	if !s.lastRefresh.CompareAndSwap(last, now) {
		pkgmetrics.ScheduleRefreshTotal.With(outcomeLabels(s.plabels(), "throttled")).Inc()
		return
	}

	pods, err := indexer.ListIdlePodsForPool(ctx, s.k8sClient, s.poolNS, s.poolName)
	if err != nil {
		pkgmetrics.ScheduleRefreshTotal.With(outcomeLabels(s.plabels(), "error")).Inc()
		klog.V(4).InfoS("schedule: ListIdlePodsForPool failed",
			"pool", s.poolNS+"/"+s.poolName, "error", err)
		return
	}

	// Clear expired reservations before admitting; otherwise a pod that was
	// claimed-and-finished a full TTL ago would still be filtered out here.
	if n := s.reserved.sweep(); n > 0 {
		pkgmetrics.ScheduleReservationTTLExpiredTotal.With(s.plabels()).Add(float64(n))
	}

	_, skippedProtected := s.queue.appendFiltered(pods, s.reserved)
	if skippedProtected > 0 {
		pkgmetrics.ScheduleSkippedScaleDownProtectedTotal.With(s.plabels()).Add(float64(skippedProtected))
	}
	pkgmetrics.ScheduleRefreshTotal.With(outcomeLabels(s.plabels(), "ok")).Inc()
	s.updateGauges()
}

// ---------------------------------------------------------------------------
// Utility: request-channel maintenance, scale-up signalling, metrics
// ---------------------------------------------------------------------------

// hasPending reports whether any request is still in flight — covers
// both reqCh-buffered and parking-lot-waiting requests via the
// atomic counter.
func (s *PoolScheduler) hasPending() bool {
	return s.pendingClaims.Load() > 0
}

// expireReqCh discards expired requests sitting in reqCh that the Run
// loop has not yet picked up. Inspects exactly len(reqCh) items at
// entry so a caller that keeps enqueueing can't live-lock this
// helper.
func (s *PoolScheduler) expireReqCh() {
	n := len(s.reqCh)
	if n == 0 {
		return
	}
	live := make([]*ClaimRequest, 0, n)
outer:
	for range n {
		select {
		case req := <-s.reqCh:
			if req.isExpired() {
				s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "expired in reqCh (expireTick)")
			} else {
				live = append(live, req)
			}
		default:
			break outer
		}
	}
	for _, req := range live {
		select {
		case s.reqCh <- req:
		default:
			s.failRequest(req, inplaceupdate.ErrNoIdlePodsAvailable, "reqCh full while re-enqueueing live request")
		}
	}
}

// drainAll rejects every remaining request in reqCh with err. Called
// from the Run() defer; must not be called while anything else is
// consuming reqCh.
func (s *PoolScheduler) drainAll(err error) {
	for {
		select {
		case req := <-s.reqCh:
			s.failRequest(req, err, "drainAll")
		default:
			return
		}
	}
}

// shouldWriteStatus decides whether the throttled status writer should issue
// a patch this cycle. Rules:
//   - last == -1 (first observation) — always write so Dashboard sees the
//     initial value rather than waiting for the first material change.
//   - last and cur differ in zero-ness (one is 0, the other > 0) — always
//     write to surface the "idle/busy" transition.
//   - relative change |cur-last|/max(last,1) >= statusWriteRelativeDelta —
//     write.
//   - otherwise — skip.
func shouldWriteStatus(last, cur int32) bool {
	if last < 0 {
		return true
	}
	if (last == 0) != (cur == 0) {
		return true
	}
	if last == 0 {
		return false
	}
	diff := cur - last
	if diff < 0 {
		diff = -diff
	}
	return float64(diff)/float64(last) >= statusWriteRelativeDelta
}

// runStatusWriter periodically mirrors the in-process QueueLen onto
// SandboxPool.Status.PendingRequests so it's visible via kubectl / Dashboard
// without scraping in-process state. Throttled by statusWriteInterval +
// statusWriteRelativeDelta to keep apiserver writes well-bounded even when
// many pools are active. Started from Run(); no-op when k8sClient is nil
// (unit-test mode).
func (s *PoolScheduler) runStatusWriter(ctx context.Context) {
	if s.k8sClient == nil {
		return
	}
	var last int32 = -1
	t := time.NewTicker(statusWriteInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			cur := int32(len(s.reqCh))
			if !shouldWriteStatus(last, cur) {
				continue
			}
			if err := patchPoolPendingRequests(ctx, s.k8sClient, s.poolNS, s.poolName, cur); err != nil {
				klog.V(4).ErrorS(err, "schedule: patch pendingRequests failed (will retry next tick)",
					"pool", s.poolNS+"/"+s.poolName, "value", cur)
				continue
			}
			last = cur
		}
	}
}

// patchPoolPendingRequests patches Status.PendingRequests on the named pool
// using the Status subresource. RetryOnConflict handles concurrent status
// updates from the pool reconciler.
func patchPoolPendingRequests(ctx context.Context, c client.Client, ns, name string, value int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pool := &agentsv1alpha1.SandboxPool{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, pool); err != nil {
			return err
		}
		if pool.Status.PendingRequests == value {
			return nil
		}
		base := pool.DeepCopy()
		pool.Status.PendingRequests = value
		return c.Status().Patch(ctx, pool, client.MergeFrom(base))
	})
}

// plabels returns the {namespace, pool} label set reused across metrics.
func (s *PoolScheduler) plabels() prometheus.Labels {
	return prometheus.Labels{
		"namespace": s.poolNS,
		"pool":      s.poolName,
		"team":      s.team,
		"user":      s.user,
	}
}

// outcomeLabels clones l and adds outcome=v.
func outcomeLabels(l prometheus.Labels, v string) prometheus.Labels {
	out := make(prometheus.Labels, len(l)+1)
	maps.Copy(out, l)
	out["outcome"] = v
	return out
}

// updateGauges refreshes the ready-queue and reservations size gauges.
func (s *PoolScheduler) updateGauges() {
	labels := s.plabels()
	pkgmetrics.ScheduleReadyQSize.With(labels).Set(float64(s.queue.len()))
	pkgmetrics.ScheduleReservationsSize.With(labels).Set(float64(s.reserved.size()))
}

// nextPollInterval doubles cur, caps at pollIntervalMax, and applies ±20%
// jitter so many pools don't sync their retries.
func nextPollInterval(cur time.Duration) time.Duration {
	next := min(cur*2, pollIntervalMax)
	jitter := time.Duration(float64(next) * 0.2 * (2*rand.Float64() - 1))
	return next + jitter
}

// resetTimer drains and resets t to d using the documented safe pattern.
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}
