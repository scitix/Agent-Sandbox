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
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	agentsv1alpha1 "github.com/scitix/agent-sandbox/api/v1alpha1"
	"github.com/scitix/agent-sandbox/pkg/activity"
	"github.com/scitix/agent-sandbox/pkg/utils/indexer"
)

// ActivityFlusher publishes what this replica has observed into the one place
// every other component can see: the `last-active` annotation on the sandbox's
// Pod.
//
// Why it exists. The ActivityTracker is per-replica memory, and with more than
// one gateway replica no single replica — and therefore no reader — can see all
// of a sandbox's traffic. The annotation is a rendezvous every replica can
// write and the controller can read, so each replica contributes what it saw
// and the union is what comes out.
//
// Why it converges. A replica writes only when the annotation is *older* than
// what it has observed (so a newer value is never rolled back) and *stale
// enough to matter* (so a fresh value written by another replica suppresses
// this one). In steady state that is one write per sandbox per refresh
// interval for the whole deployment, not one per replica.
//
// The timing contract — how stale is allowed, and therefore how much slack the
// controller has to add back — lives in pkg/activity, so the writer and the
// reader cannot drift apart.
type ActivityFlusher struct {
	tracker *ActivityTracker
	k8s     client.Client
	cfg     ActivityFlusherConfig

	// pending holds sandboxes that are due but not yet written, so a rate limit
	// (when configured) defers a write instead of dropping it. Guarded by mu.
	mu      sync.Mutex
	pending map[string]pendingWrite
	// states throttles the Pod lookup per sandbox. A tick walks every tracked
	// sandbox, and each lookup is an informer-cache read that deep-copies a Pod;
	// with thousands of tracked sandboxes that is a lot of copying per tick for
	// answers that cannot have changed (the write interval is tens of seconds).
	states map[string]evalState

	// tokens is the rate limiter's remaining budget for the current second.
	tokens int
	second int64

	now func() time.Time
}

type pendingWrite struct {
	value       time.Time
	queuedAt    time.Time
	budget      time.Duration
	idleTimeout time.Duration
}

// evalState is when a sandbox is next worth looking at. Throttling evaluation
// to the same interval as the write does not widen anything: the freshness
// bound already assumes the annotation is refreshed once per interval.
type evalState struct {
	nextCheck time.Time
}

// ActivityFlusherConfig tunes the flusher. The zero value is the production
// configuration.
type ActivityFlusherConfig struct {
	// Tick is how often the flusher looks for work. Zero means activity.FlushTick.
	Tick time.Duration

	// RateLimitPerSecond caps the Pod PATCH rate. Zero means unlimited, which is
	// the shipped default: the right number depends on how many sandboxes this
	// deployment actually keeps busy, and a limit set below the steady-state
	// demand makes the queue grow without bound — the annotations then go stale
	// and the controller releases sandboxes that are in use. Measure first
	// (agentbox_gateway_activity_flush_total), then set it above the observed
	// peak. The limiter smooths bursts; it is not a way to save API server load.
	RateLimitPerSecond int

	// Workers is how many PATCHes run concurrently. Zero means 4.
	Workers int

	// MaxPending bounds the deferred-write map. Beyond it the oldest entries are
	// dropped and counted, because an unbounded map is a memory leak and a
	// silently growing queue is worse than a counted loss.
	MaxPending int

	// WriteTimeout bounds a single PATCH. Zero means 5s.
	WriteTimeout time.Duration
}

func (c ActivityFlusherConfig) withDefaults() ActivityFlusherConfig {
	if c.Tick <= 0 {
		c.Tick = activity.FlushTick
	}
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.MaxPending <= 0 {
		c.MaxPending = 20000
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 5 * time.Second
	}
	return c
}

// NewActivityFlusher builds a flusher. k8s must be the manager's informer-backed
// client: lookups run per tracked sandbox on every tick.
func NewActivityFlusher(tracker *ActivityTracker, k8s client.Client, cfg ActivityFlusherConfig) *ActivityFlusher {
	return &ActivityFlusher{
		tracker: tracker,
		k8s:     k8s,
		cfg:     cfg.withDefaults(),
		pending: make(map[string]pendingWrite),
		states:  make(map[string]evalState),
		now:     time.Now,
	}
}

