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

// BFF route: GET /api/images-catalog  — list all image datasets
//            POST /api/images-catalog — create/update a dataset (admin only)
//
// Storage: delegates to wsproxy internal API (/internal/images-catalog),
// which persists to a ConfigMap in the master cluster.
// Falls back to static data when wsproxy is unavailable (dev mode without sync enabled).

import { NextResponse, type NextRequest } from "next/server"
import { IMAGE_DATASETS } from "@/components/images/data"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import type { ImageDataset } from "@/components/images/data"
import { requireAdmin } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

export async function GET() {
  try {
    const res = await callWsproxy("/internal/images-catalog", "GET")
    if (res.ok) {
      const data = await res.json()
      return NextResponse.json(data)
    }
  } catch {
    // wsproxy unavailable — fall through to static fallback
  }
  // Dev / fallback: return static data
  return NextResponse.json(IMAGE_DATASETS)
}

export async function POST(request: NextRequest) {
  const authResult = await requireAdmin(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  let body: ImageDataset
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  if (!body.id || !body.name) {
    return NextResponse.json({ error: "id and name are required" }, { status: 400 })
  }

  let res: Response
  try {
    res = await callWsproxy("/internal/images-catalog", "POST", body)
  } catch (e) {
    console.error("[images-catalog POST] wsproxy unavailable:", e)
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
    action: "images.create",
    method: "POST",
    path: "/api/images-catalog",
    statusCode: 201,
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
    parsed = body
  }
  return NextResponse.json(parsed, { status: 201 })
}
