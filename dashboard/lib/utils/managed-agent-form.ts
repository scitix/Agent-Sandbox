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

// Form values, schema and spec mapping for the ManagedAgent upsert form.
//
// Deliberately free of React: the form is a 1500-line client component, and the
// only way to test any of this — or to reuse it for the JSON clone envelope — is
// for it to live outside that component. Same factoring as
// lib/utils/template-crd.ts, which came out of the templates form for the same
// reason.
//
// The mapping's load-bearing property is that buildSpec starts from the STORED
// spec: the form covers well under half of the CRD's 175 fields, so every field
// it does not render (brain.extraEnv and friends) survives an edit only because
// it is cloned from `previous`. `previous` must therefore always be the spec as
// the API returned it, never one reconstructed from an import.

import type { FieldErrors } from "react-hook-form"
import { z } from "zod"

import { formatModels, handsModeOf, parseModels } from "@/components/managed-agents/model"
import type {
  HandsInstanceType,
  ManagedAgent,
  ManagedAgentCredentials,
  ManagedAgentScenario,
  ManagedAgentSpec,
} from "@/lib/api/managed-agent-types"
import type { TranslationKey } from "@/messages/_schema"

// ─── Helpers ──────────────────────────────────────────────────────────────────

const DNS_LABEL = /^[a-z]([a-z0-9-]*[a-z0-9])?$/

/** Sentinel for "no harness pin"; the CRD expresses that as an absent field. */
export const INHERIT = "inherit"

const emptyToUndef = (val: unknown) =>
  typeof val === "string" && val.trim() === "" ? undefined : val

/** Splits a textarea into trimmed, de-duplicated, non-empty entries. */
export function splitLines(value?: string): string[] {
  const seen = new Set<string>()
  return (value ?? "")
    .split(/[\n,]+/)
    .map((line) => line.trim())
    .filter((line) => line !== "" && !seen.has(line) && seen.add(line) !== undefined)
}

function toPositiveInt(value?: string): number | undefined {
  if (!value || value.trim() === "") return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? Math.trunc(parsed) : undefined
}

function trimmed(value?: string): string | undefined {
  const out = value?.trim()
  return out ? out : undefined
}

/** Deep copy used to preserve spec fields this form does not expose. */
export function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

// ─── Schema ───────────────────────────────────────────────────────────────────

export const scenarioSchema = z.object({
  name: z
    .string()
    .min(1, "managedAgents.form.errors.scenarioNameRequired")
    .regex(DNS_LABEL, "managedAgents.form.errors.scenarioNameDnsLabel"),
  displayName: z.preprocess(emptyToUndef, z.string().optional()),
  isDefault: z.boolean(),
  prompt: z.preprocess(emptyToUndef, z.string().optional()),
  runtime: z.enum([INHERIT, "claude-code", "opencode"]),
  allow: z.preprocess(emptyToUndef, z.string().optional()),
  interactive: z.boolean(),
})

export const instanceTypeSchema = z.object({
  // Requiredness lives in buildSchema's refinement, not here: an instance-type
  // row exists in the form's default values, but is only rendered under
  // handsMode "auto". A field-level rule would fail a submit in the other two
  // modes over a row nobody can see.
  name: z.string(),
  replicas: z.preprocess(emptyToUndef, z.string().optional()),
  isDefault: z.boolean(),
})

