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
 * Shared Prometheus BFF utilities.
 *
 * Security model:
 * - All routes verify JWT from Authorization header
 * - Admin-only routes additionally check payload.role === "admin"
 * - PromQL is always built server-side; the client only sends structured filter params
 * - PROMETHEUS_URL and PROMETHEUS_TOKEN are server-side env vars, never exposed to clients
 *
 * Pod resource query access model:
 * - Admin:  queries by pod name directly (pod label in container_* metrics)
 * - Tenant: queries by sandbox_id via agentbox_sandbox_running_info join.
 *           The join ensures the sandbox belongs to the requesting user/team, and
 *           pod names are never exposed outside the server.
 */

import { NextResponse, type NextRequest } from "next/server"
import { getClusterConfig } from "@/lib/cluster-config"
import type { AuthJWTPayload } from "@/lib/auth"
import type {
  PrometheusRawResponse,
  PrometheusRawInstantData,
  PrometheusRawRangeData,
  ChartSeries,
  ChartPoint,
  SandboxFilters,
  TimeRangePreset,
  TimeRange,
} from "@/lib/types/prometheus"
import { PRESET_ORDER } from "@/lib/types/prometheus"
import { requireAuth, requireAdmin } from "@/lib/server/bff-auth"
export { requireAuth, requireAdmin }

// ─── Preset validation ─────────────────────────────────────────────────────

/** All valid time range presets. Shared across all BFF route files. */
export const VALID_PRESETS: TimeRangePreset[] = PRESET_ORDER

// ─── Prometheus config ─────────────────────────────────────────────────────

export interface PrometheusConfig {
  url: string
  token?: string
}

/** Read Prometheus config from env. Returns null if not configured. */
export function getPrometheusConfig(): PrometheusConfig | null {
  const url = process.env.PROMETHEUS_URL
  if (!url) return null
  return { url, token: process.env.PROMETHEUS_TOKEN }
}

/**
 * Kubernetes namespace that envoy / sandbox pods run in. Drives the
 * `namespace="..."` label selector in every PromQL query in this package.
 *
 * Sourced from the AGENTBOX_NAMESPACE env var (set by the Helm chart to the
 * hub release namespace, which is the convention for co-located hub+worker
 * installations). Falls back to "agentbox-system" when unset so existing
 * single-namespace deployments keep working.
 */
export const AGENTBOX_NAMESPACE =
  process.env.AGENTBOX_NAMESPACE || "agentbox-system"

// ─── PromQL building ───────────────────────────────────────────────────────

/**
 * Build the PromQL cluster matcher expression.
 *
 * - If the cluster config has a `selector` field, return it verbatim. This lets
 *   operators write any full label-matcher expression, e.g.
 *     `cluster="cluster1"` or `cluster="cluster1",region="region1"`.
 * - Otherwise fall back to `cluster="<id>"` so deployments that haven't set a
 *   selector yet keep working.
 */
export function buildClusterMatcher(clusterID: string): string {
  const sel = getClusterConfig(clusterID)?.selector?.trim()
  if (sel) return sel
  return `cluster="${clusterID}"`
}

/**
 * Build a Prometheus label selector string from structured filters.
 * cluster uses the configured matcher (see buildClusterMatcher); the other
 * label filters use exact match — every caller passes a single concrete
 * value (Combobox single-select on the UI side, single JWT claim on the
 * tenant path, single URL param on the admin path). Empty filters are
 * skipped so "all" is the default.
 */
export function buildSelector(filters: SandboxFilters): string {
  const parts: string[] = [buildClusterMatcher(filters.cluster)]
  if (filters.team) parts.push(`team="${filters.team}"`)
  if (filters.user) parts.push(`user="${filters.user}"`)
  if (filters.pool) parts.push(`pool="${filters.pool}"`)
  if (filters.sandboxEnv) parts.push(`sandbox_env="${filters.sandboxEnv}"`)
  return parts.join(",")
}

/** Parse SandboxFilters from URL search params. Returns error string if invalid. */
export function parseSandboxFilters(searchParams: URLSearchParams): SandboxFilters | string {
  const cluster = searchParams.get("cluster")
  if (!cluster) return "cluster parameter is required"

  return {
    cluster,
    team: searchParams.get("team") ?? undefined,
    user: searchParams.get("user") ?? undefined,
    pool: searchParams.get("pool") ?? undefined,
    sandboxEnv: searchParams.get("sandbox_env") ?? undefined,
  }
}

