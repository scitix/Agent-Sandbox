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
 * abx — the AgentBox platform CLI.
 *
 * The whole grammar, which is the point:
 *
 *   abx <resource>                       list
 *   abx <resource> <id>                  get
 *   abx <resource> <id> <sub>            sub-list
 *   abx <resource> <id> <sub> <sub-id>   sub-get
 *   abx <path…> apply -f FILE            write the desired state
 *   abx <path…> delete
 *   abx <path…> scale --replicas N
 *
 * Adding a resource costs nothing here: it is a registry entry, not a new
 * command with a new description and a new schema. That is the whole advantage
 * over a tool-per-operation surface, and it survives only as long as nobody
 * adds a verb that does not fit the shape.
 */

import {
  RESOURCES,
  ROOT_SEGMENTS,
  actionNames,
  addressError,
  childrenOf,
  consolePath,
  parsePositional,
  resolveAction,
  resolveApi,
  resourceOf,
  rootResources,
  supports,
  type Address,
  type Verb,
} from '@headless/index'
import {
  CliError,
  clusterListUrl,
  derivedWebBase,
  routesByPath,
  type Context,
} from './context'
import {
  AmbiguousContextError,
  UnknownContextError,
  configPath,
  contextNames,
  readConfig,
  selectContext,
  writeConfig,
  type ContextEntry,
  type FileConfig,
} from './contexts'
import { request, requestAt } from './api'
import { applyFilters, hints, renderCsv, renderTable, visibleColumns } from './render'
import { agentContext } from './agent-context'

declare const AGBX_CLI_VERSION: string
const VERSION = typeof AGBX_CLI_VERSION === 'string' ? AGBX_CLI_VERSION : 'dev'

interface Parsed {
  positional: string[]
  flags: Record<string, string | boolean>
  filters: [string, string][]
}

function parseArgs(argv: string[]): Parsed {
  const positional: string[] = []
  const flags: Record<string, string | boolean> = {}
  const filters: [string, string][] = []
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (!a.startsWith('-')) {
      positional.push(a)
      continue
    }
    let key = a.replace(/^--?/, '')
    let value: string | undefined
    const eq = key.indexOf('=')
    if (eq !== -1) {
      value = key.slice(eq + 1)
      key = key.slice(0, eq)
    } else if (argv[i + 1] !== undefined && !argv[i + 1].startsWith('-')) {
      value = argv[++i]
    }
    if (key === 'filter') {
      if (!value || !value.includes('=')) {
        throw new CliError('--filter takes key=value', 'the keys are the column headings')
      }
      const at = value.indexOf('=')
      filters.push([value.slice(0, at), value.slice(at + 1)])
      continue
    }
    flags[key] = value ?? true
  }
  return { positional, flags, filters }
}

