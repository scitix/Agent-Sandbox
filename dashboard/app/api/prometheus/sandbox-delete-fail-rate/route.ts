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
 * GET /api/prometheus/sandbox-delete-fail-rate
 *
 * Returns sandbox delete abnormal-exit rate trends (as fractions 0–1) over a time window.
 * Admin only.
 *
 * Shows the fraction of sandbox deletions that ended in each non-successful state
 * (Failed, Canceled, Released) relative to total deletions, using a 5m rate window.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "Completed %"  (fraction 0–1, normal completion)
 *   series[1] = "Failed %"    (fraction 0–1, OOM / crash / eviction)
 *   series[2] = "Released %"  (fraction 0–1, idle-timeout / auto-expiry)
 *   series[3] = "Canceled %"  (fraction 0–1, explicit user/API stop)
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange

    const totalQ = `sum(rate(agentbox_sandbox_delete_total{${sel}}[${rateWindow}]))`
    const queries = [
      {
        name: "Completed %",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Completed"}[${rateWindow}])) / ${totalQ}`,
      },
      {
        name: "Failed %",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Failed"}[${rateWindow}])) / ${totalQ}`,
      },
      {
        name: "Released %",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Released"}[${rateWindow}])) / ${totalQ}`,
      },
      {
        name: "Canceled %",
        query: `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="Canceled"}[${rateWindow}])) / ${totalQ}`,
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
