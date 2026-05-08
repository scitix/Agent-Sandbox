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
 * GET /api/prometheus/replicas-trend
 *
 * Returns desired vs running replica counts over a time window.
 * * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)
 *
 * Response: TimeSeriesData
 *   series[0] = "Prewarmed"
 *   series[1] = "Running"
 *   series[2] = "Starting"
 *   series[3] = "Stopping"
 *   series[4] = "Idle"
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
        name: "Prewarmed",
        query: `sum(agentbox_sandboxpool_replicas_desired{${sel}})`,
      },
      {
        name: "Running",
        query: `sum(agentbox_sandboxpool_replicas_running{${sel}})`,
      },
      {
        name: "Starting",
        query: `sum(agentbox_sandboxpool_replicas_starting{${sel}})`,
      },
      {
        name: "Stopping",
        query: `sum(agentbox_sandboxpool_replicas_stopping{${sel}})`,
      },
      {
        name: "Idle",
        query: `sum(agentbox_sandboxpool_replicas_idle{${sel}})`,
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

