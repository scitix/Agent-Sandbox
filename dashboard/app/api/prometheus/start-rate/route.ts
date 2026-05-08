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
 * GET /api/prometheus/start-rate
 *
 * Returns sandbox creation rates per second (5-minute window), broken down by result:
 *   success  — sandbox was successfully started
 *   no_idle  — no idle pod was available (throttled / pool exhausted)
 *   error    — internal error during creation
 * * Accessible by all authenticated users.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   end=<optional unix> — point-in-time for instant query (default: now)
 *
 * Response: StartRateData
 *
 * Admin-only: response additionally includes `promql: Record<string,string>` mapping
 *   field names to their PromQL expressions.
 */

import { withPrometheusRoute, fetchPrometheusInstant, extractScalar } from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, sel, request }) => {
    const rawEnd = request.nextUrl.searchParams.get("end")
    const time = rawEnd ? parseInt(rawEnd, 10) : undefined

    const qSuccess = `sum(rate(agentbox_sandbox_create_total{${sel},result="success"}[5m]))`
    const qNoIdle = `sum(rate(agentbox_sandbox_create_total{${sel},result="no_idle"}[5m]))`
    const qError = `sum(rate(agentbox_sandbox_create_total{${sel},result="error"}[5m]))`

    const [successResult, noIdleResult, errorResult] = await Promise.all([
      fetchPrometheusInstant(config, qSuccess, time).catch(() => null),
      fetchPrometheusInstant(config, qNoIdle, time).catch(() => null),
      fetchPrometheusInstant(config, qError, time).catch(() => null),
    ])

    return {
      success: successResult ? extractScalar(successResult) : null,
      noIdle: noIdleResult ? extractScalar(noIdleResult) : null,
      error: errorResult ? extractScalar(errorResult) : null,
      _promql: { success: qSuccess, noIdle: qNoIdle, error: qError },
    }
  },
)
