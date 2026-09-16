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

import type { AgentSandboxEnv, AgentSandboxTemplateSummary } from "@/lib/api/client"

/**
 * Which sizing form a Pool gets. Three layouts, not two:
 *
 *  - `billed` — the env's template spends quota, so the Pool names a quota and
 *    an instance type. No resource-mode toggle: there is nothing to choose.
 *  - `free-form` — the env's template is not billed (or this deployment has no
 *    instance-type catalog), so CPU/memory are entered directly.
 *  - `caller-choice` — the deployment states no sizing rule at all, so the
 *    caller still picks between the two, the way every deployment did before
 *    the rule existed.
 */
export type PoolSizingLayout = "billed" | "free-form" | "caller-choice"

/** What the server says about a template, or about an env bound to one. */
export type PoolSizing = AgentSandboxEnv["poolSizing"]

export interface PoolSizingInput {
  /** `env.poolSizing`, or a template's while choosing one. Undefined = unstamped. */
  poolSizing?: PoolSizing
  /** True when a quota backend is wired into this deployment. */
  quotaEnabled: boolean
  /** True when an InstanceType catalog is wired into this deployment. */
  instanceTypeEnabled: boolean
  /**
   * Edit mode only: the instance type the Pool being edited already has.
   * `undefined` means "this is not an edit".
   */
  existingInstanceType?: string
}

/**
 * Decide the layout. This is the only place the dashboard reads the rule — the
 * form renders the answer rather than deriving its own, so the console cannot
 * drift from what the API will accept.
 *
 * Edit mode ignores the env and mirrors the Pool instead: the shape is fixed at
 * create, so a Pool that predates its template gaining a billing annotation has
 * to keep opening as what it is, not as what the env now says it should be.
 *
 * An unstamped env (`poolSizing` absent — an older worker, or an env whose
 * reconciler has not run yet) falls back to `caller-choice`, the behaviour that
 * predates the field. Never to `billed`: an env nobody can add a Pool to is a
 * worse failure than one that asks for a quota a moment later.
 */
export function resolvePoolSizingLayout(input: PoolSizingInput): PoolSizingLayout {
  const { poolSizing, instanceTypeEnabled, existingInstanceType } = input

  if (existingInstanceType !== undefined) {
    return existingInstanceType ? "billed" : "free-form"
  }

  switch (poolSizing) {
    case "billed":
      return "billed"
    case "free-form":
      return "free-form"
    default:
      // "either", or not stamped. The catalog is what makes the choice
      // meaningful; without one, free-form is the only shape left to offer.
      return instanceTypeEnabled ? "caller-choice" : "free-form"
  }
}

/**
 * Whether the resource-mode toggle is part of this layout.
 *
 * Only when nothing else decides. A billed or free-form env has already made
 * the choice, and asking the user to make it again is exactly what made two
 * kinds of template look like one form.
 */
export function showsResourceModeToggle(layout: PoolSizingLayout): boolean {
  return layout === "caller-choice"
}

/** Whether the quota picker belongs on the form. */
export function showsQuotaPicker(layout: PoolSizingLayout, quotaEnabled: boolean): boolean {
  if (layout === "billed") return true
  return layout === "caller-choice" && quotaEnabled
}

/** Whether the quota is required rather than optional. */
export function requiresQuota(layout: PoolSizingLayout): boolean {
  return layout === "billed"
}

/**
 * The resource mode a layout implies, for the form's `resourceMode` field —
 * derived in the two managed layouts, still the user's to set in the third.
 */
export function impliedResourceMode(
  layout: PoolSizingLayout,
): "instanceType" | "manual" | undefined {
  if (layout === "billed") return "instanceType"
  if (layout === "free-form") return "manual"
  return undefined
}

/**
 * Whether a template in the picker is one whose Pools are billed.
 *
 * The env form asks this so the choice is visible where it is made: which
 * template you bind decides what the pool form will ask for later, and finding
 * that out only after creating the env is how the two kinds got confused.
 */
export function templateIsBilled(
  template: Pick<AgentSandboxTemplateSummary, "poolSizing">,
): boolean {
  return template.poolSizing === "billed"
}
