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

// The toolset ships wording that mentions nothing but the sandbox itself, and a
// deployment whose image mounts something extra splices one clause in through
// SBX_WORKSPACE_NOTE. That clause is prompt contract: a deployment migrating off
// a hand-edited description has to be able to reproduce its previous wording
// byte-for-byte, or the migration silently changes the prompt — which does not
// error, it just makes the agent slightly worse at finding things.
//
// WORKSPACE_NOTE is read once at module load (it is a deployment constant, not a
// per-call argument), so each case needs a fresh module registry.
import { afterEach, describe, expect, it, vi } from 'vitest'

async function bashDescription(note?: string): Promise<string> {
  vi.resetModules()
  if (note === undefined) vi.stubEnv('SBX_WORKSPACE_NOTE', '')
  else vi.stubEnv('SBX_WORKSPACE_NOTE', note)
  const { sandboxToolset } = await import('./tools.ts')
  const bash = sandboxToolset().find(t => t.name === 'bash')
  if (!bash) throw new Error('bash tool missing from the toolset')
  return bash.description
}

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
})

describe('workspace note', () => {
  it('says nothing extra when unset', async () => {
    expect(await bashDescription()).toBe(
      'Run a shell command. Executes inside the remote sandbox bound to this ' +
        'session (NOT on the local machine). The default working directory is ' +
        'your session working directory (the cwd shown to you).'
    )
  })

  // The exact string the navix deployment carried before this package existed.
  // If this assertion ever has to change, that deployment's prompt changed with
  // it, so the change belongs in a release note rather than in a passing test.
  it('reproduces a pre-existing deployment wording exactly', async () => {
    expect(
      await bashDescription('the Volcano source is read-only at /opt/volcano')
    ).toBe(
      'Run a shell command. Executes inside the remote sandbox bound to this ' +
        'session (NOT on the local machine). The default working directory is ' +
        'your session working directory (the cwd shown to you); the Volcano ' +
        'source is read-only at /opt/volcano.'
    )
  })

  // Operators write these by hand in a Helm values file or a CRD field, so the
  // separator and the terminator are supplied for them rather than demanded of
  // them: every one of these spellings has to land on the same output.
  it('normalises a note that arrives with its own punctuation', async () => {
    const canonical = await bashDescription('mounted at /data')
    for (const written of [
      '; mounted at /data',
      ', mounted at /data',
      'mounted at /data.',
      '  mounted at /data  ',
      '; mounted at /data.',
    ]) {
      expect(await bashDescription(written)).toBe(canonical)
    }
  })

  it('is not spliced into the grep path hint', async () => {
    vi.resetModules()
    vi.stubEnv('SBX_WORKSPACE_NOTE', 'mounted at /data')
    const { sandboxToolset } = await import('./tools.ts')
    const grep = sandboxToolset().find(t => t.name === 'grep')
    expect(grep?.params['path']?.description).toBe(
      'Directory to search (default: your working directory)'
    )
  })
})
