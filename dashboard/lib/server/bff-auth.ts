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

/** Verify Bearer JWT and require admin role. Returns { payload } or { error }. */
export async function requireAdmin(authHeader: string | null): Promise<AuthResult> {
  const result = await requireAuth(authHeader)
  if ("error" in result) return result
  if (result.payload.role !== "admin") {
    return { error: NextResponse.json({ error: "Forbidden" }, { status: 403 }) }
  }
  return result
}
