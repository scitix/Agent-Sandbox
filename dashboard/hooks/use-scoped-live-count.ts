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
import { authAtom, clusterIDAtom, clustersAtom } from "@/lib/atoms"
import { getApiClient } from "@/lib/api/client"
import type { ClusterScope } from "@/hooks/use-cluster-scope-search-params"

const REFETCH_MS = 30000

/**
 * Sums the current user's Running sandbox count across the cluster scope
 * selected on `/overview`. API-key sessions are single-cluster credentials
 * (getApiClient for any other cluster would fail auth), so they always
 * resolve to their login-bound cluster regardless of the `scope` param.
 */
export function useScopedLiveCount(scope: ClusterScope): { count: number; isLoading: boolean } {
  const auth = useAtomValue(authAtom)
  const boundClusterID = useAtomValue(clusterIDAtom)
  const clustersData = useAtomValue(clustersAtom)
  const isApiKey = auth?.authMethod === "apikey"

  const targetClusterIDs = useMemo(() => {
    if (isApiKey) return [boundClusterID]
    if (scope !== "all") return [scope]
    return clustersData.clusters.map((c) => c.id)
  }, [isApiKey, boundClusterID, scope, clustersData.clusters])

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
