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
 * The CLI's own contract.
 *
 * The assertion that earns its place here is the first one: every command the
 * CLI SUGGESTS has to be one the CLI can parse. A hint is the cheapest place to
 * put domain knowledge and the most likely to rot, because nothing executes it
 * — an agent follows it, gets a usage error, and learns to distrust the hints.
 */

import { describe, expect, it } from 'bun:test'
import {
  RESOURCES,
  addressError,
  childrenOf,
  parsePositional,
  resolveApi,
  rootResources,
} from '@headless/index'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { applyFilters, hints, renderTable, visibleColumns } from '../src/render'
import {
  AmbiguousContextError,
  UnknownContextError,
  selectContext,
  type FileConfig,
} from '../src/contexts'
import { agentContext } from '../src/agent-context'
import { CliError, baseUrl, clusterListUrl, type Context } from '../src/context'

const ctx: Context = {
  endpoint: 'https://example.test/api',
  apiKey: 'k',
  cluster: 'demo',
  authScheme: 'api-key',
  format: 'table',
  webBase: 'https://example.test',
}

/** Pull the `abx …` commands out of a hint block. */
function commandsIn(text: string): string[][] {
  return text
    .split('\n')
    .map(l => l.trim())
    .filter(l => l.startsWith('abx '))
    .map(l => l.replace(/\s+#.*$/, '').split(/\s+/).slice(1))
    .map(tokens => {
      const at = tokens.indexOf('--cluster')
      return at === -1 ? tokens : tokens.slice(0, at)
    })
}

describe('hints are commands, not prose', () => {
  it('every suggested command parses and addresses something real', () => {
    for (const spec of rootResources()) {
      const addresses = [
        { cluster: ctx.cluster, resource: spec.plural },
        { cluster: ctx.cluster, resource: spec.plural, id: 'x' },
      ]
      for (const a of addresses) {
        for (const tokens of commandsIn(hints(spec, ctx, a))) {
          // Placeholders stand in for an id the user supplies.
          const concrete = tokens.map(t => (t.startsWith('<') ? 'x' : t))
          if (concrete.includes('--help')) continue
          const parsed = parsePositional(concrete)
          expect(parsed, `unparseable hint: abx ${tokens.join(' ')}`).not.toBeNull()
          expect(
            addressError({ ...parsed!, cluster: ctx.cluster }),
            `hint addresses nothing: abx ${tokens.join(' ')}`,
          ).toBeNull()
        }
      }
    }
  })

  it('never advertises a view the CLI cannot serve', () => {
    // Pool metrics come from Prometheus, not this API. Offering the command
    // would be offering an error.
    for (const spec of RESOURCES) {
      for (const v of spec.views ?? []) {
        if (v.api) continue
        const a = { resource: spec.parent ?? spec.plural, id: 'x', view: v.segment }
        const text = hints(spec, ctx, { cluster: ctx.cluster, resource: spec.plural, id: 'x' })
        expect(text).not.toContain(` ${v.segment} `)
        expect(resolveApi(a)).toBeNull()
      }
    }
  })
})

describe('the table is the vocabulary', () => {
  it('every header is the column id, and every filter key is a header', () => {
    for (const spec of RESOURCES) {
      const rendered = renderTable(spec, [], ctx, { wide: true })
      const header = rendered.split('\n')[2].split(/\s+/).filter(Boolean)
      expect(header).toEqual(visibleColumns(spec, true).map(c => c.id))
      for (const f of spec.filters ?? []) {
        expect(
          spec.columns.some(c => c.id === f.key),
          `${spec.plural}: --filter ${f.key} names no column`,
        ).toBe(true)
      }
    }
  })

  it('reads nested values through the column path', () => {
    const pools = RESOURCES.find(r => r.plural === 'pools')!
    const row = { name: 'p', spec: { replicas: 3 }, status: { idleReplicas: 2 } }
    const out = renderTable(pools, [row], ctx)
    expect(out).toContain('3')
    expect(out).toContain('2')
  })
})

describe('filters refuse what they cannot honour', () => {
  const envs = RESOURCES.find(r => r.plural === 'envs')!

  it('an unknown key names the valid set', () => {
    try {
      applyFilters(envs, [], [['nope', 'x']])
      throw new Error('should have thrown')
    } catch (e) {
      expect(e).toBeInstanceOf(CliError)
      expect((e as CliError).hint).toContain('templateName')
    }
  })

  it('a closed set rejects a value outside it, and names the members', () => {
    try {
      applyFilters(envs, [], [['mode', 'Warm']])
      throw new Error('should have thrown')
    } catch (e) {
      expect((e as CliError).hint).toContain('WarmPool')
    }
  })

  it('an open filter matches as a substring', () => {
    const rows = [{ name: 'alpha' }, { name: 'beta' }]
    expect(applyFilters(envs, rows, [['name', 'lph']])).toEqual([{ name: 'alpha' }])
  })
})

describe('agent-context describes this CLI and no other', () => {
  const doc = agentContext('test')

  it('lists only top-level resources as roots', () => {
    expect(doc.roots).toEqual(rootResources().map(r => r.plural))
    expect(doc.roots).not.toContain('pools')
  })

  it('states the address of every child under its parent', () => {
    for (const r of doc.resources) {
      if (!r.parent) continue
      const parent = doc.resources.find(x => x.plural === r.parent)!
      expect(parent.subResources.map(s => s.segment)).toContain(r.plural)
    }
  })

  it('every declared write is one the grammar can express', () => {
    const verbs = doc.grammar.write.join(' ')
    for (const r of doc.resources) {
      for (const w of r.writes) {
        // `create` is `apply -f` against the collection; the rest are literal.
        const word = w === 'create' ? 'apply' : w
        expect(verbs, `${r.plural} declares ${w}`).toContain(word)
      }
    }
  })

  it('drops a gated resource when its gate is off', () => {
    const off = agentContext('test', { quota: false })
    expect(off.resources.some(r => r.plural === 'quotas')).toBe(false)
    expect(doc.resources.some(r => r.plural === 'quotas')).toBe(true)
  })
})

describe('children are addressed under their parent', () => {
  it('a child is refused at the top level, with the form that works', () => {
    for (const r of RESOURCES) {
      if (!r.parent) continue
      const err = addressError({ resource: r.plural })
      expect(err, `${r.plural} should not be addressable alone`).toContain(r.parent)
      expect(childrenOf(r.parent).map(c => c.plural)).toContain(r.plural)
    }
  })
})

describe('contexts name deployments, clusters name their clusters', () => {
  const two: FileConfig = {
    currentContext: 'alpha',
    contexts: {
      alpha: { endpoint: 'https://a.test/api/clusters/{cluster}', apiKey: 'k1' },
      beta: { endpoint: 'https://b.test/api/clusters/{cluster}', apiKey: 'k2' },
    },
  }

  it('uses the current context when none is named', () => {
    expect(selectContext(two).name).toBe('alpha')
  })

  it('an explicit name wins', () => {
    expect(selectContext(two, 'beta').entry.apiKey).toBe('k2')
  })

  it('an unknown name lists the ones that exist', () => {
    try {
      selectContext(two, 'gamma')
      throw new Error('should have thrown')
    } catch (e) {
      expect(e).toBeInstanceOf(UnknownContextError)
      expect((e as UnknownContextError).known).toEqual(['alpha', 'beta'])
    }
  })

  it('refuses to guess between two when none is current', () => {
    // Guessing is the one outcome worth preventing: the command would succeed,
    // against the wrong platform, and nothing in the output would say so.
    const noDefault = { contexts: two.contexts }
    expect(() => selectContext(noDefault)).toThrow(AmbiguousContextError)
  })

  it('a single context needs no default', () => {
    const one = { contexts: { only: { endpoint: 'https://x.test', apiKey: 'k' } } }
    expect(selectContext(one).name).toBe('only')
  })

  it('the flat shape still works, and is what a platform sandbox has', () => {
    // The plugin hook and the in-sandbox case both write settings with no
    // contexts key at all. Requiring one would break every existing install.
    const flat = { endpoint: 'https://x.test', apiKey: 'k' }
    const got = selectContext(flat)
    expect(got.name).toBeUndefined()
    expect(got.entry.endpoint).toBe('https://x.test')
  })

  it('the CLI ships no deployment address of its own', () => {
    // The deployment owns its URLs and this repository is public. An address
    // compiled into the binary would publish an internal hostname AND point
    // every fresh install at somebody else's platform — so the only hosts
    // allowed in the source are the licence header and documentation examples.
    const allowed = /^(www\.apache\.org|.*\.example|example\.(com|test|invalid))$/
    for (const file of readdirSync(join(import.meta.dir, '..', 'src'))) {
      const src = readFileSync(join(import.meta.dir, '..', 'src', file), 'utf8')
      for (const m of src.matchAll(/https?:\/\/([a-z0-9.-]+\.[a-z]{2,})/gi)) {
        expect(m[1], `${file} names the host ${m[1]}`).toMatch(allowed)
      }
    }
    expect(selectContext({}).entry).toEqual({})
  })
})

describe('which cluster is a question, not a precondition', () => {
  const bff: Context = { ...ctx, endpoint: 'https://c.test/agentbox/api/clusters/{cluster}', cluster: undefined }

  it('asks for the cluster list one level above the placeholder', () => {
    // "Which clusters are there" cannot be answered by first naming one. A
    // path-routing endpoint publishes the list at the level its placeholder
    // sits in, so that is where it is asked — and `abx clusters` therefore
    // needs no --cluster at all.
    expect(clusterListUrl(bff)).toBe('https://c.test/agentbox/api/clusters')
  })

  it('has no such level when the endpoint serves one cluster', () => {
    expect(clusterListUrl({ ...ctx, endpoint: 'https://one.test/api' })).toBeNull()
  })

  it('refuses with the next command rather than a bare demand', () => {
    try {
      baseUrl(bff)
      throw new Error('should have thrown')
    } catch (e) {
      expect(e).toBeInstanceOf(CliError)
      // Not "pass --cluster" and nothing else: the durable fix is a default on
      // the context, and `abx clusters` is how you find the id to put there.
      expect((e as CliError).hint).toContain('abx context set')
      expect((e as CliError).hint).toContain('abx clusters')
    }
  })

  it('still substitutes when a cluster is known', () => {
    expect(baseUrl({ ...bff, cluster: 'x' })).toBe('https://c.test/agentbox/api/clusters/x/v1')
  })
})
