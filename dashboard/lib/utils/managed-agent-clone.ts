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

// ManagedAgent form clone — the generic machinery lives in lib/utils/form-clone.ts.
//
// The payload carries FORM VALUES, not the CRD spec. That is what makes the
// no-secrets requirement structural rather than a strip function somebody has to
// keep in step with 175 spec fields: the form-values type cannot express a
// credentialsRef, a secretKeyRef, a valueFrom or a Secret name at all. The only
// secret-shaped things in it are the four API-key text fields, which are blanked
// here and re-typed by whoever imports.

import { z } from "zod"

import { createFormClone } from "@/lib/utils/form-clone"
import { baseSchema, managedAgentFormDefaults } from "@/lib/utils/managed-agent-form"
import type { FormValues } from "@/lib/utils/managed-agent-form"

/** Envelope discriminator: a JSON file dropped on the form is checked for this. */
export const MANAGED_AGENT_CLONE_KIND = "ManagedAgentFormExport"

/** Bumped only for a change an older importer would read wrongly. */
export const MANAGED_AGENT_CLONE_VERSION = 1

/** The four write-only key fields. Blanked on export, re-typed on import. */
const SECRET_FIELDS = [
  "claudeApiKey",
  "opencodeApiKey",
  "classifierApiKey",
  "sandboxApiKey",
] as const satisfies readonly (keyof FormValues)[]

/** Cluster-id fields, cleared when this deployment does not know the id. */
const CLUSTER_FIELDS = ["autoClusterID", "envClusterID"] as const

/** Strips every secret the form holds. Exported for the test that asserts it. */
export function stripCloneSecrets(values: FormValues): FormValues {
  const out: FormValues = { ...values }
  for (const field of SECRET_FIELDS) out[field] = ""
  return out
}

/**
 * Builds the clone for this deployment.
 *
 * `knownClusterIDs` is a parameter rather than part of a static spec because it
 * is runtime data (the cluster list). Ids this deployment does not know are
 * cleared rather than kept: a stale id renders as an empty picker with nothing
 * to explain it.
 */
export function managedAgentClone(knownClusterIDs?: string[]) {
  return createFormClone<FormValues>({
    kind: MANAGED_AGENT_CLONE_KIND,
    version: MANAGED_AGENT_CLONE_VERSION,
    schema: baseSchema.partial(),
    filePrefix: "managed-agent",
    nameOf: (v) => v.name || "agent",
    stripSecrets: stripCloneSecrets,
    countBlankedSecrets: (v) => SECRET_FIELDS.filter((f) => !v[f]).length,
    sanitize: (values) => {
      if (!knownClusterIDs) return { values, warnings: [] }
      const out = { ...values }
      let cleared = 0
      for (const field of CLUSTER_FIELDS) {
        const id = out[field]
        if (id && !knownClusterIDs.includes(id)) {
          out[field] = ""
          cleared++
        }
      }
      return {
        values: out,
        warnings: cleared > 0 ? [{ key: "unknownClusters" as const, count: cleared }] : [],
      }
    },
  })
}

const staticClone = managedAgentClone()

export const toClonePayload = staticClone.toPayload
export const cloneFileName = staticClone.fileName

export function fromCloneJson(text: string, opts: { knownClusterIDs?: string[] } = {}) {
  return managedAgentClone(opts.knownClusterIDs).fromJson(text, managedAgentFormDefaults())
}

/** Re-exported so callers do not have to know which module owns the schema. */
export const cloneValuesSchema: z.ZodTypeAny = baseSchema.partial()
