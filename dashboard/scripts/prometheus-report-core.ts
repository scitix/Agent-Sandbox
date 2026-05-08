/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

export type Scalar = number | null

export interface QueryResult {
  key: string
  description: string
  promql: string
  value: Scalar
}

export interface ScopeReport {
  scope: string
  clusterMatcher: string
  metrics: Record<string, Scalar>
  derived: Record<string, Scalar>
  queries: QueryResult[]
}

export interface Report {
  generatedAt: string
  prometheusUrl: string
  window: {
    start: number
    end: number
    seconds: number
    literal: string
  }
  clusters: string[]
  scopes: ScopeReport[]
}

export interface QuerySpec {
  key: string
  description: string
  promql: string
}

export const DEFAULT_CLUSTERS = ["prod-foo", "prod-bar"]
export const DEFAULT_SUBQUERY_STEP = "1m"
export const ENVOY_CLUSTER_NAME = "original_dst_cluster"
export const AGENTBOX_NAMESPACE = "agentbox-system"

export function parseArgs(argv: string[]) {
  const args = new Map<string, string>()
  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i]
    if (!token.startsWith("--")) continue
    const next = argv[i + 1]
    if (!next || next.startsWith("--")) {
      args.set(token, "true")
      continue
    }
    args.set(token, next)
    i += 1
  }
  return args
}

export function parseWindow(windowText: string): number {
  const match = windowText.match(/^(\d+)([smhdw])$/)
  if (!match) {
    throw new Error(`Unsupported window: ${windowText}. Expected forms like 6h, 7d, 30m.`)
  }
  const value = Number.parseInt(match[1], 10)
  const unit = match[2]
  const factor: Record<string, number> = {
    s: 1,
    m: 60,
    h: 3600,
    d: 86400,
    w: 604800,
  }
  return value * factor[unit]
}

export function buildClusterSelector(clusterMatcher: string): string {
  return `cluster=~"${clusterMatcher}"`
}

export function buildEnvoySelector(clusterMatcher: string): string {
  return [
    `namespace="${AGENTBOX_NAMESPACE}"`,
    `cluster=~"${clusterMatcher}"`,
    `envoy_cluster_name="${ENVOY_CLUSTER_NAME}"`,
  ].join(",")
}

export function formatPercent(value: Scalar): string {
  if (value === null || Number.isNaN(value)) return "n/a"
  return `${(value * 100).toFixed(2)}%`
}

export function formatNumber(value: Scalar, digits = 2): string {
  if (value === null || Number.isNaN(value)) return "n/a"
  return new Intl.NumberFormat("en-US", {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  }).format(value)
}

export function divide(numerator: Scalar, denominator: Scalar): Scalar {
  if (numerator === null || denominator === null || denominator === 0) return null
  return numerator / denominator
}

