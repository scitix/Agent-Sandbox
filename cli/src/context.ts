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
  /**
   * The console's base URL — the address a person types in a browser, such as
   * `https://console.example/agentbox`.
   *
   * One address reaches every cluster: the API for each hangs off it at
   * `/api/clusters/{cluster}/v1`, and the cluster list at `/api/clusters`. The
   * cluster is picked per command with `--cluster`; nothing about it is baked
   * into this address.
   */
  endpoint: string
  /**
   * A single cluster's own API base, for reaching it without a console.
   *
   * Setting it switches the CLI out of console mode: the endpoint above is not
   * used, requests go straight to `<clusterApi>/v1`, and `--cluster` is refused
   * rather than silently ignored. This is how something running inside a
   * cluster's own network reaches that cluster's API.
   */
  clusterApi?: string
  apiKey: string
  cluster?: string
  /**
   * How the key travels. Omitted is the normal case: console mode sends a
   * Bearer token and direct mode sends `AGENTBOX-API-KEY`, decided by which of
   * the two addresses is in use. Set only to override — the escape hatch for a
   * deployment that answers to something else.
   */
  authScheme?: 'api-key' | 'bearer'
  format: 'table' | 'json' | 'csv'
}

const stripSlash = (url: string): string => url.replace(/\/+$/, '')

/**
 * Is this context going through the console, or straight to one cluster's API?
 *
 * The two reach the same platform by different addresses and read the
 * credential from different places, so this single question answers both "which
 * base do I build URLs from" and "which header do I send". Console mode is the
 * default because it is what a person copies out of the browser; direct mode is
 * the exception, opted into by naming a cluster API.
 */
export const viaConsole = (ctx: Context): boolean => !ctx.clusterApi

/**
 * A console base from whatever an older config called an endpoint.
 *
 * Endpoints used to be stored with the console's mount and routing placeholder
 * still attached — `.../api/clusters/{cluster}` — because that was the string
 * the CLI substituted into. The base is that address with the mount removed, so
 * an existing config keeps working without anyone rewriting it by hand.
 *
 * Only the console's own `/api/clusters` suffix is stripped. A cluster API base
 * like `https://cluster.example/api` is left alone, which is why that one
 * has to be named with `--cluster-api` rather than guessed from an `endpoint`.
 */
export function consoleBaseOf(url: string): string {
  return stripSlash(
    url
      .replace(/\/api\/clusters\/\{cluster\}$/, '')
      .replace(/\/api\/clusters$/, ''),
  )
}

/**
 * Where to ask which clusters exist, in console mode.
 *
 * The cluster list cannot be answered by first naming a cluster, and the
 * console publishes it one level above the per-cluster mount — so it is asked
 * there, and `abx clusters` therefore needs no `--cluster` at all. Returns null
 * in direct mode, where the endpoint answers for one cluster and its own
 * `/v1/clusters` is the list.
 */
export function clusterListUrl(ctx: Context): string | null {
  return viaConsole(ctx) ? `${stripSlash(ctx.endpoint)}/api/clusters` : null
}

/** The console base, when there is one — the address console links hang off. */
export function consoleBase(ctx: Context): string | undefined {
  return viaConsole(ctx) ? stripSlash(ctx.endpoint) : undefined
}

export function baseUrl(ctx: Context): string {
  if (!viaConsole(ctx)) {
    return `${stripSlash(ctx.clusterApi as string)}/v1`
  }
  if (!ctx.cluster) {
    throw new CliError(
      'the console reaches several clusters and this command needs one',
      'pass --cluster <id>; `abx clusters` lists them and needs none.',
    )
  }
  return `${stripSlash(ctx.endpoint)}/api/clusters/${encodeURIComponent(ctx.cluster)}/v1`
}

export function headers(ctx: Context): Record<string, string> {
  const h: Record<string, string> = { Accept: 'application/json' }
  const scheme = ctx.authScheme ?? (viaConsole(ctx) ? 'bearer' : 'api-key')
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
  /**
   * The HTTP status, when this came from a response.
   *
   * A caller further out sometimes knows something the layer that raised it
   * does not: `update` sees a 404 and knows the answer is "use `create`",
   * while the request layer only knows the address did not resolve. Matching
   * on the message would make that knowledge depend on the server's wording.
   */
  status?: number

  constructor(
    message: string,
    readonly hint?: string,
  ) {
    super(message)
  }
}
