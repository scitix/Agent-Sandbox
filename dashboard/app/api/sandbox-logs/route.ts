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
 * POST /api/sandbox-logs
 *
 * Body: { sandbox: Sandbox, clusterID?: string }
 *
 * Proxies the external log service for terminated sandboxes (Completed / Failed).
 * Canceled sandboxes are explicitly rejected.
 * Streams the full log history as NDJSON in the same format as the AgentBox log stream endpoint.
 *
 * Feature gate: requires LOG_DOWNLOAD_URL and LOG_TOKEN env vars.
 * When not configured, returns { configured: false }.
 *
 * Two auth schemes, selected by whether LOG_APP_ID is set:
 *   - set   → the signed scheme (Signature/AppID/Timestamp/Randstr headers)
 *   - unset → Bearer, used by the unified observability query gateway
 * LOG_PROJECT, when set, is appended as the ?project= query param the gateway
 * scopes every query by. The request body and the NDJSON response are
 * identical between the two — only the envelope differs.
 *
 * Tenant users: may only query sandboxes belonging to their own team/user.
 * Admin users: unrestricted.
 *
 * Entry line format (same as AgentBox backend):
 *   {"_timestamp":"...","container_name":"...","log":"...","pod_name":"...","namespace_name":"...","node_name":"..."}
 * Terminal meta line:
 *   {"_meta":true,"source":"external-logs","truncated":false,"pod_name":"..."}
 */

import { createHash, randomBytes } from "crypto"
import { NextResponse, type NextRequest } from "next/server"
import { getClusterConfig, listClusters } from "@/lib/cluster-config"
import { requireAuth } from "@/lib/server/bff-auth"
import type { components } from "@/lib/api/schema"

type Sandbox = components["schemas"]["Sandbox"]

// ─── External log service config ──────────────────────────────────────────────

interface LogConfig {
  url: string
  /** Empty selects Bearer auth; set selects the signed scheme. */
  appId: string
  token: string
  /** Empty omits the query param entirely. */
  project: string
}

function getLogConfig(): LogConfig | null {
  const url = process.env.LOG_DOWNLOAD_URL
  const token = process.env.LOG_TOKEN
  if (!url || !token) return null
  return {
    url,
    appId: process.env.LOG_APP_ID ?? "",
    token,
    project: process.env.LOG_PROJECT ?? "",
  }
}

/** Full request URL, carrying the gateway's project scope when configured. */
function buildLogServiceUrl(cfg: LogConfig): string {
  if (!cfg.project) return cfg.url
  const sep = cfg.url.includes("?") ? "&" : "?"
  return `${cfg.url}${sep}project=${encodeURIComponent(cfg.project)}`
}

function buildLogServiceHeaders(cfg: LogConfig): Record<string, string> {
  if (!cfg.appId) {
    return {
      Authorization: `Bearer ${cfg.token}`,
      "Content-Type": "application/json",
    }
  }
  const nonce = randomBytes(5).toString("hex") // 10-char hex
  const timestamp = String(Math.floor(Date.now() / 1000))
  const signature = createHash("sha256")
    .update(cfg.token + nonce + timestamp)
    .digest("hex")
  return {
    Signature: signature,
    AppID: cfg.appId,
    Timestamp: timestamp,
    Randstr: nonce,
    "Content-Type": "application/json",
  }
}

// ─── Route handler ─────────────────────────────────────────────────────────────

