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

import type { ColumnSpec, ResourceSpec } from '@headless/index'
import type { Address } from '@headless/index'
import { childrenOf, cliArgs, consolePath, hasConsolePage } from '@headless/index'
import { CliError, consoleBase, type Context } from './context'

/** Rows past this are not printed. Context is not free and nobody reads 8000 rows. */
const DEFAULT_LIMIT = 200

/** Read a dot path out of a response row. */
function at(row: Record<string, unknown>, path: string): unknown {
  let cur: unknown = row
  for (const part of path.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined
    cur = (cur as Record<string, unknown>)[part]
  }
  return cur
}

/**
 * One cell.
 *
 * Objects flatten to `k=v` pairs rather than JSON, because a column is one line
 * and `{"cpu":"15"}` costs more width than it carries. Nesting flattens too —
 * a cell that reads `[object Object]` is a cell that wasted its row.
 */
function format(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  if (Array.isArray(v)) return v.length ? v.map(format).join(',') : '—'
  if (typeof v === 'object') {
    const entries = Object.entries(v as Record<string, unknown>)
    // An empty map is absence, and reads as absence. Printing nothing leaves a
    // blank cell that looks like a rendering bug rather than a zero count.
    if (!entries.length) return '—'
    return entries.map(([k, vv]) => `${k}=${format(vv)}`).join(' ')
  }
  return String(v)
}

const cell = (row: Record<string, unknown>, col: ColumnSpec | string): string =>
  format(typeof col === 'string' ? at(row, col) : at(row, col.path ?? col.id))

/**
 * Which columns to print.
 *
 * The default view is a summary, not the whole row: optional columns are held
 * back until asked for, so the common case stays narrow enough to read and
 * cheap enough to send.
 */
export function visibleColumns(spec: ResourceSpec, wide: boolean): readonly ColumnSpec[] {
  return wide ? spec.columns : spec.columns.filter((c) => !c.optional)
}

export function renderTable(
  spec: ResourceSpec,
  rows: Record<string, unknown>[],
  ctx: Context,
  opts: { wide?: boolean; limit?: number } = {},
): string {
  const cols = visibleColumns(spec, opts.wide ?? false)
  const limit = opts.limit ?? DEFAULT_LIMIT
  const shown = rows.slice(0, limit)
  const out: string[] = []

  const scope = ctx.cluster ? ` · ${ctx.cluster}` : ''
  const count =
    rows.length > shown.length
      ? `${rows.length} total · rows 1–${shown.length}`
      : `${rows.length} total`
  out.push(`${spec.kind}${scope} · ${count}`, '')

  // The header IS the column id, which is also the --filter key. One name.
  const header = cols.map((c) => c.id)
  const widths = header.map((h, i) =>
    Math.max(h.length, ...shown.map((r) => cell(r, cols[i]).length), 0),
  )
  const line = (vals: string[]) => vals.map((v, i) => v.padEnd(widths[i])).join('  ').trimEnd()
  out.push(line(header))
  for (const r of shown) out.push(line(cols.map((c) => cell(r, c))))

  if (rows.length > shown.length) {
    const keys = (spec.filters ?? []).map((f) => f.key).join('|')
    out.push(
      '',
      `… ${rows.length - shown.length} more — re-run with --limit, or narrow instead of paging:`,
      keys ? `  --filter ${keys}` : '  (this resource declares no filters)',
    )
  }
  return out.join('\n')
}

export function renderCsv(
  spec: ResourceSpec,
  rows: Record<string, unknown>[],
  wide: boolean,
): string {
  const cols = visibleColumns(spec, wide)
  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s)
  return [
    cols.map((c) => c.id).join(','),
    ...rows.map((r) => cols.map((c) => esc(cell(r, c))).join(',')),
  ].join('\n')
}

/**
 * One object, as the thing it is.
 *
 * A get is not a one-row list, and rendering it with the list's columns is how
 * `abx envs demo-env` came back with `templateName`, `mode` and every
 * replica count printed as `—`: those fields are in the response, under
 * `spec`/`status`, and the list's projection never looks there. The registry
 * declares the get's own shape for exactly this reason.
 *
 * Text fields (a template's docs, a pool's pod-template YAML) print whole, with
 * one line saying how much of them there is — the places that truncate are the
 * ones that know what they are truncating for.
 */
