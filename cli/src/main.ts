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
  consoleBase,
  consoleBaseOf,
  viaConsole,
  type Context,
} from './context'
import {
  AmbiguousContextError,
  UnknownContextError,
  cachedRole,
  configPath,
  contextNames,
  readConfig,
  selectContext,
  whoamiCacheKey,
  writeConfig,
  writeWhoamiCache,
  type ContextEntry,
  type FileConfig,
} from './contexts'
import { request, requestAt } from './api'
import { readFileSync } from 'node:fs'
import {
  applyFilters,
  hints,
  renderCsv,
  renderDetail,
  renderLogs,
  renderLogsCsv,
  renderTable,
  visibleColumns,
} from './render'
import { agentContext } from './agent-context'

declare const AGBX_CLI_VERSION: string
const VERSION = typeof AGBX_CLI_VERSION === 'string' ? AGBX_CLI_VERSION : 'dev'

/**
 * The version, and — inside a sandbox image — which build of the image it is.
 *
 * The number alone answers "which abx", which is not the question anyone is
 * actually asking when a sandbox behaves like an older release: the binary and
 * the image it ships in are built separately, and a deployment that upgraded one
 * and not the other looks identical from `abx version`. The image writes
 * `repository:tag` to /opt/abx/IMAGE when it is built, so the answer is one line
 * instead of a kubectl session.
 */
function versionLine(): string {
  try {
    const image = readFileSync('/opt/abx/IMAGE', 'utf8').trim()
    return image ? `${VERSION} (image ${image})` : VERSION
  } catch {
    // Not in an image: a laptop install, a locally built binary. Nothing to add.
    return VERSION
  }
}

interface Parsed {
  positional: string[]
  flags: Record<string, string | boolean>
  filters: [string, string][]
}

/**
 * Every flag the CLI has, and whether it must be given a value.
 *
 * A flag nobody reads is worse than a missing one: `abx envs --nope` used to
 * come back with a normal list of envs, so a caller who mistyped a flag (or
 * followed a stale document) learned that the flag did nothing rather than that
 * it does not exist. Naming the valid set in the refusal is the part that makes
 * it recoverable in one retry.
 */
