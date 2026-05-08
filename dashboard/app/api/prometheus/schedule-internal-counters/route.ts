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
 * GET /api/prometheus/schedule-internal-counters
 *
 * Per-second rate of scheduler housekeeping events:
 *   - TTL Expired: reservations removed by TTL sweep
 *     (on success the CAS path intentionally keeps the reservation; TTL Expired ≈ dispatch rate is normal)
 *   - Scale-Down Skip: pods skipped during refresh due to scale-down-protected annotation
 *   - Queue Evicted: pods discarded from the ready queue at dispatch time
 *     (deleted or no longer Idle — e.g. scale-down deletions detected on pop)
 * Admin-only.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: TimeSeriesData
 *   series[0] = "TTL Expired"
 *   series[1] = "Scale-Down Skip"
 *   series[2] = "Queue Evicted"
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const queries = [
      {
        name: "TTL Expired",
        query: `sum(rate(agentbox_schedule_reservation_ttl_expired_total{${sel}}[${rateWindow}]))`,
      },
      {
        name: "Scale-Down Skip",
        query: `sum(rate(agentbox_schedule_skipped_scale_down_protected_total{${sel}}[${rateWindow}]))`,
      },
      {
        name: "Queue Evicted",
        query: `sum(rate(agentbox_schedule_ready_queue_evicted_total{${sel}}[${rateWindow}]))`,
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
