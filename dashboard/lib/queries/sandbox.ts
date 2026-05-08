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

// Query options and mutations for Sandbox resources

import { useQueryClient } from "@tanstack/react-query"
import { AgentSandbox, currentApiClient, currentFetchClient } from "@/lib/api/client"
import { delayedInvalidate, impersonationHeaders } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const sandboxesQueryOptions = (params?: {
  poolName?: string
  status?: string
  limit?: number
  offset?: number
}) =>
  currentApiClient().queryOptions(
    "get",
    "/sandboxes",
    {
      params: { query: params },
    },
    {
      select: (data: { items: AgentSandbox[] }) => data.items ?? [],
    },
  )

export const sandboxQueryOptions = (sandboxId: string) =>
  currentApiClient().queryOptions("get", "/sandboxes/{sandboxId}", {
    params: { path: { sandboxId } },
  })

// ─── Mutations ─────────────────────────────────────────────────────────────────

export function useCreateSandbox() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/sandboxes", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxes"])
      delayedInvalidate(qc, ["get", "/sandboxpools"])
    },
  })
}

export function useDeleteSandbox() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/sandboxes/{sandboxId}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxes"])
    },
  })
}

// ─── Imperative helpers (for batch operations) ─────────────────────────────────

export async function deleteSandboxImperative(sandboxId: string): Promise<void> {
  const client = currentFetchClient()
  const { error } = await client.DELETE("/sandboxes/{sandboxId}", {
    params: { path: { sandboxId } },
  })
  if (error) throw error
}

// ─── Exec token (single-use, no invalidation needed) ───────────────────────────

export function useCreateExecToken() {
  // No cache invalidation — the token is one-time use and not cacheable.
  return currentApiClient().useMutation("post", "/sandboxes/{sandboxId}/exec-token")
}
