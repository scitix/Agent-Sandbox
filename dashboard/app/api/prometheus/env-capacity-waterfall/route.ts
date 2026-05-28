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
 * GET /api/prometheus/env-capacity-waterfall
 *
 * Replica counts per member Pool of a SandboxEnv, broken down by phase, over
 * a time window. Powers the Env detail page's stacked capacity chart.
 *
 * Query params:
 *   cluster=<required>
 *   sandbox_env=<env name, required>  (typically equals the Env metadata.name)
 *   preset=...   |   start, end       (standard parseTime: "range" semantics)
 *
 * Response: TimeSeriesData with series named "<phase>/<pool>", e.g.
 *   "Desired/spot-a", "Running/spot-a", "Idle/spot-a", "Desired/spot-b", ...
 *
 * Admin-only: response additionally includes `promql: string[]`.
 */

import { withPrometheusRoute, fetchPrometheusRange } from "../_shared"
import type { ChartPoint, ChartSeries } from "@/lib/types/prometheus"

const PHASES: Array<{ label: string; metric: string }> = [
  { label: "Desired", metric: "agentbox_sandboxpool_replicas_desired" },
  { label: "Running", metric: "agentbox_sandboxpool_replicas_running" },
  { label: "Idle", metric: "agentbox_sandboxpool_replicas_idle" },
  { label: "Starting", metric: "agentbox_sandboxpool_replicas_starting" },
  { label: "Stopping", metric: "agentbox_sandboxpool_replicas_stopping" },
  { label: "Failed", metric: "agentbox_sandboxpool_replicas_failed" },
]

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step } = timeRange

    const queries = PHASES.map((p) => ({
      label: p.label,
      query: `sum by (pool) (${p.metric}{${sel}})`,
    }))

    const results = await Promise.all(
      queries.map(({ query }) =>
        fetchPrometheusRange(config, query, start, end, step).catch(() => null),
      ),
    )

    const series: ChartSeries[] = []
    results.forEach((result, i) => {
      if (!result || result.status !== "success") return
      const phaseLabel = queries[i].label
      for (const row of result.data.result) {
        const pool = row.metric["pool"] ?? "unknown"
        const points: ChartPoint[] = row.values.map(([ts, val]) => ({
          time: ts * 1000,
          value: parseFloat(val),
        }))
        series.push({ name: `${phaseLabel}/${pool}`, points })
      }
    })

    return { series, _promql: queries.map((q) => q.query) }
  },
)
