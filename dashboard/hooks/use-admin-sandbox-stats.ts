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

import { useQueries } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { authAtom, clusterIDAtom, clustersAtom } from "@/lib/atoms"
import { getApiClient } from "@/lib/api/client"
import type { ClusterScope } from "@/hooks/use-cluster-scope-search-params"

export interface AdminSandboxStatsRow {
  clusterID: string
  namespace: string
  count: number
}

type StatsResponse = { statistics?: { total?: number; byStatus?: Record<string, number>; byNamespace?: Record<string, number> } }

/**
 * Admin K8s-fallback sandbox stats (shown only when Prometheus is not
 * configured), aggregated across the cluster scope selected on `/admin`.
 * Mirrors the `useQueries` fan-out pattern in `use-scoped-live-count.ts`.
 */
export function useAdminSandboxStats(scope: ClusterScope) {
  const auth = useAtomValue(authAtom)
  const boundClusterID = useAtomValue(clusterIDAtom)
  const clustersData = useAtomValue(clustersAtom)
  const isApiKey = auth?.authMethod === "apikey"

  const targetClusterIDs = isApiKey
    ? [boundClusterID]
    : scope !== "all"
      ? [scope]
      : clustersData.clusters.map((c) => c.id)

  const results = useQueries({
    queries: targetClusterIDs.map((clusterID) => ({
      ...getApiClient(clusterID).queryOptions("get", "/admin/statistics/sandboxes", undefined),
    })),
  })

  let total = 0
  const byStatus: Record<string, number> = {}
  const rows: AdminSandboxStatsRow[] = []

  results.forEach((r, i) => {
    const stats = (r.data as StatsResponse | undefined)?.statistics
    total += stats?.total ?? 0
    for (const [status, count] of Object.entries(stats?.byStatus ?? {})) {
      byStatus[status] = (byStatus[status] ?? 0) + count
    }
    for (const [namespace, count] of Object.entries(stats?.byNamespace ?? {})) {
      rows.push({ clusterID: targetClusterIDs[i], namespace, count })
    }
  })

  const isLoading = results.some((r) => r.isLoading)
  const isFetching = results.some((r) => r.isFetching)
  const refetchAll = () => results.forEach((r) => void r.refetch())

  return { total, byStatus, rows, isMultiCluster: targetClusterIDs.length > 1, isLoading, isFetching, refetchAll }
}
