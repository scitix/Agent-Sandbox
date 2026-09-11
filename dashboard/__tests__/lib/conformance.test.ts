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
 * Console ⇄ CLI conformance.
 *
 * The console and `abx` render one platform. When a capability lands on one and
 * not the other, nothing today notices — the console simply grows a button the
 * CLI has never heard of, and an agent driving the CLI reports the platform
 * cannot do something it plainly can. These assertions make that a build
 * failure.
 *
 * Sources of truth, in order of authority:
 *   1. pkg/openapi/native/openapi.yaml — every operation that exists at all
 *   2. lib/queries/**                  — what the console reaches
 *   3. CLI capability manifest         — what `abx` reaches
 *
 * (3) is a checked-in list until the headless package exists; at that point it
 * becomes an import and this comment should be deleted along with the file.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { parse as parseYaml } from 'yaml'
import { INTENTIONALLY_ABSENT, op, type OperationKey } from '@/lib/conformance/surface'
import { CLI_OPERATIONS } from '@/lib/conformance/cli-capabilities'

const REPO = join(__dirname, '..', '..', '..')
const SPEC = join(REPO, 'pkg', 'openapi', 'native', 'openapi.yaml')
const LIB = join(__dirname, '..', '..', 'lib')

const HTTP_METHODS = ['get', 'post', 'put', 'patch', 'delete'] as const

function specOperations(): OperationKey[] {
  const doc = parseYaml(readFileSync(SPEC, 'utf8')) as {
    paths: Record<string, Record<string, unknown>>
  }
  const out: OperationKey[] = []
  for (const [path, item] of Object.entries(doc.paths)) {
    for (const method of HTTP_METHODS) {
      if (item[method]) out.push(op(method, path))
    }
  }
  return out.sort()
}

/**
 * What the console reaches, read off the openapi-fetch call sites.
 *
 * Scanning source rather than maintaining a list is the point: a query added
 * without touching this test still shows up here, which is what makes the
 * "console grew a capability" direction detectable at all.
 */
function tsFilesUnder(dir: string): string[] {
  const out: string[] = []
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, e.name)
    if (e.isDirectory()) out.push(...tsFilesUnder(full))
    else if (e.name.endsWith('.ts') || e.name.endsWith('.tsx')) out.push(full)
  }
  return out
}

function consoleOperations(): Set<OperationKey> {
  const found = new Set<OperationKey>()
  // Two call shapes, because the console reaches the API two ways: the typed
  // openapi-fetch client for per-cluster resources, and raw fetch through the
  // hub BFF for the globally-managed ones (templates, global api keys). A
  // scanner that knew only the first would under-report the console and make
  // the CLI look closer to parity than it is.
  const typed = /["'](get|post|put|patch|delete)["']\s*,\s*["'](\/[^"']*)["']/g
  const raw = /method:\s*["'](GET|POST|PUT|PATCH|DELETE)["'][\s\S]{0,400}?["'`](\/[a-z][^"'`]*)["'`]/g
  for (const file of tsFilesUnder(LIB)) {
    const src = readFileSync(file, 'utf8')
    for (const m of src.matchAll(typed)) found.add(op(m[1], m[2]))
    for (const m of src.matchAll(raw)) found.add(op(m[1], m[2]))
  }
  return found
}

describe('console ⇄ CLI conformance', () => {
  const all = specOperations()
  const web = consoleOperations()
  const cli = new Set(CLI_OPERATIONS)

  it('the spec is non-empty and parsed (guards the scanner itself)', () => {
    expect(all.length).toBeGreaterThan(20)
    expect(web.size).toBeGreaterThan(10)
  })

  it('every operation is reachable from the console, the CLI, or deliberately neither', () => {
    const orphans = all.filter(
      (o) => !web.has(o) && !cli.has(o) && !(o in INTENTIONALLY_ABSENT),
    )
    expect(
      orphans,
      `these operations exist in the API but no surface exposes them, and no reason is ` +
        `recorded in INTENTIONALLY_ABSENT:\n  ${orphans.join('\n  ')}`,
    ).toEqual([])
  })

  it('every operation the console reaches is also reachable from the CLI', () => {
    // The direction that matters for agents: a person can do it in the browser,
    // so an agent asked to do the same thing must not have to answer "the
    // platform cannot".
    const missing = all.filter(
      (o) => web.has(o) && !cli.has(o) && !(o in INTENTIONALLY_ABSENT),
    )
    expect(
      missing,
      `the console can reach these but \`abx\` cannot:\n  ${missing.join('\n  ')}`,
    ).toEqual([])
  })

  it('nothing is listed as intentionally absent while also being implemented', () => {
    // A stale exemption is worse than none: it silences the check for an
    // operation that has since been built on one surface only.
    const contradictory = Object.keys(INTENTIONALLY_ABSENT).filter(
      (o) => cli.has(o as OperationKey),
    )
    expect(contradictory, 'listed as intentionally absent but the CLI implements it').toEqual([])
  })

  it('every intentionally-absent entry names a real operation', () => {
    const known = new Set(all)
    const phantom = Object.keys(INTENTIONALLY_ABSENT).filter((o) => !known.has(o as OperationKey))
    expect(phantom, 'exempted operations that no longer exist in the spec').toEqual([])
  })
})
