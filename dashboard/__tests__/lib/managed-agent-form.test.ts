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
  agentToFormValues,
  buildCredentials,
  buildSchema,
  buildSpec,
  managedAgentFormDefaults,
} from "@/lib/utils/managed-agent-form"
import type { FormValues } from "@/lib/utils/managed-agent-form"
import type { ManagedAgent, ManagedAgentSpec } from "@/lib/api/managed-agent-types"

/** A minimal set of values that satisfies the create-mode schema. */
function validValues(over: Partial<FormValues> = {}): FormValues {
  return {
    ...managedAgentFormDefaults(),
    name: "navix",
    imageRepository: "registry.example.com/brain",
    defaultRuntime: "claude-code",
    claudeEnabled: true,
    claudeBaseURL: "https://api.example.com",
    claudeApiKey: "sk-test",
    claudeModels: "claude-sonnet-5",
    claudeDefaultModel: "claude-sonnet-5",
    handsMode: "envRef",
    envName: "navix-hands",
    scenarios: [
      {
        name: "interactive",
        displayName: "",
        isDefault: true,
        prompt: "",
        runtime: "inherit",
        allow: "",
        interactive: true,
      },
    ],
    ...over,
  } as FormValues
}

/**
 * The form renders well under half of the CRD's fields; everything it does not
 * render survives an edit only because buildSpec clones the stored spec. This is
 * the regression test for the incident where a truncate-and-append edit dropped
 * the whole brain block — 23 env vars, two ports and a volume mount — and nothing
 * failed, the agent just got dumber.
 */
describe("buildSpec preserves what the form does not render", () => {
  const previous = {
    image: { repository: "old/brain" },
    runtime: { default: "claude-code" },
    hands: { envRef: { name: "navix-hands" } },
    brain: {
      extraEnv: [{ name: "NAVIX_SOURCES", value: "scitix,bar" }],
      extraPorts: [{ name: "mcp", containerPort: 4097 }],
    },
    session: { persistence: { enabled: true } },
    observability: { langfuse: { enabled: true, baseURL: "https://lf.example.com" } },
    tools: { clientSide: [{ name: "open_page" }] },
    docs: "# how to use me",
  } as unknown as ManagedAgentSpec

  it("keeps brain, session, observability, tools and docs", () => {
    const spec = buildSpec(validValues(), previous)
    expect(spec.brain?.extraEnv).toEqual(previous.brain?.extraEnv)
    expect(spec.brain?.extraPorts).toEqual(previous.brain?.extraPorts)
    expect(spec.session).toEqual(previous.session)
    expect(spec.observability).toEqual(previous.observability)
    // tools is not in the hand-written TS mirror of the CRD, so it is checked untyped.
    expect((spec as unknown as Record<string, unknown>).tools).toEqual(
      (previous as unknown as Record<string, unknown>).tools,
    )
    expect(spec.docs).toBe(previous.docs)
  })

  it("still applies what the form does render", () => {
    const spec = buildSpec(validValues({ imageRepository: "new/brain" }), previous)
    expect(spec.image?.repository).toBe("new/brain")
  })

  it("invents none of it for a fresh agent", () => {
    const spec = buildSpec(validValues())
    expect(spec.brain).toBeUndefined()
    expect(spec.session).toBeUndefined()
    expect(spec.observability).toBeUndefined()
  })

  it("never sends owner: the server stamps it from the caller", () => {
    const spec = buildSpec(validValues(), {
      ...previous,
      owner: { team: "t", user: "u" },
    } as ManagedAgentSpec)
    expect(spec.owner).toBeUndefined()
  })
})

/** hands is one-of; a leftover branch would either win or be rejected. */
describe("buildSpec emits exactly one hands branch", () => {
  it.each([
    ["envRef", { handsMode: "envRef", envName: "e" }],
    [
      "external",
      {
        handsMode: "external",
        externalApiURL: "https://x",
        externalDomain: "d",
        externalEnvName: "e",
        sandboxApiKey: "k",
      },
    ],
    [
      "auto",
      {
        handsMode: "auto",
        autoClusterID: "c1",
        autoTemplateRef: "tpl",
        instanceTypes: [{ name: "1c2gi", replicas: "1", isDefault: true }],
      },
    ],
  ])("%s", (mode, over) => {
    const spec = buildSpec(validValues(over as Partial<FormValues>))
    const present = (["envRef", "external", "auto"] as const).filter((k) => spec.hands?.[k])
    expect(present).toEqual([mode])
  })
})

describe("buildCredentials", () => {
  it("omits blanks, so an untouched password box means 'keep the stored value'", () => {
    expect(buildCredentials(validValues({ claudeApiKey: "" }))).toBeUndefined()
  })

  it("trims what it does send", () => {
    expect(buildCredentials(validValues({ claudeApiKey: "  sk-1  " }))).toEqual({
      claudeCodeApiKey: "sk-1",
    })
  })
})

describe("the schema's cross-field rules", () => {
  const parse = (v: FormValues, stored = {}) => buildSchema(stored as never).safeParse(v)

  it("accepts a minimal agent", () => {
    expect(parse(validValues()).success).toBe(true)
  })

  it("requires the default harness to be configured", () => {
    expect(parse(validValues({ claudeEnabled: false })).success).toBe(false)
  })

  it("requires exactly one default scenario", () => {
    const two = validValues({
      scenarios: [
        {
          name: "a",
          displayName: "",
          isDefault: true,
          prompt: "",
          runtime: "inherit",
          allow: "",
          interactive: true,
        },
        {
          name: "b",
          displayName: "",
          isDefault: true,
          prompt: "",
          runtime: "inherit",
          allow: "",
          interactive: true,
        },
      ] as FormValues["scenarios"],
    })
    expect(parse(two).success).toBe(false)
  })

  it("stops demanding an API key once one is stored", () => {
    const blank = validValues({ claudeApiKey: "" })
    expect(parse(blank).success).toBe(false)
    expect(parse(blank, { claudeCode: true }).success).toBe(true)
  })
})

