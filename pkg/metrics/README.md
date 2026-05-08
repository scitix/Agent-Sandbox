# pkg/metrics — Prometheus Metrics

All custom metrics are registered to the `controller-runtime` shared registry and exposed uniformly via `--metrics-bind-address` (default `:8082`, plain HTTP).

## Files

- `metrics.go` — Metric variables definition and `init()` registration
- `middleware.go` — `GinPrometheusMiddleware(api string)` Gin middleware, where `api` accepts `"native"` or `"e2b"`

## Metrics List

| Metric Name | Type | Labels | Description |
|--------|------|------|------|
| `agentbox_sandboxpool_replicas_desired` | Gauge | namespace, pool, team, user | Desired replica count |
| `agentbox_sandboxpool_replicas_idle` | Gauge | namespace, pool, team, user | Idle replica count |
| `agentbox_sandboxpool_replicas_running` | Gauge | namespace, pool, team, user | Running replica count |
| `agentbox_sandboxpool_replicas_starting` | Gauge | namespace, pool, team, user | Starting replica count |
| `agentbox_sandboxpool_replicas_stopping` | Gauge | namespace, pool, team, user | Stopping (recycling) replica count |
| `agentbox_sandboxpool_replicas_failed` | Gauge | namespace, pool, team, user | Failed replica count |
| `agentbox_sandbox_claim_duration_seconds` | Histogram | namespace, pool, team, user, outcome | Time spent in ClaimIdlePod; outcome: success/no_idle/timeout/error |
| `agentbox_sandbox_starting_duration_seconds` | Histogram | namespace, pool, team, user, outcome | Sandbox startup duration; outcome=success: claimedAt→startedAt; outcome=canceled: claimedAt→terminatedAt (user canceled before Running) |
| `agentbox_sandbox_running_duration_seconds` | Histogram | namespace, pool, team, user, stop_reason | Actual sandbox running duration (startedAt → terminatedAt) |
| `agentbox_sandbox_recycle_duration_seconds` | Histogram | namespace, pool, team, user | Sandbox recycle duration (terminatedAt → recycledAt, i.e. Stopping→Idle image restore) |
| `agentbox_sandbox_running_info` | Gauge (always 1) | namespace, pool, pod, sandbox_id, team, user | Mapping of Running Sandbox → Pod; exists only while the Sandbox is Running, used for PromQL joins with kube metrics |
| `agentbox_sandbox_create_total` | Counter | namespace, pool, team, user, result | Creation request count; result: success/no_idle/error |
| `agentbox_sandbox_delete_total` | Counter | namespace, pool, team, user, stop_reason | Deletion count; stop_reason: Completed/Canceled/Released/Failed (includes all paths: API stop, idle timeout, OOM/Crash recycling, eviction cleanup) |
| `agentbox_inplace_update_total` | Counter | namespace, pool, target, user, team, result | In-place update attempt count; target: TargetPodPhase (running/idle); result: success/conflict/error (conflict covers k8s version conflicts and phase mismatches) |
| `agentbox_http_requests_total` | Counter | method, path, status_code, api | HTTP request count; api: native/e2b |
| `agentbox_http_request_duration_seconds` | Histogram | method, path, status_code, api | HTTP request latency |
| `agentbox_schedule_ready_queue_size` | Gauge | namespace, pool, team, user | Current number of idle pods in the per-pool scheduler ready queue (known to the scheduler, not yet dispatched) |
| `agentbox_schedule_reservations_size` | Gauge | namespace, pool, team, user | Current number of inflight reservations (pods being CAS'd or recently claimed within TTL window) |
| `agentbox_schedule_cas_outcome_total` | Counter | namespace, pool, team, user, outcome | TriggerUpdateWithOptions outcomes from the streaming scheduler; outcome: success/retriable (phase mismatch or k8s conflict)/hard (other errors) |
| `agentbox_schedule_dispatch_latency_seconds` | Histogram | namespace, pool, team, user | Time from request enqueue to CAS goroutine start (scheduler responsiveness) |
| `agentbox_schedule_refresh_total` | Counter | namespace, pool, team, user, outcome | Per-pool ready-queue refresh attempts; outcome: ok/throttled/error |
| `agentbox_schedule_reservation_ttl_expired_total` | Counter | namespace, pool, team, user | Reservations removed by TTL sweep (not explicitly released by the CAS outcome handler) |
| `agentbox_schedule_skipped_scale_down_protected_total` | Counter | namespace, pool, team, user | Pods skipped during refresh because they carry the scale-down-protected annotation |
| `agentbox_schedule_ready_queue_evicted_total` | Counter | namespace, pool, team, user | Pods discarded from the ready queue at dispatch time because they were absent from the informer cache or no longer Idle (e.g. deleted during scale-down) |

> The `team`/`user` labels are derived from `SandboxPool.Labels["scheduling.navix.sh/team"]` and `["scheduling.navix.sh/user"]`, which are passed by the caller via the API.
>
> The HTTP path label uses `c.FullPath()` (the route template, e.g., `/v1/sandboxes/:id`) to prevent high cardinality issues caused by specific parameter values.

## Associating with Kube Resource Metrics using agentbox_sandbox_running_info

`agentbox_sandbox_running_info` is an Info-type metric (its value is always 1) that exists only when the Sandbox is in the Running phase. By joining it with native kube metrics using the `namespace` + `pod` labels, you can associate CPU/Memory usage with specific Sandboxes.

```promql
# Query CPU usage rate for a specific sandbox (5m average)
rate(container_cpu_usage_seconds_total{namespace="default", container!=""}[5m])
  * on(namespace, pod) group_left(sandbox_id)
  agentbox_sandbox_running_info{sandbox_id="<your-sandbox-id>"}

# Query memory usage for a specific sandbox
container_memory_working_set_bytes{namespace="default"}
  * on(namespace, pod) group_left(sandbox_id)
  agentbox_sandbox_running_info{sandbox_id="<your-sandbox-id>"}

# Query memory usage for all running sandboxes, grouped by sandbox_id / user
container_memory_working_set_bytes
  * on(namespace, pod) group_left(sandbox_id, team, user)
  agentbox_sandbox_running_info
```

Metric Lifecycle:
- **Set**: Upon completion of Starting→Running in `syncInplaceUpdatePhases` (`sandboxpool_controller.go`)
- **Delete**: Upon completion of Stopping→Idle in `syncInplaceUpdatePhases`, right before `cleanupSandboxMetadata` is called

## Adding New Metrics

1. Declare the variable in `metrics.go` and register it in `init()` (using `MustRegister`).
2. Call methods like `.Set()` / `.Inc()` / `.Observe()` in the business logic code.
3. Verify the metric values in the corresponding unit or integration tests.

## Prometheus Scraping Configuration

`config/prometheus/monitor.yaml` contains the `ServiceMonitor` (requires Prometheus Operator to be installed in the cluster):
- Port: `http-metrics` (TCP 8082, corresponding to `config/default/metrics_service.yaml`)
- Protocol: HTTP (no TLS), scrape interval 30s

**How to enable**: Uncomment `# - ../prometheus` in `config/default/kustomization.yaml`, then run `make build-installer`.

**Local validation**:
```bash
curl http://localhost:8082/metrics | grep agentbox_
```