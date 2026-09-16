#!/usr/bin/env bun
/**
 * The write bodies, generated from the OpenAPI spec.
 *
 * Why this file exists: "what goes in the file `abx create envs -f` takes" was
 * written out by hand in five places — the assistant's system prompt, two
 * skills, two runbooks and the CLI's own error text — and none of them could be
 * right for long. The spec already carries every fact needed (field names,
 * types, which are required, `x-immutable` and its lever, `example`), so this
 * turns it into one module that the help pages, `agent-context` and anything
 * else all render from.
 *
 * Run by `make gen-all-api`; `cli/test/` regenerates it and compares byte for
 * byte, so a hand edit — or a spec change nobody regenerated — fails a test
 * rather than a caller.
 *
 *   pkg/openapi/native/openapi.yaml
 *     → headless/src/write-docs.generated.ts
 *       → abx create|update <address> --help
 *       → abx agent-context
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const SPEC = join(HERE, '..', 'pkg', 'openapi', 'native', 'openapi.yaml')
const OUT = join(HERE, '..', 'headless', 'src', 'write-docs.generated.ts')
/**
 * The console gets its own copy, in its own tree, on purpose.
 *
 * `oss/headless` sits beside `oss/dashboard` and the dashboard's tsconfig
 * reaches it as `../headless/src` — which the TYPE check resolves and the
 * BUNDLER does not, because it is outside the app directory. Until now no built
 * dashboard file imported from there (the one that does is test-only), so the
 * depth of that relationship was never exercised: importing it from a sheet
 * failed `next build` with "module not found" while `tsc` and `vitest` were
 * perfectly happy. Generating a second copy into the app's own tree keeps the
 * data single-sourced — same generator, same spec, one command — without asking
 * the bundler to look outside its root.
 */
const OUT_DASHBOARD = join(HERE, '..', 'dashboard', 'lib', 'utils', 'write-docs.generated.ts')

/**
 * Which schema documents each resource's file.
 *
 * Keyed by the CLI's plural, because that is the name every surface already
 * agrees on. `create` and `update` are named separately even where they resolve
 * to the same schema: the day they diverge, the generator has a place to say so
 * rather than a caller finding out.
 */
const BODIES = [
  {
    plural: 'envs',
    kind: 'SandboxEnv',
    create: 'CreateSandboxEnvRequest',
    update: 'UpsertSandboxEnvRequest',
  },
  {
    plural: 'pools',
    kind: 'SandboxPool',
    create: 'CreateEnvSandboxPoolRequest',
    update: 'UpsertSandboxPoolRequest',
  },
  {
    plural: 'scaling-groups',
    kind: 'ScalingGroup',
    update: 'UpdateEnvAutoscalingGroupRequest',
  },
  {
    plural: 'admin-templates',
    kind: 'SandboxTemplate',
    create: 'UpsertSandboxTemplateRequest',
    update: 'UpsertSandboxTemplateRequest',
  },
  // Keys are written with a file too, and a key that is issued without the
  // fields it needs is the one write nobody can take back.
  {
    plural: 'api-keys',
    kind: 'APIKey',
    create: 'SelfCreateAPIKeyRequest',
  },
  {
    plural: 'admin-api-keys',
    kind: 'AdminAPIKey',
    create: 'CreateAPIKeyRequest',
  },
] as const

interface Field {
  name: string
  type: string
  required: boolean
  fixed: boolean
  lever?: string
  values?: string[]
  describe: string
  fields?: Field[]
}

interface Body {
  schema: string
  describe: string
  example?: unknown
  fields: Field[]
}

type Node = Record<string, unknown>

/** Whitespace-collapsed: the spec uses block scalars, and prose wraps better than YAML does. */
function prose(node: Node): string {
  const text = typeof node.description === 'string' ? node.description : ''
  return text.replace(/\s+/g, ' ').trim()
}

