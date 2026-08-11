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

// Export / import of the SandboxEnv form as JSON, for moving a configuration
// between clusters or deployments by hand.
//
// The payload carries FORM VALUES, not the CRD spec — the same choice
// lib/utils/managed-agent-clone.ts makes, and for the same two reasons: an
// import is then a plain `reset()` needing no inverse of the payload builders,
// and the secret surface is small enough to enumerate rather than having to
// strip a credentialsRef out of a deep spec.
//
// Two fields do hold secret material and are blanked on export:
// `injectionCredentialRows[].value` (the egress credential itself) and
// `imagePullSecretRows[].password`. Neither is ever returned by the API either —
// credentials live only in a K8s Secret, the operator's memory, and the sidecar's
// tmpfs — so whoever imports has to re-type them, exactly as they would after a
// cross-cluster copy.

import { z } from "zod"

import { baseSchema, envFormDefaults } from "@/lib/utils/env-form"
import type { FormValues } from "@/lib/utils/env-form"

/** Envelope discriminator: a JSON file dropped on the form is checked for this. */
export const ENV_CLONE_KIND = "SandboxEnvFormExport"

/** Bumped only for a change an older importer would read wrongly. */
export const ENV_CLONE_VERSION = 1

export interface EnvClonePayload {
  kind: typeof ENV_CLONE_KIND
  version: number
  exportedAt: string
  values: FormValues
}

export type EnvCloneParseErrorKind = "json" | "kind" | "version" | "schema"

export interface EnvCloneParseError {
  kind: EnvCloneParseErrorKind
  detail: string
}

export interface EnvCloneWarning {
  key: "secretsOmitted"
  count: number
}

export interface EnvCloneImportResult {
  values: FormValues
  warnings: EnvCloneWarning[]
}

/**
 * Blanks every secret the form holds. Exported for the test that asserts it —
 * this function is the whole reason an exported file is safe to attach to a
 * ticket or check into a repo.
 */
export function stripEnvCloneSecrets(values: FormValues): FormValues {
  return {
    ...values,
    injectionCredentialRows: (values.injectionCredentialRows ?? []).map((row) => ({
      ...row,
      value: "",
    })),
    imagePullSecretRows: (values.imagePullSecretRows ?? []).map((row) => ({
      ...row,
      password: undefined,
    })),
  }
}

export function toEnvClonePayload(values: FormValues, now = new Date()): EnvClonePayload {
  return {
    kind: ENV_CLONE_KIND,
    version: ENV_CLONE_VERSION,
    exportedAt: now.toISOString(),
    values: stripEnvCloneSecrets(values),
  }
}

export function envCloneFileName(values: FormValues): string {
  const name = values.name?.trim() || "env"
  return `sandbox-env-${name}.json`
}

/**
 * Parses an exported file back into form values.
 *
 * Validation is structural only (`baseSchema.partial()`): an export may be a
 * draft mid-edit, and an incomplete one should import into a form that shows
 * what is still missing rather than being rejected outright. The cross-field
 * refinement on `formSchema` runs later, when the form itself validates.
 */
export function fromEnvCloneJson(
  text: string,
): { ok: true; result: EnvCloneImportResult } | { ok: false; error: EnvCloneParseError } {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (err) {
    return { ok: false, error: { kind: "json", detail: String(err) } }
  }
  if (!parsed || typeof parsed !== "object") {
    return { ok: false, error: { kind: "json", detail: "not an object" } }
  }

  const envelope = parsed as Partial<EnvClonePayload>
  if (envelope.kind !== ENV_CLONE_KIND) {
    return { ok: false, error: { kind: "kind", detail: String(envelope.kind ?? "") } }
  }
  if (envelope.version !== ENV_CLONE_VERSION) {
    return { ok: false, error: { kind: "version", detail: String(envelope.version ?? "") } }
  }

  const check = baseSchema.partial().safeParse(envelope.values ?? {})
  if (!check.success) {
    return {
      ok: false,
      error: { kind: "schema", detail: check.error.issues.map((i) => i.path.join(".")).join(", ") },
    }
  }

  // Merge over defaults so a file written by an older export still fills the
  // whole form, and so no `register`ed input flips to uncontrolled on reset.
  //
  // Undefined-valued keys are dropped first: several fields are declared with a
  // preprocess that turns "" into undefined, so parsing an exported blank yields
  // an explicit `undefined`, and spreading that would overwrite the default and
  // hand `register` an uncontrolled input.
  const parsedValues = Object.fromEntries(
    Object.entries(check.data as Record<string, unknown>).filter(([, v]) => v !== undefined),
  ) as Partial<FormValues>

  const values = { ...envFormDefaults(), ...parsedValues } as FormValues

  const warnings: EnvCloneWarning[] = []
  const blanked =
    values.injectionCredentialRows.filter((r) => !r.value).length +
    values.imagePullSecretRows.filter((r) => !r.password).length
  if (blanked > 0) warnings.push({ key: "secretsOmitted", count: blanked })

  return { ok: true, result: { values, warnings } }
}

/** Re-exported so callers do not have to know which module owns the schema. */
export const envCloneValuesSchema: z.ZodTypeAny = baseSchema.partial()
