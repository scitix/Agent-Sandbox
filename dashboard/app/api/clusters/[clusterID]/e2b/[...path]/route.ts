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

// Proxy to a cluster's E2B-compatible API.
//
// Sibling of the native proxy (../[...path]/route.ts) with three deliberate
// differences:
//
//   - Target. The native proxy uses `cluster.url` (control plane); this one uses
//     `cluster.gateway.e2bURL`, run through `resolveHostAlias` because gateway
//     hostnames are unresolvable from this pod in environments without public
//     DNS (their hostAliases live in the ConfigMap data, not on the pod spec).
//   - Auth. The E2B middleware accepts API keys only — no Bearer JWT — so the
//     caller's own key is forwarded as `X-API-Key`. The session JWT is still
//     verified here to keep the route closed to anonymous callers, but it is not
//     sent upstream.
//   - Error shape. E2B answers `{code, message}` while the client's error handler
//     parses the native `{error, errorCode, detail}`. Responses are normalised
//     below, otherwise every failure surfaces as a bare "HTTP 500" toast.
//
// Only sandbox creation uses this today; list/get deliberately stay on native.

import { NextResponse, type NextRequest } from "next/server"
import { request as undiciRequest } from "undici"
import { getClusterConfig, listClusters, resolveHostAlias } from "@/lib/cluster-config"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { AuditAction } from "@/lib/audit"
import { requireAuth } from "@/lib/server/bff-auth"
import { impersonationFromHeaders, withAccessLog } from "@/lib/server/access-log"
import type { AccessLogContext } from "@/lib/server/access-log"

/** E2B's create route is `POST /sandboxes` — no `/v1` prefix, unlike native. */
function isSandboxCreatePath(method: string, path: string[]): boolean {
  return method === "POST" && path.length === 1 && path[0] === "sandboxes"
}

/**
 * Rewrites an E2B error body into the shape `handleErrorResponse` understands.
 * Unparseable bodies fall back to the status text the caller already has.
 */
function normaliseE2BError(raw: string): string {
  try {
    const parsed = JSON.parse(raw) as { code?: number; message?: string; error?: string }
    // Already native-shaped (some paths bubble AgentBox errors through) — leave it.
    if (parsed.error) return raw
    if (parsed.message) return JSON.stringify({ error: parsed.message })
  } catch {
    // fall through
  }
  return raw
}

function proxyRequest(request: NextRequest, clusterID: string, path: string[]) {
  return withAccessLog(request, "e2b", (log) => doProxy(request, clusterID, path, log))
}

