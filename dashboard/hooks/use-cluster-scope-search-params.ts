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

import { useCallback, useMemo } from "react"
import { useRouter, useSearchParams } from "next/navigation"

/** Cluster scope for cluster-agnostic pages: "all" or a specific clusterID. */
export type ClusterScope = "all" | string

/**
 * Persists the cluster-scope selector on `/overview` and `/admin` to the
 * `cluster` URL search param. Mirrors `useTimeRangeSearchParams()`.
 */
export function useClusterScopeSearchParams(): readonly [ClusterScope, (next: ClusterScope) => void] {
  const router = useRouter()
  const searchParams = useSearchParams()

  const scope = useMemo<ClusterScope>(() => searchParams.get("cluster") ?? "all", [searchParams])

  const setScope = useCallback(
    (next: ClusterScope) => {
      const params = new URLSearchParams(searchParams)
      if (next === "all") {
        params.delete("cluster")
      } else {
        params.set("cluster", next)
      }
      router.replace(`?${params}`, { scroll: false })
    },
    [router, searchParams],
  )

  return [scope, setScope] as const
}
