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
 * GET /api/prometheus/schedule-cas-outcome
 *
 * Per-second rate of scheduler CAS (TriggerUpdateWithOptions) outcomes,
 * broken down by outcome label. Admin-only.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: TimeSeriesData
 *   series[0] = "Success"
 *   series[1] = "Retriable"
 *   series[2] = "Hard"
 */

import { withPrometheusRoute, fetchPrometheusRange, rangeResultToSeries } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, sel, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const queries = [
      {
        name: "Success",
        query: `sum(rate(agentbox_schedule_cas_outcome_total{${sel},outcome="success"}[${rateWindow}]))`,
      },
      {
        name: "Retriable",
        query: `sum(rate(agentbox_schedule_cas_outcome_total{${sel},outcome="retriable"}[${rateWindow}]))`,
      },
      {
        name: "Hard",
        query: `sum(rate(agentbox_schedule_cas_outcome_total{${sel},outcome="hard"}[${rateWindow}]))`,
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