function contextFrom(flags: Record<string, string | boolean>, file: FileConfig = {}): Context {
  const env = (k: string) => process.env[k] ?? ''
  const str = (k: string, fallback = '') =>
    typeof flags[k] === 'string' ? (flags[k] as string) : fallback

  // Which deployment, before anything about which cluster.
  let selected: { name?: string; entry: ContextEntry }
  try {
    selected = selectContext(file, str('context') || undefined)
  } catch (err) {
    if (err instanceof UnknownContextError) {
      throw new CliError(
        err.message,
        err.known.length
          ? `configured: ${err.known.join(', ')}\nadd one with \`abx context set <name> --endpoint … --api-key …\``
          : `none are configured yet — \`abx context set <name> --endpoint … --api-key …\``,
      )
    }
    if (err instanceof AmbiguousContextError) {
      throw new CliError(
        err.message,
        `pick one for this command with --context, or make it the default:\n` +
          err.known.map((n) => `  abx context use ${n}`).join('\n'),
      )
    }
    throw err
  }
  const entry = selected.entry

  // One flag name per concept, everywhere. --json and --csv are the shorthands
  // people and agents actually reach for; --format is the long form. A tool
  // that spells this differently per command costs a guess per command.
  let format: Context['format'] = 'table'
  if (flags.json) format = 'json'
  else if (flags.csv) format = 'csv'
  else if (str('format')) format = str('format') as Context['format']

  // Flag, then environment, then the selected context. The order is the usual
  // one and stated once here rather than per setting.
  //
  // Environment beats the file on purpose: inside a platform's own sandbox the
  // address arrives as an environment variable and the credential is injected
  // on the way out, and that deployment is the only one such a sandbox should
  // reach. A config file it never had cannot redirect it somewhere else.
  const ctx: Context = {
    contextName: selected.name,
    endpoint: str('endpoint', env('AGENTBOX_ENDPOINT') || entry.endpoint || ''),
    apiKey: str('api-key', env('AGENTBOX_API_KEY') || entry.apiKey || ''),
    cluster: str('cluster', env('AGENTBOX_CLUSTER') || entry.cluster || '') || undefined,
    // Only set when the caller insists; otherwise the endpoint's shape decides
    // (`headers()`), because which header the far end reads is a property of
    // the address, not something a person setting up a CLI should have to know.
    authScheme: (str('auth-scheme', env('AGENTBOX_AUTH_SCHEME') || entry.authScheme || '') ||
      undefined) as Context['authScheme'],
    format,
    webBase:
      str('web-base', env('AGENTBOX_WEB_BASE') || entry.webBase || '') || undefined,
  }
  if (!ctx.endpoint) {
    throw new CliError(
      'no endpoint configured',
      `pass --endpoint, set AGENTBOX_ENDPOINT, or write it to ${configPath()}`,
    )
  }
  // A BFF endpoint already names its console one level up, so the links in
  // hints can be built without a second address being configured. Only when the
  // caller gave none AND the shape does not imply one does it stay unset.
  if (!ctx.webBase) ctx.webBase = derivedWebBase(ctx)
  if (!ctx.apiKey) {
    throw new CliError(
      'no API key configured',
      `pass --api-key, set AGENTBOX_API_KEY, or write it to ${configPath()}\nkeys are issued in the console under API keys`,
    )
  }
  return ctx
}

function usage(): string {
  const roots = rootResources()
  const width = Math.max(...roots.map((r) => r.plural.length))
  return [
    'abx — the AgentBox platform CLI.',
    '',
    'Platform concepts live here. Sandboxes themselves are created and driven',
    'with the E2B SDK; these commands address the envs and pools they come from.',
    '',
    'Usage:',
    '  abx <resource> [<id> [<sub> [<sub-id>]]]',
    '  abx <path…> apply -f FILE      # write the desired state',
    '  abx <path…> delete',
    '  abx <path…> scale --replicas N',
    '',
    'Resources:',
    ...roots.map((r) => `  ${r.plural.padEnd(width)}  ${summary(r.describe)}`),
    '',
    'Other commands:',
    '  whoami         who this key authenticates as',
    '  context        name a deployment, and switch between them',
    '  agent-context  the whole CLI shape, as JSON',
    '',
    'Global flags:',
    '  --context <name>     which deployment (see `abx context`)',
    '  --cluster <id>       cluster this command addresses',
    '  --endpoint <url>     API base; a {cluster} placeholder routes by path',
    '  --api-key <key>      platform credential',
    '  --filter key=value   narrow a list; keys are the column headings',
    '  --limit <n>          rows to print (default 200)',
    '  --json | --csv       machine output (drops headers and hints)',
    '  --wide               include the columns held back by default',
  ].join('\n')
}

function resourceHelp(token: string): string {
  const spec = resourceOf(token)
  if (!spec) throw new CliError(`unknown resource "${token}"`, `known: ${ROOT_SEGMENTS.join(', ')}`)
  const out = [`${spec.plural} — ${spec.describe}`, '']
  if (spec.parent) {
    out.push(`Addressed under its ${resourceOf(spec.parent)?.kind}:`, `  abx ${spec.parent} <id> ${spec.plural}`, '')
  }
  if (spec.filters?.length) {
    out.push('Filters (--filter key=value):')
    for (const f of spec.filters) {
      out.push(`  ${f.key}${f.values ? ` (${f.values.join('|')})` : ''} — ${f.describe}`)
    }
    out.push('')
  }
  const subs = [
    ...childrenOf(spec.plural).map((c) => [c.plural, c.describe] as const),
    ...(spec.views ?? []).map((v) => [v.segment, v.describe] as const),
  ]
  if (subs.length) {
    out.push('Sub-resources:', ...subs.map(([s, d]) => `  ${s} — ${summary(d)}`), '')
  }
  if (spec.actions?.length) {
    out.push(
      'Actions:',
      ...spec.actions.map(
        (x) =>
          `  abx ${writeExample(spec)} ${x.name}  # ${x.describe}${
            x.agentForbidden ? ' (never from an agent key)' : ''
          }`,
      ),
      '',
    )
  }
  out.push('Columns:')
  for (const c of visibleColumns(spec, true)) {
    out.push(`  ${c.id}${c.optional ? ' (--wide)' : ''} — ${c.describe}`)
  }
  if (supports(spec, 'apply') || supports(spec, 'delete') || supports(spec, 'create')) {
    out.push(
      '',
      'Writes:',
      ...(supports(spec, 'create')
        ? [`  abx ${spec.parent ? `${spec.parent} <${resourceOf(spec.parent)?.kind}> ` : ''}${spec.plural} apply -f FILE   # create`]
        : []),
      ...(supports(spec, 'apply')
        ? [`  abx ${writeExample(spec)} apply -f FILE   # PUT the whole object; an omitted field is cleared`]
        : []),
      ...(supports(spec, 'delete') ? [`  abx ${writeExample(spec)} delete`] : []),
    )
    if (supports(spec, 'scale')) {
      out.push(`  abx ${writeExample(spec)} scale --replicas N`)
    }
  }
  return out.join('\n')
}

