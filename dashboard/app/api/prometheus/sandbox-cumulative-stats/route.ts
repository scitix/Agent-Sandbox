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
 * GET /api/prometheus/sandbox-cumulative-stats
 *
 * Returns cumulative sandbox create/delete counts, API request totals, and Envoy gateway
 * stats over a time window. Admin only.
 *
 * Uses increase() over the selected time range to compute cumulative totals.
 * Uses max_over_time() subquery for peak rate metrics.
 *
 * Query params:
 *   cluster=<required>
 *   team=<optional>     user=<optional>     pool=<optional>
 *   preset=1h|6h|24h|7d  (default: 7d)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: SandboxCumulativeStatsData
 *   createSuccess       — sum(increase(agentbox_sandbox_create_total{result="success"}[window]))
 *   createNoIdle        — sum(increase(agentbox_sandbox_create_total{result="no_idle"}[window]))
 *   createError         — sum(increase(agentbox_sandbox_create_total{result="error"}[window]))
 *   createTotal         — createSuccess + createNoIdle + createError
 *   deleteTotal         — sum(increase(agentbox_sandbox_delete_total[window]))
 *   httpNative          — sum(increase(agentbox_http_requests_total{api="native"}[window]))
 *   httpE2b             — sum(increase(agentbox_http_requests_total{api="e2b"}[window]))
 *   envoyUpstreamTotal  — sum(increase(envoy_cluster_external_upstream_rq{envoy_sel}[window]))
 *   peakEnvoyRps        — max_over_time peak envoy request rate (req/s)
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  parseRangeTime,
  fetchPrometheusInstant,
  extractScalar,
  buildClusterMatcher,
} from "../_shared"

const AGENTBOX_NAMESPACE = "agentbox-system"
const ENVOY_CLUSTER_NAME = "original_dst_cluster"

/**
 * Build Envoy-specific selector: cluster matcher + namespace + envoy_cluster_name.
 * The base sel already has cluster= but Envoy metrics use different label names,
 * so we build a fresh selector from cluster only (no team/user/pool apply to Envoy).
 */
function buildEnvoySelector(clusterID: string): string {
  const clusterMatcher = buildClusterMatcher(clusterID)
  return [
    clusterMatcher,
    `namespace="${AGENTBOX_NAMESPACE}"`,
    `envoy_cluster_name="${ENVOY_CLUSTER_NAME}"`,
  ].join(",")
}

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
  { auth: "admin", parseTime: "none" },
  async ({ config, sel, filters, request }) => {
    const sp = request.nextUrl.searchParams

    // Parse time range manually with "7d" as default (differs from the standard "1h" default)
    const parsed = parseRangeTime(sp, "7d")
    if (parsed instanceof NextResponse) return parsed

    const { start, end, rateWindow } = parsed
    const windowSec = end - start
    const endTime = end
    const windowStr = deriveWindow(windowSec)

    // Envoy selector uses cluster only (no team/user/pool labels in Envoy metrics)
    const envoySel = buildEnvoySelector(filters.cluster)

    const queries = [
      {
        key: "createSuccess",
        query: `sum(increase(agentbox_sandbox_create_total{${sel},result="success"}[${windowStr}]))`,
      },
      {
        key: "createNoIdle",
        query: `sum(increase(agentbox_sandbox_create_total{${sel},result="no_idle"}[${windowStr}]))`,
      },
      {
        key: "createError",
        query: `sum(increase(agentbox_sandbox_create_total{${sel},result="error"}[${windowStr}]))`,
      },
      {
        key: "deleteTotal",
        query: `sum(increase(agentbox_sandbox_delete_total{${sel}}[${windowStr}]))`,
      },
      {
        key: "httpNative",
        query: `sum(increase(agentbox_http_requests_total{${sel},api="native"}[${windowStr}]))`,
      },
      {
        key: "httpE2b",
        query: `sum(increase(agentbox_http_requests_total{${sel},api="e2b"}[${windowStr}]))`,
      },
      {
        key: "envoyUpstreamTotal",
        query: `sum(increase(envoy_cluster_external_upstream_rq{${envoySel}}[${windowStr}]))`,
      },
      {
        key: "peakEnvoyRps",
        query: `max_over_time((sum(rate(envoy_cluster_external_upstream_rq{${envoySel}}[${rateWindow}])))[${windowStr}:${rateWindow}])`,
      },
    ]

    const results = await Promise.all(
      queries.map(({ query }) => fetchPrometheusInstant(config, query, endTime).catch(() => null)),
    )

    const raw: Record<string, number | null> = {}
    for (let i = 0; i < queries.length; i++) {
      const result = results[i]
      const scalar = result ? extractScalar(result) : null
      // Round to integer for cumulative counts; keep precision for rates
      const key = queries[i].key
      if (key === "peakEnvoyRps") {
        raw[key] = scalar
      } else {
        raw[key] = scalar !== null ? Math.round(scalar) : null
      }
    }

    // Derive createTotal server-side
    const createSuccess = raw.createSuccess ?? null
    const createNoIdle = raw.createNoIdle ?? null
    const createError = raw.createError ?? null
    const createTotal =
      createSuccess !== null || createNoIdle !== null || createError !== null
        ? (createSuccess ?? 0) + (createNoIdle ?? 0) + (createError ?? 0)
        : null

    return {
      createSuccess,
      createNoIdle,
      createError,
      createTotal,
      deleteTotal: raw.deleteTotal ?? null,
      httpNative: raw.httpNative ?? null,
      httpE2b: raw.httpE2b ?? null,
      envoyUpstreamTotal: raw.envoyUpstreamTotal ?? null,
      peakEnvoyRps: raw.peakEnvoyRps ?? null,
      _promql: queries.map((q) => q.query),
    }
  },
)
