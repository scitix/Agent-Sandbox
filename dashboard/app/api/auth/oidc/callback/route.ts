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
import { cookies } from "next/headers"
import { exchangeCodeAndVerify, type OIDCClaims } from "@/lib/server/oidc"
import { signJWT } from "@/lib/auth"
import { listClusters, getClusterConfig } from "@/lib/cluster-config"
import { isOIDCAdmin } from "@/lib/server/oidc-admins"

const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

/**
 * GET /api/auth/oidc/callback?code=xxx&state=xxx
 *
 * Handles the Dex OIDC Authorization Code Flow callback:
 * 1. Validates the CSRF state cookie
 * 2. Uses openid-client to exchange the authorization code for tokens and
 *    verify the ID token in one step
 * 3. Signs an AgentBox JWT and redirects to the frontend callback page
 *
 * Note: namespace is NOT embedded in the JWT — it is resolved by the backend
 * on every request based on team+user identity.
 *
 * DEX_ISSUER_URL / DEX_REDIRECT_URI support relative paths:
 * the origin is inferred from request.url at runtime.
 */
export async function GET(request: Request) {
  const url = new URL(request.url)
  const stateParam = url.searchParams.get("state")

  // Validate required params (code is embedded in request.url for openid-client)
  if (!url.searchParams.get("code") || !stateParam) {
    const errorURL = new URL(`${basePath}/login`, getPublicOrigin(request))
    errorURL.searchParams.set("error", "oidc_failed")
    return NextResponse.redirect(errorURL, 302)
  }

  // Verify CSRF state
  const cookieStore = await cookies()
  const storedState = cookieStore.get("oidc_state")?.value

  if (!storedState || storedState !== stateParam) {
    return buildErrorRedirect(request, "state_mismatch")
  }

  // Exchange code for tokens and verify the ID token via openid-client
  let claims: OIDCClaims
  try {
    claims = await exchangeCodeAndVerify(request, storedState)
  } catch (e) {
    const oidcError = classifyOIDCError(e)
    console.error("[oidc/callback] Code exchange / token verification failed:", oidcError)
    return buildErrorRedirect(request, oidcError.code)
  }

  const team = await resolveTeamFromConsole(request)
  if (!team) {
    return buildErrorRedirect(request, "no_group")
  }

  const username = claims.preferred_username || claims.sub
  const name = claims.name ?? username
  const email = claims.email ?? ""

  // Resolve cluster from cookie (passed to login page then stored in cookie by OIDC init)
  const clusterIDFromCookie = cookieStore.get("oidc_cluster")?.value ?? ""
  let clusterID: string
  let clusterName: string

  if (clusterIDFromCookie) {
    const cluster = getClusterConfig(clusterIDFromCookie)
    if (cluster) {
      clusterID = cluster.id
      clusterName = cluster.name
    } else {
      const fallback = listClusters()[0]
      clusterID = fallback?.id ?? "default"
      clusterName = fallback?.name ?? "default"
    }
  } else {
    const fallback = listClusters()[0]
    clusterID = fallback?.id ?? "default"
    clusterName = fallback?.name ?? "default"
  }

  // Determine role: admin if user/org matches DEX_OIDC_ADMINS config, else tenant
  const role = isOIDCAdmin(team, username) ? "admin" : "tenant"

  // Sign AgentBox JWT — namespace and clusterID are NOT embedded
  const token = await signJWT({
    authMethod: "oidc",
    user: username,
    role,
    team,
    name,
    email,
  })

  // Build redirect URL to frontend callback page
  // clusterID is passed as a URL param so the frontend atom can store it,
  // but it is NOT in the JWT.
  const callbackURL = new URL(`${basePath}/login/oidc-callback`, getPublicOrigin(request))
  callbackURL.searchParams.set("token", token)
  callbackURL.searchParams.set("role", role)
  callbackURL.searchParams.set("user", username)
  callbackURL.searchParams.set("team", team)
  callbackURL.searchParams.set("clusterID", clusterID)
  callbackURL.searchParams.set("clusterName", clusterName)
  callbackURL.searchParams.set("name", name)
  callbackURL.searchParams.set("email", email)
  callbackURL.searchParams.set("authMethod", "oidc")

  const redirectTo = cookieStore.get("oidc_redirect")?.value ?? ""
  if (redirectTo && redirectTo.startsWith("/")) {
    callbackURL.searchParams.set("redirect", redirectTo)
  }

  const response = NextResponse.redirect(callbackURL.toString(), 302)

  // Clear OIDC cookies
  response.cookies.set("oidc_state", "", { maxAge: 0, path: "/" })
  response.cookies.set("oidc_cluster", "", { maxAge: 0, path: "/" })
  response.cookies.set("oidc_redirect", "", { maxAge: 0, path: "/" })

  return response
}

