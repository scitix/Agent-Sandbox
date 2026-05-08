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
import { isOIDCEnabled, buildAuthorizationUrl } from "@/lib/server/oidc"

/**
 * GET /api/auth/oidc/login?cluster=<clusterID>
 *
 * Initiates the Dex OIDC Authorization Code Flow:
 * 1. Generates a random CSRF state token
 * 2. Stores state (+ optional cluster) in httpOnly cookies
 * 3. Redirects to the Dex authorization endpoint
 *
 * DEX_ISSUER_URL / DEX_REDIRECT_URI support relative paths (e.g. "/dex"):
 * the origin is inferred from request.url at runtime.
 */
export async function GET(request: Request) {
  if (!isOIDCEnabled()) {
    return NextResponse.json({ error: "OIDC is not configured" }, { status: 400 })
  }

  const url = new URL(request.url)
  const clusterID = url.searchParams.get("cluster") ?? ""
  const redirectTo = url.searchParams.get("redirect") ?? ""

  // Generate a random CSRF state
  const state = crypto.randomUUID()

  let authorizationUrl: string
  try {
    const result = await buildAuthorizationUrl(request, state)
    authorizationUrl = result.authorizationUrl
  } catch (e) {
    console.error("[oidc/login] Failed to build authorization URL:", e)
    return NextResponse.json({ error: "Failed to reach identity provider" }, { status: 502 })
  }

  // Set cookies and redirect
  const response = NextResponse.redirect(authorizationUrl, 302)

  response.cookies.set("oidc_state", state, {
    httpOnly: true,
    maxAge: 300, // 5 minutes
    sameSite: "lax",
    path: "/",
    secure: process.env.NODE_ENV === "production",
  })

  if (clusterID) {
    response.cookies.set("oidc_cluster", clusterID, {
      httpOnly: true,
      maxAge: 300,
      sameSite: "lax",
      path: "/",
      secure: process.env.NODE_ENV === "production",
    })
  }

  if (redirectTo && redirectTo.startsWith("/")) {
    response.cookies.set("oidc_redirect", redirectTo, {
      httpOnly: true,
      maxAge: 300,
      sameSite: "lax",
      path: "/",
      secure: process.env.NODE_ENV === "production",
    })
  }

  return response
}