/** Which registry verb a command is asking for, once the address is known. */
function verbIntent(verb: string, collection: boolean): Verb {
  if (verb === 'apply') return collection ? 'create' : 'apply'
  return verb as Verb
}

function writeExample(spec: { plural: string; kind: string; parent?: string }): string {
  return spec.parent
    ? `${spec.parent} <${resourceOf(spec.parent)?.kind}> ${spec.plural} <${spec.kind}>`
    : `${spec.plural} <${spec.kind}>`
}

/** First sentence only. Listings stay scannable; the full text is one --help away. */
function summary(describe: string): string {
  const first = describe.split('. ')[0].replace(/\.$/, '')
  return `${first}.`
}

// The whole verb vocabulary: three that every writable resource shares, plus
// whatever actions the registry declares. Built from the registry so a new
// action is a registry entry rather than an edit here — and so a resource
// cannot quietly grow a verb the help text has never heard of.
const WRITE_VERBS = new Set(['apply', 'delete', 'scale', ...actionNames()])

export async function run(argv: string[]): Promise<number> {
  const { positional, flags, filters } = parseArgs(argv)

  if (!positional.length || flags.help || flags.h) {
    if (positional.length && resourceOf(positional[0])) {
      console.log(resourceHelp(positional[0]))
      return 0
    }
    console.log(usage())
    return 0
  }
  const fileConfig: FileConfig = await readConfig()

  if (positional[0] === 'context' || positional[0] === 'contexts') {
    return await contextCommand(positional.slice(1), flags, fileConfig)
  }

  if (positional[0] === 'version') {
    console.log(VERSION)
    return 0
  }
  if (positional[0] === 'agent-context') {
    // Credentials are optional here: without them the document still describes
    // the whole CLI, it just cannot narrow itself to this deployment.
    let gates: Record<string, boolean> | null = null
    try {
      gates = await request<Record<string, boolean>>(
        contextFrom(flags, fileConfig),
        'GET',
        '/feature-gates',
      )
    } catch {
      gates = null
    }
    console.log(JSON.stringify(agentContext(VERSION, gates), null, 2))
    return 0
  }

  const ctx = contextFrom(flags, fileConfig)

  // Two questions that precede picking a cluster, and must not require one.
  //
  // "Which clusters are there" cannot be answered by first naming one, and an
  // endpoint that routes by path publishes the list one level above its
  // placeholder — so that is where it is asked. `whoami` is about the
  // credential rather than a cluster, but the API that answers it is per
  // cluster, so it borrows a default: the only cluster when there is one, and
  // a refusal naming them when there are several. Guessing between them would
  // report a namespace resolved somewhere the caller did not ask about.
  if (positional[0] === 'clusters' && !ctx.cluster) {
    const listUrl = clusterListUrl(ctx)
    if (listUrl) {
      const rows = normalize(await requestAt<unknown>(listUrl, ctx, 'GET'))
      return printRows(resourceOf('clusters')!, rows, ctx, { resource: 'clusters' }, flags, filters)
    }
  }
  if (!ctx.cluster && routesByPath(ctx)) {
    ctx.cluster = await soleCluster(ctx)
  }

  if (positional[0] === 'whoami') {
    const who = await request<Record<string, unknown>>(ctx, 'GET', '/auth/whoami')
    console.log(ctx.format === 'json' ? JSON.stringify(who) : JSON.stringify(who, null, 2))
    return 0
  }

  // Split the address from a trailing verb. The verb goes last so reading and
  // writing share one address: an agent that just listed something appends a
  // word rather than learning a second grammar.
  const verb = WRITE_VERBS.has(positional[positional.length - 1])
    ? (positional.pop() as string)
    : undefined

  const parsed = parsePositional(positional)
  if (!parsed) {
    throw new CliError(
      `too many arguments: ${positional.join(' ')}`,
      'the deepest address is `<resource> <id> <sub> <sub-id> <view>`',
    )
  }
  const address: Address = { cluster: ctx.cluster, ...parsed }

  const bad = addressError(address)
  if (bad) throw new CliError(bad)
  await assertClusterServed(ctx)

  const resolved = resolveApi(address)
  if (!resolved) {
    // The address is valid — addressError already said so — so the only way to
    // get here is a view this API does not serve. Say which, and where it does
    // live, rather than implying the address was wrong.
    throw new CliError(
      address.view
        ? `"${address.view}" is not served by the API`
        : 'nothing to read at that address',
      address.view && ctx.webBase
        ? `it is a console view:\n  ${ctx.webBase.replace(/\/+$/, '')}${consolePath(address)}`
        : `try \`abx ${address.resource} --help\` for what it does have`,
    )
  }

  if (verb) return await write(ctx, verb, address, resolved, flags)

  const payload = await request<unknown>(ctx, 'GET', resolved.path)
  const rows = normalize(payload, resolved.listField)
  if (ctx.format === 'json' && !resolved.collection) {
    // A `get` answers with the object, not a one-element list.
    console.log(JSON.stringify(rows[0] ?? null))
    return 0
  }
  return printRows(resolved.spec, rows, ctx, address, flags, filters)
}

