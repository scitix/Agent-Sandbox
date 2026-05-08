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
import { listClusters } from "@/lib/cluster-config"
import { isOIDCEnabled } from "@/lib/server/oidc"

/**
 * POST /api/auth/mock/login
 *
 * Mock login endpoint for development / non-OIDC environments.
 * Accepts any non-empty team + username and issues a JWT.
 *
 * When OIDC is configured this endpoint returns 403 to prevent accidental
 * use of unauthenticated mock credentials in production.
 *
 * Body: { username: string; team: string }
 *
 * Flow:
 *  1. Reject if OIDC is enabled (production guard).
 *  2. Validate username and team are non-empty.
 *  3. Auto-select the first configured cluster.
 *  4. Sign a JWT with authMethod:"mock" (no apiKey field, no namespace/clusterID).
 *  5. Return { token, role, user, team, clusterID, clusterName, authMethod }.
 */

export async function POST(request: Request) {
  // Production guard: disallow mock login when OIDC is configured.
  if (isOIDCEnabled()) {
    return NextResponse.json(
      { error: "Mock login is disabled when OIDC is configured" },
      { status: 403 },
    )
  }

  let body: { username?: string; team?: string }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  const { username, team } = body

  if (!username || typeof username !== "string" || username.trim() === "") {
    return NextResponse.json({ error: "username is required" }, { status: 400 })
  }
  if (!team || typeof team !== "string" || team.trim() === "") {
    return NextResponse.json({ error: "team is required" }, { status: 400 })
  }

  const role: "admin" | "tenant" = "tenant"

  // Auto-select the first configured cluster
  const clusters = listClusters()
  const clusterID = clusters[0]?.id
  const clusterName = clusters[0]?.name

  // Sign JWT (namespace and clusterID are NOT embedded in the JWT)
  const token = await signJWT({
    authMethod: "mock",
    user: username.trim(),
    role,
    team: team.trim(),
  })

  return NextResponse.json({
    token,
    role,
    user: username.trim(),
    team: team.trim(),
    clusterID,
    clusterName,
    authMethod: "mock",
  })
}
