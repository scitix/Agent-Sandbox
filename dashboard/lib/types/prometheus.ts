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

/**
 * Prometheus Metrics Types
 *
 * This module defines types for interacting with the Prometheus BFF endpoints.
 * The BFF routes are named/fixed — the frontend never sends raw PromQL queries.
 *
 * Architecture:
 *   Frontend (React Query hooks)
 *     → lib/prometheus/client.ts (GET /api/prometheus/<endpoint>)
 *     → app/api/prometheus/<endpoint>/route.ts (BFF, builds PromQL server-side)
 *     → Prometheus HTTP API
 */

// ─── Prometheus Raw API Types (used internally by BFF routes) ──────────────

/** Raw Prometheus instant query (/api/v1/query) response */
export interface PrometheusRawInstantData {
  resultType: "vector"
  result: InstantSample[]
}

/** Raw Prometheus range query (/api/v1/query_range) response */
export interface PrometheusRawRangeData {
  resultType: "matrix"
  result: RangeSeries[]
}

export interface InstantSample {
  metric: Record<string, string>
  /** [unixTimestampSeconds, valueString] */
  value: [number, string]
}

export interface RangeSeries {
  metric: Record<string, string>
  /** Array of [unixTimestampSeconds, valueString] */
  values: [number, string][]
}

export type PrometheusRawData = PrometheusRawInstantData | PrometheusRawRangeData

/** Prometheus HTTP API response envelope (used by BFF internally) */
export interface PrometheusRawResponse<T extends PrometheusRawData = PrometheusRawData> {
  status: "success" | "error"
  data: T
  errorType?: string
  error?: string
  warnings?: string[]
}

// ─── BFF Response Types (returned to the frontend) ─────────────────────────

/**
 * All BFF Prometheus responses include a `configured` flag.
 * When `false`, Prometheus is not set up — frontend hides the entire section.
 */
export interface PrometheusConfigStatus {
  configured: boolean
}

/** A single data point for time series charts */
export interface ChartPoint {
  /** Timestamp in milliseconds (suitable for Recharts) */
  time: number
  value: number
}

/** A named series for time series charts */
export interface ChartSeries {
  name: string
  points: ChartPoint[]
}

/**
 * GET /api/prometheus/replicas-overview
 * Returns current replica counts across all pools.
 */
export interface ReplicasOverviewData extends PrometheusConfigStatus {
  data?: {
    desired: number | null
    running: number | null
    idle: number | null
    starting: number | null
    stopping: number | null
    failed: number | null
  }
}

/**
 * GET /api/prometheus/start-rate
 * Returns sandbox creation rates per second (5m window), broken down by result label.
 */
export interface StartRateData extends PrometheusConfigStatus {
  data?: {
    /** Successfully started sandboxes per second */
    success: number | null
    /** Throttled requests per second (no idle pod available) */
    noIdle: number | null
    /** Error requests per second (internal error) */
    error: number | null
  }
}

/**
 * GET /api/prometheus/peak-concurrent
 * Returns peak concurrent (running) sandboxes over a lookback window.
 */
export interface PeakConcurrentData extends PrometheusConfigStatus {
  data?: {
    peak: number | null
  }
}

/**
 * Used by range endpoints:
 *   GET /api/prometheus/replicas-trend
 *   GET /api/prometheus/claim-duration
 *   GET /api/prometheus/running-duration
 *   GET /api/prometheus/recycle-duration
 *   GET /api/prometheus/pod-cpu
 *   GET /api/prometheus/pod-memory
 */
export interface TimeSeriesData extends PrometheusConfigStatus {
  data?: {
    series: ChartSeries[]
  }
}

// ─── Filter & Time Range Types ──────────────────────────────────────────────

/**
 * Label filters for sandbox pool metrics. `cluster` is required to prevent
 * cross-cluster data mixing. The optional fields are exact-match against the
 * metric label of the same name; omitting one means "all".
 */
export interface SandboxFilters {
  /** Cluster ID matching clusters.yaml, e.g. "cluster1" */
  cluster: string
  /** Optional team filter (omit for all teams). */
  team?: string
  /** Optional user filter (omit for all users). */
  user?: string
  /** Optional pool filter (omit for all pools). */
  pool?: string
  /** Optional SandboxEnv filter (omit for all envs). */
  sandboxEnv?: string
}

/** Preset time range options for chart selectors */
export type TimeRangePreset =
  | "5m"
  | "15m"
  | "30m"
  | "1h"
  | "3h"
  | "6h"
  | "12h"
  | "1d"
  | "2d"
  | "7d"
  | "30d"
  | "90d"

/**
 * Time range value: either a preset (relative to now) or an absolute range.
 * Use `resolveTimeRange()` to compute concrete start/end/step.
 */
export type TimeRangeValue =
  | { type: "preset"; preset: TimeRangePreset }
  | { type: "absolute"; start: number; end: number } // unix seconds

export interface TimeRange {
  /** Unix seconds */
  start: number
  /** Unix seconds */
  end: number
  /** Prometheus step, e.g. "60s", "5m", "15m", "1h" */
  step: string
}

