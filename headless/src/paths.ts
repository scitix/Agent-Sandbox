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

/**
 * Address → API path.
 *
 * The route segments a person types and the paths the API publishes are not the
 * same strings, and trying to make them the same would mean renaming published
 * operations every time a label reads badly. This is the one place that
 * translates, so neither side has to know about the other's spelling.
 */

import { RESOURCES, childrenOf, resourceOf } from './resources'
import type { Address } from './navigation'
import type { ActionSpec, ResourceSpec, Verb, ViewSpec } from './types'

export interface Resolved {
  /** The registry entry the address ultimately names. */
  spec: ResourceSpec
  /** The API path, placeholders filled. */
  path: string
  /** Whether the address names a collection rather than one item. */
  collection: boolean
  /** The response field holding the list, when the resource declares one. */
  listField?: string
  /**
   * The view the address bottomed out at, when it named one.
   *
   * Callers need it because a view's response is not a row set — the renderer
   * that fits it is a property of the view, not of the resource it hangs off.
   */
  view?: ViewSpec
}

/**
 * Fill an API path template positionally.
 *
 * Placeholders are consumed left to right from the ids the address supplies,
 * so a template's parameter NAMES never have to match anything on the CLI side.
 * That is what lets `/envs/{name}/sandboxpools/{poolName}` stay spelled exactly
 * as the spec spells it while the CLI knows only "env id, then pool id".
 */
function fill(template: string, ids: string[]): string {
  let i = 0
  return template.replace(/\{[^}]+\}/g, () => encodeURIComponent(ids[i++] ?? ''))
}

/** Resolve an address to the API path that serves it, or null if it has none. */
export function resolveApi(a: Address): Resolved | null {
  const root = resourceOf(a.resource)
  if (!root) return null

  // Walk down to whatever the address bottoms out at, then take the view (if
  // any) of that. Views are resolved last because they belong to the depth the
  // rest of the address reached, not to the root.
  let spec = root
  const ids: string[] = []
  if (a.id) ids.push(a.id)
  if (a.sub) {
    const child = childrenOf(root.plural).find((c) => c.plural === a.sub)
    if (!child || !a.id) return null
    spec = child
    if (a.subId) ids.push(a.subId)
  }

  if (a.view) {
    const view = spec.views?.find((v) => v.segment === a.view)
    // A view with no native path is real but served elsewhere — pool metrics
    // come from Prometheus, not from this API — so this is "no path", not
    // "no such view". addressError is what rejects a segment that is wrong.
    return view?.api ? { spec, path: fill(view.api, ids), collection: false, view } : null
  }

  const wantsItem = a.sub ? Boolean(a.subId) : Boolean(a.id)
  if (wantsItem) {
    return spec.api.item ? { spec, path: fill(spec.api.item, ids), collection: false } : null
  }
  return spec.api.list
    ? { spec, path: fill(spec.api.list, ids), collection: true, listField: spec.api.listField }
    : null
}

/** Resolve a named action on an address, or null when the resource has none. */
export function resolveAction(a: Address, name: string): { spec: ResourceSpec; action: ActionSpec; path: string } | null {
  const root = resourceOf(a.resource)
  if (!root) return null
  let spec = root
  const ids: string[] = []
  if (a.id) ids.push(a.id)
  if (a.sub) {
    const child = childrenOf(root.plural).find((c) => c.plural === a.sub)
    if (!child) return null
    spec = child
    if (a.subId) ids.push(a.subId)
  }
  const action = spec.actions?.find((x) => x.name === name)
  return action ? { spec, action, path: fill(action.path, ids) } : null
}

/** Every action name any resource declares — the CLI's verb vocabulary. */
export function actionNames(): readonly string[] {
  const names = new Set<string>()
  for (const r of RESOURCES) for (const a of r.actions ?? []) names.add(a.name)
  return [...names]
}

/** Whether a resolved address supports a given write verb. */
export function supports(spec: ResourceSpec, verb: Verb): boolean {
  return (spec.api.verbs ?? []).includes(verb)
}