const FLAGS: Record<string, boolean> = {
  context: true,
  cluster: true,
  endpoint: true,
  'cluster-api': true,
  'api-key': true,
  'auth-scheme': true,
  filter: true,
  limit: true,
  format: true,
  f: true,
  file: true,
  replicas: true,
  json: false,
  editable: false,
  csv: false,
  wide: false,
  version: false,
  help: false,
  h: false,
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
    if (a === '--') {
      positional.push(...argv.slice(i + 1))
      break
    }
    let key = a.replace(/^--?/, '')
    let value: string | undefined
    const eq = key.indexOf('=')
    if (eq !== -1) {
      value = key.slice(eq + 1)
      key = key.slice(0, eq)
    } else if (argv[i + 1] !== undefined && (!argv[i + 1].startsWith('-') || argv[i + 1] === '-')) {
      // A bare `-` is a value, not the start of another flag: it is stdin, and
      // `-f -` is how a caller hands over a document it did not write to disk.
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
  assertFlags(flags)
  return { positional, flags, filters }
}

/** Refuse a flag no command reads, and a flag that was left without its value. */
function assertFlags(flags: Record<string, string | boolean>): void {
  const known = Object.keys(FLAGS).map((f) => (f.length === 1 ? `-${f}` : `--${f}`))
  for (const [key, value] of Object.entries(flags)) {
    if (FLAGS[key] === undefined) {
      throw new CliError(`unknown flag "${key.length === 1 ? '-' : '--'}${key}"`, `known flags: ${known.join(', ')}`)
    }
    // `abx envs --cluster` with nothing after it used to read as "no cluster
    // given" and then fail somewhere else entirely, which is a confusing way to
    // be told about a missing value.
    if (FLAGS[key] && typeof value !== 'string') {
      throw new CliError(`--${key} needs a value`, `for example: --${key} <${key === 'limit' ? 'n' : 'value'}>`)
    }
  }
  const limit = flags.limit
  if (typeof limit === 'string' && !/^\d+$/.test(limit)) {
    throw new CliError(`--limit takes a number, got "${limit}"`, 'it is how many rows to print; the default is 200')
  }
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
  // Normalised, because the previous form of this setting carried the console's
  // mount and routing placeholder — `.../api/clusters/{cluster}` — and a config
  // written then must keep working. `consoleBaseOf` strips exactly that suffix.
  const rawEndpoint = str(
    'endpoint',
    env('AGENTBOX_ENDPOINT') || entry.endpoint || '',
  )
  const ctx: Context = {
    contextName: selected.name,
    endpoint: rawEndpoint ? consoleBaseOf(rawEndpoint) : '',
    // The exception, not the norm: a sandbox in a cluster's own network can be
    // handed this to reach that cluster directly. Everywhere else — including
    // production sandboxes — the console base above is the way in.
    clusterApi:
      str('cluster-api', env('AGENTBOX_CLUSTER_API') || entry.clusterApi || '') ||
      undefined,
    apiKey: str('api-key', env('AGENTBOX_API_KEY') || entry.apiKey || ''),
    cluster: str('cluster', env('AGENTBOX_CLUSTER') || entry.cluster || '') || undefined,
    // Only set when the caller insists; otherwise console vs direct mode decides
    // (`headers()`), because which header the far end reads is a property of
    // the address, not something a person setting up a CLI should have to know.
    authScheme: (str('auth-scheme', env('AGENTBOX_AUTH_SCHEME') || entry.authScheme || '') ||
      undefined) as Context['authScheme'],
    format,
  }
  if (!ctx.endpoint && !ctx.clusterApi) {
    throw new CliError(
      'no endpoint configured',
      `pass --endpoint with your console's address, or set AGENTBOX_ENDPOINT / write it to ${configPath()}`,
    )
  }
  if (!ctx.apiKey) {
    throw new CliError(
      'no API key configured',
      `pass --api-key, set AGENTBOX_API_KEY, or write it to ${configPath()}\nkeys are issued in the console under API keys`,
    )
  }
  return ctx
}

/**
 * What a caller may see, given what their key is.
 *
 * `role` is null whenever it could not be established — no cache, no config,
 * no network. Unknown shows everything on purpose: a help page that hides a
 * command because the CLI failed to reach the API is a help page that lies
 * about what the platform has, and an error the API would have returned anyway
 * is recoverable where a hidden command is not.
 */
function visibleRoots(role: string | null) {
  const roots = rootResources()
  return role === 'admin' || role === null ? roots : roots.filter((r) => !r.admin)
}

function usage(role: string | null): string {
  const roots = visibleRoots(role)
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
    "  --endpoint <url>     the console's address; reaches every cluster",
    '  --cluster-api <url>  one cluster\'s own API, bypassing the console',
    '  --api-key <key>      platform credential',
    '  --filter key=value   narrow a list; keys are the column headings',
    '  --limit <n>          rows to print (default 200)',
    '  --editable           print the body a write takes, not the object',
    '  --json | --csv       machine output (drops headers and hints)',
    '  --wide               include the columns held back by default',
    '  --version            print the CLI version',
  ].join('\n')
}

