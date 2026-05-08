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
 * GET /api/prometheus/schedule-ready-queue
 *
 * Scheduler ready-queue size and inflight-reservation size over a time window.
 * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: TimeSeriesData
 *   series[0] = "Ready Queue"
 *   series[1] = "Reservations"
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step } = timeRange
    const queries = [
      {
        name: "Ready Queue",
        query: `sum(agentbox_schedule_ready_queue_size{${sel}})`,
      },
      {
        name: "Reservations",
        query: `sum(agentbox_schedule_reservations_size{${sel}})`,
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
