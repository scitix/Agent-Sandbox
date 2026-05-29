---
name: add-prometheus-metric
description: Use this skill when the user wants to add a new Prometheus metric, chart, or stat card to the agentbox dashboard. Triggers on phrases like "add a metrics card", "add a chart", "add a Prometheus query", "add a new stat", "add bandwidth metric", "add envoy metric", or any request to visualize a new PromQL metric in the dashboard. Covers the full stack: backend BFF route → queryOptions → i18n → UI component integration.
version: 1.0.0
---

# Add Prometheus Metric — Full-Stack Recipe

This skill guides adding a new Prometheus metric to the agentbox dashboard. The dashboard uses a strict 4-layer pipeline: BFF route → React Query → i18n → Component. Follow each step in order.

## Architecture Overview

```
Prometheus
  ↓ PromQL (server-side only)
app/api/prometheus/<route>/route.ts      ← BFF (Next.js API route, admin-only)
  ↓ typed JSON
lib/types/prometheus.ts                  ← Response type interface
lib/queries/prometheus.ts                ← queryOptions (preset + absolute versions)
messages/zh-Hans.json + en.json          ← i18n keys
components/prometheus/prometheus-section.tsx ← AdminMetricsSection UI
```

## Decision: Instant vs Time Series vs Both

| Type                 | When                                                                    | Response type                 | PromQL                                                             | UI                          |
| -------------------- | ----------------------------------------------------------------------- | ----------------------------- | ------------------------------------------------------------------ | --------------------------- |
| **Instant** (scalar) | Single number stat card (total bytes, cumulative count, peak value)     | `number \| null` fields       | `increase(metric[window])` at `end` time                           | `StatCard`                  |
| **Time Series**      | Chart showing trend over time                                           | `TimeSeriesData` (`series[]`) | `rate(metric[window])` or `histogram_quantile(...)` as range query | `MetricsChart`              |
| **Both**             | Cumulative total (instant) AND rate trend (time series) for same metric | Two separate routes           | One instant + one range route                                      | `StatCard` + `MetricsChart` |

> **Tip**: When adding bandwidth metrics, use two routes: one instant (`increase()`) for the cumulative stat cards, and one range (`rate()`) for the time series chart — as done with `envoy-bandwidth` (instant) and `envoy-bandwidth-rate` (range).

---

## Step 1 — Backend BFF Route

Create `app/api/prometheus/<slug>/route.ts`.

### Instant query (stat card)

```typescript
import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  parseRangeTime,
  fetchPrometheusInstant,
  extractScalar,
  buildClusterMatcher, // or buildSelector(filters) for user-scoped metrics
} from "../_shared"

function deriveWindow(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "none" }, // "admin" or "auth"
  async ({ config, filters, request }) => {
    const sp = request.nextUrl.searchParams
    const parsed = parseRangeTime(sp, "7d") // default preset
    if (parsed instanceof NextResponse) return parsed

    const { start, end } = parsed
    const windowStr = deriveWindow(end - start)
    const sel = buildClusterMatcher(filters.cluster)
    // For user-scoped: import buildSelector; const sel = buildSelector(filters)

    const query = `sum(increase(your_metric_total{${sel}}[${windowStr}]))`
    const result = await fetchPrometheusInstant(config, query, end).catch(() => null)
    const value = result ? extractScalar(result) : null

    return {
      myMetric: value !== null ? Math.round(value) : null,
      _promql: { myMetric: query }, // Record<string,string> for instant
    }
  },
)
```

### Time series query (chart)

```typescript
import {
  withPrometheusRoute,
  fetchPrometheusRange,
  rangeResultToSeries,
  buildClusterMatcher,
} from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, filters, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const sel = buildClusterMatcher(filters.cluster)

    const queries = [
      { name: "TX", query: `sum(rate(metric_tx_total{${sel}}[${rateWindow}]))` },
      { name: "RX", query: `sum(rate(metric_rx_total{${sel}}[${rateWindow}]))` },
    ]
    const results = await Promise.all(
      queries.map(({ query }) =>
        fetchPrometheusRange(config, query, start, end, step).catch(() => null),
      ),
    )
    const series = results.flatMap((result, i) =>
      result ? rangeResultToSeries(result, [queries[i].name]) : [],
    )
    return { series, _promql: queries.map((q) => q.query) } // string[] for range
  },
)
```

**Rate window rule** (enforced by `deriveRateWindow` in `_shared.ts`):

`window = max(2 × step, 1m)` — the window grows with the selected time range.
Never hard-code `[5m]` or `[2m]`; always interpolate `${rateWindow}` from
`timeRange`. Current ladder (15 s scrape):

