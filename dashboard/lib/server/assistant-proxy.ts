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

// Shared by both routes in front of the assistant pod: the conversation
// gateway and the workspace file API.
//
// These proxies ARE the authentication. Neither of the two servers behind them
// authenticates anything — the gateway takes the caller's word for which user
// is asking, and the file API takes the caller's word for whose workspace to
// open — because both were designed to sit behind exactly this. So identity is
// OVERWRITTEN here from a verified session rather than forwarded, and it is
// overwritten in every shape they accept it: a query parameter, a top-level
// JSON field, and the nested `forwardedProps.userKey` an AG-UI run carries.
// Missing one leaves a hole that looks closed.

import { NextResponse, type NextRequest } from "next/server"
import { request as undiciRequest } from "undici"
import { requireAuth } from "@/lib/server/bff-auth"

/** Hop-by-hop headers, plus the ones carrying the identity we replace. */
const STRIP = new Set([
  "host",
  "connection",
  "content-length",
  "transfer-encoding",
  "authorization",
  "cookie",
])

/**
 * The identity the assistant pod will see.
 *
 * Team-qualified because it becomes a directory segment for the per-user
 * workspace, and two teams may each have a `ylli`. Constrained to the
 * character set those servers accept, since a rejected key fails the whole
 * conversation rather than one request.
 */
export function assistantUserKey(payload: {
  team?: string
  user?: string
}): string {
  const raw = [payload.team, payload.user].filter(Boolean).join(".") || "default"
  return raw.replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 128)
}

/** Replace every identity a JSON body claims with the verified one. */
function rewriteIdentity(raw: Buffer, userKey: string): Buffer {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw.toString("utf8"))
  } catch {
    // Not JSON — an upload, most likely. Rewriting bytes we cannot parse would
    // corrupt it rather than secure it, and there is no identity in it to fix.
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

export async function proxyToAssistant(
  request: NextRequest,
  path: string[],
  origin: string
): Promise<Response> {
  // EventSource cannot set headers, so the push channel passes the session as
  // a query parameter instead. Accepted only here, and stripped before the
  // request is forwarded, so it never reaches the upstream's access log.
  const url = new URL(request.url)
  const queryToken = url.searchParams.get("access_token")
  if (queryToken) url.searchParams.delete("access_token")

  const auth = await requireAuth(
    request.headers.get("Authorization") ??
      (queryToken ? `Bearer ${queryToken}` : null)
  )
  if ("error" in auth) return auth.error
  const userKey = assistantUserKey(auth.payload)

  // Overwrite rather than append: a client-supplied value must not survive.
  if (url.searchParams.has("userKey")) url.searchParams.set("userKey", userKey)
  const target = `${origin}/${path.join("/")}${url.search}`

  const headers: Record<string, string> = {}
  request.headers.forEach((v, k) => {
    if (!STRIP.has(k.toLowerCase())) headers[k] = v
  })

  let body: Buffer | undefined
  if (request.method !== "GET" && request.method !== "HEAD") {
    const raw = Buffer.from(await request.arrayBuffer())
    if (raw.length) body = rewriteIdentity(raw, userKey)
  }

  let res: Awaited<ReturnType<typeof undiciRequest>>
  try {
    res = await undiciRequest(target, {
      method: request.method as "GET" | "POST" | "PUT" | "DELETE" | "PATCH",
      headers,
      ...(body ? { body } : {}),
      // A turn can be silent for minutes while the agent works, and a run IS
      // its own stream, so the body must never time out. The header timeout
      // still catches an upstream that never answers at all.
      bodyTimeout: 0,
      headersTimeout: 65_000,
      signal: request.signal,
    })
  } catch (e) {
    return NextResponse.json(
      { error: `assistant unreachable: ${String(e)}` },
      { status: 502 }
    )
  }

  const out = new Headers()
  for (const [k, v] of Object.entries(res.headers)) {
    if (v == null || STRIP.has(k.toLowerCase())) continue
    out.set(k, Array.isArray(v) ? v.join(", ") : String(v))
  }
  // For any proxy in front of this one that would otherwise accumulate the
  // stream and deliver a whole turn at once.
  out.set("x-accel-buffering", "no")
  out.set("cache-control", "no-cache, no-transform")

  return new NextResponse(res.body as unknown as ReadableStream, {
    status: res.statusCode,
    headers: out,
  })
}
