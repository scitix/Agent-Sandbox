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
 * One address, two spellings.
 *
 * A console URL and an `abx` invocation name the same thing segment for
 * segment, so each can be derived from the other by walking a path rather than
 * consulting a table. That property is what `open_page` and "turn this page
 * into a command" both rest on, and it only holds because the segments come
 * from the resource registry instead of being written out per surface.
 *
 *   console   /clusters/{cluster}/envs/{env}/scaling-groups/{group}
 *   CLI       abx envs {env} scaling-groups {group} --cluster {cluster}
 */

import { RESOURCES, resourceOf } from './resources'

/** Everything needed to name one thing, in either spelling. */
export interface Address {
  cluster?: string
  /** Resource plural, e.g. `envs`. */
  resource: string
  /** Instance id, when addressing one. */
  id?: string
  /** Sub-resource or view segment, when going a level deeper. */
  sub?: string
  /** Instance id within the sub-resource. */
  subId?: string
}

/** Pages that are not resources — they have routes but no CLI equivalent. */
export const NON_RESOURCE_PAGES = [
  'overview',
  'assistant',
  'admin',
  'general',
  'changelog',
  'approvals',
  'api-keys',
  'admin-api-keys',
  'images',
  'datasets',
  'vault',
] as const

/**
 * Validate an address against the registry.
 *
 * Returns the reason it is not addressable, or null when it is. Callers turn
 * this into an error that names the valid set — a rejection an agent can act on
 * in one retry, rather than one that sends it guessing.
 */
export function addressError(a: Address): string | null {
  const spec = resourceOf(a.resource)
  if (!spec) {
    const known = [...RESOURCE_SEGMENTS].join(', ')
    return `unknown resource "${a.resource}" (known: ${known})`
  }
  if (a.id && !spec.detail && !spec.subResources?.length) {
    return `"${spec.plural}" is a list with no per-item view`
  }
  if (a.sub) {
    if (!a.id) return `"${a.sub}" hangs off one ${spec.kind}, so an id is required`
    const isSub = spec.subResources?.some((s) => s.segment === a.sub)
    const isView = spec.views?.some((v) => v.segment === a.sub)
    if (!isSub && !isView) {
      const valid = [
        ...(spec.subResources ?? []).map((s) => s.segment),
        ...(spec.views ?? []).map((v) => v.segment),
      ]
      return valid.length
        ? `"${spec.plural}" has no "${a.sub}" (valid: ${valid.join(', ')})`
        : `"${spec.plural}" has no sub-resources`
    }
    // A view is a rendering, not a collection: `logs/{id}` addresses nothing.
    if (isView && a.subId) return `"${a.sub}" is a view, not a collection — it takes no id`
  }
  return null
}

export const RESOURCE_SEGMENTS: readonly string[] = RESOURCES.map((r) => r.plural)

/** The console path for an address, without locale prefix or origin. */
export function consolePath(a: Address): string {
  const parts: string[] = []
  if (a.cluster) parts.push('clusters', a.cluster)
  parts.push(a.resource)
  if (a.id) parts.push(a.id)
  if (a.sub) parts.push(a.sub)
  if (a.subId) parts.push(a.subId)
  return '/' + parts.map(encodeURIComponent).join('/')
}

/** The CLI invocation for an address. */
export function cliArgs(a: Address): string[] {
  const parts = [a.resource]
  if (a.id) parts.push(a.id)
  if (a.sub) parts.push(a.sub)
  if (a.subId) parts.push(a.subId)
  if (a.cluster) parts.push('--cluster', a.cluster)
  return parts
}

/**
 * Read a console path back into an address.
 *
 * The inverse of consolePath, and tested as such: a round trip that does not
 * return the same address means the two surfaces have stopped agreeing, which
 * is exactly the drift this module exists to prevent.
 */
export function parseConsolePath(path: string): Address | null {
  const seg = path.split('/').filter(Boolean).map(decodeURIComponent)
  let cluster: string | undefined
  if (seg[0] === 'clusters' && seg[1]) {
    cluster = seg[1]
    seg.splice(0, 2)
  }
  if (!seg.length) return null
  const [resource, id, sub, subId] = seg
  return { cluster, resource, id, sub, subId }
}
