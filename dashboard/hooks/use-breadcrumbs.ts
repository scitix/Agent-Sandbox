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

"use client"

import { usePathname } from "next/navigation"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import type { Locale } from "@/lib/i18n/config"
import {
  STANDALONE_PAGES,
  clusterPath,
  standalonePath,
  type DashboardPage,
} from "@/lib/cluster-path"

export interface Crumb {
  label: string
  /** Link target; absent on the current (last) crumb. */
  href?: string
  isCurrent: boolean
}

/**
 * Maps the first path segment (a dashboard page) to its sidebar i18n label so
 * the breadcrumb and the nav stay in lockstep. Pages absent here fall back to
 * the raw, decoded segment.
 */
const SEGMENT_LABEL_KEY: Partial<Record<string, TranslationKey>> = {
  overview: "nav.overview",
  sandboxes: "nav.sandboxes",
  envs: "nav.envs",
  templates: "nav.templates",
  images: "nav.images",
  datasets: "nav.datasets",
  "managed-agents": "nav.managedAgents",
  general: "nav.general",
  "api-keys": "nav.apiKeys",
  quota: "nav.quota",
  admin: "nav.adminStats",
  "admin-api-keys": "nav.apiKeys",
  changelog: "nav.changelog",
}

/**
 * One crumb for the page, then one per deeper segment. `pageHref` is where the
 * page crumb links once there are deeper segments; the trailing crumb never
 * links, because it is the current view.
 *
 * Deeper segments show verbatim from the URL rather than waiting on a fetch, so
 * a detail page's title does not flash on load.
 */
function buildCrumbs(
  t: ReturnType<typeof useTranslation>["t"],
  pageSegment: string,
  rest: string,
  pageHref: string,
): Crumb[] {
  const trimmed = rest.replace(/^\/+|\/+$/g, "")
  const segments = trimmed ? trimmed.split("/") : []
  const labelKey = SEGMENT_LABEL_KEY[pageSegment]

  const crumbs: Crumb[] = [
    {
      label: labelKey ? t(labelKey) : decodeURIComponent(pageSegment),
      href: segments.length > 0 ? pageHref : undefined,
      isCurrent: segments.length === 0,
    },
  ]

  let acc = pageHref
  segments.forEach((segment, i) => {
    acc = `${acc}/${segment}`
    const last = i === segments.length - 1
    crumbs.push({
      label: decodeURIComponent(segment),
      href: last ? undefined : acc,
      isCurrent: last,
    })
  })

  return crumbs
}

/**
 * Builds breadcrumbs purely from the route. The trailing crumb is the current
 * page, rendered bold by the header and doubling as the title.
 *
 * Returns an empty array for the bare `/clusters/{id}` redirect path, which has
 * no page segment to label.
 *
 * Split out of the hook so the route parsing is testable without React: the two
 * matchers below and `lib/cluster-path.ts` have to agree on which pages carry a
 * cluster in their URL, and when they drift the failure is a silently empty
 * breadcrumb bar (which is also the page title) rather than an error.
 */
export function breadcrumbsFor(
  pathname: string,
  clusterID: string,
  locale: Locale,
  t: ReturnType<typeof useTranslation>["t"],
): Crumb[] {
  // Standalone, cluster-agnostic pages have no [clusterID] route segment. They
  // are not all leaves — a managed agent has detail tabs under it — so capture
  // the remainder instead of anchoring the match at the page segment.
  const standalone = pathname.match(
    new RegExp(`^(?:/[a-z]{2}(?:-[A-Za-z]{2,8})?)?/(${STANDALONE_PAGES.join("|")})(?:/(.*))?$`),
  )
  if (standalone) {
    const page = standalone[1] as DashboardPage
    return buildCrumbs(t, page, standalone[2] ?? "", standalonePath(page, locale))
  }

  // Strip the optional [locale] prefix and the /clusters/{id} prefix, then keep
  // whatever follows. Mirrors the matcher shape in lib/cluster-path.ts.
  const match = pathname.match(/^(?:\/[a-z]{2}(?:-[A-Za-z]{2,8})?)?\/clusters\/[^/]+\/(.*)$/)
  const remainder = match?.[1]?.replace(/\/+$/, "") ?? ""
  if (!remainder) return []

  const [page, ...rest] = remainder.split("/")
  return buildCrumbs(t, page, rest.join("/"), clusterPath(clusterID, page as DashboardPage, locale))
}

/** Route-derived breadcrumbs for the current page. */
export function useBreadcrumbs(): Crumb[] {
  return breadcrumbsFor(usePathname(), useClusterID(), useLocale(), useTranslation().t)
}
