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

import { RESOURCES, childrenOf, resourceOf, rootResources } from './resources'
import type { ResourceSpec } from './types'

/** Everything needed to name one thing, in either spelling. */
export interface Address {
  cluster?: string
  /** Resource plural, e.g. `envs`. */
  resource: string
  /** Instance id, when addressing one. */
  id?: string
  /** Child resource segment, when going a level deeper. */
  sub?: string
  /** Instance id within the child resource. */
  subId?: string
  /**
   * A view on whatever the rest of the address names — metrics, logs.
   *
   * Always last, and never spelled as a `sub`, so there is exactly one way to
   * write "the metrics of this pool". Both surfaces then agree that
   * `/envs/e/pools/p/metrics` and `abx envs e pools p metrics` are the same
   * page, which they did not while a view was just another sub-resource.
   */
  view?: string
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
    return `unknown resource "${a.resource}" (known: ${ROOT_SEGMENTS.join(', ')})`
  }
  if (spec.parent) {
    // Naming the whole form rather than just refusing: the parent id is the
    // part the caller is missing, and it is not guessable from the refusal.
    return `"${spec.plural}" is addressed under its ${parentKind(spec)} — \`${spec.parent} <${parentKind(spec)}> ${spec.plural}\``
  }
  if (a.id && !spec.detail && !childrenOf(spec.plural).length && !addressableItem(spec)) {
    return `"${spec.plural}" is a list with no per-item view`
  }
  if (!a.id && !spec.api.list) return `"${spec.plural}" cannot be listed`

  let target = spec
  if (a.sub) {
    if (!a.id) return `"${a.sub}" hangs off one ${spec.kind}, so an id is required`
    const child = childrenOf(spec.plural).find((c) => c.plural === a.sub)
    if (!child) return noSuchSegment(spec, a.sub)
    if (a.subId && !child.detail) return `"${child.plural}" is a list with no per-item view`
    target = child
  }
  if (a.view) {
    const depthId = a.sub ? a.subId : a.id
    if (!depthId) return `"${a.view}" is a view of one ${target.kind}, so an id is required`
    if (!target.views?.some((v) => v.segment === a.view)) return noSuchSegment(target, a.view)
  }
  return null
}

function noSuchSegment(spec: ResourceSpec, segment: string): string {
  const valid = [
    ...childrenOf(spec.plural).map((c) => c.plural),
    ...(spec.views ?? []).map((v) => v.segment),
  ]
  return valid.length
    ? `"${spec.plural}" has no "${segment}" (valid: ${valid.join(', ')})`
    : `"${spec.plural}" has no sub-resources`
}

/**
 * Whether one item of this resource can be named at all.
 *
 * An API key has no GET by name but does have a DELETE by name, so refusing
 * the id would make the key undeletable. What the id is FOR is a separate
 * question, answered where the command runs.
 */
function addressableItem(spec: ResourceSpec): boolean {
  return Boolean(spec.api.item) || Boolean(spec.actions?.length)
}

function parentKind(spec: ResourceSpec): string {
  return resourceOf(spec.parent ?? '')?.kind ?? 'parent'
}

export const RESOURCE_SEGMENTS: readonly string[] = RESOURCES.map((r) => r.plural)
export const ROOT_SEGMENTS: readonly string[] = rootResources().map((r) => r.plural)

/** The console path for an address, without locale prefix or origin. */
export function consolePath(a: Address): string {
  const parts: string[] = []
  if (a.cluster && resourceOf(a.resource)?.clusterScoped !== false) {
    parts.push('clusters', a.cluster)
  }
  parts.push(a.resource)
  if (a.id) parts.push(a.id)
  if (a.sub) parts.push(a.sub)
  if (a.subId) parts.push(a.subId)
  if (a.view) parts.push(a.view)
  return '/' + parts.map(encodeURIComponent).join('/')
}

/** Whether the console has a page for this address at all. */
export function hasConsolePage(a: Address): boolean {
  for (const segment of [a.resource, a.sub]) {
    if (segment && resourceOf(segment)?.consolePage === false) return false
  }
  return true
}

/** The CLI invocation for an address. */
export function cliArgs(a: Address): string[] {
  const parts = [a.resource]
  if (a.id) parts.push(a.id)
  if (a.sub) parts.push(a.sub)
  if (a.subId) parts.push(a.subId)
  if (a.view) parts.push(a.view)
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
  // `/clusters` alone is the cluster list, not a prefix missing its id.
  if (seg[0] === 'clusters' && seg[1] && seg.length > 2) {
    cluster = seg[1]
    seg.splice(0, 2)
  }
  if (!seg.length) return null
  const parsed = parsePositional(seg)
  return parsed ? { cluster, ...parsed } : null
}

/**
 * Classify positional segments into an address.
 *
 * Shared by both directions on purpose: a console URL and a CLI invocation are
 * the same token sequence, so they are read by the same function. Whether the
 * third segment is a child collection or a view is a question only the registry
 * can answer, which is why neither surface tries to answer it alone.
 *
 * Returns null when there are more segments than any address can hold — the
 * caller reports that rather than silently ignoring the tail, which is how
 * `envs e pools p metrics` used to come back as the pool with the view quietly
 * dropped.
 */
export function parsePositional(seg: readonly string[]): Omit<Address, 'cluster'> | null {
  const [resource, id, third, fourth, fifth, ...rest] = seg
  if (rest.length) return null
  const spec = resourceOf(resource)
  const isChild = Boolean(spec && third && childrenOf(spec.plural).some((c) => c.plural === third))
  if (!third) return { resource, id }
  if (!isChild) {
    // A view: nothing may follow it.
    return fourth ? null : { resource, id, view: third }
  }
  return { resource, id, sub: third, subId: fourth, view: fifth }
}
