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

// BFF route: POST /api/global-api-keys — create a global API key
//            GET  /api/global-api-keys — list global API keys
//
// Authentication:
//   - POST: requires a valid JWT (Dashboard user) with role "admin" or "tenant";
//     the namespace/user/team are taken from the JWT payload.
//   - GET:  requires a valid JWT; returns keys scoped to the caller's namespace.
//
// All actual K8s operations are delegated to the ws-proxy internal API
// (WSPROXY_INTERNAL_URL, default http://localhost:9004).

import { NextResponse, type NextRequest } from "next/server"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import { requireAuth } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

export async function POST(request: NextRequest) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  let body: {
    description?: string
    expiresAt?: string
    // import-mode fields
    tokenHash?: string
    hashPrefix?: string
    issuedAt?: string
    namespace?: string
    user?: string
    team?: string
    quotaURL?: string
  }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  const isImport = !!body.tokenHash && !!body.hashPrefix

  // Admin impersonation: when the admin is acting on behalf of another user,
  // use the impersonated identity instead of the JWT payload.
  const impTeam = request.headers.get("X-Impersonate-Team")
  const impUser = request.headers.get("X-Impersonate-User")
  const isImpersonating = payload.role === "admin" && !!impTeam && !!impUser

  // For import mode the caller supplies the original namespace/user/team from the
  // exported Secret, so we pass those through unchanged.  For normal creation the
  // namespace/user/team are always derived from the verified JWT so that users
  // cannot impersonate each other.
  const wsproxBody = {
    namespace: isImport ? (body.namespace ?? "") : "",
    user: isImport ? (body.user ?? "") : isImpersonating ? impUser : (payload.user ?? ""),
    team: isImport ? (body.team ?? "") : isImpersonating ? impTeam : (payload.team ?? ""),
    role: isImpersonating ? "tenant" : payload.role === "admin" ? "admin" : "tenant",
    description: body.description ?? "",
    expiresAt: body.expiresAt ?? "",
    ...(isImport && {
      tokenHash: body.tokenHash,
      hashPrefix: body.hashPrefix,
      issuedAt: body.issuedAt ?? "",
      quotaURL: body.quotaURL ?? "",
    }),
  }

  let res: Response
  try {
    res = await callWsproxy("/internal/api-keys", "POST", wsproxBody)
  } catch (e) {
    console.error("[global-api-keys POST] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Key manager unavailable" }, { status: 503 })
  }

  const resBody = await res.text()
  if (!res.ok) {
    return NextResponse.json({ error: resBody || "wsproxy error" }, { status: res.status })
  }

  let parsedBody: unknown
  try {
    parsedBody = JSON.parse(resBody)
  } catch {
    return NextResponse.json({ error: "Invalid response from key manager" }, { status: 502 })
  }

  initAudit()
  writeAuditEvent({
    timestamp: new Date().toISOString(),
    action: "apikey.create",
    method: "POST",
    path: "/api/global-api-keys",
    statusCode: 201,
    actor: {
      user: payload.user,
      team: payload.team,
      role: payload.role,
      authMethod: payload.authMethod,
      name: payload.name,
      email: payload.email,
    },
    ...(isImpersonating && {
      impersonation: { asUser: impUser!, asTeam: impTeam! },
    }),
  })

  return NextResponse.json(parsedBody, { status: 201 })
}