export function renderDetail(
  spec: ResourceSpec,
  item: Record<string, unknown>,
  ctx: Context,
  a: Address,
  extras: { spec: ResourceSpec; rows: Record<string, unknown>[] }[] = [],
): string {
  const title = [spec.kind, ctx.cluster, a.sub ? a.subId : a.id].filter(Boolean).join(' · ')
  const out: string[] = [title, '']

  const inline: [string, string][] = []
  const blocks: [string, string][] = []
  for (const f of spec.detailFields ?? []) {
    const raw = at(item, f.path ?? f.id)
    if (f.text) {
      const body = raw === undefined || raw === null ? '' : String(raw)
      blocks.push([`${f.id} (${body.length} chars)`, body])
      continue
    }
    inline.push([f.id, format(raw)])
  }

  const width = Math.max(0, ...inline.map(([k]) => k.length))
  for (const [k, v] of inline) out.push(`${k.padEnd(width)}  ${v}`)

  for (const [head, body] of blocks) {
    if (out[out.length - 1] !== '') out.push('')
    out.push(head, '', body)
  }

  for (const child of extras) {
    // The child list is already a table with its own heading and count; the
    // blank line is what separates it from the fields above rather than making
    // it read as one more field.
    out.push('', renderTable(child.spec, child.rows, ctx))
  }

  out.push('', hints(spec, ctx, a))
  return out.join('\n')
}

/**
 * A log snapshot, which is not a row set and must not be rendered as one.
 *
 * `GET /sandboxes/{id}/logs` answers with the container list, where the lines
 * came from and how complete they are — all of which are the difference between
 * "the sandbox printed nothing" and "the wrong thing was asked", a distinction
 * the API goes out of its way to preserve and a table throws away.
 */
export function renderLogs(payload: unknown, ctx: Context, a: Address): string {
  const r = (payload ?? {}) as Record<string, unknown>
  const entries = Array.isArray(r.entries) ? (r.entries as Record<string, unknown>[]) : []
  const out: string[] = [`logs · ${a.id ?? a.subId ?? ''}`, '']

  const meta: [string, string][] = []
  const containers = Array.isArray(r.containers) ? r.containers.map(format) : []
  if (containers.length) meta.push(['containers', containers.join(', ')])
  if (r.source !== undefined) meta.push(['source', format(r.source)])
  if (r.namespace !== undefined) meta.push(['namespace', format(r.namespace)])
  if (r.podName !== undefined) meta.push(['pod', format(r.podName)])
  if (r.capturedAt !== undefined) meta.push(['capturedAt', format(r.capturedAt)])
  if (r.totalBytes !== undefined) meta.push(['totalBytes', format(r.totalBytes)])
  if (r.truncated !== undefined) meta.push(['truncated', r.truncated ? 'yes' : 'no'])
  // Empty for every source but `central`, and the only way to tell an empty
  // result from a mis-scoped query there.
  if (r.scope !== undefined && r.scope !== '') meta.push(['scope', format(r.scope)])

  const width = Math.max(0, ...meta.map(([k]) => k.length))
  for (const [k, v] of meta) out.push(`${k.padEnd(width)}  ${v}`)

  if (!entries.length) {
    out.push('', 'no log lines in this snapshot')
    return out.join('\n')
  }
  out.push('')
  for (const e of entries) {
    const line = String(e.log ?? '')
    const container = e.container === undefined ? '' : String(e.container)
    const ts = e.timestamp === undefined ? '' : format(e.timestamp)
    out.push(ts ? `${ts}  [${container}] ${line}` : `[${container}] ${line}`)
  }
  return out.join('\n')
}

