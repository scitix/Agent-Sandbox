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
 * GET /api/prometheus/sandbox-create-rate
 *
 * Returns sandbox create rate trends over a time window, split by result label.
 * Accessible to all authenticated users (not admin-only).
 *
 * Create requests are split by result label for anomaly/throttle analysis:
 *   success — successful creates
 *   no_idle — throttled (pool exhausted, no idle pod available)
 *   error   — internal errors during creation
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
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "range" },
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
