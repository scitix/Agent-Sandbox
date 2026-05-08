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

import { writeFile } from "node:fs/promises"
import {
  type Report,
  DEFAULT_CLUSTERS,
  parseArgs,
  formatPercent,
  formatNumber,
  buildReport,
} from "./prometheus-report-core"

const DEFAULT_WINDOW = "7d"

function renderSummaryMarkdown(report: Report): string {
  const lines: string[] = []
  lines.push("# Sandbox Report Data Snapshot")
  lines.push("")
  lines.push(`- Generated at: ${report.generatedAt}`)
  lines.push(
    `- Window: ${new Date(report.window.start * 1000).toISOString()} -> ${new Date(report.window.end * 1000).toISOString()} (${report.window.literal})`,
  )
  lines.push(`- Clusters: ${report.clusters.join(", ")}`)
  lines.push("")

  for (const scope of report.scopes) {
    lines.push(`## ${scope.scope}`)
    lines.push("")
    lines.push("| Metric | Value |")
    lines.push("| --- | ---: |")
    lines.push(
      `| Desired replicas avg / peak / current | ${formatNumber(scope.metrics.desiredReplicasAvg)} / ${formatNumber(scope.metrics.desiredReplicasPeak)} / ${formatNumber(scope.metrics.desiredReplicasCurrent)} |`,
    )
    lines.push(
      `| Running replicas avg / peak / current | ${formatNumber(scope.metrics.runningReplicasAvg)} / ${formatNumber(scope.metrics.runningReplicasPeak)} / ${formatNumber(scope.metrics.runningReplicasCurrent)} |`,
    )
    lines.push(
      `| Idle replicas avg / peak / current | ${formatNumber(scope.metrics.idleReplicasAvg)} / ${formatNumber(scope.metrics.idleReplicasPeak)} / ${formatNumber(scope.metrics.idleReplicasCurrent)} |`,
    )
    lines.push(
      `| Failed replicas peak / current | ${formatNumber(scope.metrics.failedReplicasPeak)} / ${formatNumber(scope.metrics.failedReplicasCurrent)} |`,
    )
    lines.push(`| Successful creates | ${formatNumber(scope.metrics.createSuccess, 0)} |`)
    lines.push(`| Create attempts | ${formatNumber(scope.derived.createAttemptTotal, 0)} |`)
    lines.push(`| Create success rate | ${formatPercent(scope.derived.createSuccessRate)} |`)
    lines.push(`| No-idle rate | ${formatPercent(scope.derived.createNoIdleRate)} |`)
    lines.push(`| Delete failed rate | ${formatPercent(scope.derived.deleteFailedRate)} |`)
    lines.push(
      `| Claim success rate | ${formatPercent(scope.derived.claimSuccessRate)} |`,
    )
    lines.push(
      `| Claim P50 / P95 / P99 (s) | ${formatNumber(scope.metrics.claimP50, 3)} / ${formatNumber(scope.metrics.claimP95, 3)} / ${formatNumber(scope.metrics.claimP99, 3)} |`,
    )
    lines.push(
      `| Recycle P50 / P95 / P99 (s) | ${formatNumber(scope.metrics.recycleP50, 3)} / ${formatNumber(scope.metrics.recycleP95, 3)} / ${formatNumber(scope.metrics.recycleP99, 3)} |`,
    )
    lines.push(
      `| Running duration P50 / P95 / P99 (s) | ${formatNumber(scope.metrics.runningDurationP50, 1)} / ${formatNumber(scope.metrics.runningDurationP95, 1)} / ${formatNumber(scope.metrics.runningDurationP99, 1)} |`,
    )
    lines.push(
      `| Peak successful create throughput (sandboxes/min) | ${formatNumber(scope.derived.peakCreateSuccessPerMinute)} |`,
    )
    lines.push(
      `| Peak total create attempt throughput (req/min) | ${formatNumber(scope.derived.peakCreateAttemptPerMinute)} |`,
    )
    lines.push(
      `| Native API total / peak rpm / P95(s) | ${formatNumber(scope.metrics.httpNativeTotal, 0)} / ${formatNumber(scope.derived.peakHttpNativePerMinute)} / ${formatNumber(scope.metrics.httpNativeP95, 3)} |`,
    )
    lines.push(
      `| E2B API total / peak rpm / P95(s) | ${formatNumber(scope.metrics.httpE2bTotal, 0)} / ${formatNumber(scope.derived.peakHttpE2bPerMinute)} / ${formatNumber(scope.metrics.httpE2bP95, 3)} |`,
    )
    lines.push(
      `| Envoy upstream total / peak rpm | ${formatNumber(scope.metrics.envoyUpstreamTotal, 0)} / ${formatNumber(scope.derived.peakEnvoyPerMinute)} |`,
    )
    lines.push(
      `| Envoy 2xx / 5xx rate | ${formatPercent(scope.derived.envoy2xxRate)} / ${formatPercent(scope.derived.envoy5xxRate)} |`,
    )
    lines.push(
      `| Envoy P95 / P99 | ${formatNumber(scope.metrics.envoyP95, 2)} / ${formatNumber(scope.metrics.envoyP99, 2)} |`,
    )
    lines.push("")
  }

  lines.push("## Raw Queries")
  lines.push("")
  for (const scope of report.scopes) {
    lines.push(`### ${scope.scope}`)
    lines.push("")
    lines.push("| Key | Value | PromQL |")
    lines.push("| --- | ---: | --- |")
    for (const query of scope.queries) {
      lines.push(
        `| ${query.key} | ${query.value === null ? "n/a" : formatNumber(query.value, 6)} | \`${query.promql}\` |`,
      )
    }
    lines.push("")
  }

  return lines.join("\n")
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  const url = args.get("--url") ?? process.env.PROMETHEUS_URL
  const token = args.get("--token") ?? process.env.PROMETHEUS_TOKEN
  const clusterText = args.get("--clusters") ?? DEFAULT_CLUSTERS.join(",")
  const clusters = clusterText
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
  const windowLiteral = args.get("--window") ?? DEFAULT_WINDOW
  const end = Number.parseInt(args.get("--end") ?? String(Math.floor(Date.now() / 1000)), 10)

  if (!url) {
    throw new Error("Missing --url or PROMETHEUS_URL")
  }
  if (clusters.length === 0) {
    throw new Error("At least one cluster is required")
  }

  const report = await buildReport({ url, token, clusters, windowLiteral, end })

  const jsonOut = args.get("--json-out")
  if (jsonOut) {
    await writeFile(jsonOut, `${JSON.stringify(report, null, 2)}\n`, "utf8")
  }

  console.log(renderSummaryMarkdown(report))
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(1)
})