/**
 * `abx context` — name a deployment once, then switch between them.
 *
 * Deliberately the only stateful thing the CLI does. Everything else reads
 * flags and the API; this writes a file, because the alternative is re-pasting
 * two addresses and a key every time someone moves between platforms, and the
 * failure mode of getting that wrong is a command that succeeds against the
 * wrong one.
 */
async function contextCommand(
  args: string[],
  flags: Record<string, string | boolean>,
  cfg: FileConfig,
): Promise<number> {
  const [verb, name] = args
  const str = (k: string) => (typeof flags[k] === 'string' ? (flags[k] as string) : '')

  if (!verb || verb === 'list') {
    const names = contextNames(cfg)
    if (!names.length) {
      // The addresses belong to the deployment, not to this CLI, so there is
      // nothing sensible to suggest except where to find them.
      console.log(
        [
          'no contexts configured.',
          '',
          'Each deployment\'s console prints the line that adds it — open the',
          'assistant page and look for the setup panel. Or write it yourself:',
          '',
          '  abx context set <name> \\',
          '    --endpoint https://<console>/agentbox/api/clusters/{cluster} \\',
          '    --api-key agbx_...',
          '',
          'That is the whole setup. The auth header and the console links are',
          'read off the endpoint, so neither is a flag. A single-cluster',
          'platform fills in the cluster for you; otherwise any command',
          'takes --cluster <id> (see `abx clusters`).',
        ].join('\n'),
      )
      return 0
    }
    const width = Math.max(...names.map((n) => n.length))
    for (const n of names) {
      const e = cfg.contexts![n]
      const mark = n === cfg.currentContext ? '*' : ' '
      console.log(`${mark} ${n.padEnd(width)}  ${e.endpoint ?? '—'}${e.cluster ? `  (${e.cluster})` : ''}`)
    }
    return 0
  }

  if (verb === 'use') {
    if (!name) throw new CliError('which context?', `configured: ${contextNames(cfg).join(', ')}`)
    if (!cfg.contexts?.[name]) {
      throw new CliError(`no context named "${name}"`, `configured: ${contextNames(cfg).join(', ')}`)
    }
    cfg.currentContext = name
    await writeConfig(cfg)
    console.log(`now using ${name}`)
    return 0
  }

  if (verb === 'set') {
    if (!name) throw new CliError('name the context', 'abx context set <name> --endpoint … --api-key …')
    const entry: ContextEntry = { ...(cfg.contexts?.[name] ?? {}) }
    if (str('endpoint')) entry.endpoint = str('endpoint')
    if (str('api-key')) entry.apiKey = str('api-key')
    if (str('cluster')) entry.cluster = str('cluster')
    if (str('web-base')) entry.webBase = str('web-base')
    if (str('auth-scheme')) entry.authScheme = str('auth-scheme') as ContextEntry['authScheme']
    if (!entry.endpoint) {
      throw new CliError('a context needs an endpoint', 'pass --endpoint')
    }
    cfg.contexts = { ...(cfg.contexts ?? {}), [name]: entry }
    // First one becomes current: a single configured deployment with no default
    // selected would refuse every command for no reason a reader could act on.
    if (!cfg.currentContext) cfg.currentContext = name
    await writeConfig(cfg)
    console.log(`saved ${name}${cfg.currentContext === name ? ' (now current)' : ''} to ${configPath()}`)
    return 0
  }

  if (verb === 'remove' || verb === 'delete') {
    if (!name) throw new CliError('which context?', `configured: ${contextNames(cfg).join(', ')}`)
    if (!cfg.contexts?.[name]) {
      throw new CliError(`no context named "${name}"`, `configured: ${contextNames(cfg).join(', ')}`)
    }
    delete cfg.contexts[name]
    if (cfg.currentContext === name) cfg.currentContext = contextNames(cfg)[0]
    await writeConfig(cfg)
    console.log(`removed ${name}`)
    return 0
  }

  throw new CliError(`unknown context command "${verb}"`, 'try: list, use, set, remove')
}

