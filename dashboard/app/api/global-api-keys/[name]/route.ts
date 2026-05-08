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

// BFF route: DELETE /api/global-api-keys/[name] — revoke a global API key
//
// Authentication: requires a valid JWT (admin or the key's owning user).
// Actual deletion is delegated to ws-proxy internal API.

import { NextResponse, type NextRequest } from "next/server"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import { requireAuth } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ name: string }> },
) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const { name } = await params
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 })
  }

  let res: Response
  try {
    res = await callWsproxy(`/internal/api-keys/${encodeURIComponent(name)}`, "DELETE")
  } catch (e) {
    console.error("[global-api-keys DELETE] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Key manager unavailable" }, { status: 503 })
  }

  if (res.status === 204) {
    initAudit()
    writeAuditEvent({
      timestamp: new Date().toISOString(),
      action: "apikey.delete",
      method: "DELETE",
      path: `/api/global-api-keys/${name}`,
      statusCode: 204,
      actor: {
        user: payload.user,
        team: payload.team,
        role: payload.role,
        authMethod: payload.authMethod,
        name: payload.name,
        email: payload.email,
      },
    })
    return new NextResponse(null, { status: 204 })
  }

  const resBody = await res.text()
  return NextResponse.json({ error: resBody || "wsproxy error" }, { status: res.status })
}
