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

// Package metrics defines and registers all custom Prometheus metrics for AgentBox.
// All metrics are registered to the controller-runtime shared registry so they are
// exposed via the same --metrics-bind-address endpoint as the controller metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Pool replica gauges — one per replica phase, labelled by namespace/pool/team/user.
var (
	PoolReplicasDesired  *prometheus.GaugeVec
	PoolReplicasIdle     *prometheus.GaugeVec
	PoolReplicasRunning  *prometheus.GaugeVec
	PoolReplicasStarting *prometheus.GaugeVec
	PoolReplicasStopping *prometheus.GaugeVec
	PoolReplicasFailed   *prometheus.GaugeVec
)

// Sandbox lifecycle histograms.
var (
	// SandboxClaimDuration observes how long ClaimIdlePod takes.
	// outcome: "success" | "no_idle" | "timeout" | "error"
	SandboxClaimDuration *prometheus.HistogramVec

	// SandboxStartingDuration observes the image-pull / startup time (claimedAt → startedAt).
	// stop_reason label is absent here; use for P99 startup latency breakdowns.
	SandboxStartingDuration *prometheus.HistogramVec

	// SandboxRunningDuration observes actual sandbox running time (startedAt → terminatedAt).
	// stop_reason: "Completed" | "Failed" | "Canceled" | "Evicted"
	SandboxRunningDuration *prometheus.HistogramVec

	// SandboxRecycleDuration observes the Stopping→Idle recycle time (terminatedAt → recycledAt).
	SandboxRecycleDuration *prometheus.HistogramVec
)

// Sandbox info gauges.
var (
	// SandboxRunningInfo is an info gauge (value always 1) that maps running sandbox IDs
	// to their pod names. Present only while the sandbox is in Running state.
	// Labels: namespace, pool, pod, sandbox_id, team, user.
	// Use for PromQL joins with kube CPU/memory metrics via namespace+pod labels.
	SandboxRunningInfo *prometheus.GaugeVec
)

// Sandbox operation counters.
var (
	// SandboxCreateTotal counts sandbox creation attempts.
	// result: "success" | "no_idle" | "timeout" | "error"
	SandboxCreateTotal *prometheus.CounterVec

	// SandboxDeleteTotal counts sandbox deletions.
	// stop_reason: "Completed" | "Canceled" | "Failed"
	SandboxDeleteTotal *prometheus.CounterVec

	// InplaceUpdateTotal counts TriggerUpdateWithOptions calls.
	// result: "success" | "conflict" | "error"
	// (conflict covers both k8s resource version conflicts and phase mismatches)
	// target: TargetPodPhase value (e.g. "running", "idle")
	InplaceUpdateTotal *prometheus.CounterVec
)

// HTTP API metrics (Gin middleware).
var (
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
)

// Stream scheduler metrics (pkg/lifecycle/schedule).
// Labels are namespace/pool/team/user. The current Pool model is per-user, so
// scheduler instances can retain the owning team/user when they are created.
var (
	// ScheduleReadyQSize is the current number of pods in the per-pool ready queue
	// (idle pods known to the scheduler, not yet dispatched).
	ScheduleReadyQSize *prometheus.GaugeVec

	// ScheduleReservationsSize is the current number of per-pool inflight reservations
	// (pods either being CAS'd or recently claimed within the TTL window).
	ScheduleReservationsSize *prometheus.GaugeVec

	// ScheduleCASOutcomeTotal counts TriggerUpdateWithOptions outcomes from the scheduler.
	// outcome: "success" | "retriable" (phase mismatch / k8s conflict) | "hard" (other errors).
	ScheduleCASOutcomeTotal *prometheus.CounterVec

	// ScheduleDispatchLatencySeconds measures the time from request enqueue to the
	// moment the CAS goroutine starts executing TriggerUpdateWithOptions.
	ScheduleDispatchLatencySeconds *prometheus.HistogramVec

	// ScheduleRefreshTotal counts ready-queue refresh attempts. outcome: "ok" | "throttled" | "error".
	ScheduleRefreshTotal *prometheus.CounterVec

	// ScheduleReservationTTLExpiredTotal counts reservations removed by TTL sweep
	// (i.e. reservations not explicitly released by the CAS outcome handler).
	ScheduleReservationTTLExpiredTotal *prometheus.CounterVec

	// ScheduleSkippedScaleDownProtectedTotal counts refreshes where pods were skipped
	// because they carried the scale-down-protected annotation.
	ScheduleSkippedScaleDownProtectedTotal *prometheus.CounterVec

	// ScheduleReadyQueueEvictedTotal counts pods discarded from the ready queue at
	// dispatch time because they were no longer present in the informer cache or had
	// transitioned out of Idle (e.g. deleted during scale-down).
	ScheduleReadyQueueEvictedTotal *prometheus.CounterVec
)