/**
 * Resolve SandboxFilters based on caller role — replaces parseSandboxFilters on auth-aware routes.
 *
 * - tenant: team/user are forced from the JWT payload; client-supplied values are ignored.
 * - admin:  team/user are optional query params; omitting them returns a global (unscoped) view.
 */
export function resolveFilters(
  payload: AuthJWTPayload,
  searchParams: URLSearchParams,
): SandboxFilters | string {
  const cluster = searchParams.get("cluster")
  if (!cluster) return "cluster parameter is required"

  if (payload.role !== "admin") {
    if (!payload.team || !payload.user) return "JWT missing team or user"
    return {
      cluster,
      team: payload.team,
      user: payload.user,
      pool: searchParams.get("pool") ?? undefined,
      sandboxEnv: searchParams.get("sandbox_env") ?? undefined,
    }
  }

  // Admin: optional overrides
  return {
    cluster,
    team: searchParams.get("team") ?? undefined,
    user: searchParams.get("user") ?? undefined,
    pool: searchParams.get("pool") ?? undefined,
    sandboxEnv: searchParams.get("sandbox_env") ?? undefined,
  }
}

/**
 * Derive a Prometheus step string from an absolute duration (seconds).
 * This is the single source of truth for step — the server always recomputes
 * step from end - start and ignores any `step` param sent by the client, so
 * preset and drag-zoomed ranges use identical resolution.
 *
 * Ladder (scrape interval is 15 s):
 *   ≤ 5 m   → 15s    ≤ 15 m → 30s    ≤ 1 h  → 60s     ≤ 3 h  → 2m
 *   ≤ 6 h   → 5m     ≤ 12 h → 10m    ≤ 1 d  → 15m     ≤ 2 d  → 30m
 *   ≤ 7 d   → 1h     ≤ 30 d → 4h     > 30 d → 12h
 */
export function deriveStep(durationSec: number): string {
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
 * Derive a PromQL rate/irate window from step.
 *
 * Rule: window = max(2 × step, 1m). No upper cap — the window grows with the
 * step so multi-day / multi-month views smooth appropriately (e.g. 8 h window
 * on a 30-day view) without the old 5 m ceiling flattening short views.
 */
export function deriveRateWindow(step: string): string {
  const stepSec = parseDurationSec(step)
  const windowSec = Math.max(stepSec * 2, 60)
  if (windowSec < 60) return `${windowSec}s`
  if (windowSec < 3600) return `${Math.round(windowSec / 60)}m`
  if (windowSec < 86400) return `${Math.round(windowSec / 3600)}h`
  return `${Math.round(windowSec / 86400)}d`
}

/** Parse a Prometheus duration string (e.g. "15s", "5m", "1h") to seconds. */
export function parseDurationSec(d: string): number {
  const m = /^(\d+)(ms|s|m|h|d)$/.exec(d)
  if (!m) return 60
  const n = parseInt(m[1], 10)
  switch (m[2]) {
    case "ms":
      return n / 1000
    case "s":
      return n
    case "m":
      return n * 60
    case "h":
      return n * 3600
    case "d":
      return n * 86400
    default:
      return 60
  }
}

/** Compute start/end/step from a preset label. */
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
  }
  const duration = durations[preset] ?? durations["1h"]
  return { start: now - duration, end: now, step: deriveStep(duration) }
}

// ─── Prometheus HTTP API fetch ─────────────────────────────────────────────

/** Execute a Prometheus instant query (/api/v1/query). */
export async function fetchPrometheusInstant(
  config: PrometheusConfig,
  query: string,
  time?: number,
): Promise<PrometheusRawResponse<PrometheusRawInstantData>> {
  const params = new URLSearchParams({ query })
  if (time) params.set("time", String(time))

  const headers: Record<string, string> = {}
  if (config.token) headers["Authorization"] = `Bearer ${config.token}`

  const res = await fetch(`${config.url}/api/v1/query?${params}`, { headers })
  if (!res.ok) {
    throw new Error(`Prometheus returned ${res.status}`)
  }
  return res.json() as Promise<PrometheusRawResponse<PrometheusRawInstantData>>
}

/** Execute a Prometheus range query (/api/v1/query_range). */
export async function fetchPrometheusRange(
  config: PrometheusConfig,
  query: string,
  start: number,
  end: number,
  step: string,
): Promise<PrometheusRawResponse<PrometheusRawRangeData>> {
  const params = new URLSearchParams({ query, start: String(start), end: String(end), step })

  const headers: Record<string, string> = {}
  if (config.token) headers["Authorization"] = `Bearer ${config.token}`

  const res = await fetch(`${config.url}/api/v1/query_range?${params}`, { headers })
  if (!res.ok) {
    throw new Error(`Prometheus returned ${res.status}`)
  }
  return res.json() as Promise<PrometheusRawResponse<PrometheusRawRangeData>>
}

