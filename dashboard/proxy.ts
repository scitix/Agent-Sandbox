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

import { NextResponse, type NextRequest } from "next/server"

const NON_LOCALE_PREFIXES = ["/api/", "/_next/", "/icon.svg", "/favicon.ico"]
// Non-default locales that need a URL prefix (order matters: longer prefixes first)
const SUPPORTED_LOCALES = ["zh-Hant", "zh-Hans"]
const ALL_LOCALES = ["en", "zh-Hans", "zh-Hant"]
const DEFAULT_LOCALE = "en"

function detectLocaleFromHeader(acceptLanguage: string | null): string | null {
  if (!acceptLanguage) return null

  const entries = acceptLanguage
    .split(",")
    .map((part) => {
      const [lang, ...params] = part.trim().split(";")
      const qParam = params.find((p) => p.trim().startsWith("q="))
      const q = qParam ? parseFloat(qParam.trim().slice(2)) : 1
      return { lang: lang.trim().toLowerCase(), q }
    })
    .sort((a, b) => b.q - a.q)

  for (const { lang } of entries) {
    // Exact match (case-insensitive): "zh-hans" → "zh-Hans", "zh-hant" → "zh-Hant"
    const exactMatch = ALL_LOCALES.find((l) => l.toLowerCase() === lang)
    if (exactMatch) return exactMatch

    if (lang === "zh-tw" || lang === "zh-hk" || lang === "zh-mo") return "zh-Hant"
    if (lang === "zh-cn" || lang === "zh-sg") return "zh-Hans"
    if (lang === "zh") return "zh-Hans"

    // Generic prefix fallback: "en-us" → "en", "en-gb" → "en"
    const prefix = lang.split("-")[0]
    if (prefix !== "zh") {
      const prefixMatch = ALL_LOCALES.find((l) => l.toLowerCase() === prefix)
      if (prefixMatch) return prefixMatch
    }
  }

  return null
}

/**
 * Locale routing is driven purely by URL, not cookies. Every request maps to a
 * locale as follows:
 *
 *   1. URL already has a non-default-locale prefix (e.g. /zh-Hans/...) → pass through
 *   2. URL has no locale prefix → internally rewrite to /en/... so Next.js matches
 *      the [locale] segment. The browser URL stays clean (no /en prefix).
 *   3. Exception: the root page "/" does a one-shot redirect to /zh-Hans/... when
 *      Accept-Language indicates a Chinese preference AND the user has no URL
 *      signal to the contrary. This is a best-effort first-visit nicety; the
 *      client can reconcile against localStorage on mount if the guess is wrong.
 *
 * Cookies are deliberately NOT consulted: the previous cookie-driven design
 * could trap users whose browser language differed from their explicit choice,
 * because a stale cookie would outvote the URL they just navigated to.
 */
export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl

  // Skip API routes, Next.js internals, and static assets
  if (NON_LOCALE_PREFIXES.some((prefix) => pathname.startsWith(prefix))) {
    return NextResponse.next()
  }

  // Case 1: path already has a supported locale prefix → pass through.
  // Check longer prefixes first: "/zh-Hant/" before "/zh-Hans/".
  for (const locale of SUPPORTED_LOCALES) {
    if (pathname.startsWith(`/${locale}/`) || pathname === `/${locale}`) {
      return NextResponse.next()
    }
  }

  // Case 3: bare root "/" gets a best-effort Accept-Language redirect for
  // first-time visitors. We only do this on "/" because any deeper URL
  // (e.g. /clusters/xxx/general) is either (a) an explicit in-app link
  // from a user who already picked a locale, or (b) a shared URL that
  // should be honored as-is.
  if (pathname === "/") {
    const detected = detectLocaleFromHeader(request.headers.get("accept-language"))
    if (detected && detected !== DEFAULT_LOCALE && SUPPORTED_LOCALES.includes(detected)) {
      const url = request.nextUrl.clone()
      url.pathname = `/${detected}`
      return NextResponse.redirect(url)
    }
  }

  // Case 2: default locale — rewrite so [locale] resolves, keep URL clean.
  const url = request.nextUrl.clone()
  url.pathname = `/en${pathname}`
  return NextResponse.rewrite(url)
}

export const config = {
  // Match all paths except API routes, Next.js internals, and static files
  matcher: ["/((?!api|_next/static|_next/image|icon\\.svg|favicon\\.ico).*)"],
}
