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

// Export / import of a form's own values as JSON, shared by every upsert sheet.
//
// The payload carries FORM VALUES, not the resource spec. That is the choice the
// whole design rests on: an import becomes a plain `reset()` needing no inverse
// of the payload builders, and the secret surface is whatever the form-values
// type can express — a short list a `stripSecrets` can enumerate — rather than a
// credentialsRef buried somewhere in a deep spec.
//
// Each form supplies a spec (kind, its own schema, how to blank its secrets) and
// gets back the three functions `FormCloneActions` needs.

import type { z } from "zod"

/** Envelope written to disk. `values` is whatever the form holds. */
export interface FormClonePayload<T> {
  kind: string
  version: number
  exportedAt: string
  values: T
}

export type FormCloneParseErrorKind = "json" | "kind" | "version" | "schema"

export interface FormCloneParseError {
  kind: FormCloneParseErrorKind
  detail: string
}

/**
 * Something the importer should tell the user about. `count` is rendered into
 * the message, so a warning with count 0 should not be emitted at all.
 */
export interface FormCloneWarning {
  key: "secretsOmitted" | "unknownClusters"
  count: number
}

export interface FormCloneImportResult<T> {
  values: T
  warnings: FormCloneWarning[]
}

export interface FormCloneSpec<T> {
  /** Envelope discriminator. A file is refused unless it matches exactly. */
  kind: string
  /** Bumped only for a change an older importer would read wrongly. */
  version: number
  /**
   * Structural validator for the values object. Pass a `.partial()`-ed object
   * schema: an export may be a draft mid-edit, and an incomplete one should
   * import into a form that shows what is missing rather than being refused.
   * Cross-field refinements run later, when the form itself validates.
   */
  schema: z.ZodTypeAny
  /** Filename stem, e.g. "sandbox-env" → sandbox-env-{name}.json. */
  filePrefix: string
  /** Names the file. Falls back to the prefix alone when it yields nothing. */
  nameOf?: (values: T) => string | undefined
  /** Blanks every secret the form holds. Omit for forms that hold none. */
  stripSecrets?: (values: T) => T
  /** How many secrets an importer must re-type, for the warning. */
  countBlankedSecrets?: (values: T) => number
  /** Last-mile fixups on import, e.g. dropping ids this deployment lacks. */
  sanitize?: (values: T) => { values: T; warnings: FormCloneWarning[] }
}

export interface FormClone<T> {
  kind: string
  version: number
  toPayload: (values: T, now?: Date) => FormClonePayload<T>
  fileName: (values: T) => string
  /**
   * `defaults` is passed in rather than baked into the spec because some forms
   * derive theirs at runtime (a Pool's defaults depend on its Env and on feature
   * gates), and an import has to merge onto the defaults in force right now.
   */
  fromJson: (
    text: string,
    defaults: T,
  ) => { ok: true; result: FormCloneImportResult<T> } | { ok: false; error: FormCloneParseError }
}

export function createFormClone<T>(spec: FormCloneSpec<T>): FormClone<T> {
  const toPayload = (values: T, now = new Date()): FormClonePayload<T> => ({
    kind: spec.kind,
    version: spec.version,
    exportedAt: now.toISOString(),
    values: spec.stripSecrets ? spec.stripSecrets(values) : values,
  })

  const fileName = (values: T): string => {
    const name = spec.nameOf?.(values)?.trim()
    return name ? `${spec.filePrefix}-${name}.json` : `${spec.filePrefix}.json`
  }

  const fromJson: FormClone<T>["fromJson"] = (text, defaults) => {
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch (err) {
      return { ok: false, error: { kind: "json", detail: String(err) } }
    }
    if (!parsed || typeof parsed !== "object") {
      return { ok: false, error: { kind: "json", detail: "not an object" } }
    }

    const envelope = parsed as Partial<FormClonePayload<T>>
    if (envelope.kind !== spec.kind) {
      return { ok: false, error: { kind: "kind", detail: String(envelope.kind ?? "") } }
    }
    if (envelope.version !== spec.version) {
      return { ok: false, error: { kind: "version", detail: String(envelope.version ?? "") } }
    }

    const check = spec.schema.safeParse(envelope.values ?? {})
    if (!check.success) {
      return {
        ok: false,
        error: {
          kind: "schema",
          detail: check.error.issues.map((i) => i.path.join(".")).join(", "),
        },
      }
    }

    // Undefined-valued keys are dropped before merging. Form fields declared with
    // a preprocess that turns "" into undefined parse an exported blank back to an
    // explicit `undefined`, and spreading that would overwrite the default and
    // hand a `register`ed input an uncontrolled value.
    const parsedValues = Object.fromEntries(
      Object.entries(check.data as Record<string, unknown>).filter(([, v]) => v !== undefined),
    ) as Partial<T>

    // Merged over defaults so a file written by an older export still fills the
    // whole form rather than leaving holes.
    let values = { ...defaults, ...parsedValues } as T
    const warnings: FormCloneWarning[] = []

    if (spec.sanitize) {
      const sanitized = spec.sanitize(values)
      values = sanitized.values
      warnings.push(...sanitized.warnings)
    }

    const blanked = spec.countBlankedSecrets?.(values) ?? 0
    if (blanked > 0) warnings.push({ key: "secretsOmitted", count: blanked })

    return { ok: true, result: { values, warnings } }
  }

  return { kind: spec.kind, version: spec.version, toPayload, fileName, fromJson }
}
