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
 * GET /api/prometheus/sandbox-rate-trend
 *
 * Returns sandbox create/delete rate trends over a time window.
 * Create requests are split by result label for anomaly/throttle analysis:
 *   success — successful creates
 *   no_idle — throttled (pool exhausted, no idle pod available)
 *   error   — internal errors during creation
 * Delete requests are split by stop_reason:
 *   Completed — sandbox finished normally
 *   Canceled  — explicitly stopped by user / API
 *   Released  — auto-expired while idle
 *   Failed    — unrecoverable error (OOM, crash, eviction)
 * * Admin only.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "Create Success"
 *   series[1] = "Create No Idle"
 *   series[2] = "Create Error"
 *   series[3] = "Completed"
 *   series[4] = "Canceled"
 *   series[5] = "Released"
 *   series[6] = "Failed"
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const queries = [
      {
        name: "Create Success",
        query: `sum(rate(agentbox_sandbox_create_total{${sel},result="success"}[${rateWindow}]))`,
      },
      {
        name: "Create No Idle",
        query: `sum(rate(agentbox_sandbox_create_total{${sel},result="no_idle"}[${rateWindow}]))`,
      },
      {
        name: "Create Error",
        query: `sum(rate(agentbox_sandbox_create_total{${sel},result="error"}[${rateWindow}]))`,
      },
      {
        name: "Completed",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Completed"}[${rateWindow}]))`,
      },
      {
        name: "Canceled",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Canceled"}[${rateWindow}]))`,
      },
      {
        name: "Released",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Released"}[${rateWindow}]))`,
      },
      {
        name: "Failed",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Failed"}[${rateWindow}]))`,
      },
    ]
    const results = await Promise.all(
      queries.map(({ query }) =>
        fetchPrometheusRange(config, query, start, end, step).catch(() => null),
      ),
    )
    const series = results.flatMap((result, i) =>
      result ? rangeResultToSeries(result, [queries[i].name]) : [],
    )
    return { series, _promql: queries.map((q) => q.query) }
  },
)
