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
 * GET /api/prometheus/sandbox-user-stats
 *
 * Returns sandbox lifecycle stats for the current user (or filtered scope).
 * Accessible by all authenticated users (not admin-only).
 *
 * Combines:
 *   - Cumulative counts (increase over selected time window):
 *       createSuccess  — sandboxes successfully created
 *       deleteCompleted — sandboxes deleted with stop_reason="Completed"
 *       deleteReleased  — sandboxes deleted with stop_reason="Released"
 *       deleteFailed    — sandboxes deleted with stop_reason="Failed"
 *   - Instant gauge queries (point-in-time at end of range):
 *       desired — agentbox_sandboxpool_replicas_desired (Idle+Starting+Stopping)
 *       running   — agentbox_sandboxpool_replicas_running
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: SandboxUserStatsData
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  parseRangeTime,
  fetchPrometheusInstant,
  extractScalar,
} from "../_shared"

/**
 * Convert a duration in seconds to a Prometheus range vector duration string.
 * Uses the smallest unit that avoids decimal notation.
 */
function deriveWindow(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, sel, request }) => {
    const sp = request.nextUrl.searchParams

    // Parse time range with "1h" as default
    const parsed = parseRangeTime(sp, "1h")
    if (parsed instanceof NextResponse) return parsed

    const { start, end } = parsed
    const windowSec = end - start
    const endTime = end
    const windowStr = deriveWindow(windowSec)

    // Cumulative (increase over window) + instant gauge queries
    const qCreateSuccess = `sum(increase(agentbox_sandbox_create_total{${sel},result="success"}[${windowStr}]))`
    const qDeleteCompleted = `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Completed"}[${windowStr}]))`
    const qDeleteReleased = `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Released"}[${windowStr}]))`
    const qDeleteFailed = `sum(increase(agentbox_sandbox_delete_total{${sel},stop_reason="Failed"}[${windowStr}]))`
    const qDesired = `sum(agentbox_sandboxpool_replicas_desired{${sel}})`
    const qRunning = `sum(agentbox_sandboxpool_replicas_running{${sel}})`

    const [createSuccess, deleteCompleted, deleteReleased, deleteFailed, desired, running] =
      await Promise.all([
        fetchPrometheusInstant(config, qCreateSuccess, endTime).catch(() => null),
        fetchPrometheusInstant(config, qDeleteCompleted, endTime).catch(() => null),
        fetchPrometheusInstant(config, qDeleteReleased, endTime).catch(() => null),
        fetchPrometheusInstant(config, qDeleteFailed, endTime).catch(() => null),
        fetchPrometheusInstant(config, qDesired, endTime).catch(() => null),
        fetchPrometheusInstant(config, qRunning, endTime).catch(() => null),
      ])

    // Round cumulative values to integer — increase() can return fractional values due to counter resets
    const toInt = (v: ReturnType<typeof extractScalar>) => (v !== null ? Math.round(v) : null)

    return {
      createSuccess: toInt(createSuccess ? extractScalar(createSuccess) : null),
      deleteCompleted: toInt(deleteCompleted ? extractScalar(deleteCompleted) : null),
      deleteReleased: toInt(deleteReleased ? extractScalar(deleteReleased) : null),
      deleteFailed: toInt(deleteFailed ? extractScalar(deleteFailed) : null),
      desired: desired ? extractScalar(desired) : null,
      running: running ? extractScalar(running) : null,
      _promql: [
        qCreateSuccess,
        qDeleteCompleted,
        qDeleteReleased,
        qDeleteFailed,
        qDesired,
        qRunning,
      ],
    }
  },
)