/**
 * The one cluster this endpoint reaches, when there is exactly one.
 *
 * A deployment with a single cluster should not have to name it on every
 * command; one with several must, because picking for the caller would report
 * one cluster's answer under no label at all. The refusal lists them, so the
 * next command is a copy-paste rather than another lookup.
 *
 * `--cluster` is per command and stays that way: a context names a platform,
 * and a platform has more than one cluster, so a default on the context would
 * silently answer for whichever one happened to be set — the mislabelled-rows
 * failure this whole mechanism exists to avoid.
 */
async function soleCluster(ctx: Context): Promise<string> {
  const listUrl = clusterListUrl(ctx)
  let rows: Record<string, unknown>[] = []
  if (listUrl) {
    try {
      rows = normalize(await requestAt<unknown>(listUrl, ctx, 'GET'))
    } catch {
      rows = []
    }
  }
  const ids = rows.map((c) => String(c.id ?? '')).filter(Boolean)
  if (ids.length === 1) return ids[0]
  throw new CliError(
    ids.length
      ? 'this endpoint reaches several clusters and this command needs one'
      : 'could not work out which cluster to use',
    ids.length
      ? `pass --cluster with one of them:\nreachable: ${ids.join(', ')}`
      : 'pass --cluster <id>; `abx clusters` lists what this endpoint reaches',
  )
}

/** Print a result set in whichever format was asked for. */
function printRows(
  spec: (typeof RESOURCES)[number],
  rows: Record<string, unknown>[],
  ctx: Context,
  address: Address,
  flags: Record<string, string | boolean>,
  filters: [string, string][],
): number {
  const first = spec.columns[0].id
  let out = rows.map((r) => (r !== null && typeof r === 'object' ? r : { [first]: r }))
  out = applyFilters(spec, out, filters)
  if (ctx.format === 'json') {
    console.log(JSON.stringify(out))
    return 0
  }
  if (ctx.format === 'csv') {
    console.log(renderCsv(spec, out, Boolean(flags.wide)))
    return 0
  }
  const limit = typeof flags.limit === 'string' ? Number(flags.limit) : undefined
  console.log(renderTable(spec, out, ctx, { wide: Boolean(flags.wide), limit }))
  console.log('')
  console.log(hints(spec, ctx, address))
  return 0
}

/**
 * Refuse a --cluster this endpoint cannot answer for.
 *
 * Returning the local cluster's rows under another cluster's name is
 * confidently mislabelled data, and a reader has no way to tell. An endpoint
 * whose path carries `{cluster}` routes for itself, so the question does not
 * arise there.
 */
