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

// Redirect shim: /admin moved to a top-level, cluster-agnostic route with an
// in-page cluster-scope selector. This keeps old bookmarked links working.

"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"

export default function AdminRedirectPage() {
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  useEffect(() => {
    router.replace(`${clusterPath(clusterID, "admin", locale)}?cluster=${encodeURIComponent(clusterID)}`)
  }, [router, clusterID, locale])

  return null
}