// Run drives the flush loop until ctx is done. Start it only after the informer
// cache is warm — before that every index lookup misses and the flusher spends
// its ticks doing nothing.
func (f *ActivityFlusher) Run(ctx context.Context) {
	ticker := time.NewTicker(f.cfg.Tick)
	defer ticker.Stop()

	klog.InfoS("ActivityFlusher started",
		"tick", f.cfg.Tick,
		"baseRefresh", activity.BaseRefreshInterval,
		"jitter", activity.RefreshJitter,
		"rateLimitPerSecond", f.cfg.RateLimitPerSecond,
		"rateLimitNote", rateLimitNote(f.cfg.RateLimitPerSecond))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.flushOnce(ctx)
		}
	}
}

func rateLimitNote(limit int) string {
	if limit <= 0 {
		return "unlimited (default; set after measuring the steady-state rate)"
	}
	return "burst smoothing only — must stay above the steady-state rate"
}

// flushOnce is one tick: collect what became due, then write as much of it as
// the limiter allows.
func (f *ActivityFlusher) flushOnce(ctx context.Context) {
	f.collectDue(ctx)

	batch := f.takeBatch()
	if len(batch) == 0 {
		f.publishQueueMetrics()
		return
	}

	workers := min(f.cfg.Workers, len(batch))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for _, item := range batch {
		wg.Add(1)
		sem <- struct{}{}
		go func(item flushItem) {
			defer wg.Done()
			defer func() { <-sem }()
			f.writeOne(ctx, item)
		}(item)
	}
	wg.Wait()
	f.publishQueueMetrics()
}

type flushItem struct {
	id    string
	value time.Time
}

// collectDue walks the tracker and enqueues every sandbox whose annotation is
// stale relative to what this replica has seen.
func (f *ActivityFlusher) collectDue(ctx context.Context) {
	snapshot := f.tracker.snapshot()
	now := f.now()
	trackedGauge.Set(float64(len(snapshot)))

	f.mu.Lock()
	defer f.mu.Unlock()

	// Drop bookkeeping for sandboxes the tracker no longer holds (released, or
	// collected by its GC). Without this the states map would grow forever.
	// Runs even when the snapshot is empty — that is exactly when everything
	// became stale at once.
	for id := range f.states {
		if _, live := snapshot[id]; !live {
			delete(f.states, id)
		}
	}
	if len(snapshot) == 0 {
		return
	}

	for id, lastSeen := range snapshot {
		if _, queued := f.pending[id]; queued {
			continue
		}
		if st, seen := f.states[id]; seen && now.Before(st.nextCheck) {
			continue
		}

		pod, err := indexer.GetPodBySandboxID(ctx, f.k8s, id)
		if err != nil {
			// Released, or not visible to this replica's cache yet. Nothing to
			// write; the tracker's own GC drops the entry. Re-check soonish in
			// case the cache was simply behind.
			flushTotal.WithLabelValues("skipped_no_pod").Inc()
			f.states[id] = evalState{nextCheck: now.Add(activity.FlushTick)}
			continue
		}
		if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
			// Stopping pods deliberately keep their sandbox-id label so in-flight
			// traffic is not cut off. Writing to one is pointless: the
			// controller only considers Running pods.
			flushTotal.WithLabelValues("skipped_not_running").Inc()
			f.states[id] = evalState{nextCheck: now.Add(activity.BaseRefreshInterval)}
			continue
		}

		idleTimeout := parseSecondsAnnotation(pod.Annotations[agentsv1alpha1.SandboxIdleTimeoutAnnotationKey])
		if idleTimeout <= 0 {
			// No timeout means nothing will ever be released for idleness, so
			// there is no annotation to keep fresh. This keeps the write volume
			// proportional to the sandboxes that actually have a deadline.
			flushTotal.WithLabelValues("skipped_no_timeout").Inc()
			// Not forever: set_timeout can add one later, and it stamps
			// last-active when it does.
			f.states[id] = evalState{nextCheck: now.Add(activity.BaseRefreshInterval)}
			continue
		}

		// From here the sandbox has a deadline, so evaluate at its own refresh
		// cadence rather than on every tick.
		interval := activity.RefreshIntervalFor(id, idleTimeout)
		f.states[id] = evalState{nextCheck: now.Add(interval)}

		annotation, _ := time.Parse(time.RFC3339, pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey])
		if !shouldRefresh(now, id, lastSeen, annotation, idleTimeout) {
			flushTotal.WithLabelValues("skipped_fresh").Inc()
			continue
		}

		if len(f.pending) >= f.cfg.MaxPending {
			flushDropped.Inc()
			continue
		}
		f.pending[id] = pendingWrite{
			value:       lastSeen,
			queuedAt:    now,
			budget:      activity.MaxRefreshInterval(idleTimeout),
			idleTimeout: idleTimeout,
		}
	}
}

