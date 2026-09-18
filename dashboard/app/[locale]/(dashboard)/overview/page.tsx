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

// Redirect shim: /overview went back under /clusters/{clusterID}/overview.
// The page reads every cluster by default, so the route does not pin the data
// — it only keeps the cluster the person was working in, which every link the
// sidebar builds from here on carries. Old bookmarks, and links shared while
// the page was cluster-agnostic, land on the session's cluster; a `?cluster=`
// scope rides along so a link to a narrowed view still opens narrowed.

"use client"

import { useEffect } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { useNavClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"

export default function OverviewRedirectPage() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const clusterID = useNavClusterID()
  const locale = useLocale()

  useEffect(() => {
    const query = searchParams.toString()
    router.replace(`${clusterPath(clusterID, "overview", locale)}${query ? `?${query}` : ""}`)
  }, [router, searchParams, clusterID, locale])

  return null
}
