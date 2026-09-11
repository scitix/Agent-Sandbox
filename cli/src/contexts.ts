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
 * Named deployments, the way kubectl names clusters.
 *
 * One `abx` reaches more than one platform — the same binary, different
 * addresses and different credentials — and re-exporting two environment
 * variables to switch between them is the kind of thing people get wrong
 * silently: the command succeeds, against the wrong platform.
 *
 * A context is one deployment's address plus the credential for it. `--cluster`
 * still selects WITHIN a deployment, because an endpoint carrying `{cluster}`
 * reaches every cluster that platform has. So the two axes stay separate:
 * context is which platform, cluster is which of its clusters.
 *
 * Nothing here ships with a context in it. The addresses belong to whoever
 * deployed the platform, and each deployment's own console prints the
 * `abx context set` line that adds it — so this file is written by the person
 * who owns the addresses, never by the CLI's authors.
 */

import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import { mkdir } from 'node:fs/promises'

/** One deployment: where it is, and who you are on it. */
export interface ContextEntry {
  endpoint?: string
  apiKey?: string
  cluster?: string
  authScheme?: 'api-key' | 'bearer'
  webBase?: string
}

/**
 * The config file.
 *
 * Both shapes are accepted. The flat one is what the plugin's credential hook
 * writes and what a single-platform install needs, and it keeps working
 * untouched — a file that gains a `contexts` key only when someone asks for a
 * second platform. Reading is lenient; writing produces whichever shape the
 * file is already in.
 */
export interface FileConfig extends ContextEntry {
  currentContext?: string
  contexts?: Record<string, ContextEntry>
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

export async function writeConfig(cfg: FileConfig): Promise<void> {
  const path = configPath()
  await mkdir(dirname(path), { recursive: true })
  await Bun.write(path, JSON.stringify(cfg, null, 2) + '\n')
  // The file holds credentials. 0600 rather than whatever the umask happens to
  // be, on every write, because a config that started private can be recreated
  // world-readable by a later one.
  await Bun.$`chmod 600 ${path}`.quiet().nothrow()
}

/** Every named context, in the order they should be listed. */
export function contextNames(cfg: FileConfig): string[] {
  return Object.keys(cfg.contexts ?? {}).sort()
}

/**
 * The settings a run should start from.
 *
 * `want` is an explicit `--context`; otherwise the file's current context;
 * otherwise the flat top-level fields, which is the single-deployment case.
 * Returns the entry and the name it came from, so output can say which
 * deployment answered — a command that silently used the other platform is the
 * failure this whole mechanism exists to prevent.
 */
export function selectContext(
  cfg: FileConfig,
  want?: string,
): { name?: string; entry: ContextEntry } {
  const named = cfg.contexts ?? {}
  if (want) {
    const entry = named[want]
    if (!entry) {
      const known = contextNames(cfg)
      throw new UnknownContextError(want, known)
    }
    return { name: want, entry }
  }
  if (cfg.currentContext && named[cfg.currentContext]) {
    return { name: cfg.currentContext, entry: named[cfg.currentContext] }
  }
  const names = contextNames(cfg)
  if (names.length === 1) return { name: names[0], entry: named[names[0]] }
  if (names.length > 1) {
    throw new AmbiguousContextError(names)
  }
  return { entry: cfg }
}

export class UnknownContextError extends Error {
  constructor(
    readonly want: string,
    readonly known: string[],
  ) {
    super(`no context named "${want}"`)
  }
}

export class AmbiguousContextError extends Error {
  constructor(readonly known: string[]) {
    super('several contexts are configured and none is current')
  }
}
