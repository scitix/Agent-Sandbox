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
import { listClusters, filterClustersByVisibility } from "@/lib/cluster-config"
import { verifyJWT } from "@/lib/auth"

export async function GET(request: Request) {
  const clusters = listClusters()

  // Try to get user info from JWT for filtering.
  // - No token (unauthenticated / login page): return all clusters so the user
  //   can pick a cluster before logging in.
  // - Valid token: apply visibility filtering based on user/team.
  // - Invalid token: treat as unauthenticated, return all clusters.
  const authHeader = request.headers.get("Authorization")
  if (authHeader?.startsWith("Bearer ")) {
    const token = authHeader.slice(7)
    try {
      const payload = await verifyJWT(token)
      const filtered = filterClustersByVisibility(clusters, payload.team, payload.user)
      return NextResponse.json({ clusters: filtered, multiCluster: true })
    } catch {
      // Invalid token — fall through to unauthenticated path
    }
  }

  return NextResponse.json({ clusters, multiCluster: true })
}
