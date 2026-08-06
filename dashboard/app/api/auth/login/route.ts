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
import { request as undiciRequest } from "undici"
import { signJWT } from "@/lib/auth"
import { getClusterConfig, listClusters } from "@/lib/cluster-config"

export async function POST(request: Request) {
  let body: { apiKey?: string; clusterID?: string }
  try {
    body = await request.json()
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 })
  }

  const { apiKey } = body
  if (!apiKey || typeof apiKey !== "string") {
    return NextResponse.json({ error: "apiKey is required" }, { status: 400 })
  }

  // An API key is not bound to a cluster: the signed JWT carries only the key,
  // and the per-cluster proxy injects it wherever the URL points. The cluster
  // here is just whichever backend validates the key and answers whoami, so
  // callers may omit it and get the first configured one.
  const clusterID = body.clusterID || listClusters()[0]?.id
  if (!clusterID) {
    return NextResponse.json({ error: "No clusters configured" }, { status: 503 })
  }

  const cluster = getClusterConfig(clusterID)
  if (!cluster) {
    return NextResponse.json({ error: "Cluster not found" }, { status: 404 })
  }
  const backendUrl = cluster.url
  const extraHeaders = cluster.headers ?? {}
  const clusterName = cluster.name

  // Call /v1/auth/whoami — validates the key AND returns role/user/team in one shot.
  let role: "admin" | "tenant"
  let user: string | undefined
  let team: string | undefined

  try {
    const reqHeaders: Record<string, string> = {
      "AGENTBOX-API-KEY": apiKey,
      ...extraHeaders,
    }

    // Use undici directly instead of ky/fetch: the Fetch API forbids overriding the
    // `Host` header, so ky would silently send the IP as Host and the backend's
    // virtual-host router would return 404. undici has no such restriction.
    const { statusCode, body } = await undiciRequest(`${backendUrl}/v1/auth/whoami`, {
      method: "GET",
      headers: reqHeaders,
      bodyTimeout: 10000,
      headersTimeout: 10000,
    })

    if (statusCode === 401 || statusCode === 403) {
      await body.dump()
      return NextResponse.json({ error: "Invalid API key" }, { status: 401 })
    }

    if (statusCode !== 200) {
      const text = await body.text().catch(() => "(unable to read body)")
      console.error(`[auth/login] backend HTTP error ${statusCode}: ${text}`)
      return NextResponse.json({ error: "Backend unavailable" }, { status: 502 })
    }

    const whoami = (await body.json()) as {
      role: string
      user?: string
      team?: string
    }

    role = whoami.role === "admin" ? "admin" : "tenant"
    user = whoami.user || undefined
    team = whoami.team || undefined
  } catch (e) {
    console.error("[auth/login] backend request failed:", e)
    return NextResponse.json({ error: "Backend unavailable" }, { status: 502 })
  }

  const token = await signJWT({ apiKey, role, user, team })

  return NextResponse.json({ token, role, user, team, clusterID, clusterName })
}
