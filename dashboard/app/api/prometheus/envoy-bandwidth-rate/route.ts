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
 * GET /api/prometheus/envoy-bandwidth-rate
 *
 * Returns Envoy upstream connection TX/RX byte rate trends over a time window.
 * Uses rate() to show bandwidth throughput (bytes/s) over time.
 * Admin only.
 *
 * Query params:
 *   cluster=<required>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "TX"  — rate(envoy_cluster_upstream_cx_tx_bytes_total[rateWindow])
 *   series[1] = "RX"  — rate(envoy_cluster_upstream_cx_rx_bytes_total[rateWindow])
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import {
  withPrometheusRoute,
  fetchPrometheusRange,
  rangeResultToSeries,
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

export const GET = withPrometheusRoute(
  { auth: "admin", parseTime: "range" },
  async ({ config, filters, timeRange }) => {
    const { start, end, step, rateWindow } = timeRange
    const envoySel = buildEnvoySelector(filters.cluster)

    const queries = [
      {
        name: "TX",
        query: `sum(rate(envoy_cluster_upstream_cx_tx_bytes_total{${envoySel}}[${rateWindow}]))`,
      },
      {
        name: "RX",
        query: `sum(rate(envoy_cluster_upstream_cx_rx_bytes_total{${envoySel}}[${rateWindow}]))`,
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
