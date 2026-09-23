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

import { createHash } from 'node:crypto'
import { chmodSync, renameSync, unlinkSync, writeFileSync } from 'node:fs'
import { basename, dirname, join } from 'node:path'
import { CliError } from './context'

/**
 * `abx upgrade` — the CLI replacing itself from the release bucket.
 *
 * The word is `upgrade` and not `update` because `update` is already a verb
 * this CLI has: it changes one existing object from a file, and a second
 * meaning for the same word would be a coin flip for whoever typed it. Nothing
 * about a resource is involved here — this replaces the binary on this machine.
 *
 * The release bucket is the same one `install.sh` and the plugin shim read, and
 * the checksums beside each binary are the ones they verify. `AGBX_CLI_BASE`
 * moves all three at once, which is what makes a mirror a one-variable change
 * rather than a rebuild.
 */

/** Where releases live, unless AGBX_CLI_BASE says otherwise. */
export const DEFAULT_BASE = 'https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest'

/** A download that is slow is a download that is going to be abandoned anyway. */
const TIMEOUT_MS = 120_000

export interface UpgradeDeps {
  /** The running build's version, as `abx version` prints it. `dev` for source. */
  version: string
  /** The file to replace. */
  execPath: string
  /** Bucket prefix, without a trailing slash. */
  base: string
  /** `process.platform` / `process.arch`, passed in so a test can be another machine. */
  os: string
  arch: string
  fetch: (url: string, init?: { signal?: AbortSignal }) => Promise<Response>
}

/** The release asset for a platform, or undefined where no build is published. */
export function artifactName(os: string, arch: string): string | undefined {
  if (os !== 'linux' && os !== 'darwin') return undefined
  if (arch !== 'x64' && arch !== 'arm64') return undefined
  return `abx-${os}-${arch}`
}

/**
 * Fetch, or say what could not be reached.
 *
 * A wrong bucket and a dead network are the same failure to the caller, and
 * both are fixed by knowing the address that was tried — so the address is in
 * the message rather than in a stack trace.
 */
async function get(deps: UpgradeDeps, url: string, what: string): Promise<Response> {
  try {
    return await deps.fetch(url, { signal: AbortSignal.timeout(TIMEOUT_MS) })
  } catch (err) {
    throw new CliError(
      `cannot reach the release bucket for ${what}`,
      `${url}\n${err instanceof Error ? err.message : String(err)}\n` +
        `set AGBX_CLI_BASE to the release prefix this deployment mirrors, ` +
        `or install by hand see ${deps.base}/install.sh`,
    )
  }
}

/**
 * Replace the running binary with the latest release.
 *
 * Returns the line to print rather than printing it, so the whole of this can
 * be exercised against a bucket that is a function in a test.
 *
 * Nothing is written until the download has been checked: a release that fails
 * to arrive, or arrives wrong, leaves the binary that is running right there.
 * The new file is written beside the old one and renamed over it, which is what
 * makes the swap atomic — a half-written binary at the path is a command that
 * never runs again, and the one moment to avoid that is the one moment nobody
 * is looking.
 */
export async function upgrade(
  deps: UpgradeDeps,
  opts: { check?: boolean; force?: boolean } = {},
): Promise<string> {
  if (deps.version === 'dev') {
    throw new CliError(
      'this is a source build, and it does not replace itself',
      'abx upgrade overwrites the binary it is running from — here, the runtime ' +
        `that started the source file.\ninstall a released binary instead:\n  ` +
        `curl -fsSL ${deps.base}/install.sh | sh`,
    )
  }
  const name = artifactName(deps.os, deps.arch)
  if (!name) {
    throw new CliError(
      `no abx release for ${deps.os}/${deps.arch}`,
      'releases cover linux and darwin, on x64 and arm64',
    )
  }

  const versionRes = await get(deps, `${deps.base}/VERSION`, 'the latest version')
  if (!versionRes.ok) {
    throw new CliError(
      `the release bucket has no version to compare against (${versionRes.status})`,
      `${deps.base}/VERSION`,
    )
  }
  const latest = (await versionRes.text()).trim()
  if (!latest) {
    throw new CliError(
      'the release bucket answered with an empty version',
      `${deps.base}/VERSION`,
    )
  }

  if (latest === deps.version && !opts.force) {
    return `abx ${deps.version} is the latest`
  }
  if (opts.check) {
    return `abx ${deps.version} — ${latest} is available`
  }

  const binRes = await get(deps, `${deps.base}/${name}`, name)
  if (!binRes.ok) {
    throw new CliError(
      `the release ${latest} carries no ${name} (${binRes.status})`,
      `${deps.base}/${name}`,
    )
  }
  const bytes = new Uint8Array(await binRes.arrayBuffer())

  // The checksum is required, not best-effort: `install.sh` may run unverified
  // when the bucket forgets one, but a binary that overwrites itself cannot.
  const shaRes = await get(deps, `${deps.base}/${name}.sha256`, 'its checksum')
  const want = shaRes.ok ? (await shaRes.text()).trim().split(/\s+/)[0] : ''
  if (!want) {
    throw new CliError(
      `the release ${latest} publishes no checksum for ${name}`,
      `without one there is no way to tell a truncated download from a good one.\n` +
        `report ${deps.base}/${name}.sha256, or install by hand: ` +
        `curl -fsSL ${deps.base}/install.sh | sh`,
    )
  }
  const got = createHash('sha256').update(bytes).digest('hex')
  if (got !== want) {
    throw new CliError(
      `the downloaded ${name} does not match its checksum`,
      `expected ${want}\ngot      ${got}\n` +
        'nothing was written: the abx you are running is untouched.',
    )
  }

  const dir = dirname(deps.execPath)
  const tmp = join(dir, `.${basename(deps.execPath)}.${process.pid}.new`)
  try {
    writeFileSync(tmp, bytes, { mode: 0o755 })
    chmodSync(tmp, 0o755)
    renameSync(tmp, deps.execPath)
  } catch (err) {
    try {
      unlinkSync(tmp)
    } catch {
      // Nothing to clean up, and the error below is the one worth reporting.
    }
    throw new CliError(
      `cannot replace ${deps.execPath}`,
      `${err instanceof Error ? err.message : String(err)}\n` +
        `install into a directory you own, or install the release the way you ` +
        `installed this one:\n  curl -fsSL ${deps.base}/install.sh | sh`,
    )
  }

  return `upgraded abx ${deps.version} → ${latest} — it takes effect on the next run`
}
