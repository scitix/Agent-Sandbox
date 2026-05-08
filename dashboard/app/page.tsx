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

import { useAtomValue } from "jotai"
import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { authAtom, clustersAtom } from "@/lib/atoms"
import { clusterPath, loginPath } from "@/lib/cluster-path"
import { localeAtom } from "@/lib/i18n/atoms"
import { I18N_CONFIG, type Locale } from "@/lib/i18n/config"

export default function Home() {
  const router = useRouter()
  const auth = useAtomValue(authAtom)
  const clustersData = useAtomValue(clustersAtom)
  const storedLocale = useAtomValue(localeAtom)
  const [hydrated, setHydrated] = useState(false)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  useEffect(() => {
    if (!hydrated) return

    const locale: Locale = storedLocale ?? I18N_CONFIG.defaultLocale

    if (!auth) {
      router.replace(loginPath(locale))
      return
    }

    // Prefer clusterID stored in auth, fallback to first available cluster
    const clusterID = auth.clusterID ?? clustersData.clusters[0]?.id
    if (!clusterID) {
      // Clusters not yet loaded, wait for next render
      return
    }

    const page = auth.role === "admin" ? "sandboxes" : "overview"
    router.replace(clusterPath(clusterID, page, locale))
  }, [hydrated, auth, clustersData, router, storedLocale])

  // Render nothing while redirecting
  return null
}