async function doProxy(
  request: NextRequest,
  clusterID: string,
  path: string[],
  log: AccessLogContext,
) {
  log.cluster = clusterID

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

  // The caller supplies the API key the sandbox should be created with. It is
  // read explicitly rather than by forwarding client headers wholesale, and it
  // never enters the URL, the query string, or any log line here.
  const apiKey = request.headers.get("X-API-Key")
  if (!apiKey) {
    return NextResponse.json(
      { error: "Missing X-API-Key: an AgentBox API key is required to create via E2B" },
      { status: 400 },
    )
  }

  const cluster = clusterID === "default" ? listClusters()[0] : getClusterConfig(clusterID)
  if (!cluster) {
    log.note = "cluster not found in clusters.yaml"
    return NextResponse.json({ error: "Cluster not found" }, { status: 404 })
  }

  const e2bBase = cluster.gateway?.e2bURL
  if (!e2bBase) {
    // The client checks for this before choosing the E2B path, so reaching here
    // means the config changed under it. Say so plainly rather than 404ing.
    return NextResponse.json(
      { error: `Cluster ${cluster.id} has no E2B gateway configured (gateway.e2bURL)` },
      { status: 501 },
    )
  }

  const targetPath = "/" + path.join("/")
  const searchParams = new URL(request.url).searchParams.toString()
  const { url: dialBase, hostHeader } = resolveHostAlias(e2bBase)
  const targetUrl = `${dialBase.replace(/\/$/, "")}${targetPath}${searchParams ? `?${searchParams}` : ""}`
  log.upstream = targetUrl

  const headers: Record<string, string> = { "X-API-Key": apiKey }
  // Cluster-level headers first, then the alias-derived Host so it wins: the
  // alias is what makes the dialled IP route to the right virtual host.
  for (const [key, value] of Object.entries(cluster.headers ?? {})) {
    headers[key] = value
  }
  if (hostHeader) headers["Host"] = hostHeader
  if (headers["Host"]) log.upstreamHost = headers["Host"]

  // Admin impersonation, forwarded exactly as the native proxy forwards it.
  // The console's user switcher has to change whose sandboxes and whose
  // credentials this surface addresses; without these the admin's own identity
  // is used and the switcher silently does nothing here. The upstream decides
  // whether to honour them — it ignores them for a non-admin caller.
  const impersonateTeam = request.headers.get("X-Impersonate-Team")
  const impersonateUser = request.headers.get("X-Impersonate-User")
  if (impersonateTeam) headers["X-Impersonate-Team"] = impersonateTeam
  if (impersonateUser) headers["X-Impersonate-User"] = impersonateUser

  const contentType = request.headers.get("Content-Type")
  if (contentType) headers["Content-Type"] = contentType

  const body =
    request.method !== "GET" && request.method !== "HEAD" ? await request.arrayBuffer() : undefined

  let undiciRes: Awaited<ReturnType<typeof undiciRequest>>
  try {
    // undici rather than fetch: the Fetch API silently drops an overridden Host,
    // which would send the aliased IP as Host and miss the ingress vhost rule.
    undiciRes = await undiciRequest(targetUrl, {
      method: request.method as "GET" | "POST" | "PUT" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS",
      headers,
      body: body ? Buffer.from(body) : undefined,
      signal: request.signal,
      bodyTimeout: 0,
      // 65s so the Ingress's fixed 60s fires first and the backend sees a TCP
      // close (cancelling its context and running cleanup) rather than us timing
      // out first and leaving it orphaned. Same reasoning as the native proxy.
      headersTimeout: 65000,
    })
  } catch (err) {
    if (err instanceof Error && err.name === "AbortError") {
      return new NextResponse(null, { status: 499 })
    }

    // Ingress dropped the connection before the backend answered. Only creation
    // gets the stable errorCode, which is what makes the dialog show its
    // "check the list in a moment" banner instead of an opaque toast.
    const undiciCode = (err as { code?: string }).code
    if (
      (undiciCode === "UND_ERR_SOCKET" || undiciCode === "UND_ERR_HEADERS_TIMEOUT") &&
      isSandboxCreatePath(request.method, path)
    ) {
      console.warn("[BFF] e2b sandbox create exceeded Ingress gateway timeout", {
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

  const responseHeaders = new Headers()
  const ct = resHeaders["content-type"]
  const contentTypeValue = ct ? (Array.isArray(ct) ? (ct.at(0) ?? "") : ct) : ""
  if (contentTypeValue) responseHeaders.set("Content-Type", contentTypeValue)

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
      // Marked so the audit trail distinguishes an E2B create from a native one.
      path: `/e2b${targetPath}`,
      clusterID: cluster.id,
      statusCode,
      actor: {
        user: payload.user,
        team: payload.team,
        role: payload.role,
        authMethod: payload.authMethod,
        name: payload.name,
        email: payload.email,
      },
    })
  }

  const text = await resBody.text()
  const payloadText = statusCode >= 400 ? normaliseE2BError(text) : text
  return new NextResponse(payloadText, { status: statusCode, headers: responseHeaders })
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

export async function PUT(
  request: NextRequest,
  { params }: { params: Promise<{ clusterID: string; path: string[] }> },
) {
  const { clusterID, path } = await params
  return proxyRequest(request, clusterID, path)
}
