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
 * GET /api/prometheus/user-summary
 *
 * Returns sandbox counts aggregated by team and by user, including replica
 * state breakdown (desired/desired, starting, running, stopping, failed).
 * Data sources:
 *   - agentbox_sandbox_running_info  → per-sandbox running count
 *   - agentbox_sandboxpool_replicas_* → pool-level replica state counts
 *
 * Accessible by all authenticated users.
 * - tenant: team/user forced from JWT (sees only their own data)
 * - admin:  optional team/user filter; omit for global view
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>  user=<optional>  pool=<optional>  (admin only)
 *
 * Response: UserSummaryData
 *
 * Admin-only: response additionally includes `promql: Record<string,string>` mapping
 *   query names to their PromQL expressions.
 */

import { withPrometheusRoute, fetchPrometheusInstant } from "../_shared"
import type { PrometheusRawInstantData, PrometheusRawResponse } from "@/lib/types/prometheus"

type MetricMap = Map<string, number>

function parseInstantToMap(
  data: PrometheusRawResponse<PrometheusRawInstantData> | null,
  labelKeys: string[],
): MetricMap {
  const map = new Map<string, number>()
  if (!data || data.status !== "success") return map
  for (const sample of data.data.result) {
    const key = labelKeys.map((k) => sample.metric[k] ?? "").join("|")
    map.set(key, Math.round(parseFloat(sample.value[1])))
  }
  return map
}

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, sel }) => {
    const queries = {
      runningByTeam: `count by (team) (agentbox_sandbox_running_info{${sel}})`,
      runningByUser: `count by (team, user) (agentbox_sandbox_running_info{${sel}})`,
      desiredByTeam: `sum by (team) (agentbox_sandboxpool_replicas_desired{${sel}})`,
      desiredByUser: `sum by (team, user) (agentbox_sandboxpool_replicas_desired{${sel}})`,
      startingByTeam: `sum by (team) (agentbox_sandboxpool_replicas_starting{${sel}})`,
      startingByUser: `sum by (team, user) (agentbox_sandboxpool_replicas_starting{${sel}})`,
      stoppingByTeam: `sum by (team) (agentbox_sandboxpool_replicas_stopping{${sel}})`,
      stoppingByUser: `sum by (team, user) (agentbox_sandboxpool_replicas_stopping{${sel}})`,
      failedByTeam: `sum by (team) (agentbox_sandboxpool_replicas_failed{${sel}})`,
      failedByUser: `sum by (team, user) (agentbox_sandboxpool_replicas_failed{${sel}})`,
    }

    const [
      runningByTeamResult,
      runningByUserResult,
      desiredByTeamResult,
      desiredByUserResult,
      startingByTeamResult,
      startingByUserResult,
      stoppingByTeamResult,
      stoppingByUserResult,
      failedByTeamResult,
      failedByUserResult,
    ] = await Promise.all(
      Object.values(queries).map((q) => fetchPrometheusInstant(config, q).catch(() => null)),
    )

    // Build lookup maps keyed by "team" or "team|user"
    const runningTeamMap = parseInstantToMap(runningByTeamResult, ["team"])
    const runningUserMap = parseInstantToMap(runningByUserResult, ["team", "user"])
    const desiredTeamMap = parseInstantToMap(desiredByTeamResult, ["team"])
    const desiredUserMap = parseInstantToMap(desiredByUserResult, ["team", "user"])
    const startingTeamMap = parseInstantToMap(startingByTeamResult, ["team"])
    const startingUserMap = parseInstantToMap(startingByUserResult, ["team", "user"])
    const stoppingTeamMap = parseInstantToMap(stoppingByTeamResult, ["team"])
    const stoppingUserMap = parseInstantToMap(stoppingByUserResult, ["team", "user"])
    const failedTeamMap = parseInstantToMap(failedByTeamResult, ["team"])
    const failedUserMap = parseInstantToMap(failedByUserResult, ["team", "user"])

    // Collect all unique teams
    const allTeams = new Set<string>([
      ...runningTeamMap.keys(),
      ...desiredTeamMap.keys(),
      ...startingTeamMap.keys(),
      ...stoppingTeamMap.keys(),
      ...failedTeamMap.keys(),
    ])

    const byTeam = Array.from(allTeams).map((team) => ({
      team,
      desired: desiredTeamMap.get(team) ?? 0,
      starting: startingTeamMap.get(team) ?? 0,
      running: runningTeamMap.get(team) ?? 0,
      stopping: stoppingTeamMap.get(team) ?? 0,
      failed: failedTeamMap.get(team) ?? 0,
    }))

    // Collect all unique team|user combos
    const allUserKeys = new Set<string>([
      ...runningUserMap.keys(),
      ...desiredUserMap.keys(),
      ...startingUserMap.keys(),
      ...stoppingUserMap.keys(),
      ...failedUserMap.keys(),
    ])

    const byUser = Array.from(allUserKeys).map((key) => {
      const [team, user] = key.split("|")
      return {
        team,
        user,
        desired: desiredUserMap.get(key) ?? 0,
        starting: startingUserMap.get(key) ?? 0,
        running: runningUserMap.get(key) ?? 0,
        stopping: stoppingUserMap.get(key) ?? 0,
        failed: failedUserMap.get(key) ?? 0,
      }
    })

    // Sort by running desc, then team asc
    byTeam.sort((a, b) => b.running - a.running || a.team.localeCompare(b.team))
    byUser.sort(
      (a, b) =>
        b.running - a.running ||
        (a.team ?? "").localeCompare(b.team ?? "") ||
        (a.user ?? "").localeCompare(b.user ?? ""),
    )

    return { byTeam, byUser, _promql: queries }
  },
)
