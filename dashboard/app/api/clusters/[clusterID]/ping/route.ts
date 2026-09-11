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

// GET /api/clusters/{clusterID}/ping
//
// Calls the backend /ping endpoint (no auth required) and extracts
// X-AgentBox-Server-Version from the response header.
// Returns: { serverVersion: string }

import { NextResponse, type NextRequest } from "next/server"
import { request as undiciRequest } from "undici"
import { getClusterConfig, listClusters } from "@/lib/cluster-config"
import { withAccessLog } from "@/lib/server/access-log"
import type { AccessLogContext } from "@/lib/server/access-log"

export function GET(request: NextRequest, ctx: { params: Promise<{ clusterID: string }> }) {
  return withAccessLog(request, "ping", (log) => doPing(ctx, log))
}

async function doPing(ctx: { params: Promise<{ clusterID: string }> }, log: AccessLogContext) {
  const { clusterID } = await ctx.params
  log.cluster = clusterID

  let targetBaseUrl: string
  let extraHeaders: Record<string, string> = {}

  if (clusterID === "default") {
    const first = listClusters()[0]
    if (!first) return NextResponse.json({ error: "No clusters configured" }, { status: 503 })
    targetBaseUrl = first.url
    extraHeaders = first.headers ?? {}
  } else {
    const cluster = getClusterConfig(clusterID)
    if (!cluster) {
      log.note = "cluster not found in clusters.yaml"
      return NextResponse.json({ error: "Cluster not found" }, { status: 404 })
    }
    targetBaseUrl = cluster.url
    extraHeaders = cluster.headers ?? {}
  }

  log.upstream = `${targetBaseUrl}/ping`
  if (extraHeaders.Host) log.upstreamHost = extraHeaders.Host

  try {
    // undici rather than fetch, for the same reason as the proxy and login
    // routes: the Fetch API lists `Host` as a forbidden header and drops it
    // silently. A cluster reached by IP carries its vhost in `headers.Host`, so
    // with fetch the request arrives at the ingress addressed to an IP, matches
    // no virtual host, and comes back from the ingress itself — without the
    // version header the backend would have stamped. That is how this endpoint
    // reported "unknown" for every cluster regardless of backend health.
    const { statusCode, headers, body } = await undiciRequest(`${targetBaseUrl}/ping`, {
      method: "GET",
      headers: extraHeaders,
      // Clusters can be a region away; the 5s this used to allow was tight
      // enough to be its own failure mode.
      headersTimeout: 10000,
      bodyTimeout: 10000,
    })
    await body.dump()

    // Read the header whatever the status: it is applied by a global gin
    // middleware, so even a 404 from the backend carries the running version.
    // Only a response that never reached the backend lacks it.
    const raw = headers["x-agentbox-server-version"]
    const serverVersion = (Array.isArray(raw) ? raw[0] : raw) || "unknown"
    if (serverVersion === "unknown") {
      log.note = `ping reached ${targetBaseUrl} (HTTP ${statusCode}) but no version header`
    }
    return NextResponse.json({ serverVersion })
  } catch (e) {
    log.note = `ping failed: ${e instanceof Error ? e.message : String(e)}`
    return NextResponse.json({ serverVersion: "unknown" })
  }
}
