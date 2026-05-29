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
import { clusterPath, type DashboardPage } from "@/lib/cluster-path"

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
  datasets: "nav.datasets",
  general: "nav.general",
  "api-keys": "nav.apiKeys",
  quota: "nav.quota",
  admin: "nav.adminStats",
  "admin-api-keys": "nav.apiKeys",
  changelog: "nav.changelog",
}

/**
 * Builds breadcrumbs purely from the route. The trailing crumb is the current
 * page (rendered bold by the header, doubling as the title); deeper segments
 * (detail ids/names) show verbatim from the URL so there is no load-time flash.
 *
 * Returns an empty array for the bare `/clusters/{id}` redirect path, which has
 * no page segment to label.
 */
export function useBreadcrumbs(): Crumb[] {
  const pathname = usePathname()
  const clusterID = useClusterID()
  const locale = useLocale()
  const { t } = useTranslation()

  // Strip the optional [locale] prefix and the /clusters/{id} prefix, then keep
  // whatever follows. Mirrors the matcher shape in lib/cluster-path.ts.
  const match = pathname.match(/^(?:\/[a-z]{2}(?:-[A-Za-z]{2,8})?)?\/clusters\/[^/]+\/(.*)$/)
  const remainder = match?.[1]?.replace(/\/+$/, "") ?? ""
  if (!remainder) return []

  const segments = remainder.split("/")
  const page = segments[0] as DashboardPage
  const labelKey = SEGMENT_LABEL_KEY[segments[0]]
  const pageLabel = labelKey ? t(labelKey) : decodeURIComponent(segments[0])

  const hasDetail = segments.length > 1

  const crumbs: Crumb[] = [
    {
      label: pageLabel,
      href: hasDetail ? clusterPath(clusterID, page, locale) : undefined,
      isCurrent: !hasDetail,
    },
  ]

  if (hasDetail) {
    // The detail id/name is the last segment; intermediate segments (rare) are
    // surfaced verbatim too.
    for (let i = 1; i < segments.length; i++) {
      const last = i === segments.length - 1
      crumbs.push({
        label: decodeURIComponent(segments[i]),
        isCurrent: last,
      })
    }
  }

  return crumbs
}