export async function queryPrometheus(
  url: string,
  token: string | undefined,
  query: string,
  time?: number,
): Promise<Scalar> {
  const params = new URLSearchParams({ query })
  if (time) params.set("time", String(time))

  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`

  const response = await fetch(`${url}/api/v1/query?${params.toString()}`, { headers })
  if (!response.ok) {
    throw new Error(`Prometheus returned ${response.status} for query: ${query}`)
  }

  const payload = (await response.json()) as {
    status: string
    data?: { result?: Array<{ value?: [number, string] }> }
  }

  if (payload.status !== "success") return null
  const sample = payload.data?.result?.[0]
  if (!sample?.value?.[1]) return null
  const parsed = Number.parseFloat(sample.value[1])
  return Number.isFinite(parsed) ? parsed : null
}

export interface TimeSeriesPoint {
  time: number
  value: number
}

export interface TimeSeries {
  metric: string
  points: TimeSeriesPoint[]
}

export async function queryPrometheusRange(
  url: string,
  token: string | undefined,
  query: string,
  start: number,
  end: number,
  step: string,
): Promise<TimeSeriesPoint[]> {
  const params = new URLSearchParams({
    query,
    start: String(start),
    end: String(end),
    step,
  })

  const headers: Record<string, string> = {}
  if (token) headers.Authorization = `Bearer ${token}`

  const response = await fetch(`${url}/api/v1/query_range?${params.toString()}`, { headers })
  if (!response.ok) {
    throw new Error(`Prometheus range query returned ${response.status} for: ${query}`)
  }

  const payload = (await response.json()) as {
    status: string
    data?: { result?: Array<{ values?: Array<[number, string]> }> }
  }

  if (payload.status !== "success") return []
  const series = payload.data?.result?.[0]
  if (!series?.values) return []

  return series.values
    .map(([ts, val]) => {
      const v = Number.parseFloat(val)
      return Number.isFinite(v) ? { time: ts, value: v } : null
    })
    .filter((p): p is TimeSeriesPoint => p !== null)
}

export function buildQueries(clusterMatcher: string, windowLiteral: string): QuerySpec[] {
  const sel = buildClusterSelector(clusterMatcher)
  const envoySel = buildEnvoySelector(clusterMatcher)

  const gaugeSummary = (metric: string, suffix: string, description: string): QuerySpec[] => [
    {
      key: `${suffix}Current`,
      description: `${description} current`,
      promql: `sum(${metric}{${sel}})`,
    },
    {
      key: `${suffix}Avg`,
      description: `${description} average over window`,
      promql: `avg_over_time((sum(${metric}{${sel}}))[${windowLiteral}:${DEFAULT_SUBQUERY_STEP}])`,
    },
    {
      key: `${suffix}Peak`,
      description: `${description} peak over window`,
      promql: `max_over_time((sum(${metric}{${sel}}))[${windowLiteral}:${DEFAULT_SUBQUERY_STEP}])`,
    },
  ]

  const histogramQuantile = (
    key: string,
    description: string,
    metric: string,
    quantile: number,
    extraSel?: string,
  ): QuerySpec => ({
    key,
    description,
    promql: `histogram_quantile(${quantile}, sum by (le) (increase(${metric}_bucket{${sel}${extraSel ? `,${extraSel}` : ""}}[${windowLiteral}])))`,
  })

  const envoyHistogramQuantile = (
    key: string,
    description: string,
    quantile: number,
  ): QuerySpec => ({
    key,
    description,
    promql: `histogram_quantile(${quantile}, sum by (le) (increase(envoy_cluster_external_upstream_rq_time_bucket{${envoySel}}[${windowLiteral}])))`,
  })

  return [
    ...gaugeSummary("agentbox_sandboxpool_replicas_desired", "desiredReplicas", "Desired replicas"),
    ...gaugeSummary("agentbox_sandboxpool_replicas_idle", "idleReplicas", "Idle replicas"),
    ...gaugeSummary("agentbox_sandboxpool_replicas_running", "runningReplicas", "Running replicas"),
    ...gaugeSummary(
      "agentbox_sandboxpool_replicas_starting",
      "startingReplicas",
      "Starting replicas",
    ),
    ...gaugeSummary(
      "agentbox_sandboxpool_replicas_stopping",
      "stoppingReplicas",
      "Stopping replicas",
    ),
    ...gaugeSummary("agentbox_sandboxpool_replicas_failed", "failedReplicas", "Failed replicas"),
    {
      key: "createSuccess",
      description: "Successful sandbox creates",
      promql: `sum(increase(agentbox_sandbox_create_total{${sel},result="success"}[${windowLiteral}]))`,
    },
    {
      key: "createNoIdle",
      description: "Sandbox creates blocked by no idle replicas",
      promql: `sum(increase(agentbox_sandbox_create_total{${sel},result="no_idle"}[${windowLiteral}]))`,
    },
    {
      key: "createError",
      description: "Sandbox creates failed with error",
      promql: `sum(increase(agentbox_sandbox_create_total{${sel},result="error"}[${windowLiteral}]))`,
    },
    {
      key: "deleteCompleted",
      description: "Completed sandbox deletes",
      promql: `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Completed"}[${windowLiteral}]))`,
    },
    {
      key: "deleteCanceled",
      description: "Canceled sandbox deletes",
      promql: `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Canceled"}[${windowLiteral}]))`,
    },
    {
      key: "deleteReleased",
      description: "Released sandbox deletes",
      promql: `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Released"}[${windowLiteral}]))`,
    },
    {
      key: "deleteFailed",
      description: "Failed sandbox deletes",
      promql: `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Failed"}[${windowLiteral}]))`,
    },
    {
      key: "deleteTotal",
      description: "All sandbox deletes",
      promql: `sum(increase(agentbox_sandbox_delete_total{${sel}}[${windowLiteral}]))`,
    },
    {
      key: "claimSuccess",
      description: "Claim outcomes: success",
      promql: `sum(increase(agentbox_sandbox_claim_duration_seconds_count{${sel},outcome="success"}[${windowLiteral}]))`,
    },
    {
      key: "claimNoIdle",
      description: "Claim outcomes: no_idle",
      promql: `sum(increase(agentbox_sandbox_claim_duration_seconds_count{${sel},outcome="no_idle"}[${windowLiteral}]))`,
    },
    {
      key: "claimTimeout",
      description: "Claim outcomes: timeout",
      promql: `sum(increase(agentbox_sandbox_claim_duration_seconds_count{${sel},outcome="timeout"}[${windowLiteral}]))`,
    },
    {
      key: "claimError",
      description: "Claim outcomes: error",
      promql: `sum(increase(agentbox_sandbox_claim_duration_seconds_count{${sel},outcome="error"}[${windowLiteral}]))`,
    },
    histogramQuantile(
      "claimP50",
      "Claim duration P50 (seconds)",
      "agentbox_sandbox_claim_duration_seconds",
      0.5,
      `outcome="success"`,
    ),
    histogramQuantile(
      "claimP90",
      "Claim duration P90 (seconds)",
      "agentbox_sandbox_claim_duration_seconds",
      0.9,
      `outcome="success"`,
    ),
    histogramQuantile(
      "claimP95",
      "Claim duration P95 (seconds)",
      "agentbox_sandbox_claim_duration_seconds",
      0.95,
      `outcome="success"`,
    ),
    histogramQuantile(
      "claimP99",
      "Claim duration P99 (seconds)",
      "agentbox_sandbox_claim_duration_seconds",
      0.99,
      `outcome="success"`,
    ),
    histogramQuantile(
      "recycleP50",
      "Recycle duration P50 (seconds)",
      "agentbox_sandbox_recycle_duration_seconds",
      0.5,
    ),
    histogramQuantile(
      "recycleP95",
      "Recycle duration P95 (seconds)",
      "agentbox_sandbox_recycle_duration_seconds",
      0.95,
    ),
    histogramQuantile(
      "recycleP99",
      "Recycle duration P99 (seconds)",
      "agentbox_sandbox_recycle_duration_seconds",
      0.99,
    ),
    histogramQuantile(
      "runningDurationP50",
      "Running duration P50 (seconds)",
      "agentbox_sandbox_running_duration_seconds",
      0.5,
    ),
    histogramQuantile(
      "runningDurationP95",
      "Running duration P95 (seconds)",
      "agentbox_sandbox_running_duration_seconds",
      0.95,
    ),
    histogramQuantile(
      "runningDurationP99",
      "Running duration P99 (seconds)",
      "agentbox_sandbox_running_duration_seconds",
      0.99,
    ),
    {
      key: "peakCreateSuccessRps",
      description: "Peak successful sandbox create rate (req/s, 5m rate)",
      promql: `max_over_time((sum(rate(agentbox_sandbox_create_total{${sel},result="success"}[5m])))[${windowLiteral}:5m])`,
    },
    {
      key: "peakCreateAttemptRps",
      description: "Peak sandbox create attempt rate (req/s, 5m rate)",
      promql: `max_over_time((sum(rate(agentbox_sandbox_create_total{${sel}}[5m])))[${windowLiteral}:5m])`,
    },
    {
      key: "peakHttpNativeRps",
      description: "Peak native API request rate (req/s, 5m rate)",
      promql: `max_over_time((sum(rate(agentbox_http_requests_total{${sel},api="native"}[5m])))[${windowLiteral}:5m])`,
    },
    {
      key: "peakHttpE2bRps",
      description: "Peak e2b API request rate (req/s, 5m rate)",
      promql: `max_over_time((sum(rate(agentbox_http_requests_total{${sel},api="e2b"}[5m])))[${windowLiteral}:5m])`,
    },
    {
      key: "httpNativeTotal",
      description: "Native API requests",
      promql: `sum(increase(agentbox_http_requests_total{${sel},api="native"}[${windowLiteral}]))`,
    },
    {
      key: "httpE2bTotal",
      description: "E2B API requests",
      promql: `sum(increase(agentbox_http_requests_total{${sel},api="e2b"}[${windowLiteral}]))`,
    },
    {
      key: "httpNativeP95",
      description: "Native API request duration P95 (seconds)",
      promql: `histogram_quantile(0.95, sum by (le) (increase(agentbox_http_request_duration_seconds_bucket{${sel},api="native"}[${windowLiteral}])))`,
    },
    {
      key: "httpE2bP95",
      description: "E2B API request duration P95 (seconds)",
      promql: `histogram_quantile(0.95, sum by (le) (increase(agentbox_http_request_duration_seconds_bucket{${sel},api="e2b"}[${windowLiteral}])))`,
    },
    {
      key: "envoyUpstreamTotal",
      description: "Envoy external upstream requests",
      promql: `sum(increase(envoy_cluster_external_upstream_rq{${envoySel}}[${windowLiteral}]))`,
    },
    {
      key: "envoy2xx",
      description: "Envoy external upstream 2xx responses",
      promql: `sum(increase(envoy_cluster_external_upstream_rq_xx{${envoySel},envoy_response_code_class="2"}[${windowLiteral}]))`,
    },
    {
      key: "envoy4xx",
      description: "Envoy external upstream 4xx responses",
      promql: `sum(increase(envoy_cluster_external_upstream_rq_xx{${envoySel},envoy_response_code_class="4"}[${windowLiteral}]))`,
    },
    {
      key: "envoy5xx",
      description: "Envoy external upstream 5xx responses",
      promql: `sum(increase(envoy_cluster_external_upstream_rq_xx{${envoySel},envoy_response_code_class="5"}[${windowLiteral}]))`,
    },
    {
      key: "peakEnvoyRps",
      description: "Peak Envoy external upstream request rate (req/s, 5m rate)",
      promql: `max_over_time((sum(rate(envoy_cluster_external_upstream_rq{${envoySel}}[5m])))[${windowLiteral}:5m])`,
    },
    envoyHistogramQuantile("envoyP95", "Envoy external upstream request time P95", 0.95),
    envoyHistogramQuantile("envoyP99", "Envoy external upstream request time P99", 0.99),
  ]
}

