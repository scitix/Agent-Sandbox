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

// Query options and mutations for Global SandboxTemplate management.
//
// All operations go through the hub proxy (/api/hub → wsproxy), which reads/writes
// from the Master cluster and broadcasts changes to all Worker clusters.
// This ensures ResourceVersion is always from Master, eliminating 409 conflicts
// caused by mismatched Worker/Master versions.

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { getHubApiClient, getHubFetchClient } from "@/lib/api/hub-client"
import { delayedInvalidate } from "./utils"

// ─── Query options (Master reads via hub proxy) ───────────────────────────────

export const globalTemplatesQueryOptions = () =>
  getHubApiClient().queryOptions("get", "/v1/sandbox-templates", {}, {
    select: (data) => data.items ?? [],
  })

export const globalTemplateQueryOptions = (name: string) =>
  getHubApiClient().queryOptions(
    "get",
    "/v1/sandbox-templates/{name}",
    { params: { path: { name } } },
    { enabled: !!name },
  )

// ─── Mutations ───────────────────────────────────────────────────────────────

export function useCreateGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { crdJson: string }) => {
      const { data, error } = await getHubFetchClient().POST("/v1/admin/sandbox-templates", {
        body,
      })
      if (error) throw error
      return data
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/v1/sandbox-templates"]),
  })
}

export function useUpdateGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (args: { name: string; crdJson: string }) => {
      const { data, error } = await getHubFetchClient().PUT(
        "/v1/admin/sandbox-templates/{name}",
        { params: { path: { name: args.name } }, body: { crdJson: args.crdJson } },
      )
      if (error) throw error
      return data
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/v1/sandbox-templates"]),
  })
}

export function useDeleteGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (name: string) => {
      const { error } = await getHubFetchClient().DELETE(
        "/v1/admin/sandbox-templates/{name}",
        { params: { path: { name } } },
      )
      if (error) throw error
    },
    onSuccess: () => delayedInvalidate(qc, ["get", "/v1/sandbox-templates"]),
  })
}

// ─── Imperative helpers (for batch operations) ───────────────────────────────

export async function deleteGlobalTemplateImperative(name: string): Promise<void> {
  const { error } = await getHubFetchClient().DELETE("/v1/admin/sandbox-templates/{name}", {
    params: { path: { name } },
  })
  if (error) throw error
}
