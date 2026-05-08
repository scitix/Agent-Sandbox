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
 * GET /api/prometheus/pod-memory
 *
 * Returns memory working set for a sandbox over a time range.
 * All callers (admin and tenant alike) pass sandboxId + cluster.
 * The BFF resolves the pod name server-side via agentbox_sandbox_running_info,
 * so pod names are never exposed to the client.
 *
 * Query params:
 *   sandboxId=<required>  Sandbox ID
 *   cluster=<required>    Cluster label
 *   start=<required>      Unix timestamp (seconds)
 *   end=<required>        Unix timestamp (seconds)
 *
 * Response: TimeSeriesData with series named "Memory"
 *   Unit: bytes
 *
 * Admin-only: response additionally includes `promql: string[]` with the executed query.
 */

import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  deriveStep,
  fetchPrometheusRange,
  rangeResultToSeries,
  buildClusterMatcher,
} from "../_shared"

export const GET = withPrometheusRoute(
  { auth: "auth", parseTime: "none" },
  async ({ config, request }) => {
    const sp = request.nextUrl.searchParams
    const sandboxId = sp.get("sandboxId")
    const cluster = sp.get("cluster")
    const rawStart = sp.get("start")
    const rawEnd = sp.get("end")

    if (!sandboxId || !cluster) {
      return NextResponse.json(
        { error: "sandboxId and cluster parameters are required" },
        { status: 400 },
      )
    }
    if (!rawStart || !rawEnd) {
      return NextResponse.json({ error: "start and end parameters are required" }, { status: 400 })
    }

    // Guard against PromQL label injection
    if (!/^[a-zA-Z0-9_-]+$/.test(sandboxId) || !/^[a-zA-Z0-9_-]+$/.test(cluster)) {
      return NextResponse.json({ error: "Invalid parameter format" }, { status: 400 })
    }

    const start = parseInt(rawStart, 10)
    const end = parseInt(rawEnd, 10)
    if (isNaN(start) || isNaN(end) || start >= end) {
      return NextResponse.json({ error: "Invalid start/end timestamps" }, { status: 400 })
    }

    const step = deriveStep(end - start)
    const clusterMatcher = buildClusterMatcher(cluster)
    const baseSelector = [clusterMatcher, `container!=""`, `container!="POD"`].join(",")
    const sandboxSelector = [clusterMatcher, `sandbox_id="${sandboxId}"`].join(",")

    // Join container_memory_working_set_bytes with agentbox_sandbox_running_info on pod.
    const query = `sum by (sandbox_id) (
  container_memory_working_set_bytes{${baseSelector}}
  * on(pod) group_left(sandbox_id)
  agentbox_sandbox_running_info{${sandboxSelector}}
)`

    const result = await fetchPrometheusRange(config, query, start, end, step).catch(() => null)
    const series = result ? rangeResultToSeries(result, ["Memory"]) : []

    return { series, _promql: [query] }
  },
)