async function resolveTeamFromConsole(request: Request): Promise<string | null> {
  const userInfoURI = process.env.DEX_USERINFO_URI
  if (!userInfoURI) {
    console.warn("[oidc/callback] DEX_USERINFO_URI is not set, skipping userInfo lookup")
    return null
  }

  const cookieStore = await cookies()
  const rawToken =
    cookieStore.get("JWT")?.value ??
    cookieStore.get("JWTOP")?.value ??
    cookieStore.get("jwt")?.value

  const bearerToken = normalizeBearerToken(rawToken)
  if (!bearerToken) {
    console.warn("[oidc/callback] groups missing and no JWT/JWTOP cookie found for fallback")
    return null
  }

  const userInfoURL = new URL(userInfoURI)
  const cookieHeader = request.headers.get("cookie") ?? ""

  try {
    const res = await fetch(userInfoURL.toString(), {
      method: "GET",
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${bearerToken}`,
        ...(cookieHeader ? { Cookie: cookieHeader } : {}),
      },
      cache: "no-store",
    })

    if (!res.ok) {
      const text = await res.text().catch(() => "")
      console.warn("[oidc/callback] userInfo request failed", {
        status: res.status,
        body: text,
      })
      return null
    }

    const json = (await res.json()) as {
      rows?: Array<{ orgName?: string; teamName?: string }>
    }

    const first = json.rows?.[0]
    const team = (first?.orgName || first?.teamName || "").trim()
    return team || null
  } catch (e) {
    console.warn("[oidc/callback] userInfo request error", e)
    return null
  }
}

function normalizeBearerToken(raw: string | undefined): string | null {
  if (!raw) return null

  // Cookie value may come quoted, e.g. "Bearer eyJ..."
  const unquoted = raw.replace(/^"|"$/g, "").trim()
  if (!unquoted) return null

  if (unquoted.toLowerCase().startsWith("bearer ")) {
    const token = unquoted.slice(7).trim()
    return token || null
  }

  return unquoted
}

function buildErrorRedirect(request: Request, error: string): NextResponse {
  const url = new URL(`${basePath}/login`, getPublicOrigin(request))
  url.searchParams.set("error", error)
  const response = NextResponse.redirect(url.toString(), 302)
  // Clear stale OIDC cookies on error too
  response.cookies.set("oidc_state", "", { maxAge: 0, path: "/" })
  response.cookies.set("oidc_cluster", "", { maxAge: 0, path: "/" })
  return response
}

function getPublicOrigin(request: Request): string {
  const forwardedProto = request.headers.get("x-forwarded-proto")?.split(",")[0]?.trim()
  const forwardedHost = request.headers.get("x-forwarded-host")?.split(",")[0]?.trim()

  if (forwardedProto && forwardedHost) {
    return `${forwardedProto}://${forwardedHost}`
  }

  return new URL(request.url).origin
}

function classifyOIDCError(error: unknown): { code: string; details: string[] } {
  const details = collectOIDCErrorDetails(error)
  const normalized = details.join(" | ").toLowerCase()

  if (normalized.includes("invalid_client")) {
    return { code: "oidc_invalid_client", details }
  }

  if (normalized.includes("invalid_grant")) {
    return { code: "oidc_invalid_grant", details }
  }

  if (normalized.includes("redirect_uri")) {
    return { code: "oidc_redirect_uri_mismatch", details }
  }

  if (normalized.includes("issuer") || normalized.includes('unexpected jwt "iss" claim value')) {
    return { code: "oidc_invalid_issuer", details }
  }

  if (normalized.includes("id token claims are missing")) {
    return { code: "oidc_missing_claims", details }
  }

  return { code: "oidc_failed", details }
}

function collectOIDCErrorDetails(error: unknown): string[] {
  const details: string[] = []
  const visited = new Set<unknown>()

  function walk(value: unknown) {
    if (!value || visited.has(value)) return
    visited.add(value)

    if (value instanceof Error) {
      details.push(`${value.name}: ${value.message}`)
      walk((value as Error & { cause?: unknown }).cause)
      return
    }

    if (typeof value === "object") {
      const record = value as Record<string, unknown>
      const errorCode = typeof record.error === "string" ? record.error : null
      const errorDescription =
        typeof record.error_description === "string" ? record.error_description : null
      const message = typeof record.message === "string" ? record.message : null

      if (errorCode || errorDescription || message) {
        details.push(
          [errorCode, errorDescription, message]
            .filter((item): item is string => Boolean(item))
            .join(" | "),
        )
      }

      walk(record.cause)
    }
  }

  walk(error)

  return details.length > 0 ? details : [String(error)]
}