async function assertClusterServed(ctx: Context): Promise<void> {
  if (!ctx.cluster || routesByPath(ctx)) return
  let served: string | undefined
  try {
    const cs = normalize(await request<unknown>(ctx, 'GET', '/clusters'))
    const local = cs.find((c) => c.isLocal === true) ?? (cs.length === 1 ? cs[0] : undefined)
    served = local?.id as string | undefined
  } catch {
    // Never let a diagnostic lookup be the thing that fails the command.
    return
  }
  if (served && served !== ctx.cluster) {
    throw new CliError(
      `this endpoint serves cluster "${served}", not "${ctx.cluster}"`,
      'point --endpoint at that cluster, or use an endpoint whose path contains {cluster}',
    )
  }
}

/**
 * The editable bounds a member currently declares.
 *
 * They live on the Env, not the Pool — `config` under the matching member — so
 * this reads the env rather than the pool it is about to write. An absent bound
 * stays absent: resending it as 0 would invent a floor nobody asked for.
 */
async function currentBounds(
  ctx: Context,
  env: string,
  pool: string,
): Promise<Record<string, unknown>> {
  const envelope = await request<Record<string, any>>(ctx, 'GET', `/envs/${encodeURIComponent(env)}`)
  const spec = (envelope.env ?? envelope)?.spec ?? {}
  for (const cluster of spec.clusters ?? []) {
    for (const m of cluster.members ?? []) {
      if (m.name !== pool) continue
      const cfg = m.config ?? {}
      const out: Record<string, unknown> = {}
      if (cfg.minReplicas !== undefined) out.minReplicas = cfg.minReplicas
      if (cfg.maxReplicas !== undefined) out.maxReplicas = cfg.maxReplicas
      if (cfg.updateStrategy !== undefined) out.updateStrategy = cfg.updateStrategy
      return out
    }
  }
  throw new CliError(
    `"${pool}" is not a member of env "${env}"`,
    `abx envs ${env} pools  # what it does have`,
  )
}

function normalize(payload: unknown, listField?: string): Record<string, unknown>[] {
  if (Array.isArray(payload)) return payload as Record<string, unknown>[]
  const obj = payload as Record<string, unknown>
  if (!obj) return []
  // An explicit field wins over the conventional ones: two lists can share one
  // response, and guessing would silently show whichever came first.
  if (listField) return (obj[listField] as Record<string, unknown>[]) ?? []
  for (const key of [
    'items',
    'envs',
    'pools',
    'sandboxPools',
    'sandboxes',
    'templates',
    'clusters',
    'quotas',
    'instanceTypes',
    'events',
    'groups',
  ]) {
    if (Array.isArray(obj[key])) return obj[key] as Record<string, unknown>[]
  }
  // A single-object response (a get) still renders as one row. Envelopes wrap
  // it under the kind's own name.
  const unwrapped = obj.env ?? obj.pool ?? obj.sandbox ?? obj.template ?? obj.group ?? obj
  return unwrapped ? [unwrapped as Record<string, unknown>] : []
}

/**
 * Refuse, before asking, what an agent key may never do.
 *
 * Not the approval gate — that holds a write for a person to release, and the
 * server runs it. These are the handful of writes that would dissolve the gate
 * itself, so there is no approval to wait for and nothing to queue. The answer
 * is a console link: the person does it as themselves, which is the only way it
 * can honestly happen.
 *
 * whoami is consulted only on this path, so the common write still costs one
 * request.
 */
async function refuseIfAgent(ctx: Context, reason: string, a: Address): Promise<void> {
  let mode: string | undefined
  try {
    mode = (await request<{ mode?: string }>(ctx, 'GET', '/auth/whoami')).mode
  } catch {
    // If we cannot tell, let the server decide rather than blocking a key that
    // may be perfectly entitled.
    return
  }
  if (mode !== 'agent') return
  const where = ctx.webBase
    ? `\ndo it as yourself:\n  ${ctx.webBase.replace(/\/+$/, '')}${consolePath({ cluster: ctx.cluster, resource: a.resource })}`
    : ''
  throw new CliError(`this key may not do that: ${reason}`, `open the console and do it there${where}`)
}

