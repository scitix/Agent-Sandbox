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

// Presentation helpers shared by the ManagedAgent list, form and detail views.

import type { StatusBadgeColorMap } from "@/components/custom/status-badge"
import type { HandsMode, ManagedAgentHands, ManagedAgentModel } from "@/lib/api/managed-agent-types"

/** Badge colours for `status.phase`: Pending / Provisioning / Ready / Degraded / Failed. */
export const MANAGED_AGENT_PHASE_COLORS: StatusBadgeColorMap = {
  ready: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30",
  provisioning: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  degraded: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
  failed: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  pending: "bg-gray-500/15 text-gray-500 dark:text-gray-400 border-gray-500/30",
}

/**
 * Which of the three mutually exclusive supply branches an agent declares.
 * `auto` is the answer for a spec that declares none, matching the form's
 * default rather than leaving the caller with a null to special-case.
 */
export function handsModeOf(hands?: ManagedAgentHands): HandsMode {
  if (hands?.envRef) return "envRef"
  if (hands?.external) return "external"
  return "auto"
}

/**
 * Parses the model textarea: one model per line, `id`, `id | Display Name` or
 * `id | Display Name | nonreasoning`. The list is the only source of the
 * composer's dropdown, so an empty textarea yields no models rather than a guess.
 */
export function parseModels(value?: string): ManagedAgentModel[] | undefined {
  const models: ManagedAgentModel[] = []
  for (const line of (value ?? "").split("\n")) {
    const parts = line.split("|").map((part) => part.trim())
    const id = parts[0]
    if (!id) continue
    const model: ManagedAgentModel = { id }
    if (parts[1]) model.name = parts[1]
    if (parts[2]?.toLowerCase() === "nonreasoning") model.nonReasoning = true
    models.push(model)
  }
  return models.length > 0 ? models : undefined
}

/** Inverse of parseModels, used to seed the textarea when editing. */
export function formatModels(models?: ManagedAgentModel[]): string {
  return (models ?? [])
    .map((model) =>
      [model.id, model.name ?? "", model.nonReasoning ? "nonreasoning" : ""]
        .join(" | ")
        .replace(/(\s*\|\s*)+$/, ""),
    )
    .join("\n")
}