/**
 * Derive a Prometheus step string from an absolute duration (seconds).
 * Ladder matches the server's deriveStep() in app/api/prometheus/_shared.ts;
 * keep them in sync. The server authoritatively recomputes step from
 * end - start, so this is only used to populate query keys + the wire payload.
 */
function stepFor(durationSec: number): string {
  if (durationSec <= 300) return "15s"
  if (durationSec <= 900) return "30s"
  if (durationSec <= 3600) return "60s"
  if (durationSec <= 10800) return "2m"
  if (durationSec <= 21600) return "5m"
  if (durationSec <= 43200) return "10m"
  if (durationSec <= 86400) return "15m"
  if (durationSec <= 172800) return "30m"
  if (durationSec <= 604800) return "1h"
  if (durationSec <= 2592000) return "4h"
  return "12h"
}

/**
 * Compute time range from a preset.
 * IMPORTANT: Must be called at render time (inside useMemo), NOT at module
 * load time — otherwise start/end will be stale.
 */
export function computeTimeRange(preset: TimeRangePreset): TimeRange {
  const now = Math.floor(Date.now() / 1000)
  const durations: Record<TimeRangePreset, number> = {
    "5m": 300,
    "15m": 900,
    "30m": 1800,
    "1h": 3600,
    "3h": 10800,
    "6h": 21600,
    "12h": 43200,
    "1d": 86400,
    "2d": 172800,
    "7d": 604800,
    "30d": 2592000,
    "90d": 7776000,
  }
  const duration = durations[preset]
  return { start: now - duration, end: now, step: stepFor(duration) }
}

/**
 * Resolve a TimeRangeValue into concrete start/end/step.
 * For absolute ranges, step is inferred from the duration via the same ladder
 * as presets.
 * IMPORTANT: Must be called at render time (inside useMemo).
 */
export function resolveTimeRange(value: TimeRangeValue): TimeRange {
  if (value.type === "preset") {
    return computeTimeRange(value.preset)
  }
  const duration = value.end - value.start
  return { start: value.start, end: value.end, step: stepFor(duration) }
}

/** Human-readable labels for preset options */
export const TIME_RANGE_PRESET_LABELS: Record<TimeRangePreset, string> = {
  "5m": "Last 5 minutes",
  "15m": "Last 15 minutes",
  "30m": "Last 30 minutes",
  "1h": "Last 1 hour",
  "3h": "Last 3 hours",
  "6h": "Last 6 hours",
  "12h": "Last 12 hours",
  "1d": "Last 1 day",
  "2d": "Last 2 days",
  "7d": "Last 7 days",
  "30d": "Last 30 days",
  "90d": "Last 90 days",
}

/** Short labels for compact display */
export const TIME_RANGE_PRESET_SHORT_LABELS: Record<TimeRangePreset, string> = {
  "5m": "5m",
  "15m": "15m",
  "30m": "30m",
  "1h": "1h",
  "3h": "3h",
  "6h": "6h",
  "12h": "12h",
  "1d": "1d",
  "2d": "2d",
  "7d": "7d",
  "30d": "30d",
  "90d": "90d",
}

/** Ordered list for zoom-out progression */
export const PRESET_ORDER: TimeRangePreset[] = [
  "5m",
  "15m",
  "30m",
  "1h",
  "3h",
  "6h",
  "12h",
  "1d",
  "2d",
  "7d",
  "30d",
  "90d",
]

/** Type guard for validating a searchParam string as a TimeRangePreset. */
export function isTimeRangePreset(value: string | null | undefined): value is TimeRangePreset {
  return !!value && (PRESET_ORDER as string[]).includes(value)
}

/**
 * Format a unix timestamp into "YYYY-MM-DD HH:mm" (browser local time).
 * Used by formatTimeRangeLabel and localized variants in components.
 */