func init() {
	poolLabels := []string{"namespace", "pool", "team", "user", "sandbox_env"}

	PoolReplicasDesired = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_desired",
		Help: "The desired number of replicas for a SandboxPool.",
	}, poolLabels)

	PoolReplicasIdle = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_idle",
		Help: "The number of idle (pre-warmed, unclaimed) replicas in a SandboxPool.",
	}, poolLabels)

	PoolReplicasRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_running",
		Help: "The number of running (claimed) replicas in a SandboxPool.",
	}, poolLabels)

	PoolReplicasStarting = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_starting",
		Help: "The number of starting replicas (image pull in progress) in a SandboxPool.",
	}, poolLabels)

	PoolReplicasStopping = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_stopping",
		Help: "The number of stopping replicas (being recycled back to idle) in a SandboxPool.",
	}, poolLabels)

	PoolReplicasFailed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandboxpool_replicas_failed",
		Help: "The number of failed replicas in a SandboxPool.",
	}, poolLabels)

	SandboxClaimDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentbox_sandbox_claim_duration_seconds",
		Help:    "Duration of ClaimIdlePod operations in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 18),
	}, append(poolLabels, "outcome"))

	SandboxStartingDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "agentbox_sandbox_starting_duration_seconds",
		Help: "Sandbox startup duration (from claimedAt to startedAt, i.e. image pull + container start) in seconds. " +
			"For Canceled sandboxes (never reached Running), measures claimedAt to terminatedAt. " +
			"outcome: success/canceled.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 20),
	}, append(poolLabels, "outcome"))

	SandboxRunningDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentbox_sandbox_running_duration_seconds",
		Help:    "Actual sandbox running duration (from startedAt to terminatedAt) in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 20),
	}, append(poolLabels, "stop_reason"))

	SandboxRecycleDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentbox_sandbox_recycle_duration_seconds",
		Help:    "Sandbox recycle duration (from terminatedAt to recycledAt, i.e. Stopping→Idle image restore) in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 18),
	}, poolLabels)

	SandboxRunningInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_sandbox_running_info",
		Help: "Running sandbox to pod mapping (value always 1). Present only while sandbox is Running. Join with kube metrics via namespace+pod.",
	}, []string{"namespace", "pool", "pod", "sandbox_id", "team", "user", "sandbox_env"})

	SandboxCreateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_sandbox_create_total",
		Help: "Total number of sandbox creation attempts, partitioned by result.",
	}, append(poolLabels, "result"))

	SandboxDeleteTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_sandbox_delete_total",
		Help: "Total number of sandbox deletions, partitioned by stop_reason.",
	}, append(poolLabels, "stop_reason"))

	InplaceUpdateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_inplace_update_total",
		Help: "Total number of TriggerUpdateWithOptions calls, partitioned by result. " +
			"result: success/conflict/error. " +
			"conflict covers both k8s resource version conflicts and phase mismatches. " +
			"target: TargetPodPhase (e.g. running/idle).",
	}, []string{"namespace", "pool", "target", "user", "team", "sandbox_env", "result"})

	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_http_requests_total",
		Help: "Total number of HTTP requests processed by the AgentBox API servers.",
	}, []string{"method", "path", "status_code", "api"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentbox_http_request_duration_seconds",
		Help:    "Duration of HTTP requests processed by the AgentBox API servers in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 17),
	}, []string{"method", "path", "status_code", "api"})

	scheduleLabels := []string{"namespace", "pool", "team", "user", "sandbox_env"}

	ScheduleReadyQSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_schedule_ready_queue_size",
		Help: "Current size of the per-pool scheduler ready queue (idle pods known to the scheduler, not yet dispatched).",
	}, scheduleLabels)

	ScheduleReservationsSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentbox_schedule_reservations_size",
		Help: "Current size of the per-pool scheduler inflight reservation map (pods being CAS'd or recently claimed within TTL).",
	}, scheduleLabels)

	ScheduleCASOutcomeTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_schedule_cas_outcome_total",
		Help: "Total TriggerUpdateWithOptions outcomes invoked by the streaming scheduler. " +
			"outcome: success | retriable (phase mismatch / k8s conflict) | hard (other errors).",
	}, append(scheduleLabels, "outcome"))

	ScheduleDispatchLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentbox_schedule_dispatch_latency_seconds",
		Help:    "Time from request enqueue to CAS goroutine start in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.005, 2, 17),
	}, scheduleLabels)

	ScheduleRefreshTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_schedule_refresh_total",
		Help: "Total per-pool ready-queue refresh attempts. outcome: ok | throttled | error.",
	}, append(scheduleLabels, "outcome"))

	ScheduleReservationTTLExpiredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_schedule_reservation_ttl_expired_total",
		Help: "Total reservations removed by TTL sweep (i.e. not explicitly released by the CAS outcome handler).",
	}, scheduleLabels)

	ScheduleSkippedScaleDownProtectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_schedule_skipped_scale_down_protected_total",
		Help: "Total pods skipped during refresh because they carried the scale-down-protected annotation.",
	}, scheduleLabels)

	ScheduleReadyQueueEvictedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentbox_schedule_ready_queue_evicted_total",
		Help: "Pods discarded from the ready queue at dispatch time because they were absent from the informer cache or no longer Idle (e.g. deleted during scale-down).",
	}, scheduleLabels)

	ctrlmetrics.Registry.MustRegister(
		PoolReplicasDesired,
		PoolReplicasIdle,
		PoolReplicasRunning,
		PoolReplicasStarting,
		PoolReplicasStopping,
		PoolReplicasFailed,
		SandboxClaimDuration,
		SandboxStartingDuration,
		SandboxRunningDuration,
		SandboxRecycleDuration,
		SandboxRunningInfo,
		SandboxCreateTotal,
		SandboxDeleteTotal,
		InplaceUpdateTotal,
		HTTPRequestsTotal,
		HTTPRequestDuration,
		ScheduleReadyQSize,
		ScheduleReservationsSize,
		ScheduleCASOutcomeTotal,
		ScheduleDispatchLatencySeconds,
		ScheduleRefreshTotal,
		ScheduleReservationTTLExpiredTotal,
		ScheduleSkippedScaleDownProtectedTotal,
		ScheduleReadyQueueEvictedTotal,
	)
}
