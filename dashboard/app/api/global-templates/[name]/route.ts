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

// BFF route: GET    /api/global-templates/[name] — get a single global SandboxTemplate
//            PUT    /api/global-templates/[name] — update a global SandboxTemplate
//            DELETE /api/global-templates/[name] — delete a global SandboxTemplate
//
// Authentication: requires a valid JWT with admin role.
// Actual K8s operations are delegated to the ws-proxy internal API.
//
// GET forwards the wsproxy response directly — wsproxy now returns gen.SandboxTemplate
// with computed cpu/memory/crdYaml/docs fields via SandboxTemplateService.
//
// PUT sends the full SandboxTemplate CRD object as `crdObject` to ws-proxy,
// which performs a full replacement (spec + labels + annotations).

import { NextResponse, type NextRequest } from "next/server"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import { parse as yamlParse } from "yaml"
import { requireAuth } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

// ─── GET /api/global-templates/[name] ────────────────────────────────────────

export async function GET(request: NextRequest, { params }: { params: Promise<{ name: string }> }) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error

  const { name } = await params
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 })
  }

  let res: Response
  try {
    res = await callWsproxy(`/internal/templates/${encodeURIComponent(name)}`, "GET")
  } catch (e) {
    console.error("[global-templates GET] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Template manager unavailable" }, { status: 503 })
  }

  if (!res.ok) {
    const body = await res.text()
    let parsed: unknown
    try {
      parsed = JSON.parse(body)
    } catch {
      parsed = { error: body || "wsproxy error" }
    }
    return NextResponse.json(parsed, { status: res.status })
  }

  const template = await res.json()
  return NextResponse.json({ template })
}

export async function PUT(request: NextRequest, { params }: { params: Promise<{ name: string }> }) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  const { name } = await params
  if (!name) {
    return NextResponse.json({ error: "name is required" }, { status: 400 })
  }

  let body: {
    spec?: unknown
    crdYaml?: string
    labels?: Record<string, string>
    annotations?: Record<string, string>
  }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  // Build the full CRD object to send to wsproxy as `crdObject`.
  // wsproxy will perform a full replacement (spec + labels + annotations).
  let crdObject: Record<string, unknown>

  if (body.crdYaml && body.crdYaml.trim()) {
    let parsed: Record<string, unknown>
    try {
      parsed = yamlParse(body.crdYaml) as Record<string, unknown>
    } catch (e) {
      return NextResponse.json(
        { error: `invalid crdYaml: ${(e as Error).message}` },
        { status: 400 },
      )
    }
    const specObj = (parsed.spec ?? {}) as Record<string, unknown>
    if (typeof specObj.template === "string" && specObj.template.trim()) {
      try {
        specObj.template = yamlParse(specObj.template)
      } catch {
        /* keep as-is */
      }
    }
    parsed.spec = specObj
    // Ensure the name in metadata matches the URL param.
    const meta = (parsed.metadata ?? {}) as Record<string, unknown>
    meta.name = name
    parsed.metadata = meta
    crdObject = parsed
  } else {
    const specObj = (
      body.spec && typeof body.spec === "object"
        ? { ...(body.spec as Record<string, unknown>) }
        : {}
    ) as Record<string, unknown>
    if (typeof specObj.template === "string" && specObj.template.trim()) {
      try {
        specObj.template = yamlParse(specObj.template)
      } catch {
        return NextResponse.json({ error: "invalid template YAML" }, { status: 400 })
      }
    }
    crdObject = {
      apiVersion: "agents.navix.sh/v1alpha1",
      kind: "SandboxTemplate",
      metadata: {
        name,
        ...(body.labels ? { labels: body.labels } : {}),
        ...(body.annotations ? { annotations: body.annotations } : {}),
      },
      spec: specObj,
    }
  }

  let res: Response
  try {
    res = await callWsproxy(`/internal/templates/${encodeURIComponent(name)}`, "PUT", { crdObject })
  } catch (e) {
    console.error("[global-templates PUT] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Template manager unavailable" }, { status: 503 })
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

  let parsedBody: unknown
  try {
    parsedBody = JSON.parse(resBody)
  } catch {
    parsedBody = { name }
  }

  initAudit()
  writeAuditEvent({
    timestamp: new Date().toISOString(),
    action: "template.update",
    method: "PUT",
    path: `/api/global-templates/${name}`,
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

  return NextResponse.json(parsedBody, { status: 200 })
}

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
    res = await callWsproxy(`/internal/templates/${encodeURIComponent(name)}`, "DELETE")
  } catch (e) {
    console.error("[global-templates DELETE] wsproxy unavailable:", e)
    return NextResponse.json({ error: "Template manager unavailable" }, { status: 503 })
  }

  if (res.status === 204) {
    initAudit()
    writeAuditEvent({
      timestamp: new Date().toISOString(),
      action: "template.delete",
      method: "DELETE",
      path: `/api/global-templates/${name}`,
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
  let parsed: unknown
  try {
    parsed = JSON.parse(resBody)
  } catch {
    parsed = { error: resBody || "wsproxy error" }
  }
  return NextResponse.json(parsed, { status: res.status })
}
