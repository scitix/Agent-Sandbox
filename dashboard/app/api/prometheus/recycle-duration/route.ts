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
 * GET /api/prometheus/recycle-duration
 *
 * Returns Pod recycle latency percentiles (P99/P95/P90/P50) over a time window.
 * Measures Stopping → Idle recovery time.
 * * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)
 *
 * Response: TimeSeriesData
 *   series[0] = "P99"
 *   series[1] = "P95"
 *   series[2] = "P90"
 *   series[3] = "P50"
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

const PERCENTILES = [
  { q: 0.99, name: "P99" },
  { q: 0.95, name: "P95" },
  { q: 0.9, name: "P90" },
  { q: 0.5, name: "P50" },
]

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const queries = PERCENTILES.map(
      ({ q }) =>
        `histogram_quantile(${q}, sum by (le) (rate(agentbox_sandbox_recycle_duration_seconds_bucket{${sel}}[${rateWindow}])))`,
    )
    const results = await Promise.all(
      queries.map((q) => fetchPrometheusRange(config, q, start, end, step).catch(() => null)),
    )
    const series = results.flatMap((result, i) =>
      result ? rangeResultToSeries(result, [PERCENTILES[i].name]) : [],
    )
    return { series, _promql: queries }
  },
)