export const baseSchema = z.object({
  name: z
    .string()
    .min(1, "managedAgents.form.errors.nameRequired")
    .regex(DNS_LABEL, "managedAgents.form.errors.nameDnsLabel"),
  displayName: z.preprocess(emptyToUndef, z.string().optional()),
  description: z.preprocess(emptyToUndef, z.string().optional()),
  imageRepository: z.string().min(1, "managedAgents.form.errors.imageRequired"),
  imageTag: z.preprocess(emptyToUndef, z.string().optional()),

  defaultRuntime: z.enum(["claude-code", "opencode"]),

  claudeEnabled: z.boolean(),
  claudeBaseURL: z.preprocess(emptyToUndef, z.string().optional()),
  claudeApiKey: z.preprocess(emptyToUndef, z.string().optional()),
  claudeModels: z.preprocess(emptyToUndef, z.string().optional()),
  claudeDefaultModel: z.preprocess(emptyToUndef, z.string().optional()),

  opencodeEnabled: z.boolean(),
  opencodeBaseURL: z.preprocess(emptyToUndef, z.string().optional()),
  opencodeApiKey: z.preprocess(emptyToUndef, z.string().optional()),
  opencodeModels: z.preprocess(emptyToUndef, z.string().optional()),
  opencodeDefaultModel: z.preprocess(emptyToUndef, z.string().optional()),
  opencodePort: z.preprocess(emptyToUndef, z.string().optional()),

  classifierEnabled: z.boolean(),
  classifierWire: z.enum(["anthropic-messages", "openai-chat"]),
  classifierBaseURL: z.preprocess(emptyToUndef, z.string().optional()),
  classifierModel: z.preprocess(emptyToUndef, z.string().optional()),
  classifierApiKey: z.preprocess(emptyToUndef, z.string().optional()),

  basePrompt: z.preprocess(emptyToUndef, z.string().optional()),

  scenarios: z.array(scenarioSchema).min(1, "managedAgents.form.errors.scenariosRequired"),

  handsMode: z.enum(["auto", "envRef", "external"]),
  autoClusterID: z.preprocess(emptyToUndef, z.string().optional()),
  autoTemplateRef: z.preprocess(emptyToUndef, z.string().optional()),
  autoImage: z.preprocess(emptyToUndef, z.string().optional()),
  autoIdleTimeoutSeconds: z.preprocess(emptyToUndef, z.string().optional()),
  autoStartupTimeoutSeconds: z.preprocess(emptyToUndef, z.string().optional()),
  instanceTypes: z.array(instanceTypeSchema),
  envClusterID: z.preprocess(emptyToUndef, z.string().optional()),
  envName: z.preprocess(emptyToUndef, z.string().optional()),
  envNamespace: z.preprocess(emptyToUndef, z.string().optional()),
  envScalingGroup: z.preprocess(emptyToUndef, z.string().optional()),
  envImage: z.preprocess(emptyToUndef, z.string().optional()),
  externalApiURL: z.preprocess(emptyToUndef, z.string().optional()),
  externalDomain: z.preprocess(emptyToUndef, z.string().optional()),
  externalHTTPS: z.boolean(),
  externalEnvName: z.preprocess(emptyToUndef, z.string().optional()),
  externalImage: z.preprocess(emptyToUndef, z.string().optional()),
  externalScalingGroup: z.preprocess(emptyToUndef, z.string().optional()),
  sandboxApiKey: z.preprocess(emptyToUndef, z.string().optional()),
})

export type FormValues = z.infer<typeof baseSchema>
export type FormErrors = FieldErrors<FormValues>

/** Which credentials the server already holds, so editing may leave them blank. */
export interface StoredCredentials {
  claudeCode: boolean
  opencode: boolean
  classifier: boolean
  sandbox: boolean
}

export function buildSchema(stored: StoredCredentials) {
  return baseSchema.superRefine((v, ctx) => {
    const require = (path: keyof FormValues, message: TranslationKey) =>
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: [path], message })

    // The harness a conversation starts under must also be configured, else the
    // Brain comes up reporting every backend unavailable.
    const defaultConfigured =
      v.defaultRuntime === "claude-code" ? v.claudeEnabled : v.opencodeEnabled
    if (!defaultConfigured) {
      require("defaultRuntime", "managedAgents.form.errors.runtimeNotConfigured")
    }
    if (v.claudeEnabled && !v.claudeApiKey && !stored.claudeCode) {
      require("claudeApiKey", "managedAgents.form.errors.apiKeyRequired")
    }
    if (v.scenarios.filter((s) => s.isDefault).length !== 1) {
      require("scenarios", "managedAgents.form.errors.exactlyOneDefault")
    }

    if (v.handsMode === "auto") {
      if (!v.autoClusterID)
        require("autoClusterID", "managedAgents.form.errors.autoClusterRequired")
      if (!v.autoTemplateRef) {
        require("autoTemplateRef", "managedAgents.form.errors.autoTemplateRequired")
      }
      if (v.instanceTypes.length === 0) {
        require("instanceTypes", "managedAgents.form.errors.instanceTypesRequired")
      }
      v.instanceTypes.forEach((it, i) => {
        if (!it.name.trim()) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ["instanceTypes", i, "name"],
            message: "managedAgents.form.errors.instanceTypeNameRequired",
          })
        }
      })
      return
    }
    if (v.handsMode === "envRef") {
      if (!v.envName) require("envName", "managedAgents.form.errors.envNameRequired")
      return
    }
    if (!v.externalApiURL) require("externalApiURL", "managedAgents.form.errors.apiURLRequired")
    if (!v.externalDomain) require("externalDomain", "managedAgents.form.errors.domainRequired")
    if (!v.externalEnvName) require("externalEnvName", "managedAgents.form.errors.envNameRequired")
    if (!v.sandboxApiKey && !stored.sandbox) {
      require("sandboxApiKey", "managedAgents.form.errors.apiKeyRequired")
    }
  })
}

// ─── Spec ↔ form mapping ──────────────────────────────────────────────────────

