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

"use client"

import { Fragment, useMemo } from "react"
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form"
import type { Control, FieldErrors, UseFormRegister, UseFormSetValue } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Plus, Save, Trash2 } from "lucide-react"

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import { Button } from "@/components/ui/button"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { formatModels, handsModeOf, parseModels } from "@/components/managed-agents/model"
import type {
  AgentSandboxEnvSummary,
  AgentSandboxTemplateSummary,
  ClusterEntry,
} from "@/lib/api/client"
import type {
  HandsInstanceType,
  ManagedAgent,
  ManagedAgentCredentials,
  ManagedAgentScenario,
  ManagedAgentSpec,
} from "@/lib/api/managed-agent-types"
import { clustersAtom } from "@/lib/atoms"
import {
  envsQueryOptions,
  managedAgentQueryOptions,
  templatesQueryOptions,
  useCreateManagedAgent,
  useUpdateManagedAgent,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/messages/_schema"

// ─── Helpers ──────────────────────────────────────────────────────────────────

const DNS_LABEL = /^[a-z]([a-z0-9-]*[a-z0-9])?$/

/** Sentinel for "no harness pin"; the CRD expresses that as an absent field. */
const INHERIT = "inherit"

const emptyToUndef = (val: unknown) =>
  typeof val === "string" && val.trim() === "" ? undefined : val

/** Splits a textarea into trimmed, de-duplicated, non-empty entries. */
function splitLines(value?: string): string[] {
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
function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

// ─── Schema ───────────────────────────────────────────────────────────────────

const scenarioSchema = z.object({
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

const instanceTypeSchema = z.object({
  name: z.string().min(1, "managedAgents.form.errors.instanceTypeNameRequired"),
  replicas: z.preprocess(emptyToUndef, z.string().optional()),
  isDefault: z.boolean(),
})

const baseSchema = z.object({
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

type FormValues = z.infer<typeof baseSchema>
type FormErrors = FieldErrors<FormValues>

/** Which credentials the server already holds, so editing may leave them blank. */
interface StoredCredentials {
  claudeCode: boolean
  opencode: boolean
  classifier: boolean
  sandbox: boolean
}

function buildSchema(stored: StoredCredentials) {
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

function agentToFormValues(agent: ManagedAgent | null): FormValues {
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
function buildSpec(v: FormValues, previous?: ManagedAgentSpec): ManagedAgentSpec {
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
function buildCredentials(v: FormValues): ManagedAgentCredentials | undefined {
  const credentials: ManagedAgentCredentials = {}
  if (v.claudeEnabled && v.claudeApiKey) credentials.claudeCodeApiKey = v.claudeApiKey.trim()
  if (v.opencodeEnabled && v.opencodeApiKey) credentials.openCodeApiKey = v.opencodeApiKey.trim()
  if (v.classifierEnabled && v.classifierApiKey) {
    credentials.classifierApiKey = v.classifierApiKey.trim()
  }
  if (v.sandboxApiKey) credentials.sandboxApiKey = v.sandboxApiKey.trim()
  return Object.keys(credentials).length > 0 ? credentials : undefined
}

// ─── Shared presentation ──────────────────────────────────────────────────────

const LABEL_CN = "text-muted-foreground font-mono text-[11px] tracking-wider uppercase"
const INPUT_CN = "h-9 font-mono text-sm"

function SectionHeading({ title, hint }: { title: string; hint?: string }) {
  return (
    <div>
      <h3 className="text-muted-foreground font-mono text-[11px] font-bold tracking-[0.12em] uppercase">
        {title}
      </h3>
      {hint && <p className="text-muted-foreground mt-1 text-xs">{hint}</p>}
    </div>
  )
}

/** Renders a zod message, which is always a translation key in this form. */
function ErrorText({ message }: { message?: string }) {
  const { t } = useTranslation()
  if (!message) return null
  return <FieldError>{t(message as TranslationKey)}</FieldError>
}

function RequiredMark() {
  return <span className="text-destructive">*</span>
}

interface SectionProps {
  control: Control<FormValues>
  register: UseFormRegister<FormValues>
  errors: FormErrors
  isEdit: boolean
  stored: StoredCredentials
}

type ApiKeyFieldName = "claudeApiKey" | "opencodeApiKey" | "classifierApiKey" | "sandboxApiKey"

/** Password input whose placeholder tells the user that blank means "unchanged". */
function ApiKeyField({
  label,
  register,
  name,
  errors,
  stored,
  required,
  description,
}: {
  label: string
  register: UseFormRegister<FormValues>
  name: ApiKeyFieldName
  errors: FormErrors
  stored: boolean
  required: boolean
  description?: string
}) {
  const { t } = useTranslation()
  const error = errors[name]
  return (
    <Field data-invalid={!!error}>
      <FieldLabel className={LABEL_CN}>
        {label} {required && !stored && <RequiredMark />}
      </FieldLabel>
      <Input
        type="password"
        autoComplete="new-password"
        placeholder={stored ? t("managedAgents.form.apiKeyKeep") : "sk-…"}
        className={INPUT_CN}
        {...register(name)}
      />
      <ErrorText message={error?.message} />
      <FieldDescription>
        {stored
          ? t("managedAgents.form.apiKeyStored")
          : (description ?? t("managedAgents.form.apiKeyDesc"))}
      </FieldDescription>
    </Field>
  )
}

// ─── Basics ───────────────────────────────────────────────────────────────────

function BasicsSection({ register, errors, isEdit }: SectionProps) {
  const { t } = useTranslation()
  return (
    <section className="space-y-4">
      <SectionHeading
        title={t("managedAgents.form.section.basics")}
        hint={t("managedAgents.form.section.basicsHint")}
      />
      <Field data-invalid={!!errors.name}>
        <FieldLabel className={LABEL_CN}>
          {t("managedAgents.form.name")} <RequiredMark />
        </FieldLabel>
        <Input
          disabled={isEdit}
          placeholder="support-agent"
          className={INPUT_CN}
          {...register("name")}
        />
        <ErrorText message={errors.name?.message} />
        <FieldDescription>{t("managedAgents.form.nameDesc")}</FieldDescription>
      </Field>

      <div className="grid grid-cols-2 gap-3">
        <Field>
          <FieldLabel className={LABEL_CN}>{t("managedAgents.form.displayName")}</FieldLabel>
          <Input className={INPUT_CN} {...register("displayName")} />
          <FieldDescription>{t("managedAgents.form.displayNameDesc")}</FieldDescription>
        </Field>
        <Field>
          <FieldLabel className={LABEL_CN}>{t("managedAgents.form.imageTag")}</FieldLabel>
          <Input placeholder="v1.0.0" className={INPUT_CN} {...register("imageTag")} />
        </Field>
      </div>

      <Field>
        <FieldLabel className={LABEL_CN}>{t("managedAgents.form.description")}</FieldLabel>
        <Textarea rows={2} className="font-mono text-sm" {...register("description")} />
      </Field>

      <Field data-invalid={!!errors.imageRepository}>
        <FieldLabel className={LABEL_CN}>
          {t("managedAgents.form.imageRepository")} <RequiredMark />
        </FieldLabel>
        <Input
          placeholder="registry.example.com/agents/brain"
          className={INPUT_CN}
          {...register("imageRepository")}
        />
        <ErrorText message={errors.imageRepository?.message} />
        <FieldDescription>{t("managedAgents.form.imageRepositoryDesc")}</FieldDescription>
      </Field>
    </section>
  )
}

// ─── Harnesses ────────────────────────────────────────────────────────────────

function HarnessToggle({
  control,
  name,
  title,
}: {
  control: Control<FormValues>
  name: "claudeEnabled" | "opencodeEnabled" | "classifierEnabled"
  title: string
}) {
  return (
    <div className="flex items-center justify-between">
      <h4 className="font-mono text-xs font-semibold">{title}</h4>
      <Controller
        control={control}
        name={name}
        render={({ field }) => <Switch checked={field.value} onCheckedChange={field.onChange} />}
      />
    </div>
  )
}

function RuntimeSection({ control, register, errors, stored }: SectionProps) {
  const { t } = useTranslation()
  const claudeEnabled = useWatch({ control, name: "claudeEnabled" })
  const opencodeEnabled = useWatch({ control, name: "opencodeEnabled" })

  return (
    <section className="space-y-4">
      <SectionHeading
        title={t("managedAgents.form.section.runtime")}
        hint={t("managedAgents.form.section.runtimeHint")}
      />

      <Field data-invalid={!!errors.defaultRuntime}>
        <FieldLabel className={LABEL_CN}>
          {t("managedAgents.form.defaultRuntime")} <RequiredMark />
        </FieldLabel>
        <Controller
          control={control}
          name="defaultRuntime"
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="claude-code">claude-code</SelectItem>
                <SelectItem value="opencode">opencode</SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        <ErrorText message={errors.defaultRuntime?.message} />
        <FieldDescription>{t("managedAgents.form.defaultRuntimeDesc")}</FieldDescription>
      </Field>

      <div className="space-y-4 rounded-md border p-3">
        <HarnessToggle control={control} name="claudeEnabled" title="claude-code" />
        {claudeEnabled && (
          <div className="space-y-3">
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.baseURL")}</FieldLabel>
              <Input
                placeholder="https://api.anthropic.com"
                className={INPUT_CN}
                {...register("claudeBaseURL")}
              />
              <FieldDescription>{t("managedAgents.form.claudeBaseURLDesc")}</FieldDescription>
            </Field>
            <ApiKeyField
              label={t("managedAgents.form.apiKey")}
              name="claudeApiKey"
              register={register}
              errors={errors}
              stored={stored.claudeCode}
              required
              description={t("managedAgents.form.claudeApiKeyDesc")}
            />
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.models")}</FieldLabel>
              <Textarea
                rows={4}
                placeholder={
                  "claude-sonnet-4-5 | Sonnet 4.5\nclaude-haiku-4-5 | Haiku 4.5 | nonreasoning"
                }
                className="font-mono text-sm"
                {...register("claudeModels")}
              />
              <FieldDescription>{t("managedAgents.form.modelsDesc")}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.defaultModel")}</FieldLabel>
              <Input className={INPUT_CN} {...register("claudeDefaultModel")} />
              <FieldDescription>{t("managedAgents.form.defaultModelDesc")}</FieldDescription>
            </Field>
          </div>
        )}
      </div>

      <div className="space-y-4 rounded-md border p-3">
        <HarnessToggle control={control} name="opencodeEnabled" title="opencode" />
        {opencodeEnabled && (
          <div className="space-y-3">
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.baseURL")}</FieldLabel>
              <Input
                placeholder="https://api.example.com/v1"
                className={INPUT_CN}
                {...register("opencodeBaseURL")}
              />
              <FieldDescription>{t("managedAgents.form.opencodeBaseURLDesc")}</FieldDescription>
            </Field>
            <ApiKeyField
              label={t("managedAgents.form.apiKey")}
              name="opencodeApiKey"
              register={register}
              errors={errors}
              stored={stored.opencode}
              required={false}
              description={t("managedAgents.form.opencodeApiKeyDesc")}
            />
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.models")}</FieldLabel>
              <Textarea
                rows={4}
                placeholder={"gpt-5 | GPT-5\ngpt-5-mini | GPT-5 mini | nonreasoning"}
                className="font-mono text-sm"
                {...register("opencodeModels")}
              />
              <FieldDescription>{t("managedAgents.form.modelsDesc")}</FieldDescription>
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field>
                <FieldLabel className={LABEL_CN}>{t("managedAgents.form.defaultModel")}</FieldLabel>
                <Input className={INPUT_CN} {...register("opencodeDefaultModel")} />
              </Field>
              <Field>
                <FieldLabel className={LABEL_CN}>{t("managedAgents.form.opencodePort")}</FieldLabel>
                <Input placeholder="4096" className={INPUT_CN} {...register("opencodePort")} />
                <FieldDescription>{t("managedAgents.form.opencodePortDesc")}</FieldDescription>
              </Field>
            </div>
          </div>
        )}
      </div>
    </section>
  )
}

// ─── Classifier & base prompt (advanced) ──────────────────────────────────────

function ClassifierSection({ control, register, errors, stored }: SectionProps) {
  const { t } = useTranslation()
  const enabled = useWatch({ control, name: "classifierEnabled" })
  return (
    <section className="space-y-4">
      <SectionHeading
        title={t("managedAgents.form.section.classifier")}
        hint={t("managedAgents.form.section.classifierHint")}
      />
      <div className="space-y-4 rounded-md border p-3">
        <HarnessToggle
          control={control}
          name="classifierEnabled"
          title={t("managedAgents.form.classifierToggle")}
        />
        {enabled && (
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <Field>
                <FieldLabel className={LABEL_CN}>{t("managedAgents.form.wire")}</FieldLabel>
                <Controller
                  control={control}
                  name="classifierWire"
                  render={({ field }) => (
                    <Select value={field.value} onValueChange={field.onChange}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="openai-chat">openai-chat</SelectItem>
                        <SelectItem value="anthropic-messages">anthropic-messages</SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
              </Field>
              <Field>
                <FieldLabel className={LABEL_CN}>{t("managedAgents.form.model")}</FieldLabel>
                <Input className={INPUT_CN} {...register("classifierModel")} />
                <FieldDescription>{t("managedAgents.form.classifierModelDesc")}</FieldDescription>
              </Field>
            </div>
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.baseURL")}</FieldLabel>
              <Input className={INPUT_CN} {...register("classifierBaseURL")} />
            </Field>
            <ApiKeyField
              label={t("managedAgents.form.apiKey")}
              name="classifierApiKey"
              register={register}
              errors={errors}
              stored={stored.classifier}
              required={false}
            />
          </div>
        )}
      </div>
    </section>
  )
}

function PromptSection({ register }: SectionProps) {
  const { t } = useTranslation()
  return (
    <section className="space-y-3">
      <SectionHeading
        title={t("managedAgents.form.section.prompt")}
        hint={t("managedAgents.form.section.promptHint")}
      />
      <Textarea rows={5} className="font-mono text-sm" {...register("basePrompt")} />
    </section>
  )
}

// ─── Scenarios ────────────────────────────────────────────────────────────────

function ScenariosSection({
  control,
  register,
  errors,
  setValue,
}: SectionProps & { setValue: UseFormSetValue<FormValues> }) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({ control, name: "scenarios" })
  const scenarios = useWatch({ control, name: "scenarios" })

  // Exactly one scenario carries `default: true`, so picking one clears the rest.
  const selectDefault = (index: number) => {
    fields.forEach((_, i) => setValue(`scenarios.${i}.isDefault`, i === index))
  }

  return (
    <section className="space-y-3">
      <SectionHeading
        title={t("managedAgents.form.section.scenarios")}
        hint={t("managedAgents.form.scenariosDesc")}
      />
      <ErrorText message={errors.scenarios?.message} />

      {fields.map((row, index) => (
        <div key={row.id} className="space-y-3 rounded-md border p-3">
          <div className="flex items-center justify-between">
            <span className={LABEL_CN}>
              {t("managedAgents.form.scenarioIndex", { index: index + 1 })}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="text-destructive h-6 w-6"
              disabled={fields.length === 1}
              onClick={() => remove(index)}
              aria-label={t("managedAgents.form.removeScenario")}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field data-invalid={!!errors.scenarios?.[index]?.name}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.scenarioName")} <RequiredMark />
              </FieldLabel>
              <Input className={INPUT_CN} {...register(`scenarios.${index}.name`)} />
              <ErrorText message={errors.scenarios?.[index]?.name?.message} />
            </Field>
            <Field>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.scenarioDisplayName")}
              </FieldLabel>
              <Input className={INPUT_CN} {...register(`scenarios.${index}.displayName`)} />
            </Field>
          </div>

          <Field>
            <FieldLabel className={LABEL_CN}>{t("managedAgents.form.scenarioPrompt")}</FieldLabel>
            <Textarea
              rows={3}
              className="font-mono text-sm"
              {...register(`scenarios.${index}.prompt`)}
            />
            <FieldDescription>{t("managedAgents.form.scenarioPromptDesc")}</FieldDescription>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.scenarioRuntime")}
              </FieldLabel>
              <Controller
                control={control}
                name={`scenarios.${index}.runtime`}
                render={({ field }) => (
                  <Select value={field.value} onValueChange={field.onChange}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={INHERIT}>
                        {t("managedAgents.form.inheritRuntime")}
                      </SelectItem>
                      <SelectItem value="claude-code">claude-code</SelectItem>
                      <SelectItem value="opencode">opencode</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
              <FieldDescription>{t("managedAgents.form.scenarioRuntimeDesc")}</FieldDescription>
            </Field>

            <div className="flex flex-col justify-center gap-3">
              <div className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs">{t("managedAgents.form.scenarioDefault")}</span>
                <Switch
                  checked={!!scenarios?.[index]?.isDefault}
                  onCheckedChange={(checked) => checked && selectDefault(index)}
                />
              </div>
              <div className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs">
                  {t("managedAgents.form.scenarioInteractive")}
                </span>
                <Controller
                  control={control}
                  name={`scenarios.${index}.interactive`}
                  render={({ field }) => (
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  )}
                />
              </div>
            </div>
          </div>

          <Field>
            <FieldLabel className={LABEL_CN}>{t("managedAgents.form.scenarioAllow")}</FieldLabel>
            <Textarea
              rows={2}
              className="font-mono text-sm"
              {...register(`scenarios.${index}.allow`)}
            />
            <FieldDescription>{t("managedAgents.form.scenarioAllowDesc")}</FieldDescription>
          </Field>
        </div>
      ))}

      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-7 gap-1 font-mono text-[11px]"
        onClick={() =>
          append({
            name: "",
            displayName: "",
            isDefault: false,
            prompt: "",
            runtime: INHERIT,
            allow: "",
            interactive: true,
          })
        }
      >
        <Plus className="h-3 w-3" />
        {t("managedAgents.form.addScenario")}
      </Button>
    </section>
  )
}

// ─── Hands ────────────────────────────────────────────────────────────────────

function ClusterCombobox({
  value,
  onChange,
  invalid,
}: {
  value?: string
  onChange: (next: string) => void
  invalid?: boolean
}) {
  const { t } = useTranslation()
  const clusters = useAtomValue(clustersAtom).clusters
  const selected = clusters.find((c) => c.id === value) ?? null
  return (
    <Combobox
      autoHighlight
      value={selected}
      onValueChange={(val: ClusterEntry | null) => onChange(val?.id ?? "")}
      items={clusters}
      itemToStringLabel={(c: ClusterEntry) => c.name || c.id}
    >
      <ComboboxInput aria-invalid={invalid} placeholder={t("common.search")} className={INPUT_CN} />
      <ComboboxContent>
        <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
        <ComboboxList>
          {(c: ClusterEntry) => (
            <ComboboxItem key={c.id} value={c}>
              <span>{c.name || c.id}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}

/** Sandbox image input plus the pointer at the image-build catalogue. */
function SandboxImageField({
  register,
  name,
}: {
  register: UseFormRegister<FormValues>
  name: "autoImage" | "envImage" | "externalImage"
}) {
  const { t } = useTranslation()
  return (
    <Field>
      <FieldLabel className={LABEL_CN}>{t("managedAgents.form.sandboxImage")}</FieldLabel>
      <Input
        placeholder="registry.example.com/sandboxes/base:1.0"
        className={INPUT_CN}
        {...register(name)}
      />
      <FieldDescription>{t("managedAgents.form.sandboxImageDesc")}</FieldDescription>
      <FieldDescription>{t("managedAgents.form.sandboxImageBuildHint")}</FieldDescription>
    </Field>
  )
}

function HandsSection({ control, register, errors, stored }: SectionProps) {
  const { t } = useTranslation()
  const handsMode = useWatch({ control, name: "handsMode" })
  const { data: envs } = useQuery({ ...envsQueryOptions(), enabled: handsMode === "envRef" })
  const { data: templates } = useQuery({
    ...templatesQueryOptions(),
    enabled: handsMode === "auto",
  })
  const { fields, append, remove } = useFieldArray({ control, name: "instanceTypes" })

  return (
    <section className="space-y-4">
      <SectionHeading
        title={t("managedAgents.form.section.hands")}
        hint={t("managedAgents.form.section.handsHint")}
      />

      <Controller
        control={control}
        name="handsMode"
        render={({ field }) => (
          <Tabs value={field.value} onValueChange={field.onChange}>
            <TabsList className="w-full">
              <TabsTrigger value="auto" className="flex-1 text-xs">
                {t("managedAgents.handsMode.auto")}
              </TabsTrigger>
              <TabsTrigger value="envRef" className="flex-1 text-xs">
                {t("managedAgents.handsMode.envRef")}
              </TabsTrigger>
              <TabsTrigger value="external" className="flex-1 text-xs">
                {t("managedAgents.handsMode.external")}
              </TabsTrigger>
            </TabsList>
          </Tabs>
        )}
      />
      <p className="text-muted-foreground text-xs">
        {handsMode === "auto"
          ? t("managedAgents.handsMode.autoDesc")
          : handsMode === "envRef"
            ? t("managedAgents.handsMode.envRefDesc")
            : t("managedAgents.handsMode.externalDesc")}
      </p>

      {handsMode === "auto" && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field data-invalid={!!errors.autoClusterID}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.clusterID")} <RequiredMark />
              </FieldLabel>
              <Controller
                control={control}
                name="autoClusterID"
                render={({ field, fieldState }) => (
                  <ClusterCombobox
                    value={field.value}
                    onChange={field.onChange}
                    invalid={fieldState.invalid}
                  />
                )}
              />
              <ErrorText message={errors.autoClusterID?.message} />
            </Field>
            <Field data-invalid={!!errors.autoTemplateRef}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.templateRef")} <RequiredMark />
              </FieldLabel>
              <Controller
                control={control}
                name="autoTemplateRef"
                render={({ field, fieldState }) => {
                  const selected = templates?.find((tpl) => tpl.name === field.value) ?? null
                  return (
                    <Combobox
                      autoHighlight
                      value={selected}
                      onValueChange={(val: AgentSandboxTemplateSummary | null) =>
                        field.onChange(val?.name ?? "")
                      }
                      items={templates}
                      itemToStringLabel={(tpl: AgentSandboxTemplateSummary) => tpl.name}
                    >
                      <ComboboxInput
                        aria-invalid={fieldState.invalid}
                        placeholder={t("common.search")}
                        className={INPUT_CN}
                      />
                      <ComboboxContent>
                        <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
                        <ComboboxList>
                          {(tpl: AgentSandboxTemplateSummary) => (
                            <ComboboxItem key={tpl.name} value={tpl}>
                              <span className="font-mono text-xs">{tpl.name}</span>
                            </ComboboxItem>
                          )}
                        </ComboboxList>
                      </ComboboxContent>
                    </Combobox>
                  )
                }}
              />
              <ErrorText message={errors.autoTemplateRef?.message} />
            </Field>
          </div>

          <SandboxImageField register={register} name="autoImage" />

          <div className="space-y-2">
            <span className={LABEL_CN}>
              {t("managedAgents.form.instanceTypes")} <RequiredMark />
            </span>
            <ErrorText message={errors.instanceTypes?.message} />
            {fields.map((row, index) => (
              <div key={row.id} className="flex items-end gap-2">
                <Field className="flex-1" data-invalid={!!errors.instanceTypes?.[index]?.name}>
                  <FieldLabel className={LABEL_CN}>
                    {t("managedAgents.form.instanceTypeName")}
                  </FieldLabel>
                  <Input className={INPUT_CN} {...register(`instanceTypes.${index}.name`)} />
                  <ErrorText message={errors.instanceTypes?.[index]?.name?.message} />
                </Field>
                <Field className="w-28">
                  <FieldLabel className={LABEL_CN}>
                    {t("managedAgents.form.instanceTypeReplicas")}
                  </FieldLabel>
                  <Input className={INPUT_CN} {...register(`instanceTypes.${index}.replicas`)} />
                </Field>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="text-destructive mb-1 h-8 w-8"
                  disabled={fields.length === 1}
                  onClick={() => remove(index)}
                  aria-label={t("managedAgents.form.removeInstanceType")}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 gap-1 font-mono text-[11px]"
              onClick={() => append({ name: "", replicas: "", isDefault: false })}
            >
              <Plus className="h-3 w-3" />
              {t("managedAgents.form.addInstanceType")}
            </Button>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.idleTimeout")}</FieldLabel>
              <Input className={INPUT_CN} {...register("autoIdleTimeoutSeconds")} />
            </Field>
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.startupTimeout")}</FieldLabel>
              <Input className={INPUT_CN} {...register("autoStartupTimeoutSeconds")} />
            </Field>
          </div>
        </div>
      )}

      {handsMode === "envRef" && (
        <div className="space-y-3">
          <Field data-invalid={!!errors.envName}>
            <FieldLabel className={LABEL_CN}>
              {t("managedAgents.form.envName")} <RequiredMark />
            </FieldLabel>
            <Controller
              control={control}
              name="envName"
              render={({ field, fieldState }) => {
                const selected = envs?.find((e) => e.name === field.value) ?? null
                return (
                  <Combobox
                    autoHighlight
                    value={selected}
                    onValueChange={(val: AgentSandboxEnvSummary | null) =>
                      field.onChange(val?.name ?? "")
                    }
                    items={envs}
                    itemToStringLabel={(e: AgentSandboxEnvSummary) => e.name}
                  >
                    <ComboboxInput
                      aria-invalid={fieldState.invalid}
                      placeholder={t("common.search")}
                      className={INPUT_CN}
                    />
                    <ComboboxContent>
                      <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
                      <ComboboxList>
                        {(e: AgentSandboxEnvSummary) => (
                          <ComboboxItem key={e.name} value={e}>
                            <span className="font-mono text-xs">{e.name}</span>
                          </ComboboxItem>
                        )}
                      </ComboboxList>
                    </ComboboxContent>
                  </Combobox>
                )
              }}
            />
            <ErrorText message={errors.envName?.message} />
            <FieldDescription>{t("managedAgents.form.envNameDesc")}</FieldDescription>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.clusterID")}</FieldLabel>
              <Controller
                control={control}
                name="envClusterID"
                render={({ field }) => (
                  <ClusterCombobox value={field.value} onChange={field.onChange} />
                )}
              />
              <FieldDescription>{t("managedAgents.form.clusterIDDesc")}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.namespace")}</FieldLabel>
              <Input className={INPUT_CN} {...register("envNamespace")} />
              <FieldDescription>{t("managedAgents.form.namespaceDesc")}</FieldDescription>
            </Field>
          </div>

          <Field>
            <FieldLabel className={LABEL_CN}>{t("managedAgents.form.scalingGroup")}</FieldLabel>
            <Input className={INPUT_CN} {...register("envScalingGroup")} />
          </Field>

          <SandboxImageField register={register} name="envImage" />
        </div>
      )}

      {handsMode === "external" && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field data-invalid={!!errors.externalApiURL}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.externalApiURL")} <RequiredMark />
              </FieldLabel>
              <Input
                placeholder="https://api.example.com"
                className={INPUT_CN}
                {...register("externalApiURL")}
              />
              <ErrorText message={errors.externalApiURL?.message} />
            </Field>
            <Field data-invalid={!!errors.externalDomain}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.externalDomain")} <RequiredMark />
              </FieldLabel>
              <Input
                placeholder="sandbox.example.com/gateway"
                className={INPUT_CN}
                {...register("externalDomain")}
              />
              <ErrorText message={errors.externalDomain?.message} />
              <FieldDescription>{t("managedAgents.form.externalDomainDesc")}</FieldDescription>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field data-invalid={!!errors.externalEnvName}>
              <FieldLabel className={LABEL_CN}>
                {t("managedAgents.form.externalEnvName")} <RequiredMark />
              </FieldLabel>
              <Input
                placeholder="cluster::navix"
                className={INPUT_CN}
                {...register("externalEnvName")}
              />
              <ErrorText message={errors.externalEnvName?.message} />
              <FieldDescription>{t("managedAgents.form.externalEnvNameDesc")}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel className={LABEL_CN}>{t("managedAgents.form.scalingGroup")}</FieldLabel>
              <Input className={INPUT_CN} {...register("externalScalingGroup")} />
            </Field>
          </div>

          <Controller
            control={control}
            name="externalHTTPS"
            render={({ field }) => (
              <div className="flex items-center justify-between rounded-md border p-3">
                <div className="space-y-0.5 pr-3">
                  <FieldLabel>{t("managedAgents.form.externalHTTPS")}</FieldLabel>
                  <FieldDescription>{t("managedAgents.form.externalHTTPSDesc")}</FieldDescription>
                </div>
                <Switch checked={field.value} onCheckedChange={field.onChange} />
              </div>
            )}
          />

          <SandboxImageField register={register} name="externalImage" />

          <ApiKeyField
            label={t("managedAgents.form.sandboxApiKey")}
            name="sandboxApiKey"
            register={register}
            errors={errors}
            stored={stored.sandbox}
            required
            description={t("managedAgents.form.sandboxApiKeyDesc")}
          />
        </div>
      )}
    </section>
  )
}

// ─── Sheet shell ──────────────────────────────────────────────────────────────

interface Props {
  /** Name of the agent to edit; null/omitted opens the sheet in create mode. */
  agentName?: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved?: (name: string) => void
}

export function UpsertManagedAgentSheet({ agentName, open, onOpenChange, onSaved }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        {open && (
          <UpsertLoader
            agentName={agentName ?? null}
            onClose={() => onOpenChange(false)}
            onSaved={onSaved}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

/**
 * Resolves edit mode to a full agent (refetched via GET) before mounting the
 * form, so defaults come from authoritative state rather than a list-row
 * projection. Create mode mounts immediately.
 */
function UpsertLoader({
  agentName,
  onClose,
  onSaved,
}: {
  agentName: string | null
  onClose: () => void
  onSaved?: (name: string) => void
}) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    ...managedAgentQueryOptions(agentName ?? ""),
    enabled: !!agentName,
  })

  if (agentName && isLoading) {
    return (
      <>
        <SheetHeader className="px-6 py-4">
          <SheetTitle className="font-mono text-sm tracking-wider uppercase">
            {t("managedAgents.form.editTitle")}
          </SheetTitle>
        </SheetHeader>
        <div className="text-muted-foreground flex-1 px-6 py-8 text-sm">{t("common.loading")}</div>
      </>
    )
  }

  return (
    <UpsertForm agent={agentName ? (data ?? null) : null} onClose={onClose} onSaved={onSaved} />
  )
}

function UpsertForm({
  agent,
  onClose,
  onSaved,
}: {
  agent: ManagedAgent | null
  onClose: () => void
  onSaved?: (name: string) => void
}) {
  const { t } = useTranslation()
  const isEdit = !!agent

  const stored = useMemo<StoredCredentials>(
    () => ({
      claudeCode: !!agent?.spec.runtime?.claudeCode?.credentialsRef,
      opencode: !!agent?.spec.runtime?.opencode?.credentialsRef,
      classifier: !!agent?.spec.classifier?.credentialsRef,
      sandbox:
        !!agent?.spec.hands?.external?.credentialsRef ||
        !!agent?.spec.hands?.e2b?.credentialsSecret,
    }),
    [agent],
  )
  const defaultValues = useMemo(() => agentToFormValues(agent), [agent])
  const resolver = useMemo(() => zodResolver(buildSchema(stored)), [stored])

  const {
    control,
    register,
    setValue,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({ resolver, defaultValues })

  const createMutation = useCreateManagedAgent()
  const updateMutation = useUpdateManagedAgent()

  const onSubmit = handleSubmit(async (values) => {
    const spec = buildSpec(values, agent?.spec)
    const credentials = buildCredentials(values)
    await new Promise<void>((resolve, reject) => {
      const onSuccess = () => {
        toast.success(
          isEdit
            ? t("managedAgents.updatedSuccess", { name: values.name })
            : t("managedAgents.createdSuccess", { name: values.name }),
        )
        onClose()
        onSaved?.(values.name)
        resolve()
      }
      if (isEdit) {
        updateMutation.mutate(
          { name: agent!.name, spec, credentials },
          { onSuccess, onError: reject },
        )
      } else {
        createMutation.mutate(
          { name: values.name, spec, credentials },
          { onSuccess, onError: reject },
        )
      }
      // A rejected mutation already surfaced a toast from the shared error
      // handler; the sheet stays open so the offending field can be fixed.
    }).catch(() => undefined)
  })

  const sectionProps: SectionProps = { control, register, errors, isEdit, stored }

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {isEdit ? t("managedAgents.form.editTitle") : t("managedAgents.form.createTitle")}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-5">
          <BasicsSection {...sectionProps} />
          <Separator />
          <RuntimeSection {...sectionProps} />
          <Separator />
          <ScenariosSection {...sectionProps} setValue={setValue} />
          <Separator />
          <HandsSection {...sectionProps} />

          <div className="border-border rounded-md border">
            <Accordion>
              <AccordionItem value="advanced">
                <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                  {t("common.advanced")}
                </AccordionTrigger>
                <AccordionContent className="px-3">
                  <div className="flex flex-col gap-5 pb-2">
                    <PromptSection {...sectionProps} />
                    <Separator />
                    <ClassifierSection {...sectionProps} />
                  </div>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </div>
        </div>

        <Separator />
        <div className="flex justify-end gap-2 px-6 py-3">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={isSubmitting} className="gap-1.5">
            <Save className="h-3.5 w-3.5" />
            {isEdit ? t("common.save") : t("common.create")}
          </Button>
        </div>
      </form>
    </Fragment>
  )
}
