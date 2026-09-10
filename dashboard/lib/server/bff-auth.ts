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

/**
 * BFF route auth helpers — server-only.
 *
 * Mirrors the requireAuth / requireAdmin interface already used by
 * app/api/prometheus/_shared.ts so that all BFF routes follow the same pattern.
 *
 * Usage:
 *   const result = await requireAuth(request.headers.get("Authorization"))
 *   if ("error" in result) return result.error
 *   const { payload } = result
 *
 *   // admin-only:
 *   const result = await requireAdmin(request.headers.get("Authorization"))
 *   if ("error" in result) return result.error
 */

import { NextResponse } from "next/server"
import { verifyJWT } from "@/lib/auth"
import type { AuthJWTPayload } from "@/lib/auth"

export type AuthResult = { payload: AuthJWTPayload } | { error: NextResponse }

/** Verify Bearer JWT. Returns { payload } on success or { error } on failure. */
export async function requireAuth(authHeader: string | null): Promise<AuthResult> {
  if (!authHeader?.startsWith("Bearer ")) {
    return { error: NextResponse.json({ error: "Unauthorized" }, { status: 401 }) }
  }
  try {
    const payload = await verifyJWT(authHeader.slice(7))
    return { payload }
  } catch {
    return { error: NextResponse.json({ error: "Invalid or expired token" }, { status: 401 }) }
  }
}

/**
 * The platform API key prefix, mirroring `apikey.TokenPrefix` in
 * pkg/utils/apikey/store.go. Only used to tell the two credential shapes apart
 * — the key itself is never validated here, the cluster's own API does that.
 */
const PLATFORM_KEY_PREFIX = "agbx_"

/**
 * Who is calling a proxy route, and with which of the two accepted credentials.
 *
 * `jwt` is a browser session: the BFF minted it, so it carries the resolved
 * identity and the route forwards either the raw JWT or the session's bound
 * API key depending on how the person signed in.
 *
 * `platformKey` is a caller holding a platform credential directly — the
 * assistant's sandboxes are the ones that matter, since `abx` inside them
 * reaches every cluster through this one path-routed address. There is no
 * payload to read: the key names its own tenant and the cluster API resolves it,
 * so the BFF forwards it untouched and claims nothing about who sent it.
 */
export type ProxyIdentity =
  | { kind: "jwt"; payload: AuthJWTPayload; token: string }
  | { kind: "platformKey"; apiKey: string }

export type ProxyAuthResult = { identity: ProxyIdentity } | { error: NextResponse }

/**
 * Accept either credential on a proxy route.
 *
 * A platform key is NOT a weaker check that happens to pass here: the upstream
 * cluster API is the only thing that can validate one, and it is the same
 * validation it would apply if the caller reached it directly. Running it
 * through `verifyJWT` instead — which is what this route used to do
 * unconditionally — rejects a perfectly good credential with "Invalid or
 * expired token", which reads like the key is wrong rather than like the door
 * only accepts one kind of key.
 */
export async function requireProxyAuth(authHeader: string | null): Promise<ProxyAuthResult> {
  if (!authHeader?.startsWith("Bearer ")) {
    return { error: NextResponse.json({ error: "Unauthorized" }, { status: 401 }) }
  }
  const token = authHeader.slice(7)
  if (token.startsWith(PLATFORM_KEY_PREFIX)) {
    return { identity: { kind: "platformKey", apiKey: token } }
  }
  const result = await requireAuth(authHeader)
  if ("error" in result) return result
  return { identity: { kind: "jwt", payload: result.payload, token } }
}

/** Verify Bearer JWT and require admin role. Returns { payload } or { error }. */
export async function requireAdmin(authHeader: string | null): Promise<AuthResult> {
  const result = await requireAuth(authHeader)
  if ("error" in result) return result
  if (result.payload.role !== "admin") {
    return { error: NextResponse.json({ error: "Forbidden" }, { status: 403 }) }
  }
  return result
}
