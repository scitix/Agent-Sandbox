// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Export / import of a ManagedAgent's form values, for cloning an agent.
//
// The payload carries FORM VALUES, not the CRD spec. That is what makes the
// no-secrets requirement structural rather than a strip function somebody has to
// keep in step with 175 spec fields: the form-values type cannot express a
// credentialsRef, a secretKeyRef, a valueFrom or a Secret name at all. The only
// secret-shaped things in it are the four API-key text fields, which are blanked
// here and re-typed by whoever imports.
//
// Round-tripping through the form also means an import needs no inverse of
// buildSpec — it is a `reset()` of the same shape the form already holds.

import { z } from "zod"

import { baseSchema, managedAgentFormDefaults } from "@/lib/utils/managed-agent-form"
import type { FormValues } from "@/lib/utils/managed-agent-form"

/** Envelope discriminator: a JSON file dropped on the form is checked for this. */
export const MANAGED_AGENT_CLONE_KIND = "ManagedAgentFormExport"

/** Bumped only for a change an older importer would read wrongly. */
export const MANAGED_AGENT_CLONE_VERSION = 1

export interface ManagedAgentClonePayload {
  kind: typeof MANAGED_AGENT_CLONE_KIND
  version: number
  exportedAt: string
  values: FormValues
}

/** The four write-only key fields. Blanked on export, re-typed on import. */
const SECRET_FIELDS = [
  "claudeApiKey",
  "opencodeApiKey",
  "classifierApiKey",
  "sandboxApiKey",
] as const satisfies readonly (keyof FormValues)[]

export type CloneParseErrorKind = "json" | "kind" | "version" | "schema"

export interface CloneParseError {
  kind: CloneParseErrorKind
  detail: string
}

export type CloneWarningKey = "secretsOmitted" | "unknownClusters"

export interface CloneWarning {
  key: CloneWarningKey
  count: number
}

/** Strips every secret the form holds. Exported for the test that asserts it. */
export function stripCloneSecrets(values: FormValues): FormValues {
  const out: FormValues = { ...values }
  for (const field of SECRET_FIELDS) out[field] = ""
  return out
}

export function toClonePayload(values: FormValues, now = new Date()): ManagedAgentClonePayload {
  return {
    kind: MANAGED_AGENT_CLONE_KIND,
    version: MANAGED_AGENT_CLONE_VERSION,
    exportedAt: now.toISOString(),
    values: stripCloneSecrets(values),
  }
}

export function cloneFileName(values: FormValues): string {
  const name = values.name?.trim() || "agent"
  return `managed-agent-${name}.json`
}

export interface CloneImportResult {
  values: FormValues
  warnings: CloneWarning[]
}

/**
 * Reads an exported file back into form values.
 *
 * Validates against the STRUCTURAL schema, not the refined create schema: the
 * refinements are business rules, and an import whose API keys were just stripped
 * is *supposed* to fail them. Surfacing that as red fields on the form is useful;
 * rejecting the file as "invalid" would not be.
 *
 * Cluster ids that this deployment does not know are cleared rather than kept: a
 * stale id renders as an empty picker with nothing to explain it.
 */
export function fromCloneJson(
  text: string,
  opts: { knownClusterIDs?: string[] } = {},
): { ok: true; result: CloneImportResult } | { ok: false; error: CloneParseError } {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (err) {
    return { ok: false, error: { kind: "json", detail: String(err) } }
  }
  if (!parsed || typeof parsed !== "object") {
    return { ok: false, error: { kind: "json", detail: "not an object" } }
  }
  const envelope = parsed as Partial<ManagedAgentClonePayload>
  if (envelope.kind !== MANAGED_AGENT_CLONE_KIND) {
    return { ok: false, error: { kind: "kind", detail: String(envelope.kind ?? "") } }
  }
  if (envelope.version !== MANAGED_AGENT_CLONE_VERSION) {
    return { ok: false, error: { kind: "version", detail: String(envelope.version ?? "") } }
  }

  const structural = baseSchema.partial()
  const check = structural.safeParse(envelope.values ?? {})
  if (!check.success) {
    return {
      ok: false,
      error: { kind: "schema", detail: check.error.issues.map((i) => i.path.join(".")).join(", ") },
    }
  }

  // Merge over defaults so a file written by an older export still fills the
  // whole form, and so no `register`ed input flips to uncontrolled on reset.
  //
  // Undefined-valued keys are dropped first. Several form fields are declared
  // with a preprocess that turns "" into undefined — the shape the CRD wants —
  // so a parse of an exported blank yields an explicit `undefined`, and spreading
  // that would overwrite the default "" and hand `register` an uncontrolled input.
  const parsedValues = Object.fromEntries(
    Object.entries(check.data as Record<string, unknown>).filter(([, v]) => v !== undefined),
  ) as Partial<FormValues>
  const values = { ...managedAgentFormDefaults(), ...parsedValues } as FormValues
  const warnings: CloneWarning[] = []

  const secretsBlank = SECRET_FIELDS.filter((f) => !values[f]).length
  if (secretsBlank > 0) warnings.push({ key: "secretsOmitted", count: secretsBlank })

  const known = opts.knownClusterIDs
  if (known) {
    let cleared = 0
    for (const field of ["autoClusterID", "envClusterID"] as const) {
      const id = values[field]
      if (id && !known.includes(id)) {
        values[field] = ""
        cleared++
      }
    }
    if (cleared > 0) warnings.push({ key: "unknownClusters", count: cleared })
  }

  return { ok: true, result: { values, warnings } }
}

/** Re-exported so callers do not have to know which module owns the schema. */
export const cloneValuesSchema: z.ZodTypeAny = baseSchema.partial()