export function formatUnixTimestamp(unix: number): string {
  const d = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/**
 * Format a TimeRangeValue into a human-readable label.
 * - Preset: "Last 1 hour"
 * - Absolute: "2026-04-02 17:21 to 2026-04-02 18:01"
 *
 * NOTE: This function always uses English labels for presets.
 * In React components, use the `useFormatTimeRangeLabel` hook instead for i18n support.
 */
export function formatTimeRangeLabel(value: TimeRangeValue): string {
  if (value.type === "preset") {
    return TIME_RANGE_PRESET_LABELS[value.preset]
  }
  return `${formatUnixTimestamp(value.start)} to ${formatUnixTimestamp(value.end)}`
}

// ─── Zoom Out ──────────────────────────────────────────────────────────────

/**
 * Compute the next zoom-out state:
 * - Preset: upgrade to next larger preset
 * - Absolute: range doubles (×2), end clamped to now so it never exceeds
 *   the current time. The extra space is added to the left (earlier start).
 */
export function zoomOut(current: TimeRangeValue): TimeRangeValue {
  if (current.type === "preset") {
    const idx = PRESET_ORDER.indexOf(current.preset)
    const next = PRESET_ORDER[Math.min(idx + 1, PRESET_ORDER.length - 1)]
    return { type: "preset", preset: next }
  }
  const now = Math.floor(Date.now() / 1000)
  const duration = current.end - current.start
  const newDuration = duration * 2
  // Clamp end to now; shift the entire extra width to the start side
  const newEnd = Math.min(current.end, now)
  const newStart = newEnd - newDuration
  return {
    type: "absolute",
    start: Math.floor(newStart),
    end: newEnd,
  }
}

// ─── Auto Refresh Interval ────────────────────────────────────────────────

/** Auto-refresh interval in milliseconds. 0 = Off. */
export type RefreshInterval = 0 | 5000 | 10000 | 30000 | 60000 | 300000 | 900000 | 1800000 | 3600000

export const REFRESH_INTERVALS: RefreshInterval[] = [
  0, 5000, 10000, 30000, 60000, 300000, 900000, 1800000, 3600000,
]

export const REFRESH_INTERVAL_LABELS: Record<RefreshInterval, string> = {
  0: "Off",
  5000: "5s",
  10000: "10s",
  30000: "30s",
  60000: "1m",
  300000: "5m",
  900000: "15m",
  1800000: "30m",
  3600000: "1h",
}

// ─── User Summary ─────────────────────────────────────────────────────────────

/**
 * GET /api/prometheus/user-summary
 * Returns sandbox counts grouped by team and by user, including replica state
 * breakdown (desired, starting, running, stopping, failed).
 * Data sources: agentbox_sandbox_running_info + agentbox_sandboxpool_replicas_*
 */
/**
 * GET /api/prometheus/sandbox-cumulative-stats
 * Returns cumulative sandbox create/delete counts and API request totals over the selected
 * time window, computed via increase() on the underlying counters.
 * Admin only.
 */
export interface SandboxCumulativeStatsData extends PrometheusConfigStatus {
  data?: {
    /** Total successfully created sandboxes in the window */
    createSuccess: number | null
    /** Total throttled creates (no idle pod) in the window */
    createNoIdle: number | null
    /** Total errored creates in the window */
    createError: number | null
    /** Total create attempts (success + no_idle + error) in the window */
    createTotal: number | null
    /** Total deleted sandboxes in the window (all stop_reasons) */
    deleteTotal: number | null
    /** Total requests to the native REST API in the window */
    httpNative: number | null
    /** Total requests to the E2B-compatible API in the window */
    httpE2b: number | null
    /** Total Envoy external upstream requests in the window */
    envoyUpstreamTotal: number | null
    /** Peak Envoy upstream request rate (req/s) over the window */
    peakEnvoyRps: number | null
  }
}

/**
 * GET /api/prometheus/envoy-bandwidth
 * Returns cumulative Envoy upstream connection TX/RX byte totals over the selected window.
 * Admin only.
 */
export interface EnvoyBandwidthData extends PrometheusConfigStatus {
  data?: {
    /** Total bytes sent to upstream (TX) in the window */
    txBytes: number | null
    /** Total bytes received from upstream (RX) in the window */
    rxBytes: number | null
  }
}

/**
 * GET /api/prometheus/sandbox-user-stats
 * Returns sandbox lifecycle stats for the authenticated user (or filtered scope).
 * Combines cumulative counters (increase over selected window) for Created/Completed/Released/Failed
 * and instant gauge values for Desired/Running.
 * Accessible by all authenticated users.
 */
export interface SandboxUserStatsData extends PrometheusConfigStatus {
  data?: {
    /** Sandboxes successfully created in the selected time window */
    createSuccess: number | null
    /** Sandboxes deleted with stop_reason=Completed in the window */
    deleteCompleted: number | null
    /** Sandboxes deleted with stop_reason=Released in the window */
    deleteReleased: number | null
    /** Sandboxes deleted with stop_reason=Failed in the window */
    deleteFailed: number | null
    /** Current pre-warmed capacity (desired replicas, point-in-time) */
    desired: number | null
    /** Currently running sandboxes (point-in-time) */
    running: number | null
  }
}

export interface CreateDistributionClusterRow {
  clusterID: string
  count: number
}

export interface CreateDistributionTeamRow {
  team: string
  count: number
}

export interface CreateDistributionUserRow {
  team: string
  user: string
  count: number
}

/**
 * GET /api/prometheus/create-distribution
 * Platform-wide sandbox creation totals over the selected window, broken
 * down by cluster/team/user. Not team-isolated — available to any
 * authenticated caller (shared by /overview's "overall usage" section and
 * /admin, differing only in which aggregate dimensions the UI renders).
 * `cluster` filter is "all" or a specific clusterID (the cluster-scope
 * selector), not a per-tenant restriction.
 */
export interface CreateDistributionData extends PrometheusConfigStatus {
  data?: {
    /** Total successful creates in the window, across the selected cluster scope */
    totalCreated: number
    byCluster: CreateDistributionClusterRow[]
    byTeam: CreateDistributionTeamRow[]
    byUser: CreateDistributionUserRow[]
    /** Distinct users with at least one successful create in the window */
    activeUsers: string[]
  }
}
