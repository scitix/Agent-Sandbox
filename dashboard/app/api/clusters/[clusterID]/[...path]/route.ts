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

// Cluster-aware BFF proxy.
// Routes: /api/clusters/{clusterID}/v1/... → backend /v1/...
//
// The clusterID comes from the URL — this is the authoritative source for
// cluster routing. The JWT's embedded clusterID is only used for auth
// validation (optional cross-check) but the URL clusterID drives backend
// selection.

import { NextResponse, type NextRequest } from "next/server"
import { request as undiciRequest } from "undici"
import { listClusters, getClusterConfig } from "@/lib/cluster-config"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { AuditAction } from "@/lib/audit"
import { requireAuth } from "@/lib/server/bff-auth"
import { impersonationFromHeaders, withAccessLog } from "@/lib/server/access-log"
import type { AccessLogContext } from "@/lib/server/access-log"

/**
 * Returns true when the request targets POST /v1/sandboxes (sandbox creation).
 * Does NOT match sub-resources like /v1/sandboxes/{id}/exec or /v1/sandboxes/{id}/exec-token.
 */
function isSandboxCreatePath(method: string, path: string[]): boolean {
  return method === "POST" && path.length === 2 && path[0] === "v1" && path[1] === "sandboxes"
}

function proxyRequest(request: NextRequest, clusterID: string, path: string[]) {
  return withAccessLog(request, "cluster", (log) => doProxy(request, clusterID, path, log))
}