function resourceHelp(spec: (typeof RESOURCES)[number]): string {
  const out = [`${spec.plural} — ${spec.describe}`, '']
  if (spec.helpNote) out.push(spec.helpNote, '')
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
  if (spec.detailFields?.length) {
    // A get is not a row of the list, and the fields it prints are not the
    // columns. Saying so here is what stops a reader from concluding that the
    // list is all there is.
    out.push('', `Detail (abx ${writeExample(spec)}) — the shape a get prints:`)
    for (const f of spec.detailFields) {
      out.push(`  ${f.id}${f.text ? ' (full text)' : ''} — ${f.describe}`)
    }
  }
  if (supports(spec, 'apply') || supports(spec, 'delete') || supports(spec, 'create')) {
    out.push(
      '',
      'Writes:',
      ...(supports(spec, 'create')
        ? [`  abx ${spec.parent ? `${spec.parent} <${resourceOf(spec.parent)?.kind}> ` : ''}${spec.plural} apply -f FILE   # create`]
        : []),
      ...(supports(spec, 'apply')
        ? [`  abx ${writeExample(spec)} apply -f FILE   # ${bodySummary(spec)}`]
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

/**
 * What `apply -f` expects the file to contain.
 *
 * The trap this closes: an update body is NOT the object `--json` prints. For
 * an env the PUT body is `overrides` alone, so copying the full object out of
 * `--json` and applying it clears every override the env had — a foot-gun that
 * cost nothing to describe here and would cost a caller their state to find out
 * about. Server-exported request bodies replace this when they land.
 */
function bodyShape(spec: { plural: string; parent?: string }): string {
  switch (spec.plural) {
    case 'envs':
      return 'the file is the body `abx envs <env> --json --editable` prints. Create and update take the SAME shape, and the fields fixed at create (`templateRef`, `mode`, `labels`, `annotations`) have to come back unchanged — an update that drops one is refused rather than quietly kept. The whole object `--json` prints is NOT that body.'
    case 'pools':
      return 'the file is the body `abx envs <env> pools <pool> --json --editable` prints: the size, its bounds, and the resource shape the pool was created with (`instanceType`/`multiplier`/`inlineResources`), which is fixed and must come back unchanged.'
    case 'scaling-groups':
      return 'the file is `{"enabled": bool, "minReplicas": n, "maxReplicas": n, "scaleUpPolicy": {…}, "scaleDownPolicy": {…}}`.'
    case 'templates':
    case 'admin-templates':
      return 'the file is `{"crdJson": "…"}` — the whole SandboxTemplate object as a JSON string.'
    default:
      return `the file is the JSON request body for ${spec.plural}; \`abx ${spec.parent ?? spec.plural} --help\` names the fields.`
  }
}

/** The same thing in one line, for places that have one line to give. */
function bodySummary(spec: { plural: string }): string {
  switch (spec.plural) {
    case 'envs':
      return 'PUTs the whole writable shape — read it with `--json --editable` first'
    case 'pools':
      return 'PUTs the size, its bounds and the fixed resource shape — read it with `--json --editable`'
    case 'scaling-groups':
      return 'PUTs the group’s bounds and policies'
    case 'templates':
    case 'admin-templates':
      return 'PUTs `{crdJson}` — the whole template as a JSON string'
    default:
      return 'PUT the whole object; an omitted field is cleared'
  }
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

/**
 * Verbs this CLI used to have, and what to do instead.
 *
 * Every one of these appears in a document somewhere in this repository, and
 * the failure they produce on their own is a 404 for an object named "create" —
 * which tells the caller nothing about the spelling that works. Only consulted
 * when the caller is clearly trying to write, so an env that really is called
 * `create` stays readable.
 */
const REMOVED_VERBS: Record<string, string> = {
  create: 'creating is `apply -f FILE` against the collection — the file IS the request body',
  update: 'updating is `apply -f FILE` against the item',
  patch:
    'there is no partial apply: `apply -f FILE` sends the whole object, and a field the file omits is cleared',
  post: 'creating is `apply -f FILE` against the collection',
}

/** How long the help path will wait for a whoami before showing everything. */
const HELP_WHOAMI_TIMEOUT_MS = 3000

export async function run(argv: string[]): Promise<number> {
  const { positional, flags, filters } = parseArgs(argv)

  // `--version` and `version` are the same question, and neither should print
  // a usage page: a version is what a bug report needs, and a caller who asked
  // for one and got a wall of prose has to guess whether the answer was in it.
  if (flags.version || (positional.length === 1 && positional[0] === 'version')) {
    console.log(versionLine())
    return 0
  }

  const fileConfig: FileConfig = await readConfig()

  if (!positional.length || flags.help || flags.h) {
    // What to advertise depends on who is asking, and the answer is cached
    // beside the config. A help page must not fail because the network is slow,
    // so anything unknown falls back to showing everything.
    const role = await roleForHelp(fileConfig, flags)
    const spec = positional.length ? resourceOf(positional[0]) : undefined
    if (spec) {
      console.log(resourceHelp(spec))
      return 0
    }
    console.log(usage(role))
    return 0
  }

  if (positional[0] === 'context' || positional[0] === 'contexts') {
    return await contextCommand(positional.slice(1), flags, fileConfig)
  }

  if (positional[0] === 'agent-context') {
    // Credentials are optional here: without them the document still describes
    // the whole CLI, it just cannot narrow itself to this deployment — neither
    // by its feature gates nor by the caller's role.
    let gates: Record<string, boolean> | null = null
    let role: string | null = await roleForHelp(fileConfig, flags)
    try {
      const ctx = contextFrom(flags, fileConfig)
      gates = await request<Record<string, boolean>>(ctx, 'GET', '/feature-gates')
      role = await roleForHelp(fileConfig, flags, ctx) ?? role
    } catch {
      gates = null
    }
    console.log(JSON.stringify(agentContext(VERSION, gates, role), null, 2))
    return 0
  }

  // Whoami answers about the credential, not about a cluster: the same key has
  // the same role and team on every cluster the platform reaches, and only the
  // namespace it resolves to is per cluster. Asking for one anyway reads as the
  // tool being broken, so it takes the first reachable cluster and says nothing
  // about which — there is nothing the choice changes that the caller could act
  // on.
  if (positional[0] === 'whoami') {
    const ctx = contextFrom(flags, fileConfig)
    const who = await whoami(ctx)
    await recordWhoami(ctx, who)
    console.log(ctx.format === 'json' ? JSON.stringify(who) : JSON.stringify(who, null, 2))
    return 0
  }

  // Split the address from a trailing verb. The verb goes last so reading and
  // writing share one address: an agent that just listed something appends a
  // word rather than learning a second grammar.
  const tail = positional[positional.length - 1]
  if (REMOVED_VERBS[tail] && (flags.f !== undefined || flags.file !== undefined)) {
    throw new CliError(`"${tail}" is not a verb any more`, REMOVED_VERBS[tail])
  }
  const verb = WRITE_VERBS.has(tail) ? (positional.pop() as string) : undefined

  const parsed = parsePositional(positional)
  if (!parsed) {
    throw new CliError(
      `too many arguments: ${positional.join(' ')}`,
      'the deepest address is `<resource> <id> <sub> <sub-id> <view>`',
    )
  }

  // The address is checked before anything about the deployment is, so a
  // mistyped resource is answered with "unknown resource" rather than with
  // advice about a cluster — or a missing endpoint — it was never going to
  // reach. Nothing below this line can run without a valid address.
  const bad = addressError(parsed)
  if (bad) throw new CliError(bad)

  const ctx = contextFrom(flags, fileConfig)

  // "Which clusters are there" cannot be answered by first naming one, and an
  // endpoint that routes by path publishes the list one level above its
  // placeholder — so that is where it is asked, and `abx clusters` therefore
  // needs no --cluster at all.
  if (parsed.resource === 'clusters' && !ctx.cluster) {
    const listUrl = clusterListUrl(ctx)
    if (listUrl) {
      const rows = normalize(await requestAt<unknown>(listUrl, ctx, 'GET'))
      return printRows(resourceOf('clusters')!, rows, ctx, { resource: 'clusters' }, flags, filters)
    }
  }
  if (!ctx.cluster && viaConsole(ctx)) {
    ctx.cluster = await soleCluster(ctx)
  }

  const address: Address = { cluster: ctx.cluster, ...parsed }
  await assertClusterServed(ctx)

  if (filters.length && (address.id || address.subId)) {
    // A filter narrows a list — that is what it is for, and `abx envs
    // --filter mode=WarmPool` is the common case. On a get it silently
    // filtered the one row away and printed "0 total", which reads as an
    // empty platform rather than a filter that cannot apply here.
    throw new CliError(
      '--filter narrows a list, and this address is one object',
      `drop the filter, or narrow \`abx ${address.resource}\` instead`,
    )
  }

  const resolved = resolveApi(address)
  if (!resolved) {
    // The address is valid — addressError already said so — so the only way to
    // get here is a view this API does not serve. Say which, and where it does
    // live, rather than implying the address was wrong.
    throw new CliError(
      address.view
        ? `"${address.view}" is not served by the API`
        : 'nothing to read at that address',
      address.view && consoleBase(ctx)
        ? `it is a console view:\n  ${consoleBase(ctx)}${consolePath(address)}`
        : deeperHint(address) ?? `try \`abx ${address.resource} --help\` for what it does have`,
    )
  }

  if (verb) return await write(ctx, verb, address, resolved, flags)

  if (flags.editable && resolved.collection) {
    // Refused before the request: the answer is about the address, not about
    // what the server happens to have.
    throw new CliError(
      '--editable prints what ONE object takes',
      `the body belongs to one ${resolved.spec.kind}; name it, or use --json for the list`,
    )
  }

  const payload = await request<unknown>(ctx, 'GET', resolved.path)

  if (flags.editable) return printEditable(payload, resolved, ctx)

  const rows = normalize(payload, resolved.listField)
  if (resolved.collection) return printRows(resolved.spec, rows, ctx, address, flags, filters)

  if (resolved.view) return printView(resolved.view, payload, ctx, address)

  if (ctx.format === 'json') {
    // A `get` answers with the object, not a one-element list.
    console.log(JSON.stringify(stripEnvDocs(resolved.spec, rows[0] ?? null)))
    return 0
  }
  // A one-row CSV is what `--csv` asks for, and the detail view is not that:
  // it is a page for a person, and the flat shape is for a spreadsheet.
  if (ctx.format === 'csv') return printRows(resolved.spec, rows, ctx, address, flags, filters)
  if (!resolved.spec.detailFields?.length) return printRows(resolved.spec, rows, ctx, address, flags, filters)
  return printDetail(resolved.spec, rows[0] ?? {}, ctx, address, flags)
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
          '    --endpoint https://<console>/agentbox \\',
          '    --api-key agbx_...',
          '',
          'That is the whole setup: the console address and the key. One',
          'address reaches every cluster — a single-cluster platform fills it',
          'in for you, otherwise any command takes --cluster <id> (see',
          '`abx clusters`). The auth header follows from the address, so it',
          'is not a flag.',
        ].join('\n'),
      )
      return 0
    }
    const width = Math.max(...names.map((n) => n.length))
    for (const n of names) {
      const e = cfg.contexts![n]
      const mark = n === cfg.currentContext ? '*' : ' '
      // A direct-mode context has no console address, so show the cluster API
      // it does have — and say which it is, because the two behave differently
      // (one reaches every cluster, the other refuses --cluster).
      const addr = e.clusterApi ? `${e.clusterApi}  [direct]` : (e.endpoint ?? '—')
      console.log(`${mark} ${n.padEnd(width)}  ${addr}${e.cluster ? `  (${e.cluster})` : ''}`)
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
    // Stored as the console's base. An older config may hold the mount and the
    // routing placeholder too; normalising on the way in means the file is
    // rewritten into today's shape the first time it is touched.
    if (str('endpoint')) entry.endpoint = consoleBaseOf(str('endpoint'))
    if (str('cluster-api')) entry.clusterApi = str('cluster-api')
    if (str('api-key')) entry.apiKey = str('api-key')
    if (str('cluster')) entry.cluster = str('cluster')
    if (str('auth-scheme')) entry.authScheme = str('auth-scheme') as ContextEntry['authScheme']
    if (!entry.endpoint && !entry.clusterApi) {
      throw new CliError(
        'a context needs an address',
        "pass --endpoint with your console's address (--cluster-api only to bypass the console)",
      )
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
 * The one cluster the console reaches, when there is exactly one.
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
      ? 'this platform has several clusters and this command needs one'
      : 'could not work out which cluster to use',
    ids.length
      ? `pass --cluster with one of them:\nreachable: ${ids.join(', ')}`
      : 'pass --cluster <id>; `abx clusters` lists what this deployment reaches',
  )
}

/**
 * The first cluster this deployment reaches.
 *
 * Used only by `whoami`, where the choice genuinely does not matter: a
 * credential's role, mode, team and user are the same on every cluster, and the
 * one field that is per cluster — the namespace it resolves to — is reported
 * rather than selected on. Asking for `--cluster` first made the command read
 * as broken, which is what the deployment's own chart comment says about it.
 */
async function firstCluster(ctx: Context): Promise<string> {
  const listUrl = clusterListUrl(ctx)
  const rows = listUrl ? normalize(await requestAt<unknown>(listUrl, ctx, 'GET')) : []
  const id = rows.map((c) => String(c.id ?? '')).filter(Boolean)[0]
  if (!id) {
    throw new CliError(
      'this platform reports no clusters',
      'nothing is reachable behind this address yet',
    )
  }
  return id
}

/** Who this credential is, asked of a cluster it can reach. */
async function whoami(ctx: Context): Promise<Record<string, unknown>> {
  if (!ctx.cluster && viaConsole(ctx)) {
    return request<Record<string, unknown>>(
      { ...ctx, cluster: await firstCluster(ctx) },
      'GET',
      '/auth/whoami',
    )
  }
  return request<Record<string, unknown>>(ctx, 'GET', '/auth/whoami')
}

/** Remember what whoami said, so the help page can be decided offline. */
async function recordWhoami(ctx: Context, who: Record<string, unknown>): Promise<void> {
  const str = (k: string) => (typeof who[k] === 'string' ? (who[k] as string) : undefined)
  await writeWhoamiCache(whoamiCacheKey(ctx.contextName, ctx.endpoint || ctx.clusterApi || '', ctx.apiKey), {
    role: str('role'),
    mode: str('mode'),
    user: str('user'),
    team: str('team'),
    at: new Date().toISOString(),
  })
}

/**
 * What role to render help for, best effort and never fatal.
 *
 * The cache first, because that is the only answer available without a network;
 * a live whoami when there is no cache; and null — which shows everything —
 * when neither works. Hiding a resource because the CLI could not reach the API
 * would be the help page inventing a platform that has less in it than the real
 * one, and the error the API would give is one the caller can act on.
 */
async function roleForHelp(
  cfg: FileConfig,
  flags: Record<string, string | boolean>,
  known?: Context,
): Promise<string | null> {
  // The cache is keyed on the credential, so resolving the context (and with it
  // the address and key) comes first. An unknown or ambiguous context is
  // somebody else's error to report, so a failure here just means "no cache".
  let ctx: Context | undefined = known
  if (!ctx) {
    try {
      ctx = contextFrom(flags, cfg)
    } catch {
      ctx = undefined
    }
  }
  if (ctx) {
    const key = whoamiCacheKey(ctx.contextName, ctx.endpoint || ctx.clusterApi || '', ctx.apiKey)
    const cached = await cachedRole(key)
    if (cached) return cached
  }
  try {
    ctx = ctx ?? contextFrom(flags, cfg)
    const who = await withTimeout(whoami(ctx), HELP_WHOAMI_TIMEOUT_MS)
    await recordWhoami(ctx, who)
    return typeof who.role === 'string' ? who.role : null
  } catch {
    return null
  }
}

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`timed out after ${ms}ms`)), ms)
    p.then(
      (v) => {
        clearTimeout(timer)
        resolve(v)
      },
      (e) => {
        clearTimeout(timer)
        reject(e)
      },
    )
  })
}