/** The same snapshot as CSV, for the callers that asked for one. */
export function renderLogsCsv(payload: unknown): string {
  const r = (payload ?? {}) as Record<string, unknown>
  const entries = Array.isArray(r.entries) ? (r.entries as Record<string, unknown>[]) : []
  // An absent field is an absent column, not a dash: `—` is how the table view
  // says "no value", and in a CSV it would be read as the literal text.
  const plain = (v: unknown) => (v === null || v === undefined ? '' : format(v))
  const esc = (s: string) => (/[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s)
  return [
    'timestamp,container,log',
    ...entries.map((e) => [esc(plain(e.timestamp)), esc(plain(e.container)), esc(String(e.log ?? ''))].join(',')),
  ].join('\n')
}

/**
 * What to do next, attached to what just happened.
 *
 * Three kinds: go deeper, go sideways to a related resource, or ask what the
 * filters are. Strung together across commands these are a small domain graph —
 * the output is the graph and the hints are its edges — which is the cheapest
 * way to give an agent the associations a person would have built by clicking
 * around. An agent that is not told what it can do next guesses, and a guess
 * costs a whole round trip.
 */
export function hints(spec: ResourceSpec, ctx: Context, a: Address, note?: string): string {
  const cl = ctx.cluster ? ` --cluster ${ctx.cluster}` : ''
  const prefix = cliArgs({ ...a, cluster: undefined }).join(' ')
  const lines: string[] = []

  const addressingItem = a.sub ? Boolean(a.subId) : Boolean(a.id)
  if (a.view) return viewHint(ctx, a)
  // Something the caller cannot see from the rows they just read — today, only
  // the env's pool sizing, which decides what a `create` under this address
  // has to contain. First, because it changes what the lines below mean.
  if (note) lines.push(`  # ${note}`)
  if (!addressingItem && spec.detail) {
    lines.push(`  abx ${prefix} <${spec.kind}>${cl}  # detail of one row`)
  }
  if (addressingItem) {
    for (const c of childrenOf(spec.plural)) {
      lines.push(`  abx ${prefix} ${c.plural}${cl}  # ${c.describe}`)
    }
    // Only views this CLI can actually serve. A view that lives in the console
    // (metrics come from Prometheus, not this API) is reachable through the
    // `view:` link above; advertising it as a command would be advertising an
    // error.
    for (const v of spec.views ?? []) {
      if (v.api) lines.push(`  abx ${prefix} ${v.segment}${cl}  # ${v.describe}`)
    }
  }
  lines.push(`  abx ${spec.plural} --help  # filters / columns`)

  const parts: string[] = []
  // Only when the page exists. A "view in console" line pointing at a 404 is
  // worse than no line: it reads as a working alternative right up until it is
  // clicked, and instancetypes deliberately has no page.
  //
  // Direct mode has no console base at all, so these links are simply omitted
  // there rather than guessed — a sandbox reaching one cluster's API has no way
  // to know where, or whether, a console is published.
  const web = consoleBase(ctx)
  if (web && hasConsolePage(a)) {
    parts.push(`view:\n  ${web}${consolePath(a)}`)
  }
  parts.push(`hint:\n${lines.join('\n')}`)
  return parts.join('\n')
}

function viewHint(ctx: Context, a: Address): string {
  const web = consoleBase(ctx)
  return web && hasConsolePage(a)
    ? `view:\n  ${web}${consolePath(a)}`
    : `hint:\n  abx ${cliArgs({ ...a, view: undefined, cluster: undefined }).join(' ')}  # back up one level`
}

/**
 * Apply --filter key=value client-side.
 *
 * Uniformly client-side, even where an endpoint could filter for us: the same
 * `--filter status=running` has to mean the same thing on every resource, and
 * half the list endpoints accept no query parameters at all. A filter that
 * silently changes meaning per resource is the kind of thing nobody notices
 * until a count is wrong.
 *
 * Closed-set filters match exactly; open ones match as a substring, which is
 * what a name filter is always wanted for.
 */
export function applyFilters(
  spec: ResourceSpec,
  rows: Record<string, unknown>[],
  filters: [string, string][],
): Record<string, unknown>[] {
  let out = rows
  for (const [key, value] of filters) {
    const f = (spec.filters ?? []).find((x) => x.key === key)
    if (!f) {
      const valid = (spec.filters ?? []).map((x) => x.key).join(', ')
      throw new CliError(
        `"${spec.plural}" has no filter "${key}"`,
        valid ? `valid filters: ${valid}` : 'this resource declares no filters',
      )
    }
    if (f.values && !f.values.includes(value)) {
      throw new CliError(
        `"${value}" is not a valid ${key}`,
        `valid values: ${f.values.join(', ')}`,
      )
    }
    const col = spec.columns.find((c) => c.id === key)
    out = out.filter((r) => {
      const got = cell(r, col ?? key)
      return f.values ? got === value : got.toLowerCase().includes(value.toLowerCase())
    })
  }
  return out
}
