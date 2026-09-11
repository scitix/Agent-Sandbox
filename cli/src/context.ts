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

import { homedir } from 'node:os'
import { join } from 'node:path'

/** Where requests go, and who they go as. */
export interface Context {
  endpoint: string
  apiKey: string
  cluster?: string
  /** `api-key` addresses one cluster's API; `bearer` addresses the console BFF. */
  authScheme: 'api-key' | 'bearer'
  format: 'table' | 'json' | 'csv'
  webBase?: string
}

/**
 * Settings written by something other than the caller.
 *
 * The plugin's hook writes this file, because a hook is the only place a
 * sensitive plugin option is readable at all — it never passes through argv,
 * stdin, or the agent's context. Everything here is therefore optional and
 * overridable: a flag beats an environment variable beats this file.
 */
export interface FileConfig {
  endpoint?: string
  apiKey?: string
  cluster?: string
  authScheme?: 'api-key' | 'bearer'
  webBase?: string
}

export function configPath(): string {
  return join(process.env.XDG_CONFIG_HOME || join(homedir(), '.config'), 'abx', 'config.json')
}

export async function readConfig(): Promise<FileConfig> {
  try {
    return JSON.parse(await Bun.file(configPath()).text()) as FileConfig
  } catch {
    // A missing or unreadable config is the normal case for anyone passing
    // flags, so it is not worth a word of output.
    return {}
  }
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

export function baseUrl(ctx: Context): string {
  let e = ctx.endpoint
  if (routesByPath(ctx)) {
    if (!ctx.cluster) {
      throw new CliError('this endpoint routes by cluster and needs one: pass --cluster')
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