| duration ≤ | step   | rateWindow |
| ---------- | ------ | ---------- |
| 5 m        | 15 s   | 1 m        |
| 15 m       | 30 s   | 1 m        |
| 1 h        | 60 s   | 2 m        |
| 3 h        | 2 m    | 4 m        |
| 6 h        | 5 m    | 10 m       |
| 12 h       | 10 m   | 20 m       |
| 1 d        | 15 m   | 30 m       |
| 2 d        | 30 m   | 1 h        |
| 7 d        | 1 h    | 2 h        |
| 30 d       | 4 h    | 8 h        |
| > 30 d     | 12 h   | 1 d        |

**Key notes:**

- `_promql` must be `string[]` for range routes, `Record<string,string>` for instant routes
- Always `.catch(() => null)` per-query so one failure doesn't break others
- Use `buildClusterMatcher` for Envoy/infra metrics (no team/user labels); use `buildSelector(filters)` for user-scoped sandbox metrics
- Envoy metrics typically need `namespace="agentbox-system"` and `envoy_cluster_name="original_dst_cluster"` in the selector
- For `increase(metric[windowStr])` in **instant** queries (stat cards), keep the window = selected duration (e.g. `envoy-bandwidth`'s cumulative byte totals) — `rateWindow` does not apply there.

---

## Step 2 — Type Definition

Add to `lib/types/prometheus.ts` (before `SandboxUserStatsData`):

```typescript
/**
 * GET /api/prometheus/<slug>
 * Short description. Admin only.
 */
export interface MyMetricData extends PrometheusConfigStatus {
  data?: {
    /** Description */
    myMetric: number | null
    /** Add more fields as needed */
  }
}
```

For time series, use `TimeSeriesData` (already defined — no new type needed):

```typescript
// TimeSeriesData is already: { configured, data?: { series: ChartSeries[] } }
```

---

## Step 3 — React Query Options

Add to `lib/queries/prometheus.ts` (append at the end, before `useSandboxUserStats`).

First, import the new type at the top of the file:

```typescript
import type {
  // ... existing imports ...
  MyMetricData, // ← add
} from "@/lib/types/prometheus"
```

### Instant (no `step` param):

```typescript
// ─── Admin-only: My new metric ────────────────────────────────────────────────

export const myMetricQueryOptions = (
  filters: SandboxFilters,
  preset: TimeRangePreset,
  options?: { refetchInterval?: number },
) =>
  queryOptions({
    queryKey: ["prometheus", "my-metric-slug", ...filterKey(filters), preset],
    queryFn: () =>
      prometheusGet<MyMetricData>("my-metric-slug", {
        cluster: filters.cluster,
        preset,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })

export const myMetricAbsoluteQueryOptions = (
  filters: SandboxFilters,
  start: number,
  end: number,
  options?: { refetchInterval?: number }, // NOTE: no "step" for instant queries
) =>
  queryOptions({
    queryKey: ["prometheus", "my-metric-slug", ...filterKey(filters), start, end],
    queryFn: () =>
      prometheusGet<MyMetricData>("my-metric-slug", {
        cluster: filters.cluster,
        start,
        end,
      }),
    staleTime: STALE_TIME,
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval ? { refetchInterval: options.refetchInterval } : {}),
  })
```

### Time series (with `step` param):

Same pattern but include `step` in both the function signature and `prometheusGet` call, and use `TimeSeriesData` instead of a custom type.

---

## Step 4 — i18n Keys

Add to `messages/en.json`.

In `en.json`:

```json
"prometheus.myMetric": "My Metric",
"prometheus.myMetricSub": "Short subtitle",
"prometheus.myMetricTooltip": "Total accumulated something in the selected window.",
```

Then regenerate types:

```bash
pnpm i18n:gen-types
pnpm i18n:validate  # check which language should add
```

---

## Step 5 — UI Integration (AdminMetricsSection)

In `components/prometheus/prometheus-section.tsx`:

### 5a. Import the new queryOptions

```typescript
import {
  // ... existing imports ...
  myMetricQueryOptions,
  myMetricAbsoluteQueryOptions,
} from "@/lib/queries"
```

### 5b. Add useMemo in AdminMetricsSection

For **instant** queries (no `step`):

```typescript
const myMetricOpts = useMemo(
  () =>
    (isAbsolute
      ? myMetricAbsoluteQueryOptions(filters, start, end, {
          refetchInterval: effectiveRefetch,
        })
      : myMetricQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof myMetricAbsoluteQueryOptions>,
  [isAbsolute, filters, start, end, resolvedPreset, effectiveRefetch],
)
```

For **time series** (with `step`):

```typescript
const myMetricOpts = useMemo(
  () =>
    (isAbsolute
      ? myMetricAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
      : myMetricQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof myMetricAbsoluteQueryOptions>,
  [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
)
```

### 5c. Add useQuery call

```typescript
const { data: myMetricData, isLoading: myMetricLoading } = useQuery(myMetricOpts)
```

### 5d. Extract data

```typescript
const myMetric = myMetricData?.data
```

### 5e. Add StatCard (instant) or MetricsChart (time series)

**StatCard** (add inside the CumulativeStats grid):

```tsx
<StatCard
  label={t("prometheus.myMetric")}
  value={myMetric?.myValue != null ? formatBytes(myMetric.myValue) : "—"}
  sub={t("prometheus.myMetricSub")}
  icon={Globe} // pick from lucide-react
  color="text-cyan-600 dark:text-cyan-400" // pick a color
  tooltip={t("prometheus.myMetricTooltip")}
  isLoading={myMetricLoading && !myMetric}
/>
```

**MetricsChart** (add inside a chart grid section):

Pass the full React Query response via `response` — the chart extracts series
and PromQL automatically. Pass `xStart={start} xEnd={end}` so the X-axis stays
pinned to the selected range even if the series is sparse.

Each `series` entry is `{ name, color }`. Import semantic color tokens from
`components/prometheus/colors.ts` (`C.tx`, `C.rx`, `C.p99`, `C.success`, …) so
new charts share the dashboard palette. Plain name-only strings still work —
the component auto-assigns a palette color — but prefer explicit tokens.

```tsx
import { C } from "@/components/prometheus/colors"

<MetricsChart
  title={t("prometheus.myMetric")}
  description={t("prometheus.myMetricTooltip")}
  series={[
    { name: "TX", color: C.tx },
    { name: "RX", color: C.rx },
  ]}
  response={myMetricData}
  isLoading={myMetricLoading}
  isFetching={myMetricFetching}
  valueFormatter={(v) => formatBytes(v)} // or formatRate, formatDuration, etc.
  yAxisLabel="bytes"
  xStart={start}
  xEnd={end}
  onTimeRangeSelect={onTimeRangeSelect}
/>
```

Color token cheatsheet (see `colors.ts` for the full list):

| Tokens                                | Use for                             |
| ------------------------------------- | ----------------------------------- |
| `C.p99` `C.p95` `C.p90` `C.p50`       | Latency percentiles (red→blue)      |
| `C.success` `C.warning` `C.error`     | Outcome / health                    |
| `C.tx` `C.rx`                         | Network direction                   |
| `C.desired` `C.running` `C.starting` `C.stopping` `C.idle` | Replica / sandbox states |
| `C.green` `C.blue` `C.indigo` `C.orange` `C.purple` … | Generic palette     |

No `mergeChartSeries` import, no `promqlQueries` prop — both are handled inside
the component. Callers that pre-merge series for shared state across multiple
views may still pass `data={…Merged}` (legacy); prefer `response` for new code.

---

## Step 6 — Type Check

```bash
pnpm exec tsc --noEmit
```

Fix any errors before considering the work done.

---

## Available Value Formatters

| Formatter                            | Import from                  | Use for                    |
| ------------------------------------ | ---------------------------- | -------------------------- |
| `formatBytes(n)`                     | `@/lib/prometheus/transform` | Byte counts → "1.5 GB"     |
| `formatRate(n)`                      | same                         | Per-second rates → "1.5/s" |
| `formatDuration(n)`                  | same                         | Seconds → "1m 30s"         |
| `formatMilliseconds(n)`              | same                         | Milliseconds → "45ms"      |
| `formatCores(n)`                     | same                         | CPU cores → "0.5 cores"    |
| `(v) => \`${(v\*100).toFixed(2)}%\`` | inline                       | Fractions 0–1 → "3.40%"    |
| `(v) => v.toFixed(2)`                | inline                       | Floats like req/s          |

## Selector Building Cheatsheet

```typescript
// Cluster only (for Envoy/infra metrics — no team/user labels):
const sel = buildClusterMatcher(filters.cluster)

// Cluster + team/user/pool (for sandbox metrics):
const sel = buildSelector(filters)

// Envoy-specific (also needs namespace + envoy_cluster_name):
const sel = [
  buildClusterMatcher(filters.cluster),
  `namespace="agentbox-system"`,
  `envoy_cluster_name="original_dst_cluster"`,
].join(",")
```

## Common Icon Choices (lucide-react)

| Icon            | Good for                     |
| --------------- | ---------------------------- |
| `Globe`         | Network, bandwidth, external |
| `ArrowUpRight`  | Outbound / TX                |
| `Activity`      | Active counts, rates         |
| `TrendingUp`    | Create/growth metrics        |
| `Zap`           | Peak values, speed           |
| `Clock`         | Duration, timing             |
| `Server`        | Infrastructure               |
| `AlertTriangle` | Errors, failures             |
| `CheckCircle2`  | Success counts               |
| `Users`         | User-scoped metrics          |
