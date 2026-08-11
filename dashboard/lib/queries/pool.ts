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

// Query options for SandboxPools (now env-scoped only). The top-level
// /v1/sandboxpools endpoints were removed in the 2026.06 refactor — every
// Pool lives under an Env and is reached via /v1/envs/{name}/sandboxpools.

import { useQueryClient } from "@tanstack/react-query"
import { currentApiClient, currentFetchClient } from "@/lib/api/client"
import { apiFor, delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const envPoolsQueryOptions = (envName: string, clusterID?: string) =>
  apiFor(clusterID).queryOptions(
    "get",
    "/envs/{name}/sandboxpools",
    {
      params: { path: { name: envName } },
    },
    {
      select: (data) => (data.items ?? []).slice().sort((a, b) => a.name.localeCompare(b.name)),
    },
  )

export const envPoolQueryOptions = (envName: string, poolName: string, clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/envs/{name}/sandboxpools/{poolName}", {
    params: { path: { name: envName, poolName } },
  })

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateEnvPool(envName: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/envs/{name}/sandboxpools", {
    onSuccess: () => {
      delayedInvalidate(qc, [
        "get",
        "/envs/{name}/sandboxpools",
        { params: { path: { name: envName } } },
      ])
      delayedInvalidate(qc, ["get", "/envs"])
    },
  })
}

export function useUpdateEnvPool(envName: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("put", "/envs/{name}/sandboxpools/{poolName}", {
    onSuccess: () => {
      delayedInvalidate(qc, [
        "get",
        "/envs/{name}/sandboxpools",
        { params: { path: { name: envName } } },
      ])
      delayedInvalidate(qc, ["get", "/envs"])
    },
  })
}

export function useDeleteEnvPool(envName: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/envs/{name}/sandboxpools/{poolName}", {
    onSuccess: () => {
      delayedInvalidate(qc, [
        "get",
        "/envs/{name}/sandboxpools",
        { params: { path: { name: envName } } },
      ])
      delayedInvalidate(qc, ["get", "/envs"])
    },
  })
}

/**
 * Imperative member-pool delete for the multi-select batch toolbar. Mirrors
 * `deleteSandboxImperative`; the caller invalidates the pool list afterwards.
 */
export async function deleteEnvPoolImperative(envName: string, poolName: string): Promise<void> {
  const client = currentFetchClient()
  const { error } = await client.DELETE("/envs/{name}/sandboxpools/{poolName}", {
    params: { path: { name: envName, poolName } },
  })
  if (error) throw error
}