export function deriveMetrics(metrics: Record<string, Scalar>): Record<string, Scalar> {
  const createAttemptTotal =
    (metrics.createSuccess ?? 0) + (metrics.createNoIdle ?? 0) + (metrics.createError ?? 0)
  const claimTotal =
    (metrics.claimSuccess ?? 0) +
    (metrics.claimNoIdle ?? 0) +
    (metrics.claimTimeout ?? 0) +
    (metrics.claimError ?? 0)

  return {
    createAttemptTotal,
    createSuccessRate: divide(metrics.createSuccess, createAttemptTotal),
    createNoIdleRate: divide(metrics.createNoIdle, createAttemptTotal),
    createErrorRate: divide(metrics.createError, createAttemptTotal),
    deleteFailedRate: divide(metrics.deleteFailed, metrics.deleteTotal),
    deleteReleasedRate: divide(metrics.deleteReleased, metrics.deleteTotal),
    claimTotal,
    claimSuccessRate: divide(metrics.claimSuccess, claimTotal),
    claimTimeoutRate: divide(metrics.claimTimeout, claimTotal),
    claimErrorRate: divide(metrics.claimError, claimTotal),
    peakCreateSuccessPerMinute:
      metrics.peakCreateSuccessRps === null ? null : metrics.peakCreateSuccessRps * 60,
    peakCreateAttemptPerMinute:
      metrics.peakCreateAttemptRps === null ? null : metrics.peakCreateAttemptRps * 60,
    peakEnvoyPerMinute: metrics.peakEnvoyRps === null ? null : metrics.peakEnvoyRps * 60,
    peakHttpNativePerMinute:
      metrics.peakHttpNativeRps === null ? null : metrics.peakHttpNativeRps * 60,
    peakHttpE2bPerMinute: metrics.peakHttpE2bRps === null ? null : metrics.peakHttpE2bRps * 60,
    envoy2xxRate: divide(metrics.envoy2xx, metrics.envoyUpstreamTotal),
    envoy5xxRate: divide(metrics.envoy5xx, metrics.envoyUpstreamTotal),
  }
}

