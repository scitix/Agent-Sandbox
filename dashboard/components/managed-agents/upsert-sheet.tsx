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

import { Fragment, useEffect, useMemo, useRef, useState } from "react"
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form"
import type { Control, UseFormRegister, UseFormSetValue } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { zodResolver } from "@hookform/resolvers/zod"
import { toast } from "sonner"
import { Plus, Save, Trash2 } from "lucide-react"

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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { FormCloneActions } from "@/components/custom/form-clone-actions"
import { managedAgentClone } from "@/lib/utils/managed-agent-clone"
import type {
  AgentSandboxEnvSummary,
  AgentSandboxTemplateSummary,
  ClusterEntry,
} from "@/lib/api/client"
import type { ManagedAgent, ManagedAgentDefaults } from "@/lib/api/managed-agent-types"
import { clustersAtom } from "@/lib/atoms"
import {
  MANAGED_AGENT_FORM_TABS,
  firstErrorPath,
  firstTabWithErrors,
  tabsWithErrors,
} from "@/lib/utils/managed-agent-form-tabs"
import type { ManagedAgentFormTab } from "@/lib/utils/managed-agent-form-tabs"
import {
  INHERIT,
  agentToFormValues,
  buildCredentials,
  buildSchema,
  buildSpec,
  managedAgentFormDefaults,
} from "@/lib/utils/managed-agent-form"
import type {
  DeploymentDefaults,
  FormErrors,
  FormValues,
  StoredCredentials,
} from "@/lib/utils/managed-agent-form"
import {
  envsQueryOptions,
  managedAgentDefaultsQueryOptions,
  managedAgentQueryOptions,
  templatesQueryOptions,
  useCreateManagedAgent,
  useUpdateManagedAgent,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/messages/_schema"

/** Tab labels. Namespaced under form.tab because managedAgents.tab.* is the
 *  detail page's own tab bar. */
const TAB_LABELS: Record<ManagedAgentFormTab, TranslationKey> = {
  basics: "managedAgents.form.tab.basics",
  runtime: "managedAgents.form.tab.runtime",
  scenarios: "managedAgents.form.tab.scenarios",
  classifier: "managedAgents.form.tab.classifier",
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
  /** What this deployment answers for, so a field can say so instead of asking. */
  platform: ManagedAgentDefaults
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

function BasicsSection({ register, errors, isEdit, platform }: SectionProps) {
  const { t } = useTranslation()
  const platformImage = platform.brainImage
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
          {t("managedAgents.form.imageRepository")} {!platformImage && <RequiredMark />}
        </FieldLabel>
        <Input
          // The platform's image as the placeholder, so an empty field reads as
          // "you get this one" rather than as something left unfilled.
          placeholder={
            platformImage
              ? [platformImage.repository, platformImage.tag].filter(Boolean).join(":")
              : "registry.example.com/agents/brain"
          }
          className={INPUT_CN}
          {...register("imageRepository")}
        />
        <ErrorText message={errors.imageRepository?.message} />
        <FieldDescription>
          {platformImage
            ? t("managedAgents.form.imageRepositoryPlatformDefault")
            : t("managedAgents.form.imageRepositoryDesc")}
        </FieldDescription>
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

/** One read-only line of the platform default's resolved configuration. */
function PlatformHandsRow({ label, value }: { label: string; value?: string }) {
  if (!value) return null
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="wrap-anywhere">{value}</dd>
    </>
  )
}

function HandsSection({ control, register, errors, stored, platform }: SectionProps) {
  const { t } = useTranslation()
  const handsMode = useWatch({ control, name: "handsMode" })
  const platformHands = platform.hands
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
              {/* Offered even where the deployment publishes none, so the reason
                  is visible in the panel below rather than the option being
                  silently absent. */}
              <TabsTrigger value="platformDefault" className="flex-1 text-xs">
                {t("managedAgents.handsMode.platformDefault")}
              </TabsTrigger>
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
        {handsMode === "platformDefault"
          ? t("managedAgents.handsMode.platformDefaultDesc")
          : handsMode === "auto"
            ? t("managedAgents.handsMode.autoDesc")
            : handsMode === "envRef"
              ? t("managedAgents.handsMode.envRefDesc")
              : t("managedAgents.handsMode.externalDesc")}
      </p>

      {handsMode === "platformDefault" && (
        <div className="space-y-2" data-invalid={!!errors.handsMode}>
          <ErrorText message={errors.handsMode?.message} />
          {platformHands ? (
            <>
              <dl className="bg-muted/40 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-md p-3 font-mono text-xs">
                <PlatformHandsRow
                  label={t("managedAgents.form.envName")}
                  value={platformHands.envName}
                />
                <PlatformHandsRow
                  label={t("managedAgents.form.sandboxImage")}
                  value={platformHands.image}
                />
                <PlatformHandsRow
                  label={t("managedAgents.form.externalApiURL")}
                  value={platformHands.apiURL}
                />
                <PlatformHandsRow
                  label={t("managedAgents.form.scalingGroup")}
                  value={platformHands.scalingGroup}
                />
              </dl>
              {/* A default without a credential creates sandboxes that never
                  start, and the control plane looks healthy while it happens. */}
              {!platformHands.credentialConfigured && (
                <p className="text-destructive text-xs">
                  {t("managedAgents.handsMode.platformDefaultNoCredential")}
                </p>
              )}
            </>
          ) : (
            <p className="text-muted-foreground text-xs">
              {t("managedAgents.handsMode.platformDefaultUnset")}
            </p>
          )}
        </div>
      )}

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
  // Awaited before mounting the form, not merged in afterwards: react-hook-form
  // captures defaultValues once, so a late arrival would either be ignored or have
  // to overwrite fields the user may already be typing into.
  const { data: platform, isLoading: platformLoading } = useQuery(
    managedAgentDefaultsQueryOptions(),
  )

  if ((agentName && isLoading) || platformLoading) {
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
    <UpsertForm
      agent={agentName ? (data ?? null) : null}
      platform={platform ?? {}}
      onClose={onClose}
      onSaved={onSaved}
    />
  )
}

function UpsertForm({
  agent,
  platform,
  onClose,
  onSaved,
}: {
  agent: ManagedAgent | null
  platform: ManagedAgentDefaults
  onClose: () => void
  onSaved?: (name: string) => void
}) {
  const { t } = useTranslation()
  const isEdit = !!agent
  // Cluster ids an imported file is checked against — an id this deployment does
  // not know is cleared rather than left to render as an empty picker.
  const clusters = useAtomValue(clustersAtom).clusters

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
  const deployment = useMemo<DeploymentDefaults>(
    () => ({ brainImage: !!platform.brainImage, hands: !!platform.hands }),
    [platform],
  )
  // A create starts from the platform's answers; an edit starts from the agent's
  // own, which already carry whatever it was created with.
  const defaultValues = useMemo(
    () => (agent ? agentToFormValues(agent) : managedAgentFormDefaults(platform)),
    [agent, platform],
  )
  const resolver = useMemo(
    () => zodResolver(buildSchema(stored, deployment)),
    [stored, deployment],
  )

  const {
    control,
    register,
    setValue,
    getValues,
    trigger,
    reset,
    handleSubmit,
    formState: { errors, isSubmitting, submitCount },
    // shouldFocusError is off because a kept-mounted panel of an inactive tab is
    // `hidden`, and focus() on a hidden element does nothing — the jump below
    // moves to the tab first, then scrolls.
  } = useForm<FormValues>({ resolver, defaultValues, shouldFocusError: false })

  const [tab, setTab] = useState<ManagedAgentFormTab>("basics")
  const bodyRef = useRef<HTMLDivElement>(null)
  const [focusPath, setFocusPath] = useState<string | null>(null)
  const defaultRuntime = useWatch({ control, name: "defaultRuntime" })
  const tabCtx = useMemo(() => ({ defaultRuntime }), [defaultRuntime])
  const badTabs = useMemo(() => tabsWithErrors(errors, tabCtx), [errors, tabCtx])

  // Reveal the first offending field once its tab is showing. Two frames of
  // indirection because the panel is only unhidden after the tab state lands.
  useEffect(() => {
    if (!focusPath) return
    requestAnimationFrame(() => {
      const root = bodyRef.current
      if (!root) return
      const target =
        root.querySelector<HTMLElement>(`[name="${CSS.escape(focusPath)}"]`) ??
        root.querySelector<HTMLElement>('[data-invalid="true"]')
      target?.scrollIntoView({ block: "center" })
      target?.focus?.()
    })
  }, [tab, submitCount, focusPath])

  const applyImportedValues = (values: FormValues) => {
    reset(values)
    setTab("basics")
    // Validate straight away so the tabs that still need something light up.
    void trigger()
  }

  const createMutation = useCreateManagedAgent()
  const updateMutation = useUpdateManagedAgent()

  const onSubmit = handleSubmit(
    async (values) => {
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
    },
    (invalid) => {
      // A tabbed form can hide the reason a save did nothing. Move to the first
      // tab that has a problem and say which one it was.
      const target = firstTabWithErrors(invalid, tabCtx)
      setFocusPath(firstErrorPath(invalid))
      if (target && target !== tab) setTab(target)
      toast.error(t("managedAgents.form.fixErrorsInTab", { tab: t(TAB_LABELS[target ?? tab]) }))
    },
  )

  const sectionProps: SectionProps = { control, register, errors, isEdit, stored, platform }

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {isEdit ? t("managedAgents.form.editTitle") : t("managedAgents.form.createTitle")}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <Tabs
          value={tab}
          onValueChange={(value) => setTab(value as ManagedAgentFormTab)}
          className="flex flex-1 flex-col gap-0 overflow-hidden"
        >
          <div className="px-6 py-2">
            <TabsList className="w-full">
              {MANAGED_AGENT_FORM_TABS.map((id) => (
                <TabsTrigger key={id} value={id} className="flex-1 text-xs">
                  {t(TAB_LABELS[id])}
                  {badTabs.has(id) && (
                    <span
                      aria-label={t("managedAgents.form.tabHasErrors")}
                      className="bg-destructive ml-1.5 inline-block size-1.5 rounded-full align-middle"
                    />
                  )}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
          <Separator />

          {/* One scroller for every panel, and every panel stays mounted: an
              unmounted tab drops its fields out of the form, so a required one
              would stop being validated and the save would succeed with half a
              configuration. */}
          <div ref={bodyRef} className="flex-1 overflow-y-auto px-6 py-5">
            <TabsContent value="basics" keepMounted className="space-y-6">
              <BasicsSection {...sectionProps} />
              <Separator />
              <PromptSection {...sectionProps} />
              <Separator />
              <HandsSection {...sectionProps} />
            </TabsContent>
            <TabsContent value="runtime" keepMounted className="space-y-6">
              <RuntimeSection {...sectionProps} />
            </TabsContent>
            <TabsContent value="scenarios" keepMounted className="space-y-6">
              <ScenariosSection {...sectionProps} setValue={setValue} />
            </TabsContent>
            <TabsContent value="classifier" keepMounted className="space-y-6">
              <ClassifierSection {...sectionProps} />
            </TabsContent>
          </div>
        </Tabs>

        <Separator />
        <div className="flex items-center gap-2 px-6 py-3">
          <FormCloneActions
            clone={managedAgentClone(clusters.map((c) => c.id))}
            getValues={getValues}
            defaults={managedAgentFormDefaults(platform)}
            canImport={!isEdit}
            onImport={applyImportedValues}
          />
          <div className="ml-auto flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={isSubmitting} className="gap-1.5">
              <Save className="h-3.5 w-3.5" />
              {isEdit ? t("common.save") : t("common.create")}
            </Button>
          </div>
        </div>
      </form>
    </Fragment>
  )
}
