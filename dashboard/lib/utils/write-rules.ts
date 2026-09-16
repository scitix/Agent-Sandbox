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
 * What a form may offer to change, from the same table the CLI renders.
 *
 * "Fixed at create" is a fact about the API, and it was written down three
 * times: `x-immutable` in the spec, the 400 the service raises, and the
 * `disabled` flags in these sheets. The spec is the one that cannot be wrong,
 * and `WRITE_DOCS` is generated from it — the same table `abx create <address>
 * --help` prints. Both clients therefore read the rule from the same place, and
 * a field that becomes editable stops being disabled in the console on the next
 * `make gen-all-api` rather than on the next bug report.
 */

import { WRITE_DOCS, type WriteDocField } from "./write-docs.generated"

export interface WriteFieldRule {
  /** The JSON field name, as the API spells it. */
  field: string
  /** Fixed after create: an update has to send it back unchanged. */
  fixed: boolean
  /** What to do instead, when a fixed field cannot be changed. */
  lever?: string
  /** The API's own sentence about the field. */
  describe: string
}

/**
 * Every field a write to this resource may carry.
 *
 * Create's fields first — the file a create wrote is the file an update takes,
 * so the union is the shape — with the update's descriptions winning where
 * both have one, because that is the schema the PUT is written against.
 */
export function writeFieldRules(plural: string): WriteFieldRule[] {
  const doc = WRITE_DOCS.find((d) => d.plural === plural)
  if (!doc) return []
  const byName = new Map<string, WriteDocField>()
  for (const body of [doc.create, doc.update]) {
    for (const f of body?.fields ?? []) byName.set(f.name, f)
  }
  // Keep the create body's order: identity first, then the rest.
  const order = (doc.create?.fields ?? doc.update?.fields ?? []).map((f) => f.name)
  return order.map((name) => {
    const f = byName.get(name)!
    return { field: f.name, fixed: f.fixed, lever: f.lever, describe: f.describe }
  })
}

/** The field names an update has to send back unchanged. */
export function fixedWriteFields(plural: string): Set<string> {
  return new Set(writeFieldRules(plural).filter((r) => r.fixed).map((r) => r.field))
}

/** Whether one field is fixed, asked by the name the API uses. */
export function isFixedWriteField(plural: string, field: string): boolean {
  return fixedWriteFields(plural).has(field)
}