async function doProxy(
  request: NextRequest,
  clusterID: string,
  path: string[],
  log: AccessLogContext,
) {
  log.cluster = clusterID

  // Verify JWT from Authorization header
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult
  log.actor = {
    user: payload.user,
    team: payload.team,
    role: payload.role,
    authMethod: payload.authMethod,
    ...impersonationFromHeaders(request),
  }
  const apiKey = payload.apiKey
  const authMethod = payload.authMethod
  const token = request.headers.get("Authorization")!.slice(7)

  // Determine target backend URL
  // clusterID=default → take first cluster (backward compatibility)
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

  // Build target URL: /api/clusters/{clusterID}/v1/... → backendUrl/v1/...
  const targetPath = "/" + path.join("/")
  const searchParams = new URL(request.url).searchParams.toString()
  const targetUrl = `${targetBaseUrl}${targetPath}${searchParams ? `?${searchParams}` : ""}`
  log.upstream = targetUrl

  // Forward the request to the backend.
  // - OIDC / Mock users: forward the raw JWT as Authorization: Bearer <token>
  // - API-key users: inject AGENTBOX-API-KEY header
  const headers = new Headers()
  if (authMethod === "oidc" || authMethod === "mock") {
    headers.set("Authorization", `Bearer ${token}`)
  } else {
    if (!apiKey) {
      return NextResponse.json({ error: "Invalid session: missing api key" }, { status: 401 })
    }
    headers.set("AGENTBOX-API-KEY", apiKey)
  }

  // Apply extra headers from cluster config (e.g. Host header)
  for (const [key, value] of Object.entries(extraHeaders)) {
    headers.set(key, value)
  }
  const hostOverride = headers.get("Host")
  if (hostOverride) log.upstreamHost = hostOverride

  const contentType = request.headers.get("Content-Type")
  if (contentType) {
    headers.set("Content-Type", contentType)
  }

  // Forward impersonation headers to the backend (backend enforces authorization)
  const impersonateTeam = request.headers.get("X-Impersonate-Team")
  const impersonateUser = request.headers.get("X-Impersonate-User")
  if (impersonateTeam) headers.set("X-Impersonate-Team", impersonateTeam)
  if (impersonateUser) headers.set("X-Impersonate-User", impersonateUser)

  const body =
    request.method !== "GET" && request.method !== "HEAD" ? await request.arrayBuffer() : undefined

  // Use undici instead of fetch: the Fetch API forbids overriding the `Host` header,
  // so fetch silently sends the IP as Host and backend virtual-host routing breaks.
  // undici has no such restriction.
  //
  // Note: the hub proxy (app/api/hub/[...path]/route.ts) targets a fixed internal
  // address (WSPROXY_INTERNAL_URL) with no virtual-host routing and uses native
  // fetch instead.
  const headersRecord: Record<string, string> = {}
  headers.forEach((value, key) => {
    headersRecord[key] = value
  })

  let undiciRes: Awaited<ReturnType<typeof undiciRequest>>
  try {
    undiciRes = await undiciRequest(targetUrl, {
      method: request.method as "GET" | "POST" | "PUT" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS",
      headers: headersRecord,
      body: body ? Buffer.from(body) : undefined,
      // Propagate the client's AbortSignal so that when the browser cancels the
      // request (e.g. closing the logs sheet), undici immediately aborts the
      // in-flight connection to the backend — this allows the backend to detect
      // context cancellation and clean up streaming resources (tail -f, K8s log
      // stream) without waiting for timeouts.
      signal: request.signal,
      // For streaming endpoints (NDJSON log stream, long-running exec) we need:
      //   headersTimeout: time until the backend sends the first response byte.
      //     Set to 65s so the Ingress gateway (fixed 60s) always fires first —
      //     this ensures Go backend ctx is cancelled via TCP close (not via undici timer)
      //     and cleanup runs correctly.  The old 30s value fired before the Ingress, which
      //     caused a premature cancel and an opaque 500 on the browser side.
      //   bodyTimeout: 0 disables the body-receive timeout so we can stream indefinitely.
      bodyTimeout: 0,
      headersTimeout: 65000,
    })
  } catch (err) {
    // When the client disconnects (e.g. user closes the logs sheet), the browser
    // cancels the fetch which triggers request.signal. undici then throws an
    // AbortError — this is expected and should not surface as a 500 error.
    if (err instanceof Error && err.name === "AbortError") {
      return new NextResponse(null, { status: 499 })
    }

    // Ingress gateway timeout: the Ingress (fixed 60s) drops the TCP connection
    // before the Go backend finishes sandbox creation and sends response headers.
    // undici surfaces this as UND_ERR_SOCKET (remote TCP close).  At this point
    // the Go backend's request context has been cancelled by the TCP drop, so the
    // cleanup logic (ReleaseSandboxPod) has already run correctly.
    //
    // We return 504 with a stable errorCode only for POST /v1/sandboxes so that
    // the create dialog can show an actionable "check the list" banner instead of
    // an opaque error toast.
    const undiciCode = (err as { code?: string }).code
    if (
      (undiciCode === "UND_ERR_SOCKET" || undiciCode === "UND_ERR_HEADERS_TIMEOUT") &&
      isSandboxCreatePath(request.method, path)
    ) {
      console.warn("[BFF] sandbox create exceeded Ingress gateway timeout", {
        clusterID,
        code: undiciCode,
      })
      return NextResponse.json(
        {
          error:
            "Sandbox creation is taking longer than expected. The sandbox may still be " +
            "starting — please check the Sandboxes list in a moment.",
          errorCode: "SANDBOX_CREATE_TIMEOUT",
        },
        { status: 504 },
      )
    }

    throw err
  }
  const { statusCode, headers: resHeaders, body: resBody } = undiciRes

  // Build response headers — always forward Content-Type.
  const responseHeaders = new Headers()
  const ct = resHeaders["content-type"]
  const contentTypeValue = ct ? (Array.isArray(ct) ? (ct.at(0) ?? "") : ct) : ""
  if (contentTypeValue) {
    responseHeaders.set("Content-Type", contentTypeValue)
  }

  // Audit log — record mutating requests only (non-GET/HEAD).
  // Streaming (NDJSON/SSE) responses are always triggered by GET requests
  // (log tailing, etc.) so they are implicitly excluded here.
  if (request.method !== "GET" && request.method !== "HEAD") {
    const methodUpper = request.method.toUpperCase()
    let action: AuditAction
    if (statusCode >= 400) {
      action = "api.error"
    } else if (methodUpper === "POST") {
      action = "api.create"
    } else if (methodUpper === "DELETE") {
      action = "api.delete"
    } else {
      action = "api.update"
    }
    initAudit()
    writeAuditEvent({
      timestamp: new Date().toISOString(),
      action,
      method: request.method,
      path: targetPath,
      clusterID,
      statusCode,
      actor: {
        user: payload.user,
        team: payload.team,
        role: payload.role,
        authMethod: payload.authMethod,
        name: payload.name,
        email: payload.email,
      },
      ...(impersonateUser && impersonateTeam
        ? { impersonation: { asUser: impersonateUser, asTeam: impersonateTeam } }
        : {}),
    })
  }

  // For NDJSON streaming responses, pipe the body directly without buffering.
  // Using arrayBuffer() would force the BFF to wait for the entire body before
  // returning, defeating server-sent chunked streaming and causing headersTimeout
  // errors for long-running backend operations (e.g. SPDY exec for runtime logs).
  const isNdjson = contentTypeValue.includes("ndjson") || contentTypeValue.includes("event-stream")
  if (isNdjson) {
    // Tell Nginx (and any other reverse proxy that honours X-Accel-Buffering) to
    // disable proxy buffering for this response.  We set it unconditionally here
    // rather than conditionally forwarding the backend's header, because Node.js /
    // undici may silently drop X-Accel-* hop-by-hop headers before we can read them.
    // This is the only layer that talks directly to Nginx, so it must own this header.
    responseHeaders.set("X-Accel-Buffering", "no")
    responseHeaders.set("Cache-Control", "no-cache")
    responseHeaders.set("Transfer-Encoding", "chunked")
    // Pipe the undici Readable stream directly into the Next.js response.
    // Node.js Readable implements the Web ReadableStream-compatible interface
    // when passed to NextResponse, enabling true chunked forwarding.
    return new NextResponse(resBody as unknown as ReadableStream, {
      status: statusCode,
      headers: responseHeaders,
    })
  }

  // For all other responses, buffer as before.
  const responseBody = await resBody.arrayBuffer()
  return new NextResponse(responseBody.byteLength > 0 ? responseBody : null, {
    status: statusCode,
    headers: responseHeaders,
  })
}

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}

export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}

export async function PUT(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}

export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}

export async function PATCH(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}
