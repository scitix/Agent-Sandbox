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

// Managed Agents used to live under /clusters/{clusterID}/. Agents are a
// control-plane resource with no cluster in their identity, so the page moved to
// the top level — but the old links are in bookmarks, in an agent's own
// `spec.docs`, and in the integration guide, so they keep working here.
//
// The cluster id is deliberately dropped rather than carried as a query
// parameter: the destination has nothing to do with it.
import { redirect } from "next/navigation"

import { standalonePath } from "@/lib/cluster-path"
import { isValidLocale } from "@/lib/i18n/config"

interface PageProps {
  params: Promise<{ locale: string; clusterID: string; rest?: string[] }>
}

export default async function LegacyManagedAgentsRedirect({ params }: PageProps) {
  const { locale, rest } = await params
  const suffix = rest?.length ? `/${rest.map(encodeURIComponent).join("/")}` : ""
  redirect(
    `${standalonePath("managed-agents", isValidLocale(locale) ? locale : undefined)}${suffix}`,
  )
}
