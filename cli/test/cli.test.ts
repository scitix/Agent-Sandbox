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
  DOCS_BASE,
  RESOURCES,
  ROOT_SEGMENTS,
  addressError,
  childrenOf,
  parsePositional,
  resolveApi,
  rootResources,
} from '@headless/index'
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { applyFilters, expandRows, hints, renderDetail, renderTable, visibleColumns } from '../src/render'
import { generate, generateForDashboard } from '../../scripts/gen-body-docs'
import { WRITE_DOCS } from '@headless/index'
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

/**
 * A quota holds a map per instance type, and which map has the keys decides
 * which rows exist.
 *
 * This is the projection that was missing: the columns used to name `used` and
 * `total` at the top level of a response that has neither, so every quota in
 * every deployment printed `used — total —` and the numbers — which were in the
 * response all along, one level down and keyed by instance type — were never
 * read. What each state MEANS is asserted against a verbatim live response in
 * ux.test.ts; what is asserted here is the shape of the row set.
 */
describe('one quota is one row per instance type', () => {
  const quotas = RESOURCES.find(r => r.plural === 'quotas')!
  const item = {
    id: 'alice.19.team-a.ondemand',
    name: 'alice.19.team-a.ondemand',
    team: 'team-a',
    metadata: {
      'quota.scitix.ai/pool-id': '19',
      'quota.scitix.ai/pool-name': 'demo-ondemand-shared',
      'quota.scitix.ai/pool-type': 'ondemand',
      'quota.scitix.ai/skip-check': 'true',
    },
  }
  const rowFor = (resources: unknown, skipCheck = 'true') =>
    expandRows(quotas, [
      { ...item, metadata: { ...item.metadata, 'quota.scitix.ai/skip-check': skipCheck }, resources },
    ]) as Record<string, unknown>[]

  it('takes the keys from every map, not only the ceiling one', () => {
    // No ceiling declared anywhere and 299 in use: `used` is the only map with
    // a key, and a row set built from `total` would be empty.
    const rows = rowFor({ reserved: { 'sci.c23-2': '0' }, total: null, used: { 'sci.c23-2': '299' } })
    expect(rows).toHaveLength(1)
    expect(rows[0].instanceType).toBe('sci.c23-2')
    expect(renderTable(quotas, rows, ctx)).toMatch(/sci\.c23-2\s+299\s+unlimited/)
  })

  it('keeps a quota that declares nothing as one row', () => {
    const rows = rowFor({ reserved: null, total: null, used: null })
    expect(rows).toHaveLength(1)
    expect(renderTable(quotas, rows, ctx)).toMatch(/^alice\.19\.team-a\.ondemand\s/m)
  })

  it('reads a provider hint whose key contains dots', () => {
    const rows = rowFor({ reserved: null, total: { 'sci.g21-3': '160' }, used: null })
    expect(renderTable(quotas, rows, ctx)).toMatch(/ondemand\s+demo-ondemand-shared/)
  })

  it('carries the numbers through, per instance type', () => {
    // The checked shape: a number is a ceiling, and free is what is left of it.
    const rows = rowFor(
      { reserved: { 'sci.g21-3': '0' }, total: { 'sci.g21-3': '160' }, used: { 'sci.g21-3': '40' } },
      'false',
    )
    expect(renderTable(quotas, rows, ctx)).toMatch(/sci\.g21-3\s+40\s+160\s+120/)
  })

  it('leaves a resource with no such maps exactly as it was', () => {
    const pools = RESOURCES.find(r => r.plural === 'pools')!
    const rows = [{ name: 'p', spec: { replicas: 3 } }]
    expect(expandRows(pools, rows)).toEqual(rows)
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

  it('an env detail carries no envDocs field: the document is a view of its own', () => {
    // A document between two scalars reads as a third scalar with its newlines
    // eaten; `abx envs <env> docs` prints it as what it is.
    expect(envs.detailFields!.some(f => f.id === 'envDocs' || f.path === 'envDocs')).toBe(false)
    const docs = envs.views!.find(v => v.segment === 'docs')
    expect(docs, 'envs has no docs view').toBeDefined()
    expect(docs!.api).toBe('/envs/{name}')
    expect(docs!.field).toBe('envDocs')
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
        // The verb of a write IS its own word now — `create` and `update` are
        // not two spellings of one operation, they are the two operations.
        expect(verbs, `${r.plural} declares ${w}`).toContain(`abx ${w} `)
      }
    }
  })

  it('drops a gated resource when its gate is off', () => {
    const off = agentContext('test', { quota: false })
    expect(off.resources.some(r => r.plural === 'quotas')).toBe(false)
    expect(doc.resources.some(r => r.plural === 'quotas')).toBe(true)
  })
})