/**
 * The empty form: what a create starts from, and what an import merges over.
 *
 * Defined as agentToFormValues(null) rather than a second literal, so a field
 * added to the form cannot get one default here and a different one there.
 */
export function managedAgentFormDefaults(): FormValues {
  return agentToFormValues(null)
}

export function agentToFormValues(agent: ManagedAgent | null): FormValues {
  const spec = agent?.spec
  const claude = spec?.runtime?.claudeCode
  const opencode = spec?.runtime?.opencode
  const classifier = spec?.classifier
  const hands = spec?.hands
  const auto = hands?.auto
  const envRef = hands?.envRef
  const external = hands?.external

  return {
    name: agent?.name ?? "",
    displayName: spec?.displayName ?? "",
    description: spec?.description ?? "",
    imageRepository: spec?.image?.repository ?? "",
    imageTag: spec?.image?.tag ?? "",

    defaultRuntime: spec?.runtime?.default ?? "claude-code",

    claudeEnabled: !!claude || !spec,
    claudeBaseURL: claude?.baseURL ?? "",
    claudeApiKey: "",
    claudeModels: formatModels(claude?.models),
    claudeDefaultModel: claude?.defaultModel ?? "",

    opencodeEnabled: !!opencode && opencode.enabled !== false,
    opencodeBaseURL: opencode?.baseURL ?? "",
    opencodeApiKey: "",
    opencodeModels: formatModels(opencode?.models),
    opencodeDefaultModel: opencode?.defaultModel ?? "",
    opencodePort: opencode?.port ? String(opencode.port) : "",

    classifierEnabled: !!classifier && classifier.enabled !== false,
    classifierWire: classifier?.wire ?? "openai-chat",
    classifierBaseURL: classifier?.baseURL ?? "",
    classifierModel: classifier?.model ?? "",
    classifierApiKey: "",

    basePrompt: spec?.prompt?.inline ?? "",

    scenarios: (spec?.scenarios?.length ? spec.scenarios : undefined)?.map((s) => ({
      name: s.name,
      displayName: s.displayName ?? "",
      isDefault: !!s.default,
      prompt: s.prompt?.inline ?? "",
      runtime: s.runtime ? s.runtime : INHERIT,
      allow: (s.allow ?? []).join("\n"),
      interactive: s.interactive !== false,
    })) ?? [
      {
        name: "default",
        displayName: "",
        isDefault: true,
        prompt: "",
        runtime: INHERIT,
        allow: "",
        interactive: true,
      },
    ],

    handsMode: spec ? handsModeOf(hands) : "auto",
    autoClusterID: auto?.clusterID ?? "",
    autoTemplateRef: auto?.templateRef ?? "",
    autoImage: auto?.image ?? "",
    autoIdleTimeoutSeconds: auto?.idleTimeoutSeconds ? String(auto.idleTimeoutSeconds) : "",
    autoStartupTimeoutSeconds: auto?.startupTimeoutSeconds
      ? String(auto.startupTimeoutSeconds)
      : "",
    instanceTypes: (auto?.instanceTypes?.length ? auto.instanceTypes : undefined)?.map((it) => ({
      name: it.name,
      replicas: it.replicas ? String(it.replicas) : "",
      isDefault: !!it.default,
    })) ?? [{ name: "", replicas: "", isDefault: true }],

    envClusterID: envRef?.clusterID ?? "",
    envName: envRef?.name ?? "",
    envNamespace: envRef?.namespace ?? "",
    envScalingGroup: envRef?.scalingGroup ?? "",
    envImage: envRef?.image ?? "",

    externalApiURL: external?.apiURL ?? "",
    externalDomain: external?.domain ?? "",
    externalHTTPS: external?.https !== false,
    externalEnvName: external?.envName ?? "",
    externalImage: external?.image ?? "",
    externalScalingGroup: external?.scalingGroup ?? "",
    sandboxApiKey: "",
  }
}

/**
 * Projects the form onto a spec. In edit mode the previous spec is the base, so
 * everything the console does not expose (brain tuning, session storage,
 * observability, thread binding, per-scenario sandbox env, materialised
 * credential references) survives a round-trip untouched.
 */
