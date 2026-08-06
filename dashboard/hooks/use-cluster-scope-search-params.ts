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

/**
 * Cluster scope for cluster-agnostic pages: every cluster, or an explicit
 * subset. "all" is deliberately not the same as "every ID listed" — it keeps
 * following the cluster list as clusters are added or become visible, and it
 * is what a shared link should reproduce.
 */
export type ClusterScope = "all" | string[]

/** True when the scope covers every cluster rather than a chosen subset. */
export function isAllClusters(scope: ClusterScope): scope is "all" {
  return scope === "all"
}

/**
 * Resolve a scope to the concrete cluster IDs to query.
 * `available` is the caller's visible cluster list, so a hidden cluster never
 * enters an "all" aggregate.
 */
export function resolveScopeClusterIDs(scope: ClusterScope, available: string[]): string[] {
  if (isAllClusters(scope)) return available
  const allowed = new Set(available)
  return scope.filter((id) => allowed.has(id))
}

/**
 * The `cluster` query param the Prometheus BFF routes take: a comma-separated
 * cluster list. Empty resolves to "all" so a scope whose clusters have all
 * disappeared degrades to the platform-wide view instead of querying nothing.
 */
export function clusterQueryParam(scope: ClusterScope, available: string[]): string {
  const ids = resolveScopeClusterIDs(scope, available)
  return ids.length > 0 ? ids.join(",") : "all"
}

/**
 * Persists the cluster-scope selector on `/overview` and `/admin` to the
 * `cluster` URL search param. Mirrors `useTimeRangeSearchParams()`.
 *
 * Encoding: absent or `all` → "all"; `cluster=a,b` → ["a", "b"].
 */
export function useClusterScopeSearchParams(): readonly [
  ClusterScope,
  (next: ClusterScope) => void,
] {
  const router = useRouter()
  const searchParams = useSearchParams()

  const scope = useMemo<ClusterScope>(() => {
    const raw = searchParams.get("cluster")
    if (!raw || raw === "all") return "all"
    const ids = raw
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
    return ids.length > 0 ? ids : "all"
  }, [searchParams])

  const setScope = useCallback(
    (next: ClusterScope) => {
      const params = new URLSearchParams(searchParams)
      if (isAllClusters(next) || next.length === 0) {
        params.delete("cluster")
      } else {
        params.set("cluster", next.join(","))
      }
      router.replace(`?${params}`, { scroll: false })
    },
    [router, searchParams],
  )

  return [scope, setScope] as const
}