describe("agentToFormValues", () => {
  it("uses empty strings, never undefined, so inputs stay controlled", () => {
    const values = managedAgentFormDefaults()
    for (const [key, value] of Object.entries(values)) {
      if (Array.isArray(value) || typeof value === "boolean") continue
      expect(value, `${key} must not be undefined`).not.toBeUndefined()
    }
  })

  it("round-trips a stored agent's rendered fields", () => {
    const agent = {
      name: "navix",
      spec: {
        displayName: "Navix",
        image: { repository: "r", tag: "t" },
        runtime: {
          default: "claude-code",
          claudeCode: { baseURL: "b", defaultModel: "m", models: [{ id: "m" }] },
        },
        hands: { envRef: { name: "e" } },
      },
    } as unknown as ManagedAgent
    const v = agentToFormValues(agent)
    expect(v.name).toBe("navix")
    expect(v.imageRepository).toBe("r")
    expect(v.imageTag).toBe("t")
    expect(v.handsMode).toBe("envRef")
    expect(v.envName).toBe("e")
  })
})

/**
 * The deployment answers for the Brain image and the sandbox supply, and the form
 * has to stop asking for them — that is the whole difference between "fill in nine
 * fields" and "type a name and a prompt".
 */
describe("platform defaults", () => {
  const platform = {
    brainImage: { repository: "reg/brain", tag: "v1" },
    hands: { envName: "navix", image: "reg/sbx:v1", credentialConfigured: true },
    modelProvider: {
      baseURL: "https://api.example.com/model-api",
      models: [
        { id: "claude-sonnet-5", name: "Claude Sonnet 5" },
        { id: "glm-5.2", nonReasoning: true },
      ],
      defaultModel: "claude-sonnet-5",
    },
  }

  it("starts a create on the platform's sandbox supply", () => {
    expect(managedAgentFormDefaults(platform).handsMode).toBe("platformDefault")
  })

  it("falls back to auto when the deployment publishes no supply", () => {
    expect(managedAgentFormDefaults({}).handsMode).toBe("auto")
    expect(managedAgentFormDefaults().handsMode).toBe("auto")
  })

  it("seeds the model endpoint and dropdown for both harnesses", () => {
    const v = managedAgentFormDefaults(platform)
    expect(v.claudeBaseURL).toBe("https://api.example.com/model-api")
    expect(v.opencodeBaseURL).toBe("https://api.example.com/model-api")
    expect(v.claudeModels).toContain("claude-sonnet-5 | Claude Sonnet 5")
    expect(v.claudeDefaultModel).toBe("claude-sonnet-5")
  })

  // A reasoning model in that slot returns its whole budget as chain of thought
  // and empty content, which reads as "same topic" on every turn.
  it("picks a non-reasoning model for the classifier", () => {
    expect(managedAgentFormDefaults(platform).classifierModel).toBe("glm-5.2")
  })

  it("never seeds a credential", () => {
    const v = managedAgentFormDefaults(platform)
    expect(v.claudeApiKey).toBe("")
    expect(v.opencodeApiKey).toBe("")
    expect(v.sandboxApiKey).toBe("")
  })

  it("stops requiring an image once the deployment publishes one", () => {
    const blank = validValues({ imageRepository: "" })
    expect(buildSchema({} as never).safeParse(blank).success).toBe(false)
    expect(
      buildSchema({} as never, { brainImage: true, hands: true }).safeParse(blank).success,
    ).toBe(true)
  })

  // Saved against a deployment with no default, this agent reconciles to
  // HandsReady=False — an agent that answers and then cannot run a command.
  it("refuses platformDefault where there is none", () => {
    const v = validValues({ handsMode: "platformDefault" })
    expect(buildSchema({} as never, { brainImage: true, hands: false }).safeParse(v).success).toBe(
      false,
    )
    expect(buildSchema({} as never, { brainImage: true, hands: true }).safeParse(v).success).toBe(
      true,
    )
  })

  it("submits no image at all when the field is blank", () => {
    const spec = buildSpec(validValues({ imageRepository: "" }))
    expect(spec.image).toBeUndefined()
  })

  it("submits no hands branch on platformDefault, keeping binding", () => {
    const previous = {
      runtime: { default: "claude-code" },
      hands: {
        external: { apiURL: "https://old", domain: "d", envName: "e" },
        binding: { timeoutSeconds: 120 },
      },
    } as unknown as ManagedAgentSpec
    const spec = buildSpec(validValues({ handsMode: "platformDefault" }), previous)
    expect(spec.hands?.external).toBeUndefined()
    expect(spec.hands?.envRef).toBeUndefined()
    expect(spec.hands?.auto).toBeUndefined()
    expect(spec.hands?.binding?.timeoutSeconds).toBe(120)
  })

  it("reads an agent with no branch as being on the platform default", () => {
    const agent = {
      name: "fresh",
      spec: { runtime: { default: "claude-code" }, hands: {} },
    } as unknown as ManagedAgent
    expect(agentToFormValues(agent).handsMode).toBe("platformDefault")
  })
})
