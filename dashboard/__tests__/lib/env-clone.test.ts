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

import { describe, it, expect } from "vitest"

import {
  ENV_CLONE_KIND,
  ENV_CLONE_VERSION,
  envCloneFileName,
  fromEnvCloneJson,
  stripEnvCloneSecrets,
  toEnvClonePayload,
} from "@/lib/utils/env-clone"
import { envFormDefaults } from "@/lib/utils/env-form"
import type { FormValues } from "@/lib/utils/env-form"

const EGRESS_CREDENTIAL = "sk-egress-super-secret"
const REGISTRY_PASSWORD = "hunter2-registry"

function filled(over: Partial<FormValues> = {}): FormValues {
  return {
    ...envFormDefaults(),
    name: "slimedev",
    templateName: "navix-runtime",
    image: "ghcr.io/org/runtime:1.2",
    defaultStartupTimeout: "5m",
    defaultIdleTimeout: "30m",
    networkPolicyMode: "allowlist",
    allowedDomains: "pypi.org\n*.pythonhosted.org",
    allowPrivateNetworks: true,
    imagePullSecretRows: [
      { registry: "registry.example.com", username: "robot", password: REGISTRY_PASSWORD },
    ],
    injectionCredentialRows: [
      { name: "OPENAI", value: EGRESS_CREDENTIAL, exposeAs: "Authorization" },
    ],
    injectionRuleRows: [{ host: "api.openai.com", headerName: "Authorization" }],
    autoUpdate: true,
    maxUnavailable: "20%",
    ...over,
  } as FormValues
}

/**
 * The assertion that matters. An exported file gets attached to tickets and
 * checked into repos, and the form does hold real credentials — the egress
 * injection value and the registry password.
 */
describe("an export carries no secret material", () => {
  const payload = toEnvClonePayload(filled(), new Date("2026-08-11T00:00:00Z"))
  const serialised = JSON.stringify(payload)

  it("blanks the egress credential value", () => {
    expect(payload.values.injectionCredentialRows[0]!.value).toBe("")
  })

  it("blanks the image-pull-secret password", () => {
    expect(payload.values.imagePullSecretRows[0]!.password).toBeUndefined()
  })

  it("contains no secret-shaped value anywhere in the file", () => {
    expect(serialised).not.toContain(EGRESS_CREDENTIAL)
    expect(serialised).not.toContain(REGISTRY_PASSWORD)
  })

  it("keeps the non-secret half of those rows, so only the value must be re-typed", () => {
    expect(payload.values.injectionCredentialRows[0]!.name).toBe("OPENAI")
    expect(payload.values.injectionCredentialRows[0]!.exposeAs).toBe("Authorization")
    expect(payload.values.imagePullSecretRows[0]!.registry).toBe("registry.example.com")
    expect(payload.values.imagePullSecretRows[0]!.username).toBe("robot")
  })

  it("does not mutate the caller's values", () => {
    const values = filled()
    stripEnvCloneSecrets(values)
    expect(values.injectionCredentialRows[0]!.value).toBe(EGRESS_CREDENTIAL)
    expect(values.imagePullSecretRows[0]!.password).toBe(REGISTRY_PASSWORD)
  })
})

describe("round trip", () => {
  it("restores everything but the secrets", () => {
    const original = filled()
    const text = JSON.stringify(toEnvClonePayload(original))
    const parsed = fromEnvCloneJson(text)

    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return

    const { values } = parsed.result
    expect(values.name).toBe("slimedev")
    expect(values.templateName).toBe("navix-runtime")
    expect(values.networkPolicyMode).toBe("allowlist")
    expect(values.allowedDomains).toBe("pypi.org\n*.pythonhosted.org")
    expect(values.allowPrivateNetworks).toBe(true)
    expect(values.maxUnavailable).toBe("20%")
    expect(values.injectionRuleRows[0]!.host).toBe("api.openai.com")
    expect(values.injectionCredentialRows[0]!.value).toBe("")
  })

  it("reports how many secrets need re-typing", () => {
    const parsed = fromEnvCloneJson(JSON.stringify(toEnvClonePayload(filled())))
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    expect(parsed.result.warnings).toEqual([{ key: "secretsOmitted", count: 2 }])
  })

  it("fills defaults for keys an older export omitted", () => {
    const text = JSON.stringify({
      kind: ENV_CLONE_KIND,
      version: ENV_CLONE_VERSION,
      exportedAt: "2026-08-11T00:00:00Z",
      values: { name: "minimal", templateName: "tpl" },
    })
    const parsed = fromEnvCloneJson(text)
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    // Defaults, not undefined — `register`ed inputs must not go uncontrolled.
    expect(parsed.result.values.networkPolicyMode).toBe("unrestricted")
    expect(parsed.result.values.autoUpdate).toBe(true)
    expect(parsed.result.values.imagePullSecretRows).toEqual([])
  })

  it("does not let an exported blank overwrite a default with undefined", () => {
    const exported = toEnvClonePayload(filled({ image: undefined, maxUnavailable: undefined }))
    const parsed = fromEnvCloneJson(JSON.stringify(exported))
    expect(parsed.ok).toBe(true)
    if (!parsed.ok) return
    expect(parsed.result.values.podCreationImagePolicy).toBe("IdleImage")
  })
})

describe("rejections", () => {
  it("rejects non-JSON", () => {
    const parsed = fromEnvCloneJson("not json{")
    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error.kind).toBe("json")
  })

  it("rejects another resource's export", () => {
    const parsed = fromEnvCloneJson(
      JSON.stringify({ kind: "ManagedAgentFormExport", version: 1, values: {} }),
    )
    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error.kind).toBe("kind")
  })

  it("rejects a future version rather than misreading it", () => {
    const parsed = fromEnvCloneJson(
      JSON.stringify({ kind: ENV_CLONE_KIND, version: ENV_CLONE_VERSION + 1, values: {} }),
    )
    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error.kind).toBe("version")
  })

  it("names the offending field on a schema mismatch", () => {
    const parsed = fromEnvCloneJson(
      JSON.stringify({
        kind: ENV_CLONE_KIND,
        version: ENV_CLONE_VERSION,
        values: { networkPolicyMode: "nonsense" },
      }),
    )
    expect(parsed.ok).toBe(false)
    if (parsed.ok) return
    expect(parsed.error.kind).toBe("schema")
    expect(parsed.error.detail).toContain("networkPolicyMode")
  })
})

describe("envCloneFileName", () => {
  it("uses the env name", () => {
    expect(envCloneFileName(filled())).toBe("sandbox-env-slimedev.json")
  })

  it("drops the suffix when the name is still blank", () => {
    expect(envCloneFileName(filled({ name: "  " }))).toBe("sandbox-env.json")
  })
})
