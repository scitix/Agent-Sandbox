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
 * GET /api/prometheus/sandbox-delete-rate
 *
 * Returns sandbox delete rate trend over a time window, broken down by stop_reason.
 * Accessible to all authenticated users (not admin-only).
 *
 * stop_reason values (from agentbox_sandbox_delete_total metric):
 *   Completed  — sandbox finished normally (AI agent completed its task)
 *   Canceled   — sandbox was explicitly stopped by the user / API
 *   Released   — sandbox timed out while idle (auto-expiry)
 *   Failed     — sandbox encountered an unrecoverable error (OOM, crash, eviction)
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "Completed"
 *   series[1] = "Canceled"
 *   series[2] = "Released"
 *   series[3] = "Failed"
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

const STOP_REASONS = [
  { name: "Completed", reason: "Completed" },
  { name: "Canceled", reason: "Canceled" },
  { name: "Released", reason: "Released" },
  { name: "Failed", reason: "Failed" },
]

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const queries = STOP_REASONS.map(
      ({ reason }) =>
        `sum(rate(agentbox_sandbox_delete_total{${sel},stop_reason="${reason}"}[${rateWindow}]))`,
    )
    const results = await Promise.all(
      queries.map((q) => fetchPrometheusRange(config, q, start, end, step).catch(() => null)),
    )
    const series = results.flatMap((result, i) =>
      result ? rangeResultToSeries(result, [STOP_REASONS[i].name]) : [],
    )
    return { series, _promql: queries }
  },
)