// shouldRefresh is the write rule, kept pure so it can be tested directly:
//
//   - only ever move the annotation forward, so a slower replica cannot roll
//     back a newer value another replica just wrote;
//   - otherwise wait until the annotation is older than this sandbox's own
//     refresh interval — that wait is what collapses N replicas into roughly
//     one write per interval.
func shouldRefresh(now time.Time, sandboxID string, lastSeen, annotation time.Time, idleTimeout time.Duration) bool {
	if idleTimeout <= 0 || lastSeen.IsZero() {
		return false
	}
	if !lastSeen.After(annotation) {
		return false
	}
	return now.Sub(annotation) > activity.RefreshIntervalFor(sandboxID, idleTimeout)
}

// takeBatch removes and returns what the rate limiter allows this tick.
func (f *ActivityFlusher) takeBatch() []flushItem {
	f.mu.Lock()
	defer f.mu.Unlock()

	allowed := len(f.pending)
	if f.cfg.RateLimitPerSecond > 0 {
		f.refillTokensLocked()
		if f.tokens < allowed {
			allowed = f.tokens
		}
	}
	if allowed == 0 {
		return nil
	}

	batch := make([]flushItem, 0, allowed)
	for id, item := range f.pending {
		if len(batch) == allowed {
			break
		}
		delete(f.pending, id)
		batch = append(batch, flushItem{id: id, value: item.value})
	}
	if f.cfg.RateLimitPerSecond > 0 {
		f.tokens -= len(batch)
	}
	return batch
}

// refillTokensLocked tops the token bucket up once per wall-clock second.
// Caller holds f.mu.
func (f *ActivityFlusher) refillTokensLocked() {
	now := f.now().Unix()
	if now != f.second {
		f.second = now
		f.tokens = f.cfg.RateLimitPerSecond
	}
}

var errStalePod = errors.New("pod no longer carries this sandbox")

// writeOne patches one sandbox's annotation. Every failure is best-effort: the
// entry is dropped and the tracker will re-offer it on a later tick if there is
// still newer activity.
func (f *ActivityFlusher) writeOne(ctx context.Context, item flushItem) {
	ctx, cancel := context.WithTimeout(ctx, f.cfg.WriteTimeout)
	defer cancel()

	pod, err := indexer.GetPodBySandboxID(ctx, f.k8s, item.id)
	if err != nil {
		flushTotal.WithLabelValues("skipped_no_pod").Inc()
		return
	}
	if pod.Labels[agentsv1alpha1.SandboxIDLabelKey] != item.id {
		flushTotal.WithLabelValues("skipped_reassigned").Inc()
		return
	}
	if pod.Labels[agentsv1alpha1.SandboxPhaseLabelKey] != agentsv1alpha1.SandboxPhaseRunning {
		flushTotal.WithLabelValues("skipped_not_running").Inc()
		return
	}
	// Re-check against the freshest cache: another replica may have written a
	// newer value between the scan and now.
	if current, parseErr := time.Parse(time.RFC3339,
		pod.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey]); parseErr == nil && !item.value.After(current) {
		flushTotal.WithLabelValues("skipped_fresh").Inc()
		return
	}

	if err := f.patchLastActive(ctx, pod, item.id, item.value); err != nil {
		result := "error"
		if errors.Is(err, errStalePod) {
			result = "skipped_reassigned"
		} else if client.IgnoreNotFound(err) == nil {
			result = "skipped_no_pod"
		} else if isConflict(err) {
			// Somebody else touched the Pod first (another replica, or the
			// lifecycle controllers). The newer annotation wins; if it was
			// another replica's write we are done, and the next tick re-reads.
			result = "conflict"
		}
		flushTotal.WithLabelValues(result).Inc()
		// Look at this sandbox again on the next tick rather than waiting out
		// its refresh interval: a conflict or a transient API error should not
		// cost a whole interval of staleness.
		f.reschedule(item.id)
		klog.V(2).InfoS("ActivityFlusher: annotation write failed",
			"sandboxID", item.id, "namespace", pod.Namespace, "pod", pod.Name,
			"result", result, "err", err)
		return
	}
	flushTotal.WithLabelValues("written").Inc()
	flushWritten.Inc()
}

// reschedule clears a sandbox's evaluation deadline so the next tick re-reads
// it.
func (f *ActivityFlusher) reschedule(sandboxID string) {
	f.mu.Lock()
	delete(f.states, sandboxID)
	f.mu.Unlock()
}

