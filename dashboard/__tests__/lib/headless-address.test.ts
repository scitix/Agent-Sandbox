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
import {
  RESOURCES,
  addressError,
  cliArgs,
  consolePath,
  parseConsolePath,
  type Address,
} from '@headless/index'

const ADDRESSES: Address[] = [
  { cluster: 'foo', resource: 'envs' },
  { cluster: 'foo', resource: 'envs', id: 'demo' },
  { cluster: 'foo', resource: 'envs', id: 'demo', sub: 'pools' },
  { cluster: 'foo', resource: 'envs', id: 'demo', sub: 'scaling-groups', subId: '1c2gi' },
  { cluster: 'foo', resource: 'sandboxes', id: 'abc-123' },
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
    expect(addressError({ resource: 'sandboxes', id: 'x', sub: 'logs', subId: 'y' })).toContain(
      'view',
    )
    expect(addressError({ resource: 'sandboxes', id: 'x', sub: 'logs' })).toBeNull()
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
