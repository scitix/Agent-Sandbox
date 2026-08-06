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

import { useMemo } from "react"
import { useQueries } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { clustersAtom } from "@/lib/atoms"
import { getApiClient } from "@/lib/api/client"
import { resolveScopeClusterIDs, type ClusterScope } from "@/hooks/use-cluster-scope-search-params"

const REFETCH_MS = 30000

/**
 * Sums the current user's Running sandbox count across the cluster scope
 * selected on `/overview`. An API key authenticates against every cluster —
 * the proxy injects it per cluster from the JWT — so no scope is off-limits.
 */
export function useScopedLiveCount(scope: ClusterScope): { count: number; isLoading: boolean } {
  const clustersData = useAtomValue(clustersAtom)

  const targetClusterIDs = useMemo(
    () =>
      resolveScopeClusterIDs(
        scope,
        clustersData.clusters.map((c) => c.id),
      ),
    [scope, clustersData.clusters],
  )

  const results = useQueries({
    queries: targetClusterIDs.map((clusterID) => ({
      ...getApiClient(clusterID).queryOptions("get", "/statistics/sandboxes", undefined, {
        refetchInterval: REFETCH_MS,
      }),
    })),
  })

  const count = results.reduce((sum, r) => {
    const data = r.data as { statistics?: { byStatus?: Record<string, number> } } | undefined
    return sum + (data?.statistics?.byStatus?.["Running"] ?? 0)
  }, 0)
  const isLoading = results.some((r) => r.isLoading)

  return { count, isLoading }
}