// patchLastActive advances the annotation with an optimistic lock, so a
// concurrent writer's newer value can never be overwritten by this older one.
func (f *ActivityFlusher) patchLastActive(ctx context.Context, pod *corev1.Pod, sandboxID string, value time.Time) error {
	if pod.Labels[agentsv1alpha1.SandboxIDLabelKey] != sandboxID {
		return errStalePod
	}
	// The informer hands out shared objects; patch a copy so the cache is not
	// mutated behind the informer's back. `pod` itself stays untouched and is
	// used as the diff base below.
	modified := pod.DeepCopy()
	if modified.Annotations == nil {
		modified.Annotations = map[string]string{}
	}
	modified.Annotations[agentsv1alpha1.SandboxLastActiveAnnotationKey] = value.UTC().Format(time.RFC3339)
	return f.k8s.Patch(ctx, modified,
		client.MergeFromWithOptions(pod, client.MergeFromWithOptimisticLock{}))
}

func isConflict(err error) bool {
	return apierrors.IsConflict(err)
}

// parseSecondsAnnotation reads the "<seconds>" annotations the claim writes
// (e.g. idle-timeout: "600"). Anything unparseable is treated as absent.
func parseSecondsAnnotation(raw string) time.Duration {
	if raw == "" {
		return 0
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// ── metrics ─────────────────────────────────────────────────────────────────

var (
	flushTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_gateway_activity_flush_total",
		Help: "last-active annotation refreshes attempted by this gateway replica, by result. " +
			"result=written the annotation was advanced; skipped_fresh another replica's value was already newer; " +
			"skipped_no_pod no Running Pod carries the sandbox; skipped_not_running the Pod is mid-transition; " +
			"skipped_no_timeout the sandbox has no idle timeout so nothing will ever reclaim it; " +
			"skipped_reassigned the Pod moved to another sandbox between lookup and patch; " +
			"conflict a concurrent update won; error the patch failed.",
	}, []string{"result"})

	flushWritten = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentbox_gateway_activity_flush_written_total",
		Help: "last-active annotation writes that succeeded on this replica. " +
			"Steady-state rate is (busy sandboxes with a timeout) / refresh interval; " +
			"this is the number to measure before setting a rate limit.",
	})

	flushDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agentbox_gateway_activity_flush_dropped_total",
		Help: "Refresh writes abandoned because the deferred-write map hit its bound. Non-zero means activity is being lost.",
	})

	trackedGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentbox_gateway_activity_tracked_sandboxes",
		Help: "Sandboxes with an in-memory last-active entry on this replica.",
	})

	pendingGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentbox_gateway_activity_flush_pending",
		Help: "Deferred annotation writes waiting for rate-limit budget.",
	})

	queueAgeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentbox_gateway_activity_flush_queue_age_seconds",
		Help: "Age of the oldest deferred annotation write.",
	})

	queueAgeRatioGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentbox_gateway_activity_flush_queue_age_ratio",
		Help: "Age of the oldest deferred write divided by the slack the controller can absorb for that sandbox. " +
			"Above 0.5 the rate limit is starting to eat the safety margin; at 1.0 a sandbox in use can be released. " +
			"This is the alert to watch when a rate limit is configured.",
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		flushTotal, flushWritten, flushDropped,
		trackedGauge, pendingGauge, queueAgeGauge, queueAgeRatioGauge,
	)
}

// publishQueueMetrics reports how far behind the deferred writes are. The ratio
// is the one that matters: it is measured against the slack the reader is able
// to absorb, not against an absolute number of seconds.
func (f *ActivityFlusher) publishQueueMetrics() {
	f.mu.Lock()
	defer f.mu.Unlock()

	pendingGauge.Set(float64(len(f.pending)))
	if len(f.pending) == 0 {
		queueAgeGauge.Set(0)
		queueAgeRatioGauge.Set(0)
		return
	}

	now := f.now()
	var oldestAge time.Duration
	var worstRatio float64
	for _, item := range f.pending {
		age := now.Sub(item.queuedAt)
		if age > oldestAge {
			oldestAge = age
		}
		if item.budget > 0 {
			if ratio := float64(age) / float64(item.budget); ratio > worstRatio {
				worstRatio = ratio
			}
		}
	}
	queueAgeGauge.Set(oldestAge.Seconds())
	queueAgeRatioGauge.Set(worstRatio)

	if worstRatio >= 1 {
		klog.ErrorS(nil, "ActivityFlusher: deferred writes have outrun the controller's slack; "+
			"a sandbox in active use can now be reclaimed. Raise the rate limit or the refresh interval",
			"oldestAge", oldestAge.Round(time.Second), "ratio", fmt.Sprintf("%.2f", worstRatio),
			"pending", len(f.pending))
	}
}
