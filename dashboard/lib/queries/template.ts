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

// Query options and mutations for Sandbox Template resources.
//
// Reads and writes deliberately come from different places, because "which
// templates exist" and "which template am I editing" are different questions:
//
//   * The LIST is per cluster. This page lives at /clusters/{id}/templates and
//     answers "what can I build a sandbox from HERE". The hub's catalog answers
//     a different question — what has been published globally — and the two do
//     diverge: a template applied straight to a worker is absent from the hub,
//     and one published to the hub is absent from a cluster it has not reached.
//     Listing the hub's copy under a cluster's heading shows templates that
//     cannot be used and hides ones that can, with nothing on screen saying so.
//   * The DETAIL read stays on the hub, because the edit form round-trips the
//     whole CRD JSON and PUT goes to the hub: a resourceVersion read from a
//     worker belongs to that worker's copy and the write 409s.
//
// A template the hub does not have is therefore listable but not editable —
// which is the truth, since there is nothing on the hub to edit.

import { globalTemplateQueryOptions } from "./global-template"
import { apiFor } from "./utils"

// ─── Query options ───────────────────────────────────────────────────────────

/** Templates this cluster can actually build from. */
export const templatesQueryOptions = (clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/sandbox-templates", undefined, {
    select: (data) => data.items ?? [],
  })

export const templateQueryOptions = globalTemplateQueryOptions

/**
 * The same template as this cluster holds it.
 *
 * Read only when the hub does not have it, which is how a template applied
 * straight to a worker still opens instead of showing "not found". It carries
 * the same envelope, so the detail view needs no second shape — but it cannot
 * be edited, because the write goes to a hub that has nothing to write to.
 */
export const clusterTemplateQueryOptions = (name: string, clusterID?: string) =>
  apiFor(clusterID).queryOptions(
    "get",
    "/sandbox-templates/{name}",
    { params: { path: { name } } },
    { enabled: !!name, retry: false },
  )

// ─── Mutations (global, via BFF → ws-proxy) ──────────────────────────────────
// Re-exported with backward-compatible names so existing component imports
// (useCreateTemplate, useUpdateTemplate, etc.) continue to work unchanged.

export { useCreateGlobalTemplate as useCreateTemplate } from "./global-template"
export { useUpdateGlobalTemplate as useUpdateTemplate } from "./global-template"
export { useDeleteGlobalTemplate as useDeleteTemplate } from "./global-template"
export { deleteGlobalTemplateImperative as deleteTemplateImperative } from "./global-template"
