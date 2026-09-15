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
import { getClusterConfig, listClusters, projectForNamespace } from "@/lib/cluster-config"
import { requireAuth } from "@/lib/server/bff-auth"
import type { components } from "@/lib/api/schema"

type Sandbox = components["schemas"]["Sandbox"]

/**
 * The sandbox's own container. A Pod also runs the egress proxy and the
 * injector init containers, whose output belongs to the platform rather than
 * the user. Callers name another container explicitly to read it.
 */
const SANDBOX_CONTAINER = "sandbox"

/** Line cap when the caller names none. Matches the viewer's wrap limit. */
const DEFAULT_LIMIT = 1000

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

/**
 * Full request URL, carrying the log store this query is scoped to.
 *
 * `override` wins over the configured project and over one already present in
 * the URL — the gateway's documented endpoint form is
 * `.../logs/download?project=default`, and appending a second `project` is not
 * a syntax error: the server picks one and answers 200 with whatever that store
 * holds. Replacing is the only way to be sure which was asked.
 */
function buildLogServiceUrl(cfg: LogConfig, override?: string): string {
  const project = override || cfg.project
  if (!project) return cfg.url
  const u = new URL(cfg.url)
  u.searchParams.set("project", project)
  return u.toString()
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
  let container: string
  let from: string | undefined
  let to: string | undefined
  let keyword: string
  let limit: number
  try {
    const body = (await request.json()) as {
      sandbox?: Sandbox
      clusterID?: string
      container?: string
      from?: string
      to?: string
      keyword?: string
      limit?: number
    }
    if (!body.sandbox) {
      return NextResponse.json({ error: "sandbox is required" }, { status: 400 })
    }
    sandbox = body.sandbox
    clusterID = body.clusterID ?? "default"
    container = body.container?.trim() || SANDBOX_CONTAINER
    from = body.from
    to = body.to
    keyword = body.keyword?.trim() || ""
    limit = Number.isFinite(body.limit) ? Number(body.limit) : DEFAULT_LIMIT
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
  // The caller's window when it gave one, otherwise the run itself. startedAt
  // rather than claimedAt: claiming precedes arming, and the seconds before the
  // sandbox was usable are not what anyone is looking for. Both ends are widened
  // by a second — the record's clock and the log shipper's are not the same one,
  // and the line written just before teardown is often the interesting one.
  const startTime = from
    ? new Date(from).getTime()
    : new Date(sandbox.startedAt ?? sandbox.claimedAt).getTime() - 1_000
  const endTime = to
    ? new Date(to).getTime()
    : new Date(sandbox.terminatedAt ?? sandbox.claimedAt).getTime() + 1_000

  const requestBody = {
    kind: "container_stdout",
    filters: {
      ...extraFilters,
      pod_name: { op: "eq", value: podName },
      // The collector ships every container of the Pod. Without this the
      // egress proxy's per-connection lines — one per outbound request it
      // evaluates — are interleaved with the sandbox's own output, and there
      // are far more of them.
      container_name: { op: "eq", value: container },
      ...(containerIdRaw ? { container_id: { op: "eq", value: containerIdRaw } } : {}),
    },
    start_time: startTime,
    end_time: endTime,
    sort_order: "asc",
    limit,
    // Server-side substring match. Left off entirely when empty: the gateway
    // treats an empty `query` as a filter that matches nothing rather than as
    // no filter.
    ...(keyword ? { query: keyword } : {}),
  }

  // 7. Call the external log service and stream the NDJSON response
  //
  // A sharded deployment holds tenant namespaces and platform namespaces in
  // different stores, so the store is a property of this query rather than of
  // the deployment. Asking the wrong one returns 200 with no rows.
  const logHeaders = buildLogServiceHeaders(logConfig)
  const logServiceUrl = buildLogServiceUrl(
    logConfig,
    projectForNamespace(clusterLogs?.splitProject, sandbox.namespace),
  )

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
