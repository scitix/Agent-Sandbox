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
 * GET /api/prometheus/envoy-error-rate
 *
 * Returns Envoy external upstream error rate trends (as fractions 0–1) over a time window.
 * Admin only.
 *
 * Computes: rate(4xx or 5xx) / rate(total) using a 5m window.
 * Returns null series points when total traffic is 0 (avoids division-by-zero artifacts).
 *
 * Query params:
 *   cluster=<required>
 *   preset=1h|6h|24h|7d  (default: 1h)  — OR —
 *   start=<unix>  end=<unix>  step=<optional>
 *
 * Response: TimeSeriesData
 *   series[0] = "4xx%"   (fraction 0–1, e.g. 0.03 = 3%)
 *   series[1] = "5xx%"
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

    // Use or() with a zero vector to avoid no-data gaps; clamp to [0,1] via clamp()
    const queries = [
      {
        name: "4xx%",
        query: `sum(rate(envoy_cluster_external_upstream_rq_xx{${envoySel},envoy_response_code_class="4"}[${rateWindow}])) / sum(rate(envoy_cluster_external_upstream_rq{${envoySel}}[${rateWindow}]))`,
      },
      {
        name: "5xx%",
        query: `sum(rate(envoy_cluster_external_upstream_rq_xx{${envoySel},envoy_response_code_class="5"}[${rateWindow}])) / sum(rate(envoy_cluster_external_upstream_rq{${envoySel}}[${rateWindow}]))`,
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
