// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Which tab of the ManagedAgent form owns which field.
//
// A tabbed form can hide a validation error, and a hidden error reads as "the
// save button does nothing". So the mapping is data, not layout: it drives the
// error dot on each tab and the jump-to-the-offending-tab on a failed submit,
// and a test asserts every field is claimed by exactly one tab — a field added
// without a home would otherwise become unreachable in silence.

import type { FieldErrors } from "react-hook-form"

import type { FormValues } from "@/lib/utils/managed-agent-form"

export type ManagedAgentFormTab = "basics" | "runtime" | "scenarios" | "classifier"

/** Left-to-right order, which is also the order a failed submit searches. */
export const MANAGED_AGENT_FORM_TABS: readonly ManagedAgentFormTab[] = [
  "basics",
  "runtime",
  "scenarios",
  "classifier",
]

/**
 * The essentials tab. It holds every field that is required unconditionally, on
 * purpose: the CRD makes the default harness's credentials mandatory, and a
 * required field parked behind an unopened tab turns a failed save into a
 * mystery. The rest of that harness's knobs live under "runtime".
 */
const BASICS: readonly (keyof FormValues)[] = [
  "name",
  "displayName",
  "description",
  "imageRepository",
  "imageTag",
  "basePrompt",
  "defaultRuntime",
  // Sandbox connection: one of three branches, all of them here.
  "handsMode",
  "autoClusterID",
  "autoTemplateRef",
  "autoImage",
  "autoIdleTimeoutSeconds",
  "autoStartupTimeoutSeconds",
  "instanceTypes",
  "envClusterID",
  "envName",
  "envNamespace",
  "envScalingGroup",
  "envImage",
  "externalApiURL",
  "externalDomain",
  "externalHTTPS",
  "externalEnvName",
  "externalImage",
  "externalScalingGroup",
  "sandboxApiKey",
]

const RUNTIME: readonly (keyof FormValues)[] = [
  "claudeEnabled",
  "claudeBaseURL",
  "claudeApiKey",
  "claudeModels",
  "claudeDefaultModel",
  "opencodeEnabled",
  "opencodeBaseURL",
  "opencodeApiKey",
  "opencodeModels",
  "opencodeDefaultModel",
  "opencodePort",
]

const SCENARIOS: readonly (keyof FormValues)[] = ["scenarios"]

const CLASSIFIER: readonly (keyof FormValues)[] = [
  "classifierEnabled",
  "classifierWire",
  "classifierBaseURL",
  "classifierModel",
  "classifierApiKey",
]

/**
 * The default harness's connection fields live on "basics", the other harness's
 * identical set on "runtime" — so where a field belongs depends on which harness
 * is the default. Only one of the two blocks is rendered per tab, so no field is
 * registered twice.
 */
export function tabOfField(
  field: keyof FormValues,
  ctx: { defaultRuntime: FormValues["defaultRuntime"] },
): ManagedAgentFormTab {
  const essential = ctx.defaultRuntime === "claude-code" ? CLAUDE_ESSENTIALS : OPENCODE_ESSENTIALS
  if (essential.includes(field)) return "basics"
  if (BASICS.includes(field)) return "basics"
  if (RUNTIME.includes(field)) return "runtime"
  if (SCENARIOS.includes(field)) return "scenarios"
  if (CLASSIFIER.includes(field)) return "classifier"
  // An unclaimed field would be invisible; put it where the submit button is.
  return "basics"
}

const CLAUDE_ESSENTIALS: readonly (keyof FormValues)[] = [
  "claudeBaseURL",
  "claudeApiKey",
  "claudeDefaultModel",
]

const OPENCODE_ESSENTIALS: readonly (keyof FormValues)[] = [
  "opencodeBaseURL",
  "opencodeApiKey",
  "opencodeDefaultModel",
]

/** Every field this module knows about, for the exhaustiveness test. */
export const MANAGED_AGENT_TAB_FIELDS: readonly (keyof FormValues)[] = [
  ...BASICS,
  ...RUNTIME,
  ...SCENARIOS,
  ...CLASSIFIER,
]

export function tabsWithErrors(
  errors: FieldErrors<FormValues>,
  ctx: { defaultRuntime: FormValues["defaultRuntime"] },
): Set<ManagedAgentFormTab> {
  const out = new Set<ManagedAgentFormTab>()
  // RHF nests, so a deep error like scenarios.1.name surfaces under the top-level
  // key "scenarios" — the level this mapping is keyed by.
  for (const key of Object.keys(errors)) {
    out.add(tabOfField(key as keyof FormValues, ctx))
  }
  return out
}

export function firstTabWithErrors(
  errors: FieldErrors<FormValues>,
  ctx: { defaultRuntime: FormValues["defaultRuntime"] },
): ManagedAgentFormTab | null {
  const bad = tabsWithErrors(errors, ctx)
  return MANAGED_AGENT_FORM_TABS.find((tab) => bad.has(tab)) ?? null
}

/**
 * The dotted path of the first error, depth-first, for scrolling to it. Returns
 * null when the errors carry no leaf — which happens for a root-level refinement.
 */
export function firstErrorPath(errors: unknown, prefix = ""): string | null {
  if (!errors || typeof errors !== "object") return null
  const record = errors as Record<string, unknown>
  if (typeof record.message === "string" || typeof record.type === "string") {
    return prefix || null
  }
  for (const [key, value] of Object.entries(record)) {
    if (key === "ref" || key === "root") continue
    const found = firstErrorPath(value, prefix ? `${prefix}.${key}` : key)
    if (found) return found
  }
  return null
}
