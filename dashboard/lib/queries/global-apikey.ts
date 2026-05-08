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
// Create and Delete still go through BFF → ws-proxy → Manager for broadcast.

import { useMutation, useQueryClient } from "@tanstack/react-query"
import {
  currentApiClient,
  type GlobalCreateApiKeyResult,
} from "@/lib/api/client"
import { bff } from "@/lib/api/bff-client"
import { delayedInvalidate } from "./utils"

// ─── BFF calls (create/delete go through BFF → ws-proxy → Manager for broadcast) ──

async function createGlobalApiKey(body: {
  description?: string
  expiresAt?: string
}): Promise<GlobalCreateApiKeyResult> {
  return bff.post("api/global-api-keys", { json: body }).json()
}

async function deleteGlobalApiKey(name: string): Promise<void> {
  await bff.delete(`api/global-api-keys/${encodeURIComponent(name)}`)
}

// ─── Query options ────────────────────────────────────────────────────────────

/** List keys from the current Worker cluster via the per-cluster OpenAPI endpoint. */
export function globalApiKeysQueryOptions() {
  return currentApiClient().queryOptions(
    "get",
    "/api-keys",
    {},
    {
      select: (data) => data.items ?? [],
    },
  )
}

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateGlobalApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: createGlobalApiKey,
    onSuccess: () => delayedInvalidate(qc, ["get", "/api-keys"]),
  })
}

export function useDeleteGlobalApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: deleteGlobalApiKey,
    onSuccess: () => delayedInvalidate(qc, ["get", "/api-keys"]),
  })
}
