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
 * GET /api/prometheus/envoy-latency
 *
 * Returns Envoy external upstream request latency percentiles over a time window.
 * Admin only.
 *
 * Query params:
 *   cluster=<required>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "P99"  (milliseconds)
 *   series[1] = "P95"
 *   series[2] = "P90"
 *   series[3] = "P50"
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed queries.
 */

import {
  withPrometheusRoute,
  fetchPrometheusRange,
  rangeResultToSeries,
  buildClusterMatcher,
} from "../_shared"

const AGENTBOX_NAMESPACE = "agentbox-system"
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

    // envoy_cluster_external_upstream_rq_time_bucket unit is milliseconds
    const queries = [
      {
        name: "P99",
        query: `histogram_quantile(0.99, sum by (le) (rate(envoy_cluster_external_upstream_rq_time_bucket{${envoySel}}[${rateWindow}])))`,
      },
      {
        name: "P95",
        query: `histogram_quantile(0.95, sum by (le) (rate(envoy_cluster_external_upstream_rq_time_bucket{${envoySel}}[${rateWindow}])))`,
      },
      {
        name: "P90",
        query: `histogram_quantile(0.90, sum by (le) (rate(envoy_cluster_external_upstream_rq_time_bucket{${envoySel}}[${rateWindow}])))`,
      },
      {
        name: "P50",
        query: `histogram_quantile(0.50, sum by (le) (rate(envoy_cluster_external_upstream_rq_time_bucket{${envoySel}}[${rateWindow}])))`,
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
