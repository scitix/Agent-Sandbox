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
 * GET /api/prometheus/peak-concurrent
 *
 * Returns peak concurrent (running) sandboxes over a lookback window.
 * * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   lookbackHours=<optional, default 1>    (integer, 1-168)
 *
 * Response: PeakConcurrentData
 *
 * Admin-only: response additionally includes `promql: Record<string,string>` mapping
 *   field names to their PromQL expressions.
 */

import { withPrometheusRoute, fetchPrometheusInstant, extractScalar } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, sel, request }) => {
    const searchParams = request.nextUrl.searchParams

    // Lookback window in hours (clamped to 1-168h = 1h to 1 week)
    const rawLookback = parseInt(searchParams.get("lookbackHours") ?? "1", 10)
    const lookbackHours = Math.min(168, Math.max(1, isNaN(rawLookback) ? 1 : rawLookback))

    // Optional end timestamp: when set, queries return point-in-time data at that moment
    // instead of the current "now". Used when the user selects an absolute time range.
    const rawEnd = searchParams.get("end")
    const time = rawEnd ? parseInt(rawEnd, 10) : undefined

    const query = `max_over_time((sum(agentbox_sandboxpool_replicas_running{${sel}}))[${lookbackHours}h:1m])`
    const result = await fetchPrometheusInstant(config, query, time).catch(() => null)

    return {
      peak: result ? extractScalar(result) : null,
      _promql: { peak: query },
    }
  },
)
