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
import { applyFilters, hints, renderDetail, renderTable, visibleColumns } from '../src/render'
import {
  AmbiguousContextError,
  UnknownContextError,
  selectContext,
  whoamiCacheKey,
  type FileConfig,
} from '../src/contexts'
import { agentContext } from '../src/agent-context'
import {
  CliError,
  baseUrl,
  clusterListUrl,
  consoleBase,
  consoleBaseOf,
  headers,
  viaConsole,
  type Context,
} from '../src/context'

const ctx: Context = {
  endpoint: 'https://example.test',
  apiKey: 'k',
  cluster: 'demo',
  authScheme: 'api-key',
  format: 'table',
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

describe('a detail view is the get, not the list', () => {
  const envs = RESOURCES.find(r => r.plural === 'envs')!

  it('reads every declared field through the path it declares', () => {
    // The bug this encodes: a get printed the LIST's columns, so every field
    // that lives under spec/status came out as `—` with the value sitting in
    // the same response. A declared path that is not the id is the whole
    // mechanism, so it is what gets asserted.
    for (const spec of RESOURCES) {
      if (!spec.detailFields?.length) continue
      const item: Record<string, unknown> = {}
      for (const f of spec.detailFields) {
        const parts = (f.path ?? f.id).split('.')
        let cur = item
        for (const p of parts.slice(0, -1)) cur = (cur[p] ??= {}) as Record<string, unknown>
        cur[parts[parts.length - 1]] = f.text ? 'x'.repeat(4000) : `${f.id}-value`
      }
      const out = renderDetail(spec, item, ctx, { cluster: ctx.cluster, resource: spec.plural, id: 'x' })
      for (const f of spec.detailFields) {
        if (f.text) {
          expect(out, `${spec.plural}.${f.id}`).toContain(`${f.id} (4000 chars)`)
        } else {
          const line = new RegExp(`^${f.id}\\s+${f.id}-value$`, 'm')
          expect(out, `${spec.plural}.${f.id} is not rendered`).toMatch(line)
        }
      }
    }
  })

  it('an env detail carries no envDocs field at all', () => {
    // Not a rendering choice: the server substitutes the caller's own plaintext
    // key into that Markdown, so a field that would print it must not exist.
    expect(envs.detailFields!.some(f => f.id === 'envDocs' || f.path === 'envDocs')).toBe(false)
  })

  it('a field the response does not carry reads as absent, not as its own name', () => {
    const out = renderDetail(envs, {}, ctx, { cluster: ctx.cluster, resource: 'envs', id: 'e' })
    expect(out).toMatch(/^template\s+—$/m)
  })

  it('the parent detail carries the child tables it declares', () => {
    const child = childrenOf('envs').find(c => c.inParentDetail)!
    const out = renderDetail(envs, { name: 'e' }, ctx, { cluster: ctx.cluster, resource: 'envs', id: 'e' }, [
      { spec: child, rows: [{ name: 'e-pool' }] },
    ])
    expect(out).toContain('e-pool')
    expect(out).toContain('hint:')
  })
})

describe('the help page offers only what exists', () => {
  it('never links to a console page the resource does not have', () => {
    for (const spec of RESOURCES) {
      if (spec.consolePage !== false) continue
      const text = hints(spec, ctx, { cluster: ctx.cluster, resource: spec.plural, id: 'x' })
      expect(text, `${spec.plural} links to a page that 404s`).not.toContain('view:')
    }
  })

  it('an admin-only resource is named as one, and hidden from a tenant', () => {
    for (const r of RESOURCES) {
      if (!r.admin || r.parent) continue
      expect(r.plural, `${r.plural} is admin-only but not named like it`).toStartWith('admin-')
    }
    const tenant = agentContext('test', null, 'tenant')
    expect(tenant.roots).not.toContain('admin-teams')
    expect(tenant.roots).not.toContain('admin-namespaces')
    expect(tenant.resources.some(r => r.plural === 'admin-teams')).toBe(false)
    // The template catalog is read by everyone; hiding it would say templates
    // cannot be read at all.
    expect(tenant.roots).toContain('templates')
    const admin = agentContext('test', null, 'admin')
    expect(admin.roots).toContain('admin-teams')
  })

  it('shows everything when the role could not be established', () => {
    const unknown = agentContext('test', null, null)
    expect(unknown.roots).toEqual(rootResources().map(r => r.plural))
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

describe('the role cache follows the credential, not just the deployment', () => {
  it('two keys for one context are two entries', () => {
    // A person can hold an admin key and an agent key for the same platform,
    // and a plugin hook can rewrite the configured one. Keyed on the context
    // alone they shared an entry, so whichever ran `whoami` last decided what
    // the other key's help page advertised — which showed up as an admin being
    // told the admin commands do not exist.
    const admin = whoamiCacheKey('work', 'https://c.test', 'agbx_admin')
    const agent = whoamiCacheKey('work', 'https://c.test', 'agbx_agent')
    expect(admin).not.toBe(agent)
    expect(admin.startsWith('work:')).toBe(true)
    // Two deployments with the same key are different entries too.
    expect(whoamiCacheKey('a', 'https://a.test', 'k')).not.toBe(
      whoamiCacheKey('b', 'https://b.test', 'k'),
    )
  })

  it('names the context it came from, and never carries the key', () => {
    const key = whoamiCacheKey(undefined, 'https://c.test', 'agbx_secret_value')
    expect(key.startsWith('default:')).toBe(true)
    expect(key).not.toContain('agbx_secret_value')
  })
})

describe('the console is the default way in', () => {
  const console_: Context = { ...ctx, endpoint: 'https://c.test/agentbox', cluster: undefined }

  it('an endpoint alone means console mode', () => {
    // The mode is not a setting anyone chooses. Configuring only a console
    // address — which is what a person copies out of their browser — is
    // console mode, and that is the overwhelmingly common case.
    expect(viaConsole(console_)).toBe(true)
  })

  it('asks for the cluster list above the per-cluster mount', () => {
    // "Which clusters are there" cannot be answered by first naming one, so it
    // is asked one level up — and `abx clusters` therefore needs no --cluster.
    expect(clusterListUrl(console_)).toBe('https://c.test/agentbox/api/clusters')
  })

  it('refuses with the next command rather than a bare demand', () => {
    try {
      baseUrl(console_)
      throw new Error('should have thrown')
    } catch (e) {
      expect(e).toBeInstanceOf(CliError)
      // The cluster is named per command, not baked into the context: the
      // durable answer is `abx clusters` to find the id, then `--cluster` on
      // the command that needs it.
      expect((e as CliError).hint).toContain('--cluster')
      expect((e as CliError).hint).toContain('abx clusters')
    }
  })

  it('routes to whichever cluster the command names', () => {
    expect(baseUrl({ ...console_, cluster: 'x' })).toBe('https://c.test/agentbox/api/clusters/x/v1')
    expect(baseUrl({ ...console_, cluster: 'y' })).toBe('https://c.test/agentbox/api/clusters/y/v1')
  })
})

describe('direct mode is the exception, and has to be asked for', () => {
  const direct: Context = { ...ctx, clusterApi: 'https://cluster.test/api', cluster: undefined }

  it('is entered only by naming a cluster API', () => {
    expect(viaConsole(direct)).toBe(false)
    // Nothing about an endpoint's shape can trigger it: a sandbox that cannot
    // reach the console has to be told so explicitly, so the fallback is never
    // silent.
    expect(viaConsole({ ...ctx, endpoint: 'https://anything.test/whatever' })).toBe(true)
  })

  it('goes straight to that cluster, with no console in the path', () => {
    expect(baseUrl(direct)).toBe('https://cluster.test/api/v1')
  })

  it('publishes no cluster list one level up, because there is no console', () => {
    expect(clusterListUrl(direct)).toBeNull()
  })

  it('offers no console links, rather than inventing an address', () => {
    expect(consoleBase(direct)).toBeUndefined()
    expect(consoleBase(ctx)).toBe('https://example.test')
  })
})

describe('the mode decides the auth header, not a flag', () => {
  it('console mode sends the key as a Bearer token', () => {
    // The BFF proxy reads Authorization only; a cluster API reads the other
    // header. Deriving it from the mode means a reader who copied a URL out of
    // the console never has to learn which is which.
    const viaBff = { ...ctx, authScheme: undefined }
    expect(headers(viaBff).Authorization).toBe('Bearer k')
    expect(headers(viaBff)['AGENTBOX-API-KEY']).toBeUndefined()
  })

  it('direct mode sends the key as AGENTBOX-API-KEY', () => {
    const api = { ...ctx, clusterApi: 'https://cluster.test/api', authScheme: undefined }
    expect(headers(api)['AGENTBOX-API-KEY']).toBe('k')
    expect(headers(api).Authorization).toBeUndefined()
  })

  it('an explicit scheme is the escape hatch for a deployment answering to neither', () => {
    const forced = { ...ctx, clusterApi: 'https://cluster.test/api', authScheme: 'bearer' as const }
    expect(headers(forced).Authorization).toBe('Bearer k')
  })
})

describe('a config written for the older endpoint form keeps working', () => {
  it('drops the console mount and the routing placeholder', () => {
    // Endpoints used to be stored with `/api/clusters/{cluster}` attached,
    // because that was the string the CLI substituted into. Normalising on read
    // means nobody has to rewrite a working config by hand.
    expect(consoleBaseOf('https://c.test/agentbox/api/clusters/{cluster}')).toBe('https://c.test/agentbox')
    expect(consoleBaseOf('https://c.test/agentbox/api/clusters')).toBe('https://c.test/agentbox')
  })

  it('leaves an address that is already a console base alone', () => {
    expect(consoleBaseOf('https://c.test/agentbox')).toBe('https://c.test/agentbox')
    expect(consoleBaseOf('https://c.test/agentbox/')).toBe('https://c.test/agentbox')
  })
})
