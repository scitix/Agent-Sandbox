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

// Parameter descriptors for the sandbox toolset.
//
// WHY A DESCRIPTOR INSTEAD OF ZOD: the same tool definitions are bound into
// three harnesses, and each supplies its own schema library — OpenCode exposes
// `tool.schema` (its bundled zod), the Claude Agent SDK takes a zod raw shape,
// and a plain MCP server wants JSON Schema. Declaring the parameters once in a
// tiny neutral shape lets each binding build objects with ITS OWN zod, so we
// never depend on two copies of zod agreeing on `instanceof`.
//
// Deliberately minimal: these seven tools only ever needed string / number /
// boolean, all optionally optional. Widen it when a tool actually needs more.

export type ParamSpec = {
  type: 'string' | 'number' | 'boolean'
  description: string
  /** Omit for a required parameter. */
  optional?: true
}

export type ParamsSpec = Record<string, ParamSpec>

/** JSON Schema (draft-07 subset) for a plain MCP server. */
export function toJsonSchema(params: ParamsSpec): {
  type: 'object'
  properties: Record<string, { type: string; description: string }>
  required: string[]
  additionalProperties: false
} {
  const properties: Record<string, { type: string; description: string }> = {}
  const required: string[] = []
  for (const [name, spec] of Object.entries(params)) {
    properties[name] = { type: spec.type, description: spec.description }
    if (!spec.optional) required.push(name)
  }
  return { type: 'object', properties, required, additionalProperties: false }
}

/**
 * Minimal surface a harness's schema library must provide, so a binding can turn
 * a {@link ParamsSpec} into that harness's own schema objects. Structural, not
 * nominal — `tool.schema` and `zod` both satisfy it.
 */
export interface SchemaFactory<T> {
  string(): SchemaBuilder<T>
  number(): SchemaBuilder<T>
  boolean(): SchemaBuilder<T>
}

export interface SchemaBuilder<T> {
  optional(): SchemaBuilder<T>
  describe(text: string): T
}

/** Build `{ [param]: <harness schema> }` from a descriptor. */
export function toSchemaShape<T>(
  params: ParamsSpec,
  factory: SchemaFactory<T>
): Record<string, T> {
  const shape: Record<string, T> = {}
  for (const [name, spec] of Object.entries(params)) {
    let b: SchemaBuilder<T> =
      spec.type === 'string'
        ? factory.string()
        : spec.type === 'number'
          ? factory.number()
          : factory.boolean()
    if (spec.optional) b = b.optional()
    shape[name] = b.describe(spec.description)
  }
  return shape
}