/**
 * The prose links the CLI prints, and the one thing that can rot about them.
 *
 * `docs` is a slug into the documentation site, and the site is generated from
 * this same repository — so "the page exists" is a filesystem question this
 * test can answer rather than a hope. A rename that misses the registry is a
 * 404 printed by `abx envs --help`, which is a bad place to find out.
 */
describe('every resource points at a page that exists', () => {
  const contentDir = join(__dirname, '..', '..', 'docs', 'website', 'content', 'docs')

  it('names a slug that is a real page in this repository', () => {
    const linked = RESOURCES.filter(r => r.docs)
    expect(linked.length).toBeGreaterThan(0)
    for (const r of linked) {
      const file = join(contentDir, `${r.docs}.mdx`)
      expect(existsSync(file), `${r.plural} → ${r.docs}.mdx`).toBe(true)
    }
  })

  it('prints the link in the list footer, including in direct mode', () => {
    const spec = RESOURCES.find(r => r.plural === 'envs')!
    expect(hints(spec, ctx, { resource: 'envs' })).toContain(`${DOCS_BASE}/concepts/envs.md`)
    // The console link is the deployment's and is dropped without one. The
    // prose is not: a sandbox reaching one cluster's own API is exactly where
    // the caller has nothing else to read.
    const direct: Context = { clusterApi: 'http://cluster.example:8080', apiKey: 'k' }
    expect(hints(spec, direct, { resource: 'envs' })).toContain(`${DOCS_BASE}/concepts/envs.md`)
  })

  it('carries the link to an agent, per resource and as an entry point', () => {
    const doc = agentContext('test')
    expect(doc.docs.index).toBe(`${DOCS_BASE}/concepts/index.md`)
    expect(doc.resources.find(r => r.plural === 'envs')!.docs).toBe(`${DOCS_BASE}/concepts/envs.md`)
  })
})

/**
 * The skills are one directory each, and the surfaces around them describe that
 * set by hand: the installer fetches them by name, and several documents say
 * how many there are. Adding a skill is then a directory away from being
 * *almost* installed — fetched by nobody, described by a number that is now
 * wrong.
 */
describe('the skills that exist are the skills that ship', () => {
  const root = join(__dirname, '..', '..')
  const dirs = readdirSync(join(root, 'plugin', 'skills'))
    .filter((d) => d.startsWith('abx-'))
    .sort()

  it('the installer fetches every skill, and names no other', () => {
    const installer = readFileSync(join(root, 'plugin', 'install.sh'), 'utf8')
    const loop = installer.match(/for s in([\s\S]*?);\s*do/)?.[1] ?? ''
    expect(loop, 'no `for s in …; do` list in the installer to check').not.toBe('')
    expect([...loop.matchAll(/\b(abx-[a-z-]+)\b/g)].map((m) => m[1]).sort()).toEqual(dirs)
  })

  it('whatever says "nine skills" says the number there are', () => {
    const words = ['zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten']
    const claimed = [
      'README.md',
      'plugin/README.md',
      'docs/website/content/docs/installation.mdx',
      'docs/website/content/docs/skills/index.mdx',
      'docs/website/content/docs/tutorials/cli.mdx',
      'dashboard/components/assistant-ui/cli-guide.ts',
    ]
    const wrong: string[] = []
    let seen = 0
    for (const rel of claimed) {
      const text = readFileSync(join(root, rel), 'utf8')
      // "nine skills", "The nine", "nine of them" — these files write the word,
      // not the numeral, so the number is checked where it is readable.
      for (const m of text.matchAll(/\b(zero|one|two|three|four|five|six|seven|eight|nine|ten)\b(?=[^.\n]{0,24}\bskills?\b)/g)) {
        seen++
        if (m[1] !== words[dirs.length]) wrong.push(`${rel}: says "${m[1]} skills", there are ${dirs.length}`)
      }
      // …and the Chinese guide writes it as a numeral.
      const zh = { 九: 'nine', 十: 'ten', 八: 'eight' } as Record<string, string>
      for (const m of text.matchAll(/([九十])个 skill/g)) {
        seen++
        if (zh[m[1]] !== words[dirs.length]) wrong.push(`${rel}: says "${m[1]}个 skill"`)
      }
    }
    expect(wrong).toEqual([])
    // A pattern that matches nothing passes silently; these files are the
    // reason the check exists, so it has to have read some claims.
    expect(seen).toBeGreaterThan(5)
  })
})

/**
 * The commands the documentation prints, run through the CLI's own parser.
 *
 * The tutorial that shipped `abx envs` on the line after `abx context set` was
 * wrong in a way no reviewer caught and no test could have caught, because
 * nothing ever looked at the pages. These are the pages the CLI links to, so
 * they are the CLI's problem: a command that does not parse, or that names a
 * resource or a flag that does not exist, is a reader (or an agent) following
 * the documentation into an error message.
 *
 * What is checked is grammar, not behaviour — a page may say `abx envs YOUR_ENV
 * --cluster YOUR_CLUSTER` about an env that exists only on the reader's
 * cluster, and that is exactly the shape this has to accept.
 */
