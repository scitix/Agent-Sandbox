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

// Hub BFF proxy.
// Routes: /api/hub/<path...> → WSPROXY_INTERNAL_URL/<path...>
//
// Verifies the caller's JWT (requireAuth) and forwards the Bearer token
// to wsproxy, which enforces its own RBAC (admin enforcement for write paths).
// No per-path permission checks in this layer.

import { NextResponse, type NextRequest } from "next/server"
import { requireAuth } from "@/lib/server/bff-auth"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { AuditAction } from "@/lib/audit"

const WSPROXY_INTERNAL_URL = process.env.WSPROXY_INTERNAL_URL ?? "http://localhost:9004"

async function proxyRequest(request: NextRequest, path: string[]) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const authHeader = request.headers.get("Authorization")
  const targetPath = "/" + path.join("/")
  const searchParams = new URL(request.url).searchParams.toString()
  const targetUrl = `${WSPROXY_INTERNAL_URL}${targetPath}${searchParams ? `?${searchParams}` : ""}`

  const headers: Record<string, string> = {}
  if (authHeader) headers["Authorization"] = authHeader
  const contentType = request.headers.get("Content-Type")
  if (contentType) headers["Content-Type"] = contentType

  const body =
    request.method !== "GET" && request.method !== "HEAD"
      ? await request.arrayBuffer()
      : undefined

  let res: Response
  try {
    // Native fetch is sufficient here: the hub proxy targets a fixed internal
    // address (WSPROXY_INTERNAL_URL, typically http://localhost:9004) where no
    // virtual-host routing is in play, so the Fetch API restriction on overriding
    // the `Host` header is irrelevant.
    //
    // Contrast: the cluster proxy (app/api/clusters/[clusterID]/[...path]/route.ts)
    // forwards to user-configured cluster URLs that may rely on virtual-host routing
    // (e.g. Ingress host rules). There the `Host` header must be set explicitly —
    // the Fetch API silently ignores it — so that proxy uses undici instead.
    res = await fetch(targetUrl, {
      method: request.method,
      headers,
      body: body ? Buffer.from(body) : undefined,
    })
  } catch (e) {
    console.error("[hub proxy] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Hub unavailable" }, { status: 503 })
  }

  // Audit log for mutating requests.
  if (request.method !== "GET" && request.method !== "HEAD") {
    const methodUpper = request.method.toUpperCase()
    let action: AuditAction
    if (res.status >= 400) {
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
      statusCode: res.status,
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

  const resBody = await res.arrayBuffer()
  const responseHeaders = new Headers()
  const ct = res.headers.get("Content-Type")
  if (ct) responseHeaders.set("Content-Type", ct)

  return new NextResponse(resBody.byteLength > 0 ? resBody : null, {
    status: res.status,
    headers: responseHeaders,
  })
}

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params
  return proxyRequest(request, path)
}

export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params
  return proxyRequest(request, path)
}

export async function PUT(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params
  return proxyRequest(request, path)
}

export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params
  return proxyRequest(request, path)
}

export async function PATCH(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const { path } = await params
  return proxyRequest(request, path)
}