function refName(node: Node): string | undefined {
  const ref = node.$ref
  if (typeof ref !== 'string') return undefined
  return ref.replace(/^#\/components\/schemas\//, '')
}

/**
 * A node with its `$ref`/`allOf` flattened.
 *
 * `allOf` here is never a real intersection — it is how the spec attaches
 * `x-immutable` and a longer description to a referenced schema. Later entries
 * win, and the sibling keys of the node itself win over both, which is the
 * order the spec is written in.
 */
function deref(schemas: Record<string, Node>, node: Node, depth = 0): Node {
  if (depth > 8) return node
  const parts: Node[] = []
  const ref = refName(node)
  if (ref) parts.push(deref(schemas, schemas[ref] ?? {}, depth + 1))
  if (Array.isArray(node.allOf)) {
    for (const part of node.allOf) parts.push(deref(schemas, part as Node, depth + 1))
  }
  const own: Node = { ...node }
  delete own.allOf
  delete own.$ref
  const merged: Node = Object.assign({}, ...parts, own)
  // Properties and required accumulate rather than replace: that is what an
  // `allOf` of two object schemas means.
  if (parts.length) {
    const properties = Object.assign({}, ...parts.map((p) => (p.properties as Node) ?? {}), own.properties ?? {})
    if (Object.keys(properties).length) merged.properties = properties
    const required = [...new Set(parts.flatMap((p) => (p.required as string[]) ?? []).concat((own.required as string[]) ?? []))]
    if (required.length) merged.required = required
  }
  return merged
}

/** How a field's value is spelled, in the words a caller writing JSON uses. */
function typeOf(schemas: Record<string, Node>, node: Node): string {
  const resolved = deref(schemas, node)
  if (Array.isArray(resolved.enum)) return (resolved.enum as string[]).join(' | ')
  if (resolved.type === 'array') {
    const items = deref(schemas, (resolved.items as Node) ?? {})
    return `${scalarType(items)}[]`
  }
  return scalarType(resolved)
}

function scalarType(node: Node): string {
  if (node.type === 'integer') return 'int'
  if (node.type === 'number') return 'number'
  if (node.type === 'boolean') return 'bool'
  if (node.type === 'string') return 'string'
  if (node.type === 'object' || node.properties) return 'object'
  return 'any'
}

/**
 * One level of nesting, deliberately.
 *
 * The pages are read while holding a file, and two levels in is where a table
 * stops being scannable. `overrides` is the one place that matters, and one
 * level is exactly what it needs.
 */
function fieldsOf(schemas: Record<string, Node>, node: Node, depth = 0): Field[] {
  const resolved = deref(schemas, node)
  const properties = (resolved.properties as Record<string, Node>) ?? {}
  const required = new Set((resolved.required as string[]) ?? [])
  return Object.entries(properties).map(([name, raw]) => {
    const field = deref(schemas, raw)
    const out: Field = {
      name,
      type: typeOf(schemas, raw),
      required: required.has(name),
      fixed: field['x-immutable'] === true,
      describe: prose(field) || prose(raw),
    }
    const lever = field['x-immutable-lever']
    if (typeof lever === 'string') out.lever = lever
    if (Array.isArray(field.enum)) out.values = field.enum as string[]
    if (depth === 0) {
      const nested = deref(schemas, field)
      if (nested.properties && Object.keys(nested.properties as Node).length) {
        const kids = fieldsOf(schemas, nested, depth + 1)
        if (kids.length) out.fields = kids
      }
    }
    return out
  })
}

function bodyOf(schemas: Record<string, Node>, schema: string): Body {
  const node = schemas[schema]
  if (!node) throw new Error(`no schema ${schema} in the spec`)
  const resolved = deref(schemas, node)
  const body: Body = {
    schema,
    describe: prose(resolved),
    fields: fieldsOf(schemas, resolved),
  }
  if (resolved.example !== undefined) body.example = resolved.example
  return body
}

/** The module's text, so a test can compare it without touching the disk. */
export function generate(specText: string): string {
  return moduleText(specText, `/**
 * GENERATED — do not edit. \`make gen-all-api\`, or \`bun scripts/gen-body-docs.ts\`.
 *
 * The file each write takes, read out of \`pkg/openapi/native/openapi.yaml\`.
 * Every word on \`abx create|update <address> --help\`, and the field list in
 * \`abx agent-context\`, comes from here — so there is one description of a
 * write body in the repository rather than one per document that mentions it.
 */`)
}

/** The same data, for the console — see OUT_DASHBOARD for why it is a copy. */
export function generateForDashboard(specText: string): string {
  return moduleText(specText, `/**
 * GENERATED — do not edit. \`oss/scripts/gen-body-docs.ts\`, via \`make gen-all-api\`.
 *
 * Which fields a write may change, read out of \`pkg/openapi/native/openapi.yaml\`
 * — the same table \`abx create <address> --help\` renders, generated a second
 * time into this tree because the bundler cannot reach \`../headless\` (see the
 * generator). The console's forms and the CLI therefore lock the same fields.
 */`)
}

function moduleText(specText: string, header: string): string {
  const spec = Bun.YAML.parse(specText) as Record<string, unknown>
  const schemas = (spec.components as Record<string, unknown>)?.schemas as Record<string, Node>
  if (!schemas) throw new Error('spec has no components.schemas')

  const docs = BODIES.map((b) => {
    const doc: Record<string, unknown> = { plural: b.plural, kind: b.kind }
    if ('create' in b && b.create) doc.create = bodyOf(schemas, b.create)
    if ('update' in b && b.update) doc.update = bodyOf(schemas, b.update)
    // The fields only a create carries go first — a body is read from its
    // identity outwards, and `name` is not a footnote to the rest of it.
    const create = doc.create as Body | undefined
    const update = doc.update as Body | undefined
    if (create && update) {
      const inUpdate = new Set(update.fields.map((f) => f.name))
      const first = create.fields.filter((f) => !inUpdate.has(f.name))
      const rest = create.fields.filter((f) => inUpdate.has(f.name))
      create.fields = [...first, ...rest]
    }
    return doc
  })

  return `${header}

/** One field of a write body. \`fields\` is the one level of nesting worth showing. */
export interface WriteDocField {
  name: string
  /** How the value is spelled in JSON: \`string\`, \`int\`, \`bool\`, or an enum's values. */
  type: string
  /** Required by the schema — for a create that is the whole of "you must say". */
  required: boolean
  /** Fixed after create: an update has to carry it back unchanged. */
  fixed: boolean
  /** What to do instead, when a fixed field cannot be changed. */
  lever?: string
  /** The accepted values, when the field is an enum. */
  values?: string[]
  describe: string
  fields?: WriteDocField[]
}

export interface WriteDocBody {
  schema: string
  describe: string
  example?: unknown
  fields: WriteDocField[]
}

export interface WriteDoc {
  plural: string
  kind: string
  create?: WriteDocBody
  update?: WriteDocBody
}

export const WRITE_DOCS: readonly WriteDoc[] = ${JSON.stringify(docs, null, 2)}
`
}

export function write(specPath = SPEC, outPath = OUT): void {
  const spec = readFileSync(specPath, 'utf8')
  writeFileSync(outPath, generate(spec))
  writeFileSync(OUT_DASHBOARD, generateForDashboard(spec))
}

// Only when run as a program: importing this from a test must not rewrite the
// tree, which is what makes "regenerate and compare" a usable assertion.
if (import.meta.main) {
  write()
  console.log(`wrote ${OUT.replace(`${HERE}/../`, '')} and ${OUT_DASHBOARD.replace(`${HERE}/../`, '')}`)
}
