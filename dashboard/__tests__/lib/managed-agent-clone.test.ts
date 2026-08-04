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

import { describe, expect, it } from "vitest"

import {
  MANAGED_AGENT_CLONE_KIND,
  cloneFileName,
  fromCloneJson,
  toClonePayload,
} from "@/lib/utils/managed-agent-clone"
import { managedAgentFormDefaults } from "@/lib/utils/managed-agent-form"
import type { FormValues } from "@/lib/utils/managed-agent-form"

function filled(over: Partial<FormValues> = {}): FormValues {
  return {
    ...managedAgentFormDefaults(),
    name: "navix",
    displayName: "Navix",
    imageRepository: "registry.example.com/brain",
    imageTag: "develop-abc123",
    defaultRuntime: "claude-code",
    claudeEnabled: true,
    claudeBaseURL: "https://api.example.com",
    claudeApiKey: "sk-super-secret",
    claudeModels: "claude-sonnet-5",
    claudeDefaultModel: "claude-sonnet-5",
    opencodeApiKey: "sk-oc-secret",
    classifierApiKey: "sk-cls-secret",
    sandboxApiKey: "agbx_secret",
    handsMode: "envRef",
    envName: "navix-hands",
    envClusterID: "gw-a",
    basePrompt: "you are navix",
    ...over,
  } as FormValues
}

/**
 * The point of exporting form values rather than the CRD spec: the type cannot
 * express a credentialsRef, a secretKeyRef or a Secret name, so the only secrets
 * in reach are the four key fields — and those are blanked. This scan is the
 * assertion that matters; everything else is convenience.
 */
describe("an export carries no secret material", () => {
  const payload = toClonePayload(filled(), new Date("2026-08-04T00:00:00Z"))
  const serialised = JSON.stringify(payload)

  it("blanks every API key", () => {
    expect(payload.values.claudeApiKey).toBe("")
    expect(payload.values.opencodeApiKey).toBe("")
    expect(payload.values.classifierApiKey).toBe("")
    expect(payload.values.sandboxApiKey).toBe("")
  })

  it("contains no secret-shaped value anywhere", () => {
    expect(serialised).not.toContain("sk-super-secret")
    expect(serialised).not.toContain("sk-oc-secret")
    expect(serialised).not.toContain("sk-cls-secret")
    expect(serialised).not.toContain("agbx_secret")
  })

  it("contains no reference to a Secret either", () => {
    for (const key of Object.keys(flatten(payload))) {
      expect(key).not.toMatch(/credentialsref|secretkeyref|valuefrom|secretname/i)
    }
  })

  it("does not mutate the values it was given", () => {
    const values = filled()
    toClonePayload(values)
    expect(values.claudeApiKey).toBe("sk-super-secret")
  })

  it("keeps everything that describes the agent", () => {
    expect(payload.values.imageRepository).toBe("registry.example.com/brain")
    expect(payload.values.basePrompt).toBe("you are navix")
    expect(payload.values.envName).toBe("navix-hands")
  })
})

describe("import", () => {
  const exported = JSON.stringify(toClonePayload(filled()))

  it("round-trips everything but the secrets", () => {
    const res = fromCloneJson(exported)
    if (!res.ok) throw new Error(`import failed: ${res.error.kind}`)
    expect(res.result.values.imageRepository).toBe("registry.example.com/brain")
    expect(res.result.values.claudeApiKey).toBe("")
    expect(res.result.warnings.some((w) => w.key === "secretsOmitted")).toBe(true)
  })

  it("fills fields the file does not mention from the defaults", () => {
    const partial = JSON.stringify({
      kind: MANAGED_AGENT_CLONE_KIND,
      version: 1,
      exportedAt: "2026-08-04T00:00:00Z",
      values: { name: "only-a-name" },
    })
    const res = fromCloneJson(partial)
    if (!res.ok) throw new Error(`import failed: ${res.error.kind}`)
    expect(res.result.values.name).toBe("only-a-name")
    expect(res.result.values.scenarios).toEqual(managedAgentFormDefaults().scenarios)
    // No undefined leaks in: a register()ed input would flip to uncontrolled.
    for (const [key, value] of Object.entries(res.result.values)) {
      if (Array.isArray(value) || typeof value === "boolean") continue
      expect(value, `${key}`).not.toBeUndefined()
    }
  })

  it("clears cluster ids this deployment does not know", () => {
    const res = fromCloneJson(exported, { knownClusterIDs: ["other"] })
    if (!res.ok) throw new Error("import failed")
    expect(res.result.values.envClusterID).toBe("")
    expect(res.result.warnings.some((w) => w.key === "unknownClusters")).toBe(true)
  })

  it("keeps a cluster id this deployment does know", () => {
    const res = fromCloneJson(exported, { knownClusterIDs: ["gw-a"] })
    if (!res.ok) throw new Error("import failed")
    expect(res.result.values.envClusterID).toBe("gw-a")
  })

  it.each([
    ["not json at all", "{", "json"],
    ["another tool's file", `{"kind":"Something","version":1,"values":{}}`, "kind"],
    ["a newer export", `{"kind":"${MANAGED_AGENT_CLONE_KIND}","version":2,"values":{}}`, "version"],
    [
      "a structurally wrong field",
      `{"kind":"${MANAGED_AGENT_CLONE_KIND}","version":1,"values":{"scenarios":"nope"}}`,
      "schema",
    ],
  ])("reports %s", (_name, text, kind) => {
    const res = fromCloneJson(text)
    expect(res.ok).toBe(false)
    if (!res.ok) expect(res.error.kind).toBe(kind)
  })

  it("accepts values that break the business rules, so the form can show why", () => {
    // Blank API keys fail the create schema on purpose; the import must still land
    // and let the badged fields explain what to fill in.
    const res = fromCloneJson(exported)
    expect(res.ok).toBe(true)
  })
})

describe("cloneFileName", () => {
  it("names the file after the agent", () => {
    expect(cloneFileName(filled())).toBe("managed-agent-navix.json")
  })

  it("falls back when the name is empty", () => {
    expect(cloneFileName(filled({ name: "" }))).toBe("managed-agent-agent.json")
  })
})

/** Flattens to dotted keys so a key-name scan can look at nested objects too. */
function flatten(value: unknown, prefix = "", out: Record<string, unknown> = {}) {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    for (const [k, v] of Object.entries(value)) flatten(v, prefix ? `${prefix}.${k}` : k, out)
  } else if (Array.isArray(value)) {
    value.forEach((v, i) => flatten(v, `${prefix}.${i}`, out))
  } else {
    out[prefix] = value
  }
  return out
}
