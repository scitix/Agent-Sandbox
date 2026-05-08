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
// All operations go through BFF → ws-proxy internal API, which reads/writes
// from the Master cluster and broadcasts changes to all Worker clusters.
// This ensures ResourceVersion is always from Master, eliminating 409 conflicts
// caused by mismatched Worker/Master versions.

import { useMutation, useQueryClient, queryOptions } from "@tanstack/react-query"
import type { AgentSandboxTemplate, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { bff } from "@/lib/api/bff-client"
import { delayedInvalidate } from "./utils"

// ─── BFF calls ───────────────────────────────────────────────────────────────

async function fetchGlobalTemplates(): Promise<AgentSandboxTemplateSummary[]> {
  const data = await bff.get("api/global-templates").json<{ items?: AgentSandboxTemplateSummary[] }>()
  return data.items ?? []
}

async function fetchGlobalTemplate(name: string): Promise<{ template: AgentSandboxTemplate }> {
  return bff.get(`api/global-templates/${encodeURIComponent(name)}`).json()
}

async function createGlobalTemplate(body: {
  name?: string
  spec?: unknown
  crdYaml?: string
}): Promise<{ name: string }> {
  return bff.post("api/global-templates", { json: body }).json()
}

async function updateGlobalTemplate(args: {
  name: string
  spec?: unknown
  crdYaml?: string
}): Promise<{ name: string }> {
  const { name, ...body } = args
  return bff.put(`api/global-templates/${encodeURIComponent(name)}`, { json: body }).json()
}

async function deleteGlobalTemplate(name: string): Promise<void> {
  await bff.delete(`api/global-templates/${encodeURIComponent(name)}`)
}

// ─── Query options (Master reads via BFF) ────────────────────────────────────

export const globalTemplatesQueryOptions = () =>
  queryOptions({
    queryKey: ["global-templates"],
    queryFn: fetchGlobalTemplates,
  })

export const globalTemplateQueryOptions = (name: string) =>
  queryOptions({
    queryKey: ["global-templates", name],
    queryFn: () => fetchGlobalTemplate(name),
    enabled: !!name,
  })

// ─── Mutations ───────────────────────────────────────────────────────────────

export function useCreateGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: createGlobalTemplate,
    onSuccess: () => delayedInvalidate(qc, ["global-templates"]),
  })
}

export function useUpdateGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: updateGlobalTemplate,
    onSuccess: () => delayedInvalidate(qc, ["global-templates"]),
  })
}

export function useDeleteGlobalTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: deleteGlobalTemplate,
    onSuccess: () => delayedInvalidate(qc, ["global-templates"]),
  })
}

// ─── Imperative helpers (for batch operations) ───────────────────────────────

export async function deleteGlobalTemplateImperative(name: string): Promise<void> {
  return deleteGlobalTemplate(name)
}
