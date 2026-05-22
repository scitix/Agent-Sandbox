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
 * GET /api/prometheus/envoy-bandwidth
 *
 * Returns cumulative Envoy upstream connection TX/RX byte totals over a time window.
 * Uses increase() over the selected range to compute transferred bytes.
 * Admin only.
 *
 * Query params:
 *   cluster=<required>
 *   preset=1h|6h|24h|7d  (default: 7d)  — OR —
 *   start=<unix>  end=<unix>
 *
 * Response: EnvoyBandwidthData
 *   txBytes  — sum(increase(envoy_cluster_upstream_cx_tx_bytes_total[window]))
 *   rxBytes  — sum(increase(envoy_cluster_upstream_cx_rx_bytes_total[window]))
 *
 * Admin-only: response additionally includes `promql: Record<string,string>` with the executed queries.
 */

import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  parseRangeTime,
  fetchPrometheusInstant,
  extractScalar,
  buildClusterMatcher,
  AGENTBOX_NAMESPACE,
} from "../_shared"

const ENVOY_CLUSTER_NAME = "original_dst_cluster"

function buildEnvoySelector(clusterID: string): string {
  const clusterMatcher = buildClusterMatcher(clusterID)
  return [
    clusterMatcher,
    `namespace="${AGENTBOX_NAMESPACE}"`,
    `envoy_cluster_name="${ENVOY_CLUSTER_NAME}"`,
  ].join(",")
}

function deriveWindow(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "none" },
  async ({ config, filters, request }) => {
    const sp = request.nextUrl.searchParams

    // Default to 7d (same as sandbox-cumulative-stats)
    const parsed = parseRangeTime(sp, "7d")
    if (parsed instanceof NextResponse) return parsed

    const { start, end } = parsed
    const windowStr = deriveWindow(end - start)
    const envoySel = buildEnvoySelector(filters.cluster)

    const queries = {
      txBytes: `sum(increase(envoy_cluster_upstream_cx_tx_bytes_total{${envoySel}}[${windowStr}]))`,
      rxBytes: `sum(increase(envoy_cluster_upstream_cx_rx_bytes_total{${envoySel}}[${windowStr}]))`,
    }

    const [txResult, rxResult] = await Promise.all([
      fetchPrometheusInstant(config, queries.txBytes, end).catch(() => null),
      fetchPrometheusInstant(config, queries.rxBytes, end).catch(() => null),
    ])

    const txRaw = txResult ? extractScalar(txResult) : null
    const rxRaw = rxResult ? extractScalar(rxResult) : null

    return {
      txBytes: txRaw !== null ? Math.round(txRaw) : null,
      rxBytes: rxRaw !== null ? Math.round(rxRaw) : null,
      _promql: queries,
    }
  },
)