// ─── Result extraction helpers ─────────────────────────────────────────────

/** Extract a single scalar value from an instant query result. Returns null if no data. */
export function extractScalar(
  data: PrometheusRawResponse<PrometheusRawInstantData>,
): number | null {
  if (data.status !== "success") return null
  const sample = data.data.result[0]
  if (!sample) return null
  return parseFloat(sample.value[1])
}

/**
 * Convert a Prometheus range query result into ChartSeries format.
 * The series name is taken from the `name` parameter.
 * Values are converted to millisecond timestamps for Recharts.
 */
export function rangeResultToSeries(
  data: PrometheusRawResponse<PrometheusRawRangeData>,
  seriesNames: string[],
): ChartSeries[] {
  if (data.status !== "success") return []
  return data.data.result.map((rangeSeries, i): ChartSeries => {
    const name = seriesNames[i] ?? `series_${i}`
    const points: ChartPoint[] = rangeSeries.values.map(([ts, val]) => ({
      time: ts * 1000, // convert to milliseconds
      value: parseFloat(val),
    }))
    return { name, points }
  })
}

// ─── Route wrapper ─────────────────────────────────────────────────────────

/** Resolved time range from start/end params or a preset. */
export interface ParsedTimeRange {
  start: number // Unix seconds
  end: number // Unix seconds
  step: string // Prometheus duration string, e.g. "60s", "5m"
  /** PromQL rate/irate window, derived from step (see deriveRateWindow). */
  rateWindow: string
}

/**
 * Context object injected into every withPrometheusRoute handler.
 *
 * TTimeRange is inferred from the `parseTime` option:
 *   parseTime: "range" → TTimeRange = ParsedTimeRange  (timeRange is never undefined)
 *   parseTime: "none"  → TTimeRange = undefined        (timeRange is always undefined)
 */
export interface PrometheusRouteContext<TTimeRange = undefined> {
  /** Verified JWT payload. */
  payload: AuthJWTPayload
  /** Resolved Prometheus connection config. */
  config: PrometheusConfig
  /** Pre-built PromQL label selector string. */
  sel: string
  /** Resolved SandboxFilters (before building the selector). */
  filters: SandboxFilters
  /** Resolved time range — only populated when parseTime: "range". */
  timeRange: TTimeRange
  /** Convenience alias: payload.role === "admin". */
  isAdmin: boolean
  /** The raw NextRequest for params not covered by the wrapper. */
  request: NextRequest
}

/**
 * What a route handler may return:
 *   - NextResponse → passed through as-is (for validation error returns)
 *   - Plain object → wrapped as { configured: true, data: <object>, promql?: ... }
 *
 * The optional `_promql` field carries the PromQL strings the handler used:
 *   - string[]             → for range routes (series order matches query order)
 *   - Record<string,string> → for instant routes (key = response data field name)
 *
 * The wrapper strips `_promql` from `data` and surfaces it as a top-level `promql`
 * field only when the caller is an admin.
 */
export type PrometheusHandlerResult =
  | NextResponse
  | ({ _promql?: string[] | Record<string, string> } & Record<string, unknown>)

export interface WithPrometheusRouteOptions {
  /** Which auth check to run. Default: "auth" */
  auth?: "auth" | "admin"
  /** Whether the wrapper parses the time range from query params. Default: "none" */
  parseTime?: "range" | "none"
  /** Fallback preset when no start/end or preset param is found. Default: "1h" */
  defaultPreset?: TimeRangePreset
}

/**
 * Parse a Prometheus range time window from URL search params.
 *
 * Resolution priority:
 *   1. Explicit `start` + `end` Unix-second params (validated).
 *   2. `preset` param, falling back to `defaultPreset` when absent or unrecognised.
 *
 * Note: any `step` query param is ignored — step and rateWindow are always
 * recomputed from the duration via deriveStep / deriveRateWindow so preset
 * and drag-zoomed ranges share one ladder.
 *
 * Returns ParsedTimeRange on success, or a NextResponse 400 on invalid input.
 */