export function buildSpec(v: FormValues, previous?: ManagedAgentSpec): ManagedAgentSpec {
  const spec: ManagedAgentSpec = previous
    ? clone(previous)
    : { image: { repository: "" }, runtime: { default: "claude-code" }, hands: {} }

  // Ownership is stamped by the server from the caller's identity.
  delete spec.owner

  spec.displayName = trimmed(v.displayName)
  spec.description = trimmed(v.description)
  spec.image = { ...spec.image, repository: v.imageRepository, tag: trimmed(v.imageTag) }

  spec.runtime = { ...spec.runtime, default: v.defaultRuntime }
  if (v.claudeEnabled) {
    spec.runtime.claudeCode = {
      ...spec.runtime.claudeCode,
      baseURL: trimmed(v.claudeBaseURL),
      models: parseModels(v.claudeModels),
      defaultModel: trimmed(v.claudeDefaultModel),
    }
  } else {
    delete spec.runtime.claudeCode
  }
  if (v.opencodeEnabled) {
    spec.runtime.opencode = {
      ...spec.runtime.opencode,
      enabled: true,
      port: toPositiveInt(v.opencodePort),
      baseURL: trimmed(v.opencodeBaseURL),
      models: parseModels(v.opencodeModels),
      defaultModel: trimmed(v.opencodeDefaultModel),
    }
  } else {
    delete spec.runtime.opencode
  }

  if (v.classifierEnabled) {
    spec.classifier = {
      ...spec.classifier,
      enabled: true,
      wire: v.classifierWire,
      baseURL: trimmed(v.classifierBaseURL),
      model: trimmed(v.classifierModel),
    }
  } else if (spec.classifier) {
    spec.classifier = { ...spec.classifier, enabled: false }
  }

  const basePrompt = trimmed(v.basePrompt)
  if (basePrompt) {
    spec.prompt = { ...spec.prompt, inline: basePrompt }
  } else if (spec.prompt) {
    delete spec.prompt.inline
    if (!spec.prompt.from && !spec.prompt.append) delete spec.prompt
  }

  const previousScenarios = previous?.scenarios ?? []
  spec.scenarios = v.scenarios.map((s): ManagedAgentScenario => {
    const before = previousScenarios.find((p) => p.name === s.name)
    const scenario: ManagedAgentScenario = {
      ...before,
      name: s.name,
      displayName: trimmed(s.displayName),
      default: s.isDefault || undefined,
      runtime: s.runtime === INHERIT ? undefined : s.runtime,
      allow: s.allow ? splitLines(s.allow) : undefined,
      interactive: s.interactive,
    }
    const prompt = trimmed(s.prompt)
    if (prompt) {
      scenario.prompt = { ...before?.prompt, inline: prompt }
    } else {
      delete scenario.prompt
    }
    return scenario
  })

  // Only the selected branch is submitted; the other two are cleared so the
  // controller never sees two competing declarations of sandbox supply.
  const hands = { ...spec.hands }
  delete hands.auto
  delete hands.envRef
  delete hands.external
  if (v.handsMode === "auto") {
    const instanceTypes: HandsInstanceType[] = v.instanceTypes.map((it) => ({
      ...previous?.hands?.auto?.instanceTypes?.find((p) => p.name === it.name),
      name: it.name,
      replicas: toPositiveInt(it.replicas),
      default: it.isDefault || undefined,
    }))
    hands.auto = {
      clusterID: v.autoClusterID ?? "",
      templateRef: v.autoTemplateRef ?? "",
      image: trimmed(v.autoImage),
      instanceTypes,
      idleTimeoutSeconds: toPositiveInt(v.autoIdleTimeoutSeconds),
      startupTimeoutSeconds: toPositiveInt(v.autoStartupTimeoutSeconds),
    }
  } else if (v.handsMode === "envRef") {
    hands.envRef = {
      name: v.envName ?? "",
      clusterID: trimmed(v.envClusterID),
      namespace: trimmed(v.envNamespace),
      scalingGroup: trimmed(v.envScalingGroup),
      image: trimmed(v.envImage),
    }
  } else {
    hands.external = {
      ...previous?.hands?.external,
      apiURL: v.externalApiURL ?? "",
      domain: v.externalDomain ?? "",
      https: v.externalHTTPS,
      envName: v.externalEnvName ?? "",
      image: trimmed(v.externalImage),
      scalingGroup: trimmed(v.externalScalingGroup),
    }
  }
  spec.hands = hands

  return spec
}

/** Only the keys the user actually typed; a blank field keeps the stored one. */
export function buildCredentials(v: FormValues): ManagedAgentCredentials | undefined {
  const credentials: ManagedAgentCredentials = {}
  if (v.claudeEnabled && v.claudeApiKey) credentials.claudeCodeApiKey = v.claudeApiKey.trim()
  if (v.opencodeEnabled && v.opencodeApiKey) credentials.openCodeApiKey = v.opencodeApiKey.trim()
  if (v.classifierEnabled && v.classifierApiKey) {
    credentials.classifierApiKey = v.classifierApiKey.trim()
  }
  if (v.sandboxApiKey) credentials.sandboxApiKey = v.sandboxApiKey.trim()
  return Object.keys(credentials).length > 0 ? credentials : undefined
}