export async function POST(request: NextRequest): Promise<NextResponse> {
  // 1. Auth
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  // 2. Feature gate
  const logConfig = getLogConfig()
  if (!logConfig) return NextResponse.json({ configured: false })

  // 3. Parse body
  let sandbox: Sandbox
  let clusterID: string
  try {
    const body = (await request.json()) as { sandbox?: Sandbox; clusterID?: string }
    if (!body.sandbox) {
      return NextResponse.json({ error: "sandbox is required" }, { status: 400 })
    }
    sandbox = body.sandbox
    clusterID = body.clusterID ?? "default"
  } catch {
    return NextResponse.json({ error: "Invalid JSON body" }, { status: 400 })
  }

  // 4. Ownership check for non-admin users
  if (payload.role !== "admin") {
    if (sandbox.team !== payload.team || sandbox.user !== payload.user) {
      return NextResponse.json({ error: "Forbidden" }, { status: 403 })
    }
  }

  // 5. Only serve Completed / Failed / Released — Canceled is explicitly rejected
  const allowedStatuses = ["Completed", "Failed", "Released"]
  if (!allowedStatuses.includes(sandbox.status)) {
    return NextResponse.json(
      { error: "Logs are only available for Completed, Failed or Released sandboxes" },
      { status: 400 },
    )
  }

  // 6. Build external log service request body
  const clusterLogs = (clusterID === "default" ? listClusters()[0] : getClusterConfig(clusterID))
    ?.logs
  const extraFilters: Record<string, { op: string; value: string }> = {}
  for (const [k, v] of Object.entries(clusterLogs?.filters ?? {})) {
    extraFilters[k] = { op: "eq", value: v }
  }

  // Strip the runtime prefix from containerId (e.g. "docker://abc123" → "abc123")

  // TODO: now log service doesn't support container_id filter, so we skip it for now to avoid empty results.
  // const containerIdRaw = sandbox.containerId?.replace(/^[^:]+:\/\//, "") ?? ""
  const containerIdRaw = ""

  const podName = sandbox.podName
  const startTime = new Date(sandbox.claimedAt).getTime() - 1_000
  const endTime = new Date(sandbox.terminatedAt ?? sandbox.claimedAt).getTime() + 1_000

  const requestBody = {
    kind: "container_stdout",
    filters: {
      ...extraFilters,
      pod_name: { op: "eq", value: podName },
      ...(containerIdRaw ? { container_id: { op: "eq", value: containerIdRaw } } : {}),
    },
    start_time: startTime,
    end_time: endTime,
    sort_order: "asc",
  }

  // 7. Call the external log service and stream the NDJSON response
  const logHeaders = buildLogServiceHeaders(logConfig)
  const logServiceUrl = buildLogServiceUrl(logConfig)

  // DEBUG: print equivalent cURL command. Authorization carries the raw token
  // under Bearer auth, so it is redacted — the signed scheme's headers are
  // derived values and safe to print.
  {
    const headerFlags = Object.entries(logHeaders)
      .map(([k, v]) => `-H '${k}: ${k === "Authorization" ? "Bearer <redacted>" : v}'`)
      .join(" ")
    console.log(
      `[sandbox-logs] cURL:\ncurl -s -X POST '${logServiceUrl}' ${headerFlags} -d '${JSON.stringify(requestBody)}'`,
    )
    console.log("[sandbox-logs] requestBody:", JSON.stringify(requestBody, null, 2))
  }

  let externalRes: Response
  try {
    externalRes = await fetch(logServiceUrl, {
      method: "POST",
      headers: logHeaders,
      body: JSON.stringify(requestBody),
      // TLS verification: Next.js server-side fetch uses Node's http module;
      // use NODE_TLS_REJECT_UNAUTHORIZED=0 env var for self-signed certs if needed.
    })
  } catch (err) {
    console.error("[sandbox-logs] external log service fetch error:", err)
    return NextResponse.json({ error: "External log service unavailable" }, { status: 502 })
  }

  if (!externalRes.ok) {
    const body = await externalRes.text().catch(() => "")
    console.error("[sandbox-logs] external log service returned error:", externalRes.status, body)
    return NextResponse.json(
      { error: `External log service error: ${externalRes.status}` },
      { status: 502 },
    )
  }

  // 8. Pipe the NDJSON stream, appending our meta line at the end
  const encoder = new TextEncoder()
  const metaLine =
    JSON.stringify({ _meta: true, source: "external-logs", truncated: false, pod_name: podName }) +
    "\n"

  const responseHeaders = new Headers()
  responseHeaders.set("Content-Type", "application/x-ndjson")
  responseHeaders.set("X-Accel-Buffering", "no")
  responseHeaders.set("Cache-Control", "no-cache")

  const externalBody = externalRes.body
  if (!externalBody) {
    // Empty response — just return meta line
    return new NextResponse(encoder.encode(metaLine), {
      status: 200,
      headers: responseHeaders,
    })
  }

  const passthrough = new TransformStream()
  const writer = passthrough.writable.getWriter()

  // Pipe the external stream then append our meta line
  void (async () => {
    try {
      const reader = externalBody.getReader()
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        await writer.write(value)
      }
    } finally {
      // Always emit the meta line so the frontend knows the stream is complete
      await writer.write(encoder.encode(metaLine)).catch(() => {})
      await writer.close().catch(() => {})
    }
  })()

  return new NextResponse(passthrough.readable, {
    status: 200,
    headers: responseHeaders,
  })
}
