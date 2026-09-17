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
  /** How the value is spelled in JSON: `string`, `int`, `map[string]string`, `EnvVolumeMount[]`, or an enum's values. */
  type: string
  /**
   * The named component the value is — or, for an array or a map, the
   * component its elements are. `schemas` resolves it.
   */
  ref?: string
  required: boolean
  fixed: boolean
  lever?: string
  /** The accepted values, when the field is an enum. */
  values?: string[]
  /** The schema's own default, when it states one. */
  default?: unknown
  describe: string
  fields?: Field[]
}

interface Body {
  schema: string
  describe: string
  example?: unknown
  fields: Field[]
  /**
   * Every named component the body reaches, each expanded once, so a `ref` is
   * a lookup rather than a dead end. Keyed by name, which also makes a cycle
   * impossible: a component that references itself was already rendered.
   */
  schemas?: Record<string, Field[]>
}

type Node = Record<string, unknown>

/** Whitespace-collapsed: the spec uses block scalars, and prose wraps better than YAML does. */
function prose(node: Node): string {
  const text = typeof node.description === 'string' ? node.description : ''
  return text.replace(/\s+/g, ' ').trim()
}

/**
 * The component a node references.
 *
 * Through `allOf` too, because that is how this spec attaches a note —
 * `x-immutable`, a longer description — to a `$ref`: `labels` is
 * `allOf: [$ref: StringMap]` plus a description, and stopping at `$ref` alone
 * is what made it print as an anonymous `object`.
 */
function refName(node: Node): string | undefined {
  if (typeof node.$ref === 'string') {
    return node.$ref.replace(/^#\/components\/schemas\//, '')
  }
  if (Array.isArray(node.allOf)) {
    for (const part of node.allOf) {
      const name = refName(part as Node)
      if (name) return name
    }
  }
  return undefined
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

/**
 * How a field's value is spelled, in the words a caller writing JSON uses.
 *
 * A named component answers with its own name (`EnvVolumeMount`, not
 * `object`), an array with its element's name (`EnvVolumeMount[]`), and a
 * free-form map with what it maps to (`map[string]string`). `object` is kept
 * for an anonymous shape, and `any` for a schema that states nothing — the two
 * used to be the same answer, which told a reader nothing either way.
 */
function typeOf(schemas: Record<string, Node>, node: Node): string {
  const resolved = deref(schemas, node)
  if (Array.isArray(resolved.enum)) return (resolved.enum as string[]).join(' | ')
  if (resolved.type === 'array') return `${typeOf(schemas, (resolved.items as Node) ?? {})}[]`
  if (resolved.type === 'integer') return 'int'
  if (resolved.type === 'number') return 'number'
  if (resolved.type === 'boolean') return 'bool'
  if (resolved.type === 'string') return 'string'
  const properties = resolved.properties as Node | undefined
  if (properties && Object.keys(properties).length) return refName(node) ?? 'object'
  const values = resolved.additionalProperties
  if (values && typeof values === 'object') return `map[string]${typeOf(schemas, values as Node)}`
  if (values === true) return 'map[string]any'
  if (resolved.type === 'object') return refName(node) ?? 'object'
  return 'any'
}

/**
 * The named component that describes this field's value — or, when the value
 * is a list or a map, the component its elements are.
 *
 * A map of scalars (`StringMap`) has no such component: `map[string]string`
 * already says everything, and inventing a `ref` for it would only send a
 * reader to an empty table entry.
 */
function valueRef(schemas: Record<string, Node>, node: Node): string | undefined {
  const resolved = deref(schemas, node)
  if (resolved.type === 'array') return valueRef(schemas, (resolved.items as Node) ?? {})
  const properties = resolved.properties as Node | undefined
  if (properties && Object.keys(properties).length) return refName(node)
  const values = resolved.additionalProperties
  if (values && typeof values === 'object') return valueRef(schemas, values as Node)
  return undefined
}

/** How deep the inline expansion goes. Beyond it, `ref` and `schemas` take over. */
const MAX_DEPTH = 3

/**
 * The fields of a value, arrays and maps transparent.
 *
 * A reader who asks what `volumes` carries is asking about its elements, so an
 * array does not cost a level of nesting — `claimName` and `mountPath` sit
 * directly under `volumes`, where the question was. `ref` still names the
 * element's component, and `schemas` carries it, so nothing below the depth
 * limit is unreachable: it is a lookup rather than a guess.
 */
function fieldsOf(schemas: Record<string, Node>, node: Node, depth = 0, path: string[] = []): Field[] {
  const resolved = deref(schemas, node)
  if (resolved.type === 'array') {
    return fieldsOf(schemas, (resolved.items as Node) ?? {}, depth, path)
  }
  const values = resolved.additionalProperties
  const hasProperties = Boolean(resolved.properties && Object.keys(resolved.properties as Node).length)
  if (!hasProperties && values && typeof values === 'object') {
    return fieldsOf(schemas, values as Node, depth, path)
  }
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
    if (field.default !== undefined) out.default = field.default
    const ref = valueRef(schemas, raw)
    if (ref) out.ref = ref
    if (depth < MAX_DEPTH && !(ref && path.includes(ref))) {
      const kids = fieldsOf(schemas, raw, depth + 1, ref ? [...path, ref] : path)
      if (kids.length) out.fields = kids
    }
    return out
  })
}

