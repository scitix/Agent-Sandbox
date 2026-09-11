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
  /** `api-key` addresses one cluster's API; `bearer` addresses the console BFF. */
  authScheme: 'api-key' | 'bearer'
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
        'pass --cluster, or set a default:\n  abx context set <name> --cluster <id>\n`abx clusters` lists them and needs none.',
      )
    }
    e = e.replaceAll('{cluster}', ctx.cluster)
  }
  return e.replace(/\/+$/, '') + '/v1'
}

export function headers(ctx: Context): Record<string, string> {
  const h: Record<string, string> = { Accept: 'application/json' }
  if (ctx.authScheme === 'bearer') h.Authorization = `Bearer ${ctx.apiKey}`
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
