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

// ManagedAgent BFF proxy.
// Routes: /api/managed-agents[/<name>] → WSPROXY_INTERNAL_URL/internal/managedagents[/<name>]
//
// ManagedAgent is a control-plane resource held on the Master cluster, so it is
// reached through wsproxy rather than the per-cluster AgentBox API. This layer
// verifies the caller's JWT and forwards the Bearer token; wsproxy enforces its
// own RBAC and the team scoping the agent's spec.owner declares.

import { NextResponse, type NextRequest } from "next/server"
import { requireAuth } from "@/lib/server/bff-auth"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { AuditAction } from "@/lib/audit"

const WSPROXY_INTERNAL_URL = process.env.WSPROXY_INTERNAL_URL ?? "http://localhost:9004"
const UPSTREAM_PREFIX = "/internal/managedagents"

type RouteContext = { params: Promise<{ path?: string[] }> }

async function proxyRequest(request: NextRequest, path: string[]) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const authHeader = request.headers.get("Authorization")
  const suffix = path.map(encodeURIComponent).join("/")
  const targetPath = suffix ? `${UPSTREAM_PREFIX}/${suffix}` : UPSTREAM_PREFIX
  const searchParams = new URL(request.url).searchParams.toString()
  const targetUrl = `${WSPROXY_INTERNAL_URL}${targetPath}${searchParams ? `?${searchParams}` : ""}`

  const headers: Record<string, string> = {}
  if (authHeader) headers["Authorization"] = authHeader
  const contentType = request.headers.get("Content-Type")
  if (contentType) headers["Content-Type"] = contentType

  const body =
    request.method !== "GET" && request.method !== "HEAD" ? await request.arrayBuffer() : undefined

  let res: Response
  try {
    // Native fetch is sufficient: the target is a fixed internal address with no
    // virtual-host routing, so the Fetch API's refusal to override `Host` is
    // irrelevant here (unlike the per-cluster proxy, which uses undici).
    res = await fetch(targetUrl, {
      method: request.method,
      headers,
      body: body ? Buffer.from(body) : undefined,
    })
  } catch (e) {
    console.error("[managed-agents proxy] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Hub unavailable" }, { status: 503 })
  }

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

export async function GET(request: NextRequest, { params }: RouteContext) {
  const { path } = await params
  return proxyRequest(request, path ?? [])
}

export async function POST(request: NextRequest, { params }: RouteContext) {
  const { path } = await params
  return proxyRequest(request, path ?? [])
}

export async function PUT(request: NextRequest, { params }: RouteContext) {
  const { path } = await params
  return proxyRequest(request, path ?? [])
}

export async function PATCH(request: NextRequest, { params }: RouteContext) {
  const { path } = await params
  return proxyRequest(request, path ?? [])
}

export async function DELETE(request: NextRequest, { params }: RouteContext) {
  const { path } = await params
  return proxyRequest(request, path ?? [])
}