/** Every ref a field tree names, so the table below is complete rather than one level deep. */
function refsOf(fields: readonly Field[]): string[] {
  const out: string[] = []
  const walk = (fs: readonly Field[]) => {
    for (const f of fs) {
      if (f.ref) out.push(f.ref)
      if (f.fields) walk(f.fields)
    }
  }
  walk(fields)
  return out
}

/** Each named component the body reaches, expanded once. */
function componentsOf(schemas: Record<string, Node>, fields: readonly Field[]): Record<string, Field[]> {
  const table: Record<string, Field[]> = {}
  const seen = new Set<string>()
  const queue = refsOf(fields)
  while (queue.length) {
    const name = queue.shift() as string
    if (seen.has(name)) continue
    seen.add(name)
    const node = schemas[name]
    if (!node) continue
    const expanded = fieldsOf(schemas, node)
    table[name] = expanded
    queue.push(...refsOf(expanded))
  }
  return table
}

function bodyOf(schemas: Record<string, Node>, schema: string): Body {
  const node = schemas[schema]
  if (!node) throw new Error(`no schema ${schema} in the spec`)
  const resolved = deref(schemas, node)
  const fields = fieldsOf(schemas, resolved)
  const body: Body = {
    schema,
    describe: prose(resolved),
    fields,
  }
  if (resolved.example !== undefined) body.example = resolved.example
  const components = componentsOf(schemas, fields)
  if (Object.keys(components).length) body.schemas = components
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

/** One field of a write body. \`fields\` expands the value; \`ref\` names the component it is. */
export interface WriteDocField {
  name: string
  /** How the value is spelled in JSON: \`string\`, \`int\`, \`map[string]string\`, \`EnvVolumeMount[]\`, or an enum's values. */
  type: string
  /** The named component the value — or its elements, for a list or map — is. Resolved in \`schemas\`. */
  ref?: string
  /** Required by the schema — for a create that is the whole of "you must say". */
  required: boolean
  /** Fixed after create: an update has to carry it back unchanged. */
  fixed: boolean
  /** What to do instead, when a fixed field cannot be changed. */
  lever?: string
  /** The accepted values, when the field is an enum. */
  values?: string[]
  /** The schema's own default, when it states one. */
  default?: unknown
  describe: string
  fields?: WriteDocField[]
}

export interface WriteDocBody {
  schema: string
  describe: string
  example?: unknown
  fields: WriteDocField[]
  /** Every named component the body reaches, expanded once. */
  schemas?: Record<string, WriteDocField[]>
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
