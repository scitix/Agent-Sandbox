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
import {
  effectiveIdentity,
  getOrCreateSessionKey,
  IMPERSONATE_TEAM_PARAM,
  IMPERSONATE_USER_PARAM,
  SessionKeyError,
  type EffectiveIdentity,
} from "@/lib/server/assistant-session-key"

/**
 * Hop-by-hop headers, plus every header carrying something we derive here
 * ourselves.
 *
 * The last four are the security-relevant ones. The impersonation pair decides
 * WHOSE key a conversation gets, and the two `x-agentbox-*` headers ARE that
 * key and that identity — so a value arriving from the browser must not
 * survive, or a tenant could name someone else and be handed their credential.
 * They are stripped on the way in and set from the verified session below.
 */
/**
 * Does this deployment give every conversation's sandbox the same identity?
 *
 * Rendered from the same chart value that sets SBX_IDENTITY_MODE on the
 * assistant, so the two cannot disagree. Read per call rather than at module
 * load: the tests set it, and a value captured at import would make the first
 * test to run decide for the rest.
 */
function staticSandboxIdentity(): boolean {
  return (process.env.ASSISTANT_SANDBOX_IDENTITY_MODE ?? "session").trim().toLowerCase() === "static"
}

const STRIP = new Set([
  "host",
  "connection",
  "content-length",
  "transfer-encoding",
  "authorization",
  "cookie",
  "x-impersonate-team",
  "x-impersonate-user",
  "x-agentbox-session-key",
  "x-agentbox-identity",
])

/**
 * The identity the assistant pod will see.
 *
 * Team-qualified because it becomes a directory segment for the per-user
 * workspace, and two teams may each have a `ylli`. Constrained to the
 * character set those servers accept, since a rejected key fails the whole
 * conversation rather than one request.
 *
 * Derived from the EFFECTIVE identity, so an admin acting as someone else gets
 * that person's workspace rather than their own — the same separation the
 * sandbox itself gets from being created with that person's key.
 */
export function assistantUserKey(payload: {
  team?: string
  user?: string
}): string {
  const raw = [payload.team, payload.user].filter(Boolean).join(".") || "default"
  return raw.replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 128)
}

/** `<team>/<user>`, the form the daemon compares to detect a switch. */
function identityHeader(id: EffectiveIdentity): string {
  return `${id.team}/${id.user}`
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
  origin: string,
  /**
   * Whether this upstream creates sandboxes and therefore needs the caller's
   * platform credential.
   *
   * True for the conversation gateway. FALSE for the workspace file API, which
   * only reads a directory the gateway already made — obtaining a credential it
   * cannot use would make file browsing fail whenever the hub is unreachable,
   * for a reason that has nothing to do with files.
   */
  needsSandboxKey = true
): Promise<Response> {
  // EventSource cannot set headers, so the push channel passes the session as
  // a query parameter instead. Accepted only here, and stripped before the
  // request is forwarded, so it never reaches the upstream's access log.
  const url = new URL(request.url)
  const queryToken = url.searchParams.get("access_token")
  if (queryToken) url.searchParams.delete("access_token")

  const authHeader =
    request.headers.get("Authorization") ??
    (queryToken ? `Bearer ${queryToken}` : null)
  const auth = await requireAuth(authHeader)
  if ("error" in auth) return auth.error
  // Verified above, so this is the token the hub will accept as this caller.
  const jwt = authHeader!.slice("Bearer ".length)

  const identity = effectiveIdentity(
    auth.payload,
    request.headers,
    url.searchParams
  )
  const userKey = assistantUserKey(identity)
  // Consumed here like access_token: the upstream derives nothing from them and
  // logging a selection that was already applied is noise at best.
  url.searchParams.delete(IMPERSONATE_TEAM_PARAM)
  url.searchParams.delete(IMPERSONATE_USER_PARAM)

  // The key the conversation's sandbox is created with. Obtained BEFORE
  // forwarding, and a failure here fails the request: the alternative is
  // forwarding without it, which the daemon would answer by creating no
  // sandbox at all — or, worse, by falling back to the deployment's own key and
  // silently handing an admin-scoped sandbox to whoever asked.
  //
  // Unless the deployment has said every sandbox belongs to ONE identity. Then
  // minting a key per person is work with no consumer: the daemon ignores it,
  // and the only trace it leaves is a credential per person accumulating in a
  // key list, hitting the per-user cap, and making `abx whoami` inside a
  // sandbox answer with a tenant nobody configured. One switch decides it in
  // both places, because two halves of one decision is how this got confusing.
  let sessionKey: string | undefined
  if (needsSandboxKey && !staticSandboxIdentity()) {
    try {
      sessionKey = await getOrCreateSessionKey(jwt, identity)
    } catch (e) {
      if (e instanceof SessionKeyError) {
        return NextResponse.json({ error: e.message }, { status: e.status })
      }
      return NextResponse.json(
        { error: `could not obtain a sandbox API key: ${String(e)}` },
        { status: 503 }
      )
    }
  }

  // Overwrite rather than append: a client-supplied value must not survive.
  if (url.searchParams.has("userKey")) url.searchParams.set("userKey", userKey)
  const target = `${origin}/${path.join("/")}${url.search}`

  const headers: Record<string, string> = {}
  request.headers.forEach((v, k) => {
    if (!STRIP.has(k.toLowerCase())) headers[k] = v
  })
  if (sessionKey) headers["x-agentbox-session-key"] = sessionKey
  headers["x-agentbox-identity"] = identityHeader(identity)

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
