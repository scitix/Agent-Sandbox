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

// BFF route: GET  /api/global-templates — list global SandboxTemplates (from wsproxy/Master)
//            POST /api/global-templates — create a global SandboxTemplate
//
// Authentication: requires a valid JWT with admin role.
// Actual K8s operations are delegated to the ws-proxy internal API
// (WSPROXY_INTERNAL_URL, default http://localhost:9004).
//
// GET forwards the wsproxy response directly — wsproxy now returns gen.SandboxTemplateSummary[]
// with computed cpu/memory/hasDocs fields via SandboxTemplateService.
//
// POST sends the full SandboxTemplate CRD object as `crdObject` to ws-proxy,
// eliminating the need to manually split name/spec/labels/annotations.
// ws-proxy injects the agentbox.io/sync-source=global label before writing
// to Master and broadcasting to all Worker clusters.

import { NextResponse, type NextRequest } from "next/server"
import { initAudit, writeAuditEvent } from "@/lib/audit"
import { parse as yamlParse } from "yaml"
import { requireAuth } from "@/lib/server/bff-auth"
import { callWsproxy } from "@/lib/server/wsproxy"

// ─── GET /api/global-templates ────────────────────────────────────────────────

export async function GET(request: NextRequest) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error

  let res: Response
  try {
    res = await callWsproxy("/internal/templates", "GET")
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

  const body = (await res.json()) as { items?: unknown[]; total?: number }
  const items = body.items ?? []
  const total = body.total ?? items.length
  return NextResponse.json({ items, total, limit: total, offset: 0 })
}

/**
 * Ensure spec.template is a JSON object, not a YAML string.
 * wsproxy Go side expects JSON when deserialising the SandboxTemplate spec.
 */
function normaliseSpecTemplate(specObj: Record<string, unknown>): Record<string, unknown> {
  if (typeof specObj.template === "string" && specObj.template.trim()) {
    try {
      specObj.template = yamlParse(specObj.template)
    } catch {
      // keep as-is; wsproxy will handle or reject
    }
  }
  return specObj
}

export async function POST(request: NextRequest) {
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult

  let body: {
    name?: string
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
  // Two input paths:
  //   1. crdYaml: parse the full YAML and use it directly.
  //   2. Structured fields (name + spec + labels + annotations).
  let crdObject: Record<string, unknown>
  let templateName: string

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
    const meta = parsed.metadata as Record<string, unknown> | undefined
    templateName = (meta?.name as string | undefined) ?? ""
    if (!templateName) {
      return NextResponse.json({ error: "crdYaml must include metadata.name" }, { status: 400 })
    }
    // Normalise spec.template in-place.
    const specObj = (parsed.spec ?? {}) as Record<string, unknown>
    parsed.spec = normaliseSpecTemplate(specObj)
    crdObject = parsed
  } else {
    if (!body.name) {
      return NextResponse.json({ error: "name is required" }, { status: 400 })
    }
    templateName = body.name
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
        name: body.name,
        ...(body.labels ? { labels: body.labels } : {}),
        ...(body.annotations ? { annotations: body.annotations } : {}),
      },
      spec: specObj,
    }
  }

  let res: Response
  try {
    res = await callWsproxy("/internal/templates", "POST", { crdObject })
  } catch (e) {
    console.error("[global-templates POST] wsproxy unavailable:", e)
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
    parsedBody = { name: templateName }
  }

  initAudit()
  writeAuditEvent({
    timestamp: new Date().toISOString(),
    action: "template.create",
    method: "POST",
    path: "/api/global-templates",
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

  return NextResponse.json(parsedBody, { status: 201 })
}