describe('the documented commands are commands this CLI has', () => {
  const docsDir = join(__dirname, '..', '..', 'docs', 'website', 'content', 'docs')
  const VERBS = ['create', 'update', 'delete', 'scale']
  /** Commands outside the resource grammar: they address nothing. */
  const OUTSIDE = ['context', 'agent-context', 'whoami']
  /** Every flag the CLI takes a value for; all of them precede an address. */
  const FLAGS_WITH_VALUE = [
    '--context',
    '--cluster',
    '--endpoint',
    '--cluster-api',
    '--api-key',
    '--filter',
    '--limit',
    '--auth-scheme',
    '--replicas',
    '-f',
  ]
  const FLAGS = [...FLAGS_WITH_VALUE, '--json', '--csv', '--wide', '--editable', '--schema', '--help', '-h', '--version']

  /** Every page under a directory, as absolute paths. */
  function pages(dir: string): string[] {
    const out: string[] = []
    for (const entry of readdirSync(dir)) {
      const abs = join(dir, entry)
      if (statSync(abs).isDirectory()) out.push(...pages(abs))
      else if (/\.mdx?$/.test(entry)) out.push(abs)
    }
    return out
  }

  /** `abx …` lines out of the fenced blocks, continuations joined. */
  function commands(abs: string): string[] {
    const out: string[] = []
    let fenced = false
    let pending = ''
    for (const raw of readFileSync(abs, 'utf8').split('\n')) {
      if (/^\s*```/.test(raw)) {
        fenced = !fenced
        continue
      }
      if (!fenced) continue
      const line = (pending + raw.replace(/\s+#\s.*$/, '')).trim()
      if (line.endsWith('\\')) {
        pending = `${line.slice(0, -1).trim()} `
        continue
      }
      pending = ''
      if (/^abx\s/.test(line)) out.push(line)
    }
    return out
  }

  it('parses, and names resources and flags that exist', () => {
    const checked: string[] = []
    for (const abs of [...pages(join(docsDir, 'concepts')), ...pages(join(docsDir, 'tutorials'))]) {
      const rel = abs.slice(docsDir.length + 1)
      for (const command of commands(abs)) {
        // A pipeline or a redirection belongs to the shell; what is checked is
        // the `abx` half of `abx … | jq …`. Cut at the first operator TOKEN, so
        // an `<env>` placeholder in an argument is not mistaken for one.
        const words = command.split(/\s+/).slice(1)
        const op = words.findIndex((w) => /^(\d*>|[|;>])|^&&$|^\|\|$/.test(w))
        const tokens = op === -1 ? words : words.slice(0, op)
        for (const t of tokens) {
          if (!t.startsWith('-')) continue
          expect(FLAGS, `${rel}: unknown flag in \`${command}\``).toContain(t)
        }
        // Positionals, with every flag's value removed. `<id>` stands in for a
        // value the reader supplies, exactly as it does in the CLI's own hints.
        const positional: string[] = []
        for (let i = 0; i < tokens.length; i++) {
          if (FLAGS_WITH_VALUE.includes(tokens[i])) i++
          else if (!tokens[i].startsWith('-')) positional.push(tokens[i])
        }
        expect(positional.length, `${rel}: \`${command}\` addresses nothing`).toBeGreaterThan(0)

        const verb = VERBS.includes(positional[0]) ? positional[0] : undefined
        const subject = verb ? positional.slice(1) : positional
        // Grammar illustrations (`abx <resource> <id>`, `abx create <collection…>`)
        // name no resource, so there is nothing to check them against.
        if (subject[0].startsWith('<')) continue
        const concrete = subject.map((t) => (t.startsWith('<') ? 'x' : t))
        if (OUTSIDE.includes(concrete[0])) continue
        expect(
          ROOT_SEGMENTS,
          `${rel}: \`${command}\` names an unknown resource`,
        ).toContain(concrete[0])
        if (verb) {
          // The address it writes to is the same grammar as the read.
          expect(parsePositional(concrete), `${rel}: \`${command}\``).not.toBeNull()
        } else if (!concrete.includes('--help')) {
          const parsed = parsePositional(concrete)
          expect(parsed, `${rel}: \`${command}\``).not.toBeNull()
          expect(addressError({ ...parsed!, cluster: 'c' }), `${rel}: \`${command}\``).toBeNull()
        }
        checked.push(command)
      }
    }
    // A parser that stops matching nothing passes silently; the pages are the
    // reason this exists, so at least a handful of commands have to be read.
    expect(checked.length).toBeGreaterThan(20)
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

describe('the write-body docs are generated, not transcribed', () => {
  const root = join(import.meta.dir, '..', '..')

  it('the committed module is exactly what the spec produces', () => {
    // The assertion that makes the rest of the file worth trusting: if someone
    // edits the generated module by hand, or changes the spec and forgets
    // `make gen-all-api`, this fails here instead of in a caller's terminal.
    const spec = readFileSync(join(root, 'pkg', 'openapi', 'native', 'openapi.yaml'), 'utf8')
    const committed = readFileSync(join(root, 'headless', 'src', 'write-docs.generated.ts'), 'utf8')
    expect(generate(spec)).toBe(committed)
    // The console gets a second copy in its own tree (the bundler cannot reach
    // `../headless`), and a copy is only safe while something proves it is a
    // copy. This is that something.
    const dash = readFileSync(join(root, 'dashboard', 'lib', 'utils', 'write-docs.generated.ts'), 'utf8')
    expect(generateForDashboard(spec)).toBe(dash)
  })

  it('every resource that can be written to is documented', () => {
    for (const r of RESOURCES) {
      const writes = (r.api.verbs ?? []).filter((v) => v === 'create' || v === 'update')
      if (!writes.length) continue
      const doc = WRITE_DOCS.find((d) => d.plural === r.plural)
      expect(doc, `${r.plural} declares a write but has no body docs`).toBeDefined()
      for (const verb of writes) {
        expect(doc?.[verb], `${r.plural} declares ${verb} but has no ${verb} body`).toBeDefined()
      }
    }
  })

  it('update takes the same file create does', () => {
    // The whole point of one file for both verbs: everything a create can say,
    // an update must be able to say back. `name` is the one field the address
    // already carries — and it still has to be ACCEPTED, because the file a
    // caller exports from a create is the file they edit and send back.
    for (const doc of WRITE_DOCS) {
      if (!doc.create || !doc.update) continue
      const inUpdate = new Set(doc.update.fields.map((f) => f.name))
      const missing = doc.create.fields.filter((f) => !inUpdate.has(f.name)).map((f) => f.name)
      expect(missing, `${doc.plural}: update would refuse a field create wrote`).toEqual([])
    }
  })

  it('a fixed field says how to change it instead', () => {
    for (const doc of WRITE_DOCS) {
      const bodies = [doc.create, doc.update].filter(Boolean)
      for (const body of bodies) {
        for (const f of body!.fields) {
          if (!f.fixed) continue
          // "Fixed" with no way out is a dead end; the lever is what makes it a
          // rule rather than a wall.
          expect(f.lever, `${doc.plural}.${f.name} is fixed but names no lever`).toBeTruthy()
        }
      }
    }
  })
})

describe('the file shape lives in the spec, and nowhere else', () => {
  const root = join(import.meta.dir, '..', '..')

  /**
   * The field names that only exist in a write body.
   *
   * `name` is deliberately absent: it is a key in every JSON document this
   * project prints, so a rule about it would fire on output examples that are
   * nobody's business to change. The rest are specific enough that seeing one
   * as a JSON key means somebody transcribed a file.
   */
  const distinctive = new Set(
    WRITE_DOCS.flatMap((d) => [...(d.create?.fields ?? []), ...(d.update?.fields ?? [])])
      .map((f) => f.name)
      .filter((n) => n !== 'name'),
  )

  const offenders = (text: string): string[] =>
    text
      .split('\n')
      .filter((l) => /"[A-Za-z][A-Za-z0-9]*"\s*:/.test(l))
      .filter((l) => [...distinctive].some((f) => l.includes(`"${f}"`)))

  it('the assistant prompt and every skill point at --help instead of quoting it', () => {
    // These documents are the reason the generator exists: the same body was
    // written out in four of them, and the one that was wrong was the one an
    // agent read. A transcription here is not a documentation style problem —
    // it is an agent that will write a file the API refuses.
    const targets = [
      join(root, 'brain', 'AGENTS.md'),
      ...readdirSync(join(root, 'plugin', 'skills')).map((d) =>
        join(root, 'plugin', 'skills', d, 'SKILL.md'),
      ),
    ]
    const found: string[] = []
    for (const path of targets) {
      let text: string
      try {
        text = readFileSync(path, 'utf8')
      } catch {
        continue
      }
      for (const line of offenders(text)) found.push(`${path}: ${line.trim()}`)
    }
    expect(found).toEqual([])
  })

  it('and the skill that describes writing says where the shape comes from', () => {
    const common = readFileSync(join(root, 'plugin', 'skills', 'abx-common', 'SKILL.md'), 'utf8')
    expect(common).toContain('abx create envs --help')
    expect(common).toContain('--editable')
  })
})
