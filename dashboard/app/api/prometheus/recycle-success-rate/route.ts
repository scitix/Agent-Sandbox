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
 * GET /api/prometheus/recycle-success-rate
 *
 * Per-second rate of sandbox recycle attempts (Stopping → Idle inplace update
 * with target=idle), broken down by result label.
 * Accessible to all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "Success"
 *   series[1] = "Conflict"
 *   series[2] = "Error"
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
        name: "Success",
        query: `sum(rate(agentbox_inplace_update_total{${sel},target="idle",result="success"}[${rateWindow}]))`,
      },
      {
        name: "Conflict",
        query: `sum(rate(agentbox_inplace_update_total{${sel},target="idle",result="conflict"}[${rateWindow}]))`,
      },
      {
        name: "Error",
        query: `sum(rate(agentbox_inplace_update_total{${sel},target="idle",result="error"}[${rateWindow}]))`,
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