/** Where a caller who named a resource that is only a parent should go next. */
function deeperHint(a: Address): string | undefined {
  const spec = resourceOf(a.resource)
  if (!spec || !a.id) return undefined
  const kids = childrenOf(spec.plural)
  if (!kids.length) return undefined
  // `abx teams team1` used to answer "nothing to read at that address", which
  // is true and useless: the thing to read is one level further down.
  return [
    'that address itself holds nothing — go one level deeper:',
    ...kids.map((c) => `  abx ${a.resource} ${a.id} ${c.plural}`),
  ].join('\n')
}

/**
 * `--editable` — the body a write takes, rather than the object it produces.
 *
 * These are two different documents and treating them as one is a trap that
 * bites silently: `abx envs X --json` returns the whole resource, and feeding
 * that back to `apply -f` asks the API to clear every field the file does not
 * carry — which for an env means every override. The projection printed here
 * comes from the server (the same schema the PUT accepts), so this output is
 * exactly what `apply -f` takes.
 */
function printEditable(
  payload: unknown,
  resolved: { spec: (typeof RESOURCES)[number]; collection: boolean },
  ctx: Context,
): number {
  const editable = (payload as Record<string, unknown> | null)?.['editable']
  if (editable === undefined || editable === null) {
    throw new CliError(
      'this deployment returns no editable body for that object',
      'the field arrives with the write contract that pairs create and update.\n' +
        'until this API has it, --json is the raw object — and the raw object is NOT\n' +
        'the file `apply -f` takes: sending it clears what it leaves out',
    )
  }
  console.log(ctx.format === 'json' ? JSON.stringify(editable) : JSON.stringify(editable, null, 2))
  return 0
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
 * Print one object in the shape the registry says a get has.
 *
 * The child tables cost two extra requests (the env's pools and its autoscaling
 * groups), which is why they are only fetched for the table view: `--json` is a
 * promise about the response, and making it fetch three of them would keep that
 * promise with a different document than the one asked for.
 */
async function printDetail(
  spec: (typeof RESOURCES)[number],
  item: Record<string, unknown>,
  ctx: Context,
  address: Address,
  flags: Record<string, string | boolean>,
): Promise<number> {
  const extras: { spec: (typeof RESOURCES)[number]; rows: Record<string, unknown>[] }[] = []
  for (const child of childrenOf(spec.plural)) {
    if (!child.inParentDetail) continue
    const resolved = resolveApi({ ...address, sub: child.plural, subId: undefined, view: undefined })
    if (!resolved) continue
    const rows = normalize(await request<unknown>(ctx, 'GET', resolved.path), resolved.listField)
    extras.push({ spec: child, rows })
  }
  console.log(renderDetail(spec, item, ctx, address, extras))
  return 0
}

/** A view — logs today — which is a snapshot rather than a row set. */
function printView(
  view: { segment: string; shape?: 'logs' },
  payload: unknown,
  ctx: Context,
  address: Address,
): number {
  if (ctx.format === 'json') {
    console.log(JSON.stringify(payload))
    return 0
  }
  if (ctx.format === 'csv') {
    console.log(renderLogsCsv(payload))
    return 0
  }
  console.log(view.shape === 'logs' ? renderLogs(payload, ctx, address) : JSON.stringify(payload, null, 2))
  return 0
}

/**
 * Drop the one field that is not safe to hand back, even as JSON.
 *
 * `envDocs` is Markdown rendered with the caller's own plaintext API key
 * substituted in, so printing it puts a credential into whatever reads the
 * output. The template's own docs are the same text with a placeholder instead,
 * which is why they are safe to print — and why the note points there.
 */
function stripEnvDocs(spec: { plural: string }, obj: unknown): unknown {
  if (spec.plural !== 'envs' || !obj || typeof obj !== 'object') return obj
  const row = obj as Record<string, unknown>
  if (!('envDocs' in row)) return obj
  const { envDocs: _omitted, ...rest } = row
  console.error(
    'note: envDocs is omitted, even from --json — the server renders it with your own\n' +
      '      plaintext API key substituted in. `abx templates <name>` prints the same\n' +
      '      documentation with a placeholder where the key goes.',
  )
  return rest
}

/**
 * Refuse a --cluster this address cannot answer for.
 *
 * Returning the local cluster's rows under another cluster's name is
 * confidently mislabelled data, and a reader has no way to tell. The console
 * routes to every cluster, so the question only arises in direct mode, where
 * the address answers for exactly one.
 */
async function assertClusterServed(ctx: Context): Promise<void> {
  if (!ctx.cluster || viaConsole(ctx)) return
  let served: string | undefined
  try {
    const cs = normalize(await request<unknown>(ctx, 'GET', '/clusters'))
    // `local`, not `isLocal`: the field this API sends is spelled without the
    // prefix (ClusterSummary, required [id, local]). Reading the wrong name
    // made the guard inert for every deployment that has more than one cluster
    // in its routing table, which is where it matters — the answer then came
    // from THIS cluster under the other one's name.
    const local = cs.find((c) => c.local === true) ?? (cs.length === 1 ? cs[0] : undefined)
    served = local?.id as string | undefined
  } catch {
    // Never let a diagnostic lookup be the thing that fails the command.
    return
  }
  if (served && served !== ctx.cluster) {
    throw new CliError(
      `this address serves cluster "${served}", not "${ctx.cluster}"`,
      'it is a single cluster\'s API. To reach several clusters, point --endpoint ' +
        "at the console instead; one address reaches them all. If this sandbox has " +
        'no route to the console, "' +
        served +
        '" is the only cluster it can reach.',
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
  // The write body first, and the env's member config only as a fallback.
  //
  // `scale` puts this back on the wire unchanged, and the fields a PUT must
  // carry now include the ones the read shape does not show at all
  // (`instanceType`, `multiplier`, the labels the server stamped on create), so
  // reading the env's projection is what made `scale` a 400 on a server that
  // enforces the contract. The member's own `editable` is that same body, which
  // is what it exists for. The fallback covers a server too old to project it,
  // where the narrower body is still accepted.
  const editable = await poolEditable(ctx, env, pool)
  if (editable) return editable
  return await memberBounds(ctx, env, pool)
}

/** The body a write to this member takes, as the server projects it. */
async function poolEditable(
  ctx: Context,
  env: string,
  pool: string,
): Promise<Record<string, unknown> | null> {
  const envelope = await request<Record<string, any>>(
    ctx,
    'GET',
    `/envs/${encodeURIComponent(env)}/sandboxpools/${encodeURIComponent(pool)}`,
  )
  const editable = envelope?.editable
  if (!editable || typeof editable !== 'object') return null
  // Everything but the number being set, so the PUT carries the fixed half back
  // byte for byte.
  const { replicas: _replicas, ...rest } = editable as Record<string, unknown>
  return rest
}

async function memberBounds(
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
  const web = consoleBase(ctx)
  const where = web
    ? `\ndo it as yourself:\n  ${web}${consolePath({ cluster: ctx.cluster, resource: a.resource })}`
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
    throw new CliError(
      'apply needs -f FILE',
      bodyShape(spec) + `\n\`-f -\` reads the document from stdin instead.`,
    )
  }
  // A bare `-` is stdin: the document a caller built in a pipeline never has to
  // be spilled to a file (and a file literally named `-` is not a thing anyone
  // means to name).
  // YAML is refused by NAME rather than by what the parser says about it: the
  // API takes JSON, an agent that writes YAML has made a decision about format
  // rather than a syntax slip, and "Unexpected identifier" does not tell it
  // which of the two the CLI wanted.
  if (file !== '-' && /\.(ya?ml)$/i.test(file)) {
    throw new CliError(
      `this CLI takes JSON, not YAML (${file})`,
      'the body is a JSON document — the same one the API takes. Convert it first ' +
        '(for example `yq -o=json`, or write it out with --json) and apply that.',
    )
  }
  const body = JSON.parse(file === '-' ? await Bun.stdin.text() : await Bun.file(file).text())

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
