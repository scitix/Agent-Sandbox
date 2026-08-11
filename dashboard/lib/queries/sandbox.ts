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
import { SUPPRESSED_ERROR_CODES, basePath, getToken, handleErrorResponse } from "@/lib/api/client"
import type { E2BCreateBody } from "@/lib/utils/e2b-sandbox"
import { apiFor, delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const sandboxesQueryOptions = (
  params?: {
    poolName?: string
    status?: string
    limit?: number
    offset?: number
  },
  clusterID?: string,
) =>
  apiFor(clusterID).queryOptions(
    "get",
    "/sandboxes",
    {
      params: { query: params },
    },
    {
      select: (data: { items: AgentSandbox[] }) => data.items ?? [],
    },
  )

export const sandboxQueryOptions = (sandboxId: string, clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/sandboxes/{sandboxId}", {
    params: { path: { sandboxId } },
  })

// ─── Mutations ─────────────────────────────────────────────────────────────────

export function useCreateSandbox() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/sandboxes", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxes"])
      // Env-scoped pool lists pick up replica/idle deltas from a new sandbox
      // claim — refresh the envs cache so the per-env pool table sees it.
      delayedInvalidate(qc, ["get", "/envs"])
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

/**
 * Creates a sandbox through the cluster's E2B-compatible API.
 *
 * Not on the openapi client: the E2B surface has no generated types here, and it
 * needs a header the typed clients do not send. `apiKey` is the caller's own
 * AgentBox key — the E2B auth middleware accepts API keys only, never the session
 * JWT — and the BFF forwards it as `X-API-Key` (see
 * app/api/clusters/[clusterID]/e2b/[...path]/route.ts).
 *
 * Reads stay on the native API; only creation goes through here.
 */
export async function createSandboxViaE2B(
  body: E2BCreateBody,
  opts: { clusterID: string; apiKey: string },
): Promise<{ sandboxID?: string }> {
  const res = await fetch(`${basePath}/api/clusters/${opts.clusterID}/e2b/sandboxes`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${getToken()}`,
      "X-API-Key": opts.apiKey,
    },
    body: JSON.stringify(body),
  })

  if (!res.ok) {
    // The BFF has already normalised E2B's {code,message} into the native
    // {error,errorCode} shape, so this is the same handling every other call gets.
    return handleErrorResponse(res, SUPPRESSED_ERROR_CODES)
  }
  return (await res.json()) as { sandboxID?: string }
}
