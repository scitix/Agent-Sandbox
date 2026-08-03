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

// Query options and mutations for ManagedAgent.
//
// ManagedAgent is a control-plane resource: it lives on the Master cluster and
// is reached through the BFF proxy (/api/managed-agents → wsproxy
// /internal/managedagents), not through the per-cluster OpenAPI client. The
// requests are raw `fetch` for the same reason global-template's writes are —
// the endpoint has no entry in the per-cluster OpenAPI spec.

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"
import { basePath, getToken, handleErrorResponse } from "@/lib/api/client"
import type {
  CreateManagedAgentRequest,
  ManagedAgent,
  ManagedAgentListResult,
  UpdateManagedAgentRequest,
} from "@/lib/api/managed-agent-types"
import { delayedInvalidate } from "./utils"

const API_ROOT = `${basePath}/api/managed-agents`

/** Root query key; every managed-agent cache entry hangs off it. */
export const MANAGED_AGENTS_QUERY_KEY = ["managed-agents"] as const

async function managedAgentFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken()
  const headers = new Headers(init?.headers)
  if (token) headers.set("Authorization", `Bearer ${token}`)
  if (init?.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json")

  const res = await fetch(`${API_ROOT}${path}`, { ...init, headers })
  if (!res.ok) return handleErrorResponse(res)
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

// ─── Query options ────────────────────────────────────────────────────────────

/** Lists the ManagedAgents visible to the caller's team. */
export const managedAgentsQueryOptions = () =>
  queryOptions({
    queryKey: [...MANAGED_AGENTS_QUERY_KEY],
    queryFn: () => managedAgentFetch<ManagedAgentListResult>(""),
    select: (data: ManagedAgentListResult) => data.items ?? [],
  })

/** Fetches one ManagedAgent, including its status and rendered CRD YAML. */
export const managedAgentQueryOptions = (name: string) =>
  queryOptions({
    queryKey: [...MANAGED_AGENTS_QUERY_KEY, name],
    queryFn: () => managedAgentFetch<ManagedAgent>(`/${encodeURIComponent(name)}`),
    enabled: !!name,
  })

// ─── Mutations ────────────────────────────────────────────────────────────────

export function useCreateManagedAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateManagedAgentRequest) =>
      managedAgentFetch<ManagedAgent>("", { method: "POST", body: JSON.stringify(body) }),
    onSuccess: () => delayedInvalidate(qc, [...MANAGED_AGENTS_QUERY_KEY]),
  })
}

export function useUpdateManagedAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ name, ...body }: { name: string } & UpdateManagedAgentRequest) =>
      managedAgentFetch<ManagedAgent>(`/${encodeURIComponent(name)}`, {
        method: "PUT",
        body: JSON.stringify(body),
      }),
    onSuccess: () => delayedInvalidate(qc, [...MANAGED_AGENTS_QUERY_KEY]),
  })
}

export function useDeleteManagedAgent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) =>
      managedAgentFetch<void>(`/${encodeURIComponent(name)}`, { method: "DELETE" }),
    onSuccess: () => delayedInvalidate(qc, [...MANAGED_AGENTS_QUERY_KEY]),
  })
}
