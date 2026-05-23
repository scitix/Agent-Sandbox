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

// Query options and mutations for Global API Keys.
//
// List uses the per-cluster /api-keys endpoint (self-service, tenant-scoped)
// so keys are read from the Worker the user is currently connected to.
// Create and Delete go through hub proxy → wsproxy, which derives user/team
// from the JWT and broadcasts key creation to all connected Workers.

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { currentApiClient } from "@/lib/api/client"
import { getHubFetchClient } from "@/lib/api/hub-client"
import { delayedInvalidate } from "./utils"

// ─── Query options ────────────────────────────────────────────────────────────

/** List keys from the current Worker cluster via the per-cluster OpenAPI endpoint. */
export function globalApiKeysQueryOptions() {
  return currentApiClient().queryOptions(
    "get",
    "/api-keys",
    {},
    {
      // ListAPIKeysResult is a bare APIKeyItem[] (the endpoint returns the
      // full list without pagination), not an envelope with `.items`.
      select: (data) => data ?? [],
    },
  )
}

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateGlobalApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { description?: string; expiresAt?: string }) => {
      const { data, error } = await getHubFetchClient().POST("/v1/api-keys", {
        body: {
          description: body.description,
          expiresAt: body.expiresAt,
        },
      })
      if (error) throw error
      return data
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/api-keys"]),
  })
}

export function useDeleteGlobalApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (name: string) => {
      const { error } = await getHubFetchClient().DELETE("/v1/api-keys/{name}", {
        params: { path: { name } },
      })
      if (error) throw error
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/api-keys"]),
  })
}
