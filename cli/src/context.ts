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

/** Where requests go, and who they go as. */
export interface Context {
  /** The named deployment this run resolved to, when there is one. */
  contextName?: string
  endpoint: string
  apiKey: string
  cluster?: string
  /**
   * How the key travels. Omitted is the normal case: the endpoint's shape
   * decides it, so a caller never has to know which header the far end reads.
   * Set only to override the derived answer — the escape hatch for an address
   * that does not look like either of the two shapes.
   */
  authScheme?: 'api-key' | 'bearer'
  format: 'table' | 'json' | 'csv'
  webBase?: string
}

/**
 * Does the endpoint carry the cluster in its path?
 *
 * A `{cluster}` placeholder means the address itself routes, so one endpoint
 * reaches every cluster and `--cluster` is a substitution rather than a claim
 * the endpoint cannot honour. Without it an endpoint answers for its own
 * cluster and nothing else, which is why `--cluster` has to be refused there
 * rather than silently ignored: returning the local cluster's rows under a
 * different cluster's name is worse than an error.
 */
export const routesByPath = (ctx: Context): boolean => ctx.endpoint.includes('{cluster}')

/**
 * Is this endpoint the console's BFF rather than a cluster API of its own?
 *
 * The two answer for the same platform but read the credential from different
 * places: a cluster API takes `AGENTBOX-API-KEY`, while the console proxy only
 * inspects `Authorization: Bearer`. The shape that tells them apart is the
 * console's own mount point, `/api/clusters` — a caller who copied the address
 * out of the console has no reason to know which header the far end wants, so
 * the URL is read instead of a flag being demanded.
 */
export const isConsoleBff = (ctx: Context): boolean => /\/api\/clusters(\/|$)/.test(ctx.endpoint)

/**
 * The console base a BFF endpoint hangs off, when there is one.
 *
 * `https://example.com/agentbox/api/clusters/{cluster}` is the console's proxy
 * mount; the pages it serves sit at `https://example.com/agentbox` one level
 * up. Deriving it means the console links in a hint are right without anyone
 * being asked to retype the same host twice.
 */
export function derivedWebBase(ctx: Context): string | undefined {
  if (!isConsoleBff(ctx)) return undefined
  const at = ctx.endpoint.search(/\/api\/clusters(\/|$)/)
  if (at <= 0) return undefined
  return ctx.endpoint.slice(0, at).replace(/\/+$/, '') || undefined
}

/**
 * Where to ask WHICH clusters exist, when the endpoint routes by path.
 *
 * The placeholder segment is what makes one address reach every cluster, so
 * the level above it is the question "which are there" — and that question
 * cannot be answered by first naming one. Returns null for a single-cluster
 * endpoint, which answers it from its own API like anything else.
 */
export function clusterListUrl(ctx: Context): string | null {
  if (!routesByPath(ctx)) return null
  const stripped = ctx.endpoint.replace(/\/*\{cluster\}\/*$/, '')
  return stripped === ctx.endpoint ? null : stripped
}

export function baseUrl(ctx: Context): string {
  let e = ctx.endpoint
  if (routesByPath(ctx)) {
    if (!ctx.cluster) {
      throw new CliError(
        'this endpoint reaches several clusters and this command needs one',
        'pass --cluster <id>; `abx clusters` lists them and needs none.',
      )
    }
    e = e.replaceAll('{cluster}', ctx.cluster)
  }
  return e.replace(/\/+$/, '') + '/v1'
}

export function headers(ctx: Context): Record<string, string> {
  const h: Record<string, string> = { Accept: 'application/json' }
  const scheme = ctx.authScheme ?? (isConsoleBff(ctx) ? 'bearer' : 'api-key')
  if (scheme === 'bearer') h.Authorization = `Bearer ${ctx.apiKey}`
  else h['AGENTBOX-API-KEY'] = ctx.apiKey
  return h
}

/**
 * An error that already reads as advice.
 *
 * `hint` is the part that matters: a message naming the valid set and the next
 * command is one an agent recovers from in a single retry, where a bare refusal
 * costs a round trip per guess. Domain knowledge belongs here first and in
 * --help last, because the two are not consumed at the same rate.
 */
export class CliError extends Error {
  constructor(
    message: string,
    readonly hint?: string,
  ) {
    super(message)
  }
}
