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
 * GET /api/prometheus/replicas-overview
 *
 * Returns current replica counts for all sandbox pools.
 * * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *
 * Response: ReplicasOverviewData
 *
 * Admin-only: response additionally includes `promql: Record<string,string>` mapping
 *   field names to their PromQL expressions.
 */

import { withPrometheusRoute, fetchPrometheusInstant, extractScalar } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, sel, request }) => {
    // Optional end timestamp: when set, queries return point-in-time data at that moment
    // instead of the current "now". Used when the user selects an absolute time range.
    const rawEnd = request.nextUrl.searchParams.get("end")
    const time = rawEnd ? parseInt(rawEnd, 10) : undefined

    const qDesired = `sum(agentbox_sandboxpool_replicas_desired{${sel}})`
    const qRunning = `sum(agentbox_sandboxpool_replicas_running{${sel}})`
    const qIdle = `sum(agentbox_sandboxpool_replicas_idle{${sel}})`
    const qStarting = `sum(agentbox_sandboxpool_replicas_starting{${sel}})`
    const qStopping = `sum(agentbox_sandboxpool_replicas_stopping{${sel}})`
    const qFailed = `sum(agentbox_sandboxpool_replicas_failed{${sel}})`

    const [desired, running, idle, starting, stopping, failed] = await Promise.all([
      fetchPrometheusInstant(config, qDesired, time).catch(() => null),
      fetchPrometheusInstant(config, qRunning, time).catch(() => null),
      fetchPrometheusInstant(config, qIdle, time).catch(() => null),
      fetchPrometheusInstant(config, qStarting, time).catch(() => null),
      fetchPrometheusInstant(config, qStopping, time).catch(() => null),
      fetchPrometheusInstant(config, qFailed, time).catch(() => null),
    ])

    return {
      desired: desired ? extractScalar(desired) : null,
      running: running ? extractScalar(running) : null,
      idle: idle ? extractScalar(idle) : null,
      starting: starting ? extractScalar(starting) : null,
      stopping: stopping ? extractScalar(stopping) : null,
      failed: failed ? extractScalar(failed) : null,
      _promql: {
        desired: qDesired,
        running: qRunning,
        idle: qIdle,
        starting: qStarting,
        stopping: qStopping,
        failed: qFailed,
      },
    }
  },
)