async function write(
  ctx: Context,
  verb: string,
  a: Address,
  resolved: { spec: (typeof RESOURCES)[number]; path: string; collection: boolean },
  flags: Record<string, string | boolean>,
): Promise<number> {
  const { spec, path, collection } = resolved
  const what = a.subId ?? a.id ?? a.resource

  const action = resolveAction(a, verb)
  if (action) {
    if (!a.id) throw new CliError(`${verb} acts on one ${spec.kind}, so an id is required`)
    if (action.action.agentForbidden) {
      await refuseIfAgent(ctx, action.action.agentForbidden, a)
    }
    await request(ctx, action.action.method, action.path, action.action.body)
    console.log(`${verb}d ${what}`)
    return 0
  }
  if (actionNames().includes(verb)) {
    const valid = (spec.actions ?? []).map((x) => x.name)
    throw new CliError(
      `"${spec.plural}" has no "${verb}" action`,
      valid.length ? `it does have: ${valid.join(', ')}` : 'it has no actions',
    )
  }

  const forbidden = spec.api.agentForbidden?.[verbIntent(verb, collection)]
  if (forbidden) await refuseIfAgent(ctx, forbidden, a)

  if (verb === 'delete') {
    if (collection) throw new CliError('delete needs an id', `abx ${writeExample(spec)} delete`)
    if (!supports(spec, 'delete')) throw new CliError(`"${spec.plural}" cannot be deleted`)
    await request(ctx, 'DELETE', path)
    console.log(`deleted ${what}`)
    return 0
  }

  if (verb === 'scale') {
    if (!supports(spec, 'scale')) {
      throw new CliError(
        `"${spec.plural}" has no size to set`,
        'scale applies to pools; a group is sized by its bounds, through `apply -f`',
      )
    }
    if (collection || !a.id || !a.subId) {
      throw new CliError('scale needs a pool', `abx ${writeExample(spec)} scale --replicas N`)
    }
    const n = Number(flags.replicas)
    if (!Number.isInteger(n) || n < 0) {
      throw new CliError(
        'scale needs --replicas <n>',
        'scale changes size and nothing else; to change bounds use `apply -f`',
      )
    }
    // Read-modify-write, deliberately, because the update is whole-object: a
    // PUT carrying only `replicas` would CLEAR the member's bounds and its
    // update strategy, which is the opposite of what "scale" means to anyone
    // who has used kubectl. So the current bounds are read back and resent
    // unchanged, and this command's entire effect is the one number.
    const body = { ...(await currentBounds(ctx, a.id, a.subId)), replicas: n }
    await request(ctx, 'PUT', path, body)
    console.log(`scaled ${what} to ${n}`)
    return 0
  }

  const file = typeof flags.f === 'string' ? flags.f : typeof flags.file === 'string' ? flags.file : ''
  if (!file) {
    throw new CliError('apply needs -f FILE', 'write the desired state as JSON, then `apply -f` it')
  }
  const body = JSON.parse(await Bun.file(file).text())

  if (collection) {
    if (!supports(spec, 'create')) throw new CliError(`"${spec.plural}" cannot be created here`)
    await request(ctx, 'POST', path, body)
    console.log(`created from ${file}`)
    return 0
  }
  if (!supports(spec, 'apply')) throw new CliError(`"${spec.plural}" is read-only`)
  // apply is a PUT and means it: the file is the desired state, and a field it
  // leaves out is one the caller wants gone.
  await request(ctx, 'PUT', path, body)
  console.log(`applied ${file} to ${what}`)
  return 0
}

/**
 * The editable bounds a member currently declares.
 *
 * They live on the Env, not on the Pool: the Pool is the materialised object
 * and the member config is the request that produced it. Reading the Env is
 * therefore the only way to resend those bounds untouched.
 */
async function currentMemberConfig(ctx: Context, a: Address): Promise<Record<string, unknown>> {
  const env = a.sub ? a.id : undefined
  if (!env) return {}
  const payload = await request<Record<string, any>>(ctx, 'GET', `/envs/${encodeURIComponent(env)}`)
  const spec = (payload.env ?? payload).spec ?? {}
  for (const cluster of spec.clusters ?? []) {
    for (const m of cluster.members ?? []) {
      if (m.name !== a.subId) continue
      const c = m.config ?? {}
      const out: Record<string, unknown> = {}
      if (c.minReplicas !== undefined) out.minReplicas = c.minReplicas
      if (c.maxReplicas !== undefined) out.maxReplicas = c.maxReplicas
      if (c.updateStrategy !== undefined) out.updateStrategy = c.updateStrategy
      return out
    }
  }
  return {}
}