export function parseRangeTime(
  searchParams: URLSearchParams,
  defaultPreset: TimeRangePreset = "1h",
): ParsedTimeRange | NextResponse {
  const rawStart = searchParams.get("start")
  const rawEnd = searchParams.get("end")

  if (rawStart && rawEnd) {
    const start = parseInt(rawStart, 10)
    const end = parseInt(rawEnd, 10)
    if (isNaN(start) || isNaN(end) || start >= end) {
      return NextResponse.json({ error: "Invalid start/end timestamps" }, { status: 400 })
    }
    const step = deriveStep(end - start)
    return { start, end, step, rateWindow: deriveRateWindow(step) }
  }

  const rawPreset = searchParams.get("preset") ?? defaultPreset
  const preset: TimeRangePreset = VALID_PRESETS.includes(rawPreset as TimeRangePreset)
    ? (rawPreset as TimeRangePreset)
    : defaultPreset
  const { start, end, step } = computeTimeRange(preset)
  return { start, end, step, rateWindow: deriveRateWindow(step) }
}

// Overload A: parseTime: "range" → timeRange is ParsedTimeRange
export function withPrometheusRoute(
  options: WithPrometheusRouteOptions & { parseTime: "range" },
  handler: (ctx: PrometheusRouteContext<ParsedTimeRange>) => Promise<PrometheusHandlerResult>,
): (request: NextRequest) => Promise<NextResponse>

// Overload B: parseTime omitted or "none" → timeRange is undefined
export function withPrometheusRoute(
  options: WithPrometheusRouteOptions & { parseTime?: "none" },
  handler: (ctx: PrometheusRouteContext<undefined>) => Promise<PrometheusHandlerResult>,
): (request: NextRequest) => Promise<NextResponse>

/**
 * Higher-order function that wraps a Prometheus BFF route handler.
 *
 * Handles in order:
 *   1. JWT auth (requireAuth or requireAdmin based on options.auth)
 *   2. Prometheus config check (returns { configured: false } if unset)
 *   3. Filter resolution + selector building
 *   4. Optional time range parsing (when options.parseTime === "range")
 *   5. Calls the handler with a typed context object
 *   6. Strips _promql from the handler result and injects it as a top-level
 *      `promql` field in the response — only when the caller is an admin.
 *
 * Usage:
 *   export const GET = withPrometheusRoute(
 *     { auth: "admin", parseTime: "range" },
 *     async ({ config, sel, timeRange, isAdmin }) => {
 *       const { start, end, step } = timeRange
 *       // ... build queries, fetch data ...
 *       return { series, _promql: queries }
 *     }
 *   )
 */
export function withPrometheusRoute(
  options: WithPrometheusRouteOptions,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  handler: (ctx: PrometheusRouteContext<any>) => Promise<PrometheusHandlerResult>,
): (request: NextRequest) => Promise<NextResponse> {
  const { auth: authLevel = "auth", parseTime = "none", defaultPreset = "1h" } = options

  return async function GET(request: NextRequest): Promise<NextResponse> {
    // 1. Auth
    const authFn = authLevel === "admin" ? requireAdmin : requireAuth
    const authResult = await authFn(request.headers.get("Authorization"))
    if ("error" in authResult) return authResult.error
    const { payload } = authResult
    const isAdmin = payload.role === "admin"

    // 2. Prometheus config
    const config = getPrometheusConfig()
    if (!config) return NextResponse.json({ configured: false })

    // 3. Resolve filters
    const searchParams = request.nextUrl.searchParams
    const filtersOrError = resolveFilters(payload, searchParams)
    if (typeof filtersOrError === "string") {
      return NextResponse.json({ error: filtersOrError }, { status: 400 })
    }
    const filters = filtersOrError
    const sel = buildSelector(filters)

    // 4. Optionally parse time range
    let timeRange: ParsedTimeRange | undefined
    if (parseTime === "range") {
      const parsed = parseRangeTime(searchParams, defaultPreset)
      if (parsed instanceof NextResponse) return parsed
      timeRange = parsed
    }

    // 5. Call route-specific handler
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const ctx: PrometheusRouteContext<any> = {
      payload,
      config,
      sel,
      filters,
      timeRange,
      isAdmin,
      request,
    }
    const result = await handler(ctx)

    // 6. Pass through raw NextResponse (early errors from handler)
    if (result instanceof NextResponse) return result

    // 7. Strip _promql, wrap in { configured, data }, inject promql for admins
    const { _promql, ...data } = result as {
      _promql?: string[] | Record<string, string>
    } & Record<string, unknown>

    return NextResponse.json({
      configured: true,
      data,
      ...(isAdmin && _promql ? { promql: _promql } : {}),
    })
  }
}
