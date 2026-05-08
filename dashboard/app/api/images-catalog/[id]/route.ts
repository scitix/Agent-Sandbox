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

// BFF route: PUT    /api/images-catalog/[id] — update a dataset (admin only)
//            DELETE /api/images-catalog/[id] — remove a dataset (admin only)
//
// Delegates to wsproxy internal API (/internal/images-catalog).

import { NextResponse, type NextRequest } from "next/server"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { ImageDataset } from "@/components/images/data"
import { requireAdmin } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

export async function PUT(request: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const authResult = await requireAdmin(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const { id } = await params
  let body: ImageDataset
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  let res: Response
  try {
    res = await callWsproxy(`/internal/images-catalog/${encodeURIComponent(id)}`, "PUT", {
      ...body,
      id,
    })
  } catch (e) {
    console.error("[images-catalog PUT] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Images catalog manager unavailable" }, { status: 503 })
  }

  const resBody = await res.text()
  if (!res.ok) {
    let parsed: unknown
    try {
      parsed = JSON.parse(resBody)
    } catch {
      parsed = { error: resBody || "wsproxy error" }
    }
    return NextResponse.json(parsed, { status: res.status })
  }

  initAudit()
  writeAuditEvent({
    timestamp: new Date().toISOString(),
    action: "images.update",
    method: "PUT",
    path: `/api/images-catalog/${id}`,
    statusCode: 200,
    actor: {
      user: payload.user,
      team: payload.team,
      role: payload.role,
      authMethod: payload.authMethod,
      name: payload.name,
      email: payload.email,
    },
  })

  let parsed: unknown
  try {
    parsed = JSON.parse(resBody)
  } catch {
    parsed = { ...body, id }
  }
  return NextResponse.json(parsed)
}

export async function DELETE(
  request: NextRequest,
  { params }: { params: Promise<{ id: string }> },
) {
  const authResult = await requireAdmin(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const { id } = await params

  let res: Response
  try {
    res = await callWsproxy(`/internal/images-catalog/${encodeURIComponent(id)}`, "DELETE")
  } catch (e) {
    console.error("[images-catalog DELETE] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Images catalog manager unavailable" }, { status: 503 })
  }

  if (!res.ok) {
    const resBody = await res.text()
    let parsed: unknown
    try {
      parsed = JSON.parse(resBody)
    } catch {
      parsed = { error: resBody || "wsproxy error" }
    }
    return NextResponse.json(parsed, { status: res.status })
  }

  initAudit()
  writeAuditEvent({
    timestamp: new Date().toISOString(),
    action: "images.delete",
    method: "DELETE",
    path: `/api/images-catalog/${id}`,
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
