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
// All reads and writes go through BFF → ws-proxy, which operates on the Master
// cluster. This ensures ResourceVersion always matches between GET and PUT,
// eliminating 409 conflicts that occurred when reads came from Worker clusters.

import { globalTemplatesQueryOptions, globalTemplateQueryOptions } from "./global-template"

// ─── Query options (Master reads via BFF) ────────────────────────────────────

export const templatesQueryOptions = globalTemplatesQueryOptions
export const templateQueryOptions = globalTemplateQueryOptions

// ─── Mutations (global, via BFF → ws-proxy) ──────────────────────────────────
// Re-exported with backward-compatible names so existing component imports
// (useCreateTemplate, useUpdateTemplate, etc.) continue to work unchanged.

export { useCreateGlobalTemplate as useCreateTemplate } from "./global-template"
export { useUpdateGlobalTemplate as useUpdateTemplate } from "./global-template"
export { useDeleteGlobalTemplate as useDeleteTemplate } from "./global-template"
export { deleteGlobalTemplateImperative as deleteTemplateImperative } from "./global-template"
