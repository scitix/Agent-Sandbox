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

// Query options and mutations for Sandbox Pool resources

import { useQueryClient } from "@tanstack/react-query"
import { currentApiClient, currentFetchClient } from "@/lib/api/client"
import { delayedInvalidate, impersonationHeaders } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const poolsQueryOptions = () =>
  currentApiClient().queryOptions("get", "/sandboxpools", undefined, {
    select: (data) => data.items ?? [],
  })

export const poolQueryOptions = (name: string) =>
  currentApiClient().queryOptions("get", "/sandboxpools/{name}", {
    params: { path: { name } },
  })

// Admin impersonation variant: calls the regular /sandboxpools endpoint with
// X-Impersonate-Team and X-Impersonate-User headers.
export const adminUserPoolsQueryOptions = (team: string, user: string) =>
  currentApiClient().queryOptions(
    "get",
    "/sandboxpools",
    { headers: impersonationHeaders(team, user) },
    {
      select: (data) => data.items ?? [],
    },
  )

// ─── Mutations ─────────────────────────────────────────────────────────────────

export function useCreatePool() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/sandboxpools", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/sandboxpools"]),
  })
}

export function useUpdatePool() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("put", "/sandboxpools/{name}", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/sandboxpools"]),
  })
}

export function useDeletePool() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/sandboxpools/{name}", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/sandboxpools"]),
  })
}

export function useSyncPoolTemplate() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/sandboxpools/{name}/sync-template", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/sandboxpools"]),
  })
}

export const previewSyncTemplateQueryOptions = (poolName: string) =>
  currentApiClient().queryOptions(
    "post",
    "/sandboxpools/{name}/sync-template/preview",
    { params: { path: { name: poolName } } },
  )

export function useAdminCreateUserPool(team: string, user: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/sandboxpools", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxpools", { headers: impersonationHeaders(team, user) }])
    },
  })
}

export function useAdminUpdateUserPool(team: string, user: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("put", "/sandboxpools/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxpools", { headers: impersonationHeaders(team, user) }])
    },
  })
}

export function useAdminDeleteUserPool(team: string, user: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/sandboxpools/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/sandboxpools", { headers: impersonationHeaders(team, user) }])
    },
  })
}

// ─── Imperative helpers (for batch operations) ─────────────────────────────────

export async function deletePoolImperative(name: string): Promise<void> {
  const client = currentFetchClient()
  const { error } = await client.DELETE("/sandboxpools/{name}", {
    params: { path: { name } },
  })
  if (error) throw error
}

export async function adminDeleteUserPoolImperative(
  team: string,
  user: string,
  name: string,
): Promise<void> {
  const client = currentFetchClient()
  const { error } = await client.DELETE("/sandboxpools/{name}", {
    params: { path: { name } },
    headers: impersonationHeaders(team, user),
  })
  if (error) throw error
}
