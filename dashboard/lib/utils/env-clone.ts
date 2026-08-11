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

// SandboxEnv form clone — the generic machinery lives in lib/utils/form-clone.ts.
//
// The Env form holds two pieces of real secret material, and both are blanked on
// export: `injectionCredentialRows[].value` (the egress credential itself) and
// `imagePullSecretRows[].password`. Neither is ever returned by the API either —
// credentials live only in a K8s Secret, the operator's memory, and the sidecar's
// tmpfs — so whoever imports has to re-type them, exactly as they would after a
// cross-cluster copy.

import { createFormClone } from "@/lib/utils/form-clone"
import { baseSchema, envFormDefaults } from "@/lib/utils/env-form"
import type { FormValues } from "@/lib/utils/env-form"

/** Envelope discriminator: a JSON file dropped on the form is checked for this. */
export const ENV_CLONE_KIND = "SandboxEnvFormExport"

/** Bumped only for a change an older importer would read wrongly. */
export const ENV_CLONE_VERSION = 1

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

export const envClone = createFormClone<FormValues>({
  kind: ENV_CLONE_KIND,
  version: ENV_CLONE_VERSION,
  schema: baseSchema.partial(),
  filePrefix: "sandbox-env",
  nameOf: (v) => v.name,
  stripSecrets: stripEnvCloneSecrets,
  countBlankedSecrets: (v) =>
    v.injectionCredentialRows.filter((r) => !r.value).length +
    v.imagePullSecretRows.filter((r) => !r.password).length,
})

export const toEnvClonePayload = envClone.toPayload
export const envCloneFileName = envClone.fileName

/** Defaults default to a blank create form, which is what the tests exercise. */
export const fromEnvCloneJson = (text: string, defaults: FormValues = envFormDefaults()) =>
  envClone.fromJson(text, defaults)
