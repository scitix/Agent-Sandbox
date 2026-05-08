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
 * GET /api/prometheus/pod-disk-io
 *
 * Returns disk read and write I/O rates for a sandbox over a time range.
 * Uses container_fs_reads_bytes_total / container_fs_writes_bytes_total joined
 * with agentbox_sandbox_running_info via label_replace(exported_pod → pod).
 *
 * container_fs_* metrics use the sandbox pod name as their pod label but
 * agentbox_sandbox_running_info carries the sandbox pod in exported_pod (not pod),
 * so we label_replace before the vector join.
 *
 * Query params:
 *   sandboxId=<required>  Sandbox ID
 *   cluster=<required>    Cluster label
 *   start=<required>      Unix timestamp (seconds)
 *   end=<required>        Unix timestamp (seconds)
 *
 * Response: TimeSeriesData with series named ["Read", "Write"]
 *   Unit: bytes/s
 */

import { NextResponse } from "next/server"
import {
  withPrometheusRoute,
  deriveStep,
  deriveRateWindow,
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

    if (!/^[a-zA-Z0-9_-]+$/.test(sandboxId) || !/^[a-zA-Z0-9_-]+$/.test(cluster)) {
      return NextResponse.json({ error: "Invalid parameter format" }, { status: 400 })
    }

    const start = parseInt(rawStart, 10)
    const end = parseInt(rawEnd, 10)
    if (isNaN(start) || isNaN(end) || start >= end) {
      return NextResponse.json({ error: "Invalid start/end timestamps" }, { status: 400 })
    }

    const step = deriveStep(end - start)
    const durationSec = end - start
    const clusterMatcher = buildClusterMatcher(cluster)
    // container filter is needed for container_fs_* to get per-container data
    const fsSelector = [clusterMatcher, `container!=""`, `container!="POD"`].join(",")
    const sandboxSelector = [clusterMatcher, `sandbox_id="${sandboxId}"`].join(",")
    const rateWindow = deriveRateWindow(step)

    // container_fs_* pod label is the sandbox pod; agentbox_sandbox_running_info
    // has the sandbox pod in exported_pod, so we label_replace it to pod for the join.
    const rateExpr = (metric: string) =>
      durationSec <= 300
        ? `irate(${metric}{${fsSelector}}[${rateWindow}])`
        : `rate(${metric}{${fsSelector}}[${rateWindow}])`

    const joinExpr = `label_replace(agentbox_sandbox_running_info{${sandboxSelector}}, "pod", "$1", "exported_pod", "(.*)")`

    const readQuery = `sum by (sandbox_id) (
  ${rateExpr("container_fs_reads_bytes_total")}
  * on(pod) group_left(sandbox_id)
  ${joinExpr}
)`
    const writeQuery = `sum by (sandbox_id) (
  ${rateExpr("container_fs_writes_bytes_total")}
  * on(pod) group_left(sandbox_id)
  ${joinExpr}
)`

    const [readResult, writeResult] = await Promise.all([
      fetchPrometheusRange(config, readQuery, start, end, step).catch(() => null),
      fetchPrometheusRange(config, writeQuery, start, end, step).catch(() => null),
    ])

    const readSeries = readResult ? rangeResultToSeries(readResult, ["Read"]) : []
    const writeSeries = writeResult ? rangeResultToSeries(writeResult, ["Write"]) : []

    return {
      series: [...readSeries, ...writeSeries],
      _promql: [readQuery, writeQuery],
    }
  },
)
