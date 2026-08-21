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

import { NextResponse } from "next/server"
import { signJWT } from "@/lib/auth"
import { getClusterConfig } from "@/lib/cluster-config"
import { requireAuth } from "@/lib/server/bff-auth"

/**
 * POST /api/auth/switch-cluster
 *
 * Allows a Mock/OIDC-authenticated user to switch clusters without re-entering credentials.
 *
 * Body: { clusterID: string }
 * Headers: Authorization: Bearer <current JWT>
 *
 * Flow:
 *  1. Verify the current JWT.
 *  2. Re-sign JWT preserving identity fields (no namespace or clusterID in JWT).
 *    For API-key sessions the embedded apiKey claim is carried over — the BFF
 *    per-cluster proxy needs it to authenticate against the target cluster.
 *  3. Return the new AuthState with clusterID in the response body (for frontend atom).
 */
export async function POST(request: Request) {
  // Verify current JWT
  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const currentPayload = authResult.payload

  let body: { clusterID?: string }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  const { clusterID } = body
  if (!clusterID) {
    return NextResponse.json({ error: "clusterID is required" }, { status: 400 })
  }

  // Get target cluster config
  const cluster = getClusterConfig(clusterID)
  if (!cluster) {
    return NextResponse.json({ error: "Cluster not found" }, { status: 404 })
  }
  const clusterName = cluster.name

  const username = currentPayload.user ?? ""
  const team = currentPayload.team ?? ""

  // Re-sign JWT preserving identity fields; namespace and clusterID are NOT embedded
  const newToken = await signJWT({
    authMethod: currentPayload.authMethod,
    user: username,
    role: currentPayload.role,
    team,
    name: currentPayload.name,
    email: currentPayload.email,
    ...(currentPayload.apiKey ? { apiKey: currentPayload.apiKey } : {}),
  })

  return NextResponse.json({
    token: newToken,
    role: currentPayload.role,
    user: username,
    team,
    clusterID,
    clusterName,
    authMethod: currentPayload.authMethod ?? "apikey",
    name: currentPayload.name,
    email: currentPayload.email,
  })
}