export async function collectScope(
  url: string,
  token: string | undefined,
  scope: string,
  clusterMatcher: string,
  endTime: number,
  windowLiteral: string,
): Promise<ScopeReport> {
  const querySpecs = buildQueries(clusterMatcher, windowLiteral)
  const values = await Promise.all(
    querySpecs.map((item) =>
      queryPrometheus(url, token, item.promql, endTime).then((value) => ({
        key: item.key,
        description: item.description,
        promql: item.promql,
        value,
      })),
    ),
  )

  const metrics = Object.fromEntries(values.map((item) => [item.key, item.value]))
  const derived = deriveMetrics(metrics)

  return {
    scope,
    clusterMatcher,
    metrics,
    derived,
    queries: values,
  }
}

export interface BuildReportOptions {
  url: string
  token?: string
  clusters: string[]
  windowLiteral: string
  end?: number
}

export async function buildReport(options: BuildReportOptions): Promise<Report> {
  const { url, token, clusters, windowLiteral } = options
  const end = options.end ?? Math.floor(Date.now() / 1000)
  const windowSeconds = parseWindow(windowLiteral)
  const start = end - windowSeconds

  const scopes: ScopeReport[] = []
  scopes.push(await collectScope(url, token, "combined", clusters.join("|"), end, windowLiteral))
  for (const cluster of clusters) {
    scopes.push(await collectScope(url, token, cluster, cluster, end, windowLiteral))
  }

  return {
    generatedAt: new Date().toISOString(),
    prometheusUrl: url,
    window: { start, end, seconds: windowSeconds, literal: windowLiteral },
    clusters,
    scopes,
  }
}
