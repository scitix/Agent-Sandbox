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
 * OIDC client module for Dex integration.
 *
 * Only runs on the server (BFF routes). Uses `openid-client` (panva) for
 * standards-compliant Authorization Code Flow handling — discovery, token
 * exchange and ID token verification are all handled by the library.
 *
 * Set DEX_ISSUER_URL to enable OIDC mode; leave unset for Mock mode.
 *
 * DEX_ISSUER_URL and DEX_REDIRECT_URI support relative paths (starting with "/").
 * When a relative path is provided the request URL is used to construct the
 * absolute URL at runtime, so no domain needs to be hard-coded in config.
 */

import * as oidcClient from "openid-client"

type RequestLike = Pick<Request, "url" | "headers">

// ─── Config ───────────────────────────────────────────────────────────────────

export function isOIDCEnabled(): boolean {
  return Boolean(process.env.DEX_ISSUER_URL)
}

function firstHeaderValue(value: string | null): string | null {
  if (!value) return null
  return value.split(",")[0]?.trim() || null
}

/**
 * Best-effort public origin resolver for reverse-proxy deployments.
 * Prefers forwarded headers set by ingress, then falls back to request.url.
 */
function resolvePublicOrigin(request: RequestLike): string {
  const forwardedProto = firstHeaderValue(request.headers.get("x-forwarded-proto"))
  const forwardedHost = firstHeaderValue(request.headers.get("x-forwarded-host"))

  if (forwardedProto && forwardedHost) {
    return `${forwardedProto}://${forwardedHost}`
  }

  return new URL(request.url).origin
}

// ─── Relative-path helpers ────────────────────────────────────────────────────

/**
 * Resolve DEX_ISSUER_URL to an absolute URL.
 * If the env var starts with "/" it is treated as a path relative to the
 * origin of `requestUrl` (the URL of the incoming Next.js API route request).
 */
export function resolveIssuerUrl(): string {
  const raw = process.env.DEX_ISSUER_URL ?? ""
  if (!raw) {
    throw new Error("DEX_ISSUER_URL is not set")
  }

  return new URL(raw).toString()
}

/**
 * Resolve DEX_REDIRECT_URI to an absolute URL.
 * If the env var starts with "/" it is treated as a path relative to the
 * origin of `requestUrl`.
 */
export function resolveRedirectUri(): string {
  const raw = process.env.DEX_REDIRECT_URI ?? ""
  if (!raw) {
    throw new Error("DEX_REDIRECT_URI is not set")
  }

  return new URL(raw).toString()
}

// ─── openid-client configuration cache ───────────────────────────────────────

// Cached per resolved issuer URL (stable within a single K8s pod).
const _configCache = new Map<string, oidcClient.Configuration>()

/**
 * Returns a cached openid-client Configuration for the Dex issuer.
 * Performs OIDC Discovery if not yet cached for this issuer URL.
 *
 * @param requestUrl - The URL of the incoming Next.js API route request,
 *   used to resolve relative DEX_ISSUER_URL values.
 */
export async function getOIDCClient(request: RequestLike): Promise<oidcClient.Configuration> {
  const issuer = resolveIssuerUrl()

  const cached = _configCache.get(issuer)
  if (cached) return cached

  const clientID = process.env.DEX_CLIENT_ID
  const clientSecret = process.env.DEX_CLIENT_SECRET

  if (!clientID || !clientSecret) {
    throw new Error("DEX_CLIENT_ID and DEX_CLIENT_SECRET must both be set")
  }

  const config = await oidcClient.discovery(new URL(issuer), clientID, clientSecret)
  _configCache.set(issuer, config)
  return config
}

// ─── Authorization URL builder ────────────────────────────────────────────────

/**
 * Returns the Dex authorization URL to redirect the user to, along with the
 * CSRF state value that must be stored in an httpOnly cookie.
 */
export async function buildAuthorizationUrl(
  request: RequestLike,
  state: string,
): Promise<{ authorizationUrl: string; redirectUri: string }> {
  const config = await getOIDCClient(request)
  const redirectUri = resolveRedirectUri()

  console.info("[oidc/login] Resolved authorization request", {
    issuer: resolveIssuerUrl(),
    redirectUri,
  })

  const authorizationUrl = oidcClient.buildAuthorizationUrl(config, {
    redirect_uri: redirectUri,
    response_type: "code",
    scope: "openid profile email",
    state,
  })

  return { authorizationUrl: authorizationUrl.href, redirectUri }
}

// ─── ID Token Claims ──────────────────────────────────────────────────────────

export interface OIDCClaims {
  sub: string
  name?: string
  email?: string
  preferred_username?: string
}

// ─── Authorization Code Exchange ─────────────────────────────────────────────

/**
 * Exchanges the authorization code for an ID token and verifies it.
 * Uses openid-client's `authorizationCodeGrant` which handles token endpoint
 * call + ID token validation (signature, issuer, audience, nonce, state) in
 * one step.
 *
 * @param requestUrl - Full URL of the callback request (includes code & state params).
 * @param expectedState - The CSRF state value retrieved from the httpOnly cookie.
 */
export async function exchangeCodeAndVerify(
  request: RequestLike,
  expectedState: string,
): Promise<OIDCClaims> {
  const config = await getOIDCClient(request)
  const redirectUri = resolveRedirectUri()

  // Reconstruct the callback URL from the already-resolved redirect_uri instead
  // of request.url. In Next.js basePath deployments request.url inside the pod
  // may be /api/auth/oidc/callback (without the external /agentbox prefix),
  // which would make the callback URL differ from the original authorization
  // request and cause Dex to reject the code exchange.
  const internalUrl = new URL(request.url)
  const currentUrl = new URL(redirectUri)
  currentUrl.search = internalUrl.search

  const tokens = await oidcClient.authorizationCodeGrant(
    config,
    currentUrl,
    { expectedState },
    { redirect_uri: redirectUri },
  )

  const claims = tokens.claims()
  if (!claims) {
    throw new Error("ID token claims are missing after code exchange")
  }

  return {
    sub: claims.sub,
    name: claims["name"] as string | undefined,
    email: claims["email"] as string | undefined,
    preferred_username: claims["preferred_username"] as string | undefined,
  }
}
