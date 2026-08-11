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
import { envsQueryOptions } from "@/lib/queries"
import { useClusterID } from "@/hooks/use-cluster-id"
import type { AgentSandboxEnvSummary } from "@/lib/api/client"

/**
 * Where an Env name stands on one cluster.
 *
 * `present` carries the bound template, which is all the two callers need:
 * the create form asks "is this name taken?", and the extend dialog treats
 * same-name-same-template as "already extended" and leaves per-cluster config
 * (autoscaling above all) free to differ.
 */
export type EnvPresence =
  | { clusterID: string; clusterName: string; state: "loading" }
  | { clusterID: string; clusterName: string; state: "absent" }
  | { clusterID: string; clusterName: string; state: "present"; env: AgentSandboxEnvSummary }
  | { clusterID: string; clusterName: string; state: "failed"; error: unknown }

export interface EnvNameAcrossClusters {
  /** Every configured cluster, current one included, in cluster-list order. */
  all: EnvPresence[]
  /** Everything except the cluster being viewed. */
  others: EnvPresence[]
  /** The cluster being viewed, if it is in the cluster list. */
  current: EnvPresence | undefined
  /** True while any cluster is still unresolved. */
  isProbing: boolean
}

/**
 * Probes every configured cluster for an Env of the given name.
 *
 * Deliberately reads the LIST endpoint per cluster rather than
 * `GET /envs/{name}`. A miss on the by-name endpoint is a 404, and the fetch
 * middleware turns every non-2xx into an error toast off a global error-code
 * allow-list (`SUPPRESSED_ERROR_CODES` in lib/api/client.ts) with no per-request
 * opt-out — so probing N clusters by name would fire a toast for each cluster
 * that legitimately doesn't have the Env. The list endpoint answers 200 either
 * way, and `SandboxEnvSummary` already carries `templateName`.
 *
 * `retry: false` matters too: an unreachable cluster has to settle promptly,
 * because the create form blocks input while this is in flight.
 *
 * Passing an empty name disables the probe entirely.
 */
export function useEnvNameAcrossClusters(name: string): EnvNameAcrossClusters {
  const currentClusterID = useClusterID()
  const clusters = useAtomValue(clustersAtom).clusters
  const enabled = name.trim().length > 0

  const results = useQueries({
    queries: clusters.map((c) => ({
      ...envsQueryOptions(c.id),
      enabled,
      retry: false,
    })),
  })

  return useMemo(() => {
    const all: EnvPresence[] = clusters.map((c, i) => {
      const clusterName = c.name ?? c.id
      const base = { clusterID: c.id, clusterName }
      const q = results[i]

      if (!enabled || !q || q.isPending) return { ...base, state: "loading" }
      if (q.isError) return { ...base, state: "failed", error: q.error }

      const env = (q.data as AgentSandboxEnvSummary[] | undefined)?.find((e) => e.name === name)
      return env ? { ...base, state: "present", env } : { ...base, state: "absent" }
    })

    return {
      all,
      others: all.filter((p) => p.clusterID !== currentClusterID),
      current: all.find((p) => p.clusterID === currentClusterID),
      isProbing: enabled && all.some((p) => p.state === "loading"),
    }
  }, [clusters, results, enabled, name, currentClusterID])
}
