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
 * The console address and the CLI invocation are one thing with two spellings.
 *
 * These assertions are what keep that true. Four separate copies of this
 * mapping drifted before the registry existed — one emitted a link to a
 * different resource, one to a page that 404s — and every one of those would
 * have failed here.
 */

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { parse as parseYaml } from 'yaml'
import {
  RESOURCES,
  addressError,
  cliArgs,
  consolePath,
  operations,
  parseConsolePath,
  parsePositional,
  type Address,
} from '@headless/index'

const ADDRESSES: Address[] = [
  { cluster: 'foo', resource: 'envs' },
  { cluster: 'foo', resource: 'envs', id: 'demo' },
  { cluster: 'foo', resource: 'envs', id: 'demo', sub: 'pools' },
  { cluster: 'foo', resource: 'envs', id: 'demo', sub: 'scaling-groups', subId: '1c2gi' },
  { cluster: 'foo', resource: 'envs', id: 'demo', view: 'metrics' },
  { cluster: 'foo', resource: 'envs', id: 'demo', sub: 'pools', subId: 'p1', view: 'metrics' },
  { cluster: 'foo', resource: 'sandboxes', id: 'abc-123' },
  { cluster: 'foo', resource: 'sandboxes', id: 'abc-123', view: 'logs' },
  { cluster: 'bar', resource: 'quotas' },
]

describe('one address, two spellings', () => {
  it('round-trips through the console path', () => {
    for (const a of ADDRESSES) {
      expect(parseConsolePath(consolePath(a))).toEqual(a)
    }
  })

  it('spells the same segments in the CLI, in the same order', () => {
    // The console path minus its cluster prefix must be exactly the CLI's
    // positional tokens. If these ever diverge, `open_page` and "turn this page
    // into a command" stop being derivable and go back to needing a table.
    for (const a of ADDRESSES) {
      const positional = cliArgs(a).slice(0, cliArgs(a).indexOf('--cluster'))
      const fromPath = consolePath(a)
        .split('/')
        .filter(Boolean)
        .slice(2) // drop clusters/{cluster}
        .map(decodeURIComponent)
      expect(positional).toEqual(fromPath)
    }
  })

  it('names the valid set when a sub-resource does not exist', () => {
    const err = addressError({ resource: 'envs', id: 'x', sub: 'autoscaling' })
    expect(err).toContain('scaling-groups')
    // The old route segment is exactly what a person or an agent would try
    // first, so the error has to carry the replacement rather than just refuse.
  })

  it('refuses to address a view as if it were a collection', () => {
    // A view is always the LAST segment, never a sub-resource, so there is one
    // spelling for "the logs of this sandbox" rather than two that drift.
    expect(parsePositional(['sandboxes', 'x', 'logs', 'y'])).toBeNull()
    expect(addressError({ resource: 'sandboxes', id: 'x', view: 'logs' })).toBeNull()
    expect(addressError({ resource: 'sandboxes', id: 'x', sub: 'logs' })).toContain('no "logs"')
  })

  it('reads a view at whichever depth the address reached', () => {
    expect(parsePositional(['envs', 'e', 'pools', 'p', 'metrics'])).toEqual({
      resource: 'envs',
      id: 'e',
      sub: 'pools',
      subId: 'p',
      view: 'metrics',
    })
    // The tail is refused rather than dropped. Silently ignoring it is how
    // `envs e pools p metrics` used to come back as the pool itself.
    expect(parsePositional(['envs', 'e', 'pools', 'p', 'metrics', 'x'])).toBeNull()
  })

  it('a child resource is not addressable on its own', () => {
    const err = addressError({ resource: 'pools' })
    expect(err).toContain('envs <env> pools')
  })

  it('every API path template the registry declares exists in the spec', () => {
    // The registry is what the CLI dispatches on AND what the conformance
    // manifest projects, so a typo here would silently claim a capability that
    // 404s. Checked against the spec itself rather than against a copy.
    const doc = parseYaml(
      readFileSync(join(__dirname, '..', '..', '..', 'pkg', 'openapi', 'native', 'openapi.yaml'), 'utf8'),
    ) as { paths: Record<string, Record<string, unknown>> }
    for (const o of operations()) {
      expect(doc.paths[o.path], `${o.resource} declares ${o.path}`).toBeTruthy()
      expect(
        doc.paths[o.path][o.method.toLowerCase()],
        `${o.method} ${o.path} (${o.resource})`,
      ).toBeTruthy()
    }
  })

  it('every column that declares a filter names one the resource accepts', () => {
    for (const r of RESOURCES) {
      const keys = new Set((r.filters ?? []).map((f) => f.key))
      for (const c of r.columns) {
        if (!c.filter) continue
        expect(keys, `${r.plural}.${c.id} filters on "${c.filter}"`).toContain(c.filter)
      }
    }
  })

  it('resource plurals are unique', () => {
    const seen = RESOURCES.map((r) => r.plural)
    expect(new Set(seen).size).toBe(seen.length)
  })
})
