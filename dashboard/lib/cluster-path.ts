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

// Path builder utilities for cluster-scoped dashboard routes.
// All navigation within the dashboard should use these helpers to ensure
// consistent URL structure: /clusters/{clusterID}/{page}
// With i18n: /{locale}/clusters/{clusterID}/{page} (locale omitted for "en")

import { isValidLocale, type Locale } from "@/lib/i18n/config"

export type DashboardPage =
  | "overview"
  | "sandboxes"
  | "pools"
  | "templates"
  | "datasets"
  | "admin"
  | "general"
  | "api-keys"
  | "admin-api-keys"
  | "quota"
  | "admin-users"

/**
 * Returns the locale prefix for a URL. Default locale ("en") has no prefix.
 */
function localePrefix(locale?: Locale): string {
  if (!locale || locale === "en") return ""
  return `/${locale}`
}

/** Returns /{locale}/clusters/{clusterID}/{page} (locale omitted for "en") */
export function clusterPath(clusterID: string, page: DashboardPage, locale?: Locale): string {
  return `${localePrefix(locale)}/clusters/${clusterID}/${page}`
}

/** Returns /{locale}/login (locale omitted for "en") */
export function loginPath(locale?: Locale): string {
  return `${localePrefix(locale)}/login`
}

/**
 * Given the current pathname, returns the equivalent path for a different cluster.
 * e.g. /clusters/a/sandboxes → /clusters/b/sandboxes
 * e.g. /zh/clusters/a/sandboxes → /zh/clusters/b/sandboxes
 *
 * Falls back to defaultPage when the current path doesn't match the cluster pattern.
 */
export function switchClusterPath(
  currentPathname: string,
  newClusterID: string,
  defaultPage: DashboardPage = "sandboxes",
): string {
  // Match optional locale prefix (e.g. "zh", "zh-Hant") + /clusters/{id}/{page}
  const match = currentPathname.match(
    /^(?:\/([a-z]{2}(?:-[A-Za-z]{2,8})?))?\/clusters\/[^/]+\/([^/]+)/,
  )
  const rawLocale = match?.[1]
  const locale = rawLocale && isValidLocale(rawLocale) ? rawLocale : undefined
  const page = (match?.[2] as DashboardPage | undefined) ?? defaultPage
  return clusterPath(newClusterID, page, locale)
}

/**
 * Extract the current locale from a pathname.
 * Returns "en" if no locale prefix is found.
 * Supports multi-segment locales like "zh-Hant".
 */
export function getLocaleFromPath(pathname: string): Locale {
  // Try longer locale codes first (e.g. "zh-Hant" before "zh")
  const match = pathname.match(/^\/([a-z]{2}(?:-[A-Za-z]{2,8})?)(?:\/|$)/)
  const segment = match?.[1]
  if (segment && isValidLocale(segment)) return segment
  return "en"
}
