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

// The browser's only route to the assistant gateway.
//
// This proxy is not a convenience — it is the gateway's authentication. The
// gateway itself authenticates NOTHING: it takes the caller's word for which
// user is asking, because it was designed to sit behind exactly this. So the
// identity is not forwarded from the client, it is OVERWRITTEN here with the
// one this route just verified. Anything that can reach the gateway directly
// can act as anyone, which is why its Service is ClusterIP and there is no
// Ingress for it.
//
// The overwrite has to cover both shapes the gateway accepts a userKey in — a
// query parameter and a JSON body field, including the nested
// `forwardedProps.userKey` the AG-UI run carries. Missing one of them leaves a
// hole that looks closed.

import { NextResponse, type NextRequest } from "next/server"
import { request as undiciRequest } from "undici"
import { requireAuth } from "@/lib/server/bff-auth"

/** Where the gateway listens, in-cluster. */
function gatewayOrigin(): string {
  return (
    process.env.ASSISTANT_GATEWAY_URL ??
    "http://agentbox-dashboard-assistant:4099"
  )
}

/**
 * The identity the gateway will see.
 *
 * Team-qualified because the gateway uses it as a directory segment for the
 * per-user workspace, and two teams may each have a `ylli`. Constrained to the
 * character set the gateway accepts for that reason — a rejected key fails the
 * whole conversation rather than one request.
 */
function userKeyOf(payload: { team?: string; user?: string }): string {
  const raw = [payload.team, payload.user].filter(Boolean).join(".") || "default"
  return raw.replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 128)
}

/** Hop-by-hop and identity headers we refuse to forward. */
const STRIP = new Set([
  "host",
  "connection",
  "content-length",
  "transfer-encoding",
  "authorization",
  "cookie",
])

async function proxy(request: NextRequest, path: string[]) {
  const auth = await requireAuth(request.headers.get("Authorization"))
  if ("error" in auth) return auth.error
  const userKey = userKeyOf(auth.payload)

  const url = new URL(request.url)
  // Overwrite rather than append: a client-supplied value must not survive.
  if (url.searchParams.has("userKey")) url.searchParams.set("userKey", userKey)
  const target = `${gatewayOrigin()}/${path.join("/")}${url.search}`

  const headers: Record<string, string> = {}
  request.headers.forEach((v, k) => {
    if (!STRIP.has(k.toLowerCase())) headers[k] = v
  })

  let body: Buffer | undefined
  if (request.method !== "GET" && request.method !== "HEAD") {
    const raw = Buffer.from(await request.arrayBuffer())
    body = raw.length ? rewriteIdentity(raw, userKey) : undefined
    if (body) headers["content-type"] = "application/json"
  }

  let res: Awaited<ReturnType<typeof undiciRequest>>
  try {
    res = await undiciRequest(target, {
      method: request.method as "GET" | "POST" | "PUT" | "DELETE" | "PATCH",
      headers,
      ...(body ? { body } : {}),
      // A turn can be silent for minutes while the agent works, and the run IS
      // its own stream, so the body must never time out. The header timeout is
      // what still catches a gateway that never answers at all.
      bodyTimeout: 0,
      headersTimeout: 65_000,
      signal: request.signal,
    })
  } catch (e) {
    return NextResponse.json(
      { error: `assistant gateway unreachable: ${String(e)}` },
      { status: 502 }
    )
  }

  const out = new Headers()
  for (const [k, v] of Object.entries(res.headers)) {
    if (v == null || STRIP.has(k.toLowerCase())) continue
    out.set(k, Array.isArray(v) ? v.join(", ") : String(v))
  }
  // Belt and braces for any proxy in front of this one that would otherwise
  // accumulate the stream and deliver the whole turn at once.
  out.set("x-accel-buffering", "no")
  out.set("cache-control", "no-cache, no-transform")

  return new NextResponse(res.body as unknown as ReadableStream, {
    status: res.statusCode,
    headers: out,
  })
}

/**
 * Replace every identity the body claims with the verified one.
 *
 * Both the top-level field the REST calls use and the nested one an AG-UI run
 * carries. A body that is not JSON travels untouched: the gateway's own
 * surfaces are all JSON, and rewriting bytes we cannot parse would corrupt an
 * upload rather than secure it.
 */
function rewriteIdentity(raw: Buffer, userKey: string): Buffer {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw.toString("utf8"))
  } catch {
    return raw
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return raw
  const obj = parsed as Record<string, unknown>
  if ("userKey" in obj) obj.userKey = userKey
  const fwd = obj.forwardedProps
  if (fwd && typeof fwd === "object" && !Array.isArray(fwd)) {
    ;(fwd as Record<string, unknown>).userKey = userKey
  }
  return Buffer.from(JSON.stringify(obj))
}

export async function GET(
  request: NextRequest,
  ctx: { params: Promise<{ path: string[] }> }
) {
  return proxy(request, (await ctx.params).path)
}
export async function POST(
  request: NextRequest,
  ctx: { params: Promise<{ path: string[] }> }
) {
  return proxy(request, (await ctx.params).path)
}
export async function PUT(
  request: NextRequest,
  ctx: { params: Promise<{ path: string[] }> }
) {
  return proxy(request, (await ctx.params).path)
}
export async function PATCH(
  request: NextRequest,
  ctx: { params: Promise<{ path: string[] }> }
) {
  return proxy(request, (await ctx.params).path)
}
export async function DELETE(
  request: NextRequest,
  ctx: { params: Promise<{ path: string[] }> }
) {
  return proxy(request, (await ctx.params).path)
}
