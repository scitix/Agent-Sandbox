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
import { useQuery } from "@tanstack/react-query"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { ChevronDown, Plus, Save, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
} from "@/components/ui/combobox"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { useFeatureGates } from "@/hooks/use-feature-gates"
import type {
  AgentEnvAutoscalingGroup,
  AgentEnvAutoscalingSpec,
  AgentInstanceTypeItem,
  AgentSandboxEnv,
  AgentSandboxTemplateSummary,
  QuotaItem,
} from "@/lib/api/client"
import {
  instanceTypesQueryOptions,
  quotasQueryOptions,
  templatesQueryOptions,
  useCreateEnv,
  useUpdateEnv,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { formatCores, parseCpuToCore, parseMemoryToMiB } from "@/lib/resources"

interface Props {
  env?: AgentSandboxEnv | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

const QUOTA_URL_LABEL = "quota.scitix.ai/url"

// ─── Form schema ─────────────────────────────────────────────────────────────

const emptyToUndef = (val: unknown) =>
  typeof val === "string" && val.trim() === "" ? undefined : val

const dnsLabel = /^[a-z]([a-z0-9-]*[a-z0-9])?$/

// Resource mode toggle per member. `instanceType` references the catalog;
// `manual` collects integer CPU cores and integer GiB memory, written into
// both Requests and Limits with the same value.
const resourceMode = z.enum(["instanceType", "manual"])

const intGte1 = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(1).optional(),
)

const memberSchema = z
  .object({
    name: z
      .string()
      .min(1, "envs.form.errors.memberNameRequired")
      .max(63)
      .regex(dnsLabel, "envs.form.errors.memberNameDnsLabel"),
    quotaUrl: z.preprocess(emptyToUndef, z.string().optional()),
    replicas: z.preprocess(
      (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
      z.number().int().min(0).optional(),
    ),
    resourceMode,
    instanceType: z.preprocess(emptyToUndef, z.string().optional()),
    multiplier: intGte1,
    cpuCores: intGte1,
    memoryGiB: intGte1,
  })
  .superRefine((m, ctx) => {
    if (m.resourceMode === "instanceType") {
      if (!m.instanceType) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "envs.form.errors.instanceTypeRequired",
          path: ["instanceType"],
        })
      }
      if (m.multiplier === undefined) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "envs.form.errors.multiplierRequired",
          path: ["multiplier"],
        })
      }
    } else if (m.resourceMode === "manual") {
      if (m.cpuCores === undefined) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "envs.form.errors.cpuRequired",
          path: ["cpuCores"],
        })
      }
      if (m.memoryGiB === undefined) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "envs.form.errors.memoryRequired",
          path: ["memoryGiB"],
        })
      }
    }
  })

const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

const optionalSeconds = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(0).optional(),
)

const optionalReplicas = optionalSeconds

const autoscalingSchema = z.object({
  enabled: z.boolean(),
  groupName: z.string().min(1).default("default"),
  minReplicas: optionalReplicas,
  maxReplicas: optionalReplicas,
  scaleUpMode: z.enum(["Conservative", "Default", "Aggressive"]).optional(),
  cooldownSeconds: optionalSeconds,
  idleThresholdSeconds: optionalSeconds,
  saturationCooldownSeconds: optionalSeconds,
  idleTimeoutSeconds: optionalSeconds,
  stabilizationSeconds: optionalSeconds,
  protectionWindowSeconds: optionalSeconds,
})

const formSchema = z.object({
  name: z
    .string()
    .min(1, "envs.form.errors.nameRequired")
    .max(63)
    .regex(dnsLabel, "envs.form.errors.nameDnsLabel"),
  templateName: z.string().min(1, "envs.form.errors.templateRequired"),
  image: z.preprocess(emptyToUndef, z.string().optional()),
  podCreationImagePolicy: z.enum(["PoolDefaultImage", "IdleImage"]).optional(),
  imagePullSecretRows: z.array(registryRowSchema),
  defaultStartupTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  defaultIdleTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  members: z.array(memberSchema),
  autoscaling: autoscalingSchema,
})

type FormValues = z.infer<typeof formSchema>
type MemberInput = z.infer<typeof memberSchema>

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function UpsertEnvSheet({ env, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-3xl"
      >
        {open && <UpsertEnvInner env={env ?? null} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

interface InnerProps {
  env: AgentSandboxEnv | null
  onClose: () => void
}

function UpsertEnvInner({ env, onClose }: InnerProps) {
  const { t } = useTranslation()
  const isEdit = !!env
  const gates = useFeatureGates()

  const { data: templates = [] } = useQuery(templatesQueryOptions())
  const { data: quotas = [] } = useQuery(quotasQueryOptions())
  const { data: instanceTypes = [] } = useQuery(
    instanceTypesQueryOptions({ enabled: gates.instanceType }),
  )

  const defaultValues = useMemo<FormValues>(
    () => envToFormValues(env, gates.instanceType),
    [env, gates.instanceType],
  )

  const {
    control,
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues,
  })

  const { fields, append, remove } = useFieldArray({ control, name: "members" })

  const createMutation = useCreateEnv()
  const updateMutation = useUpdateEnv()

  const onSubmit = handleSubmit(async (values) => {
    if (isEdit) {
      const body = formValuesToUpdateBody(values)
      await new Promise<void>((resolve, reject) => {
        updateMutation.mutate(
          { params: { path: { name: env!.name } }, body },
          {
            onSuccess: () => {
              toast.success(t("envs.form.toast.updated", { name: env!.name }))
              onClose()
              resolve()
            },
            onError: (err) => reject(err),
          },
        )
      }).catch((err: unknown) => toast.error(extractError(err)))
    } else {
      const body = formValuesToCreateBody(values)
      await new Promise<void>((resolve, reject) => {
        createMutation.mutate(
          { body },
          {
            onSuccess: () => {
              toast.success(t("envs.form.toast.created", { name: values.name }))
              onClose()
              resolve()
            },
            onError: (err) => reject(err),
          },
        )
      }).catch((err: unknown) => toast.error(extractError(err)))
    }
  })

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {isEdit ? t("envs.form.editTitle") : t("envs.form.createTitle")}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-5">
          {/* Basics */}
          <section className="space-y-3">
            <h3 className="font-mono text-[11px] tracking-wider uppercase text-muted-foreground">
              {t("envs.form.section.basics")}
            </h3>

            <Field>
              <FieldLabel htmlFor="env-name">{t("envs.form.name")}</FieldLabel>
              <Input
                id="env-name"
                disabled={isEdit}
                {...register("name")}
                placeholder="my-env"
              />
              {errors.name && <FieldError>{t(errors.name.message as never)}</FieldError>}
              <FieldDescription>{t("envs.form.nameDescription")}</FieldDescription>
            </Field>

            <Field>
              <FieldLabel>{t("envs.form.template")}</FieldLabel>
              <Controller
                control={control}
                name="templateName"
                render={({ field, fieldState }) => (
                  <TemplateCombobox
                    items={templates}
                    value={field.value}
                    onChange={field.onChange}
                    invalid={fieldState.invalid}
                    disabled={isEdit}
                  />
                )}
              />
              {errors.templateName && (
                <FieldError>{t(errors.templateName.message as never)}</FieldError>
              )}
            </Field>
          </section>

          <Separator />

          {/* Env-level overrides */}
          <section className="space-y-3">
            <h3 className="font-mono text-[11px] tracking-wider uppercase text-muted-foreground">
              {t("envs.form.section.overrides")}
            </h3>
            <p className="text-xs text-muted-foreground">{t("envs.form.overridesHint")}</p>

            <Field>
              <FieldLabel htmlFor="env-image">{t("envs.form.image")}</FieldLabel>
              <Input
                id="env-image"
                {...register("image")}
                placeholder="ghcr.io/org/runtime:1.2"
              />
            </Field>

            <Field>
              <FieldLabel>{t("envs.form.podCreationImagePolicy")}</FieldLabel>
              <Controller
                control={control}
                name="podCreationImagePolicy"
                render={({ field }) => (
                  <Select
                    value={field.value ?? ""}
                    onValueChange={(v) => field.onChange(v || undefined)}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={t("envs.form.imagePolicyDefault")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="IdleImage">IdleImage</SelectItem>
                      <SelectItem value="PoolDefaultImage">PoolDefaultImage</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              />
            </Field>

            <div className="grid grid-cols-2 gap-3">
              <Field>
                <FieldLabel htmlFor="env-startup">
                  {t("envs.form.defaultStartupTimeout")}
                </FieldLabel>
                <Input id="env-startup" placeholder="5m" {...register("defaultStartupTimeout")} />
              </Field>
              <Field>
                <FieldLabel htmlFor="env-idle">{t("envs.form.defaultIdleTimeout")}</FieldLabel>
                <Input id="env-idle" placeholder="30m" {...register("defaultIdleTimeout")} />
              </Field>
            </div>
          </section>

          <Separator />

          <ImagePullSecretSection control={control} register={register} />

          <Separator />

          {/* Members */}
          <section className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="font-mono text-[11px] tracking-wider uppercase text-muted-foreground">
                {t("envs.form.section.members")}
              </h3>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() =>
                  append(emptyMember(env?.name ?? "", fields.length, gates.instanceType))
                }
                className="h-7 gap-1 font-mono text-[11px]"
              >
                <Plus className="h-3 w-3" />
                {t("envs.form.addMember")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{t("envs.form.membersHint")}</p>

            {fields.length === 0 && (
              <p className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
                {t("envs.form.noMembersHint")}
              </p>
            )}

            <div className="space-y-3">
              {fields.map((field, index) => (
                <MemberRow
                  key={field.id}
                  index={index}
                  control={control}
                  register={register}
                  errors={errors}
                  quotas={quotas}
                  instanceTypes={instanceTypes}
                  instanceTypeGate={gates.instanceType}
                  onRemove={() => remove(index)}
                />
              ))}
            </div>
          </section>

          <Separator />

          {/* Autoscaling — collapsed by default, opened when the existing env
              already has autoscaling.enabled so editors land on the relevant
              fields. */}
          <AutoscalingSection
            control={control}
            register={register}
            errors={errors}
            defaultOpen={env?.spec.autoscaling?.enabled === true}
          />
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

// ─── Member row ──────────────────────────────────────────────────────────────

interface MemberRowProps {
  index: number
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
  quotas: QuotaItem[]
  instanceTypes: AgentInstanceTypeItem[]
  instanceTypeGate: boolean
  onRemove: () => void
}

function MemberRow({
  index,
  control,
  register,
  errors,
  quotas,
  instanceTypes,
  instanceTypeGate,
  onRemove,
}: MemberRowProps) {
  const { t } = useTranslation()
  const memberErrors = errors.members?.[index]
  const mode = useWatch({ control, name: `members.${index}.resourceMode` }) ?? "manual"
  const selectedInstanceTypeName = useWatch({
    control,
    name: `members.${index}.instanceType`,
  })
  const watchedMultiplier = useWatch({ control, name: `members.${index}.multiplier` })

  const selectedInstanceType =
    instanceTypes.find((it) => it.name === selectedInstanceTypeName) ?? null
  const preview = computeInstanceTypePreview(selectedInstanceType, watchedMultiplier)

  return (
    <div className="rounded-md border bg-muted/30 p-3 space-y-2.5">
      <div className="flex items-start gap-2">
        <div className="grid flex-1 grid-cols-2 gap-2">
          <Field>
            <FieldLabel htmlFor={`members.${index}.name`} className="text-[11px]">
              {t("envs.form.member.name")}
            </FieldLabel>
            <Input
              id={`members.${index}.name`}
              {...register(`members.${index}.name` as const)}
              placeholder="env-a-ondemand"
            />
            {memberErrors?.name && (
              <FieldError>{t(memberErrors.name.message as never)}</FieldError>
            )}
          </Field>

          <Field>
            <FieldLabel className="text-[11px]">{t("envs.form.member.quota")}</FieldLabel>
            <Controller
              control={control}
              name={`members.${index}.quotaUrl` as const}
              render={({ field }) => (
                <QuotaCombobox
                  items={quotas}
                  value={field.value ?? null}
                  onChange={field.onChange}
                />
              )}
            />
          </Field>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={onRemove}
          className="mt-5 text-destructive"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="grid grid-cols-3 gap-2 items-end">
        <Field>
          <FieldLabel htmlFor={`members.${index}.replicas`} className="text-[11px]">
            {t("envs.form.member.replicas")}
          </FieldLabel>
          <Input
            id={`members.${index}.replicas`}
            type="number"
            min={0}
            placeholder="1"
            {...register(`members.${index}.replicas` as const)}
          />
        </Field>
        {instanceTypeGate && (
          <div className="col-span-2 flex flex-col gap-1">
            <span className="font-mono text-[11px] text-muted-foreground">
              {t("envs.form.member.resourceMode")}
            </span>
            <Controller
              control={control}
              name={`members.${index}.resourceMode` as const}
              render={({ field }) => (
                <ModeToggle
                  value={field.value}
                  onChange={field.onChange}
                  options={[
                    { value: "instanceType", label: t("envs.form.member.modeInstanceType") },
                    { value: "manual", label: t("envs.form.member.modeManual") },
                  ]}
                />
              )}
            />
          </div>
        )}
      </div>

      {mode === "instanceType" ? (
        <div className="space-y-2">
          <div className="grid grid-cols-3 gap-2">
            <Field className="col-span-2">
              <FieldLabel className="text-[11px]">{t("envs.form.member.instanceType")}</FieldLabel>
              <Controller
                control={control}
                name={`members.${index}.instanceType` as const}
                render={({ field, fieldState }) => (
                  <InstanceTypeCombobox
                    items={instanceTypes}
                    value={field.value ?? null}
                    onChange={field.onChange}
                    invalid={fieldState.invalid}
                  />
                )}
              />
              {memberErrors?.instanceType && (
                <FieldError>{t(memberErrors.instanceType.message as never)}</FieldError>
              )}
            </Field>
            <Field>
              <FieldLabel className="text-[11px]">{t("envs.form.member.multiplier")}</FieldLabel>
              <Input
                type="number"
                min={1}
                max={selectedInstanceType?.maxMultiplier || undefined}
                placeholder="1"
                {...register(`members.${index}.multiplier` as const)}
              />
              {memberErrors?.multiplier && (
                <FieldError>{t(memberErrors.multiplier.message as never)}</FieldError>
              )}
            </Field>
          </div>
          {preview && (
            <div className="rounded border bg-background px-3 py-2 font-mono text-[11px] text-muted-foreground">
              <span className="mr-1 uppercase tracking-wider">
                {t("envs.form.member.preview")}
              </span>
              <span className="text-foreground">
                {selectedInstanceTypeName}
                {watchedMultiplier && Number(watchedMultiplier) > 1
                  ? ` × ${watchedMultiplier}`
                  : ""}{" "}
                ={" "}
              </span>
              <span className="text-foreground">{preview}</span>
            </div>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-2">
          <Field>
            <FieldLabel className="text-[11px]">{t("envs.form.member.cpuCores")}</FieldLabel>
            <Input
              type="number"
              min={1}
              placeholder="2"
              {...register(`members.${index}.cpuCores` as const)}
            />
            {memberErrors?.cpuCores && (
              <FieldError>{t(memberErrors.cpuCores.message as never)}</FieldError>
            )}
          </Field>
          <Field>
            <FieldLabel className="text-[11px]">{t("envs.form.member.memoryGiB")}</FieldLabel>
            <Input
              type="number"
              min={1}
              placeholder="8"
              {...register(`members.${index}.memoryGiB` as const)}
            />
            {memberErrors?.memoryGiB && (
              <FieldError>{t(memberErrors.memoryGiB.message as never)}</FieldError>
            )}
          </Field>
        </div>
      )}
    </div>
  )
}

// ModeToggle is a tiny two-button segmented control. Lighter than the
// BaseUI ToggleGroup which is array-typed and aimed at multi-select use.
function ModeToggle<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T
  onChange: (v: T) => void
  options: Array<{ value: T; label: string }>
}) {
  return (
    <div className="inline-flex h-9 w-fit overflow-hidden rounded-md border bg-background font-mono text-[11px]">
      {options.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onChange(opt.value)}
          className={cn(
            "px-3 transition-colors",
            opt.value === value
              ? "bg-foreground text-background"
              : "text-muted-foreground hover:bg-muted",
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}

// computeInstanceTypePreview returns a human-readable resource summary for a
// (catalog entry, multiplier) pair, or null when either input is missing.
// Memory is shown in GiB when the underlying value is a whole number of GiB.
function computeInstanceTypePreview(
  it: AgentInstanceTypeItem | null,
  multiplierRaw: number | string | undefined,
): string | null {
  if (!it) return null
  const reqs = it.baseResources?.requests ?? it.baseResources?.limits
  if (!reqs) return null
  const baseCpu = parseCpuToCore(reqs["cpu"] as string | undefined)
  const baseMem = parseMemoryToMiB(reqs["memory"] as string | undefined)
  const m = Number(multiplierRaw)
  const mult = Number.isFinite(m) && m >= 1 ? Math.floor(m) : 1
  const cpu = baseCpu != null ? baseCpu * mult : undefined
  const mem = baseMem != null ? baseMem * mult : undefined
  const parts: string[] = []
  if (cpu != null) parts.push(`${formatCores(cpu)}c`)
  if (mem != null) parts.push(formatMemPreview(mem))
  if (parts.length === 0) return null
  return parts.join(" / ")
}

function formatMemPreview(mib: number): string {
  // Prefer GiB when the value is a whole GiB count.
  if (mib >= 1024 && mib % 1024 === 0) return `${mib / 1024}Gi`
  return `${Math.round(mib)}Mi`
}

// ─── Autoscaling section ─────────────────────────────────────────────────────

interface AutoscalingSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
  defaultOpen: boolean
}

function AutoscalingSection({
  control,
  register,
  errors,
  defaultOpen,
}: AutoscalingSectionProps) {
  const { t } = useTranslation()
  const enabled = useWatch({ control, name: "autoscaling.enabled" }) ?? false

  return (
    <Collapsible defaultOpen={defaultOpen} className="space-y-3">
      <CollapsibleTrigger
        className={cn(
          "flex w-full items-center justify-between rounded border px-3 py-2",
          "font-mono text-[11px] tracking-wider uppercase text-muted-foreground",
          "[&[data-panel-open]>svg]:rotate-180",
        )}
      >
        <span>{t("envs.form.section.autoscaling")}</span>
        <ChevronDown className="h-3.5 w-3.5 transition-transform" />
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-3 pt-2">
        <Controller
          control={control}
          name="autoscaling.enabled"
          render={({ field }) => (
            <div className="flex items-start justify-between gap-4 rounded border p-3">
              <div>
                <div className="text-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("envs.editAutoscaling.field.enabled")}
                </div>
                <p className="text-muted-foreground mt-1 text-[11px]">
                  {t("envs.editAutoscaling.field.enabledDesc")}
                </p>
              </div>
              <Switch
                checked={!!field.value}
                onCheckedChange={(v: boolean) => field.onChange(v)}
              />
            </div>
          )}
        />

        <div className="grid grid-cols-2 gap-4">
          <Field data-invalid={!!errors.autoscaling?.minReplicas}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.field.minReplicas")}
            </FieldLabel>
            <Input
              {...register("autoscaling.minReplicas")}
              type="number"
              min={0}
              disabled={!enabled}
              className="h-9 font-mono text-sm"
            />
          </Field>
          <Field data-invalid={!!errors.autoscaling?.maxReplicas}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.field.maxReplicas")}
            </FieldLabel>
            <Input
              {...register("autoscaling.maxReplicas")}
              type="number"
              min={0}
              disabled={!enabled}
              className="h-9 font-mono text-sm"
            />
          </Field>
        </div>

        <fieldset disabled={!enabled} className="space-y-3 rounded border p-3">
          <legend className="text-foreground px-1 font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.editAutoscaling.scaleUpSection")}
          </legend>
          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.field.scaleUpMode")}
            </FieldLabel>
            <Controller
              control={control}
              name="autoscaling.scaleUpMode"
              render={({ field }) => (
                <Select
                  value={field.value ?? ""}
                  onValueChange={(v: string | null) =>
                    field.onChange(v === "" || v === null ? undefined : v)
                  }
                >
                  <SelectTrigger className="h-9 font-mono text-sm">
                    <SelectValue placeholder="—" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="Conservative">
                      {t("envs.editAutoscaling.modeConservative")}
                    </SelectItem>
                    <SelectItem value="Default">
                      {t("envs.editAutoscaling.modeDefault")}
                    </SelectItem>
                    <SelectItem value="Aggressive">
                      {t("envs.editAutoscaling.modeAggressive")}
                    </SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <SecondsField
              label={t("envs.editAutoscaling.field.cooldownSeconds")}
              {...register("autoscaling.cooldownSeconds")}
            />
            <SecondsField
              label={t("envs.editAutoscaling.field.idleThresholdSeconds")}
              {...register("autoscaling.idleThresholdSeconds")}
            />
            <SecondsField
              label={t("envs.editAutoscaling.field.saturationCooldownSeconds")}
              {...register("autoscaling.saturationCooldownSeconds")}
            />
          </div>
        </fieldset>

        <fieldset disabled={!enabled} className="space-y-3 rounded border p-3">
          <legend className="text-foreground px-1 font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.editAutoscaling.scaleDownSection")}
          </legend>
          <div className="grid grid-cols-3 gap-3">
            <SecondsField
              label={t("envs.editAutoscaling.field.idleTimeoutSeconds")}
              {...register("autoscaling.idleTimeoutSeconds")}
            />
            <SecondsField
              label={t("envs.editAutoscaling.field.stabilizationSeconds")}
              {...register("autoscaling.stabilizationSeconds")}
            />
            <SecondsField
              label={t("envs.editAutoscaling.field.protectionWindowSeconds")}
              {...register("autoscaling.protectionWindowSeconds")}
            />
          </div>
        </fieldset>
      </CollapsibleContent>
    </Collapsible>
  )
}

const SecondsField = ((
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  { label, ...rest }: { label: string } & any,
) => (
  <Field>
    <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-[0.12em] uppercase">
      {label}
    </FieldLabel>
    <Input {...rest} type="number" min={0} className="h-9 font-mono text-sm" />
  </Field>
)) as (props: { label: string; name: string }) => React.ReactElement

// ─── Comboboxes ──────────────────────────────────────────────────────────────

function TemplateCombobox({
  items,
  value,
  onChange,
  invalid,
  disabled,
}: {
  items: AgentSandboxTemplateSummary[]
  value: string | undefined
  onChange: (v: string) => void
  invalid?: boolean
  disabled?: boolean
}) {
  const selected = items.find((it) => it.name === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item.name}
      value={selected}
      onValueChange={(v) => onChange(v?.name ?? "")}
      disabled={disabled}
    >
      <ComboboxTrigger>
        <ComboboxInput aria-invalid={invalid} placeholder="select template" />
      </ComboboxTrigger>
      <ComboboxContent>
        <ComboboxList>
          {(item: AgentSandboxTemplateSummary) => (
            <ComboboxItem key={item.name} value={item}>
              {item.name}
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
  )
}

function QuotaCombobox({
  items,
  value,
  onChange,
}: {
  items: QuotaItem[]
  value: string | null
  onChange: (v: string | undefined) => void
}) {
  const selected = items.find((q) => q.quotaUrl === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item.label || item.quotaUrl}
      value={selected}
      onValueChange={(v) => onChange(v?.quotaUrl ?? undefined)}
    >
      <ComboboxTrigger>
        <ComboboxInput placeholder="(none)" />
      </ComboboxTrigger>
      <ComboboxContent>
        <ComboboxList>
          {(item: QuotaItem) => (
            <ComboboxItem key={item.quotaUrl} value={item}>
              <div className="flex flex-col">
                <span className="font-mono text-xs">{item.label || item.quotaUrl}</span>
                <span className="text-[10px] text-muted-foreground">{item.quotaUrl}</span>
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
  )
}

function InstanceTypeCombobox({
  items,
  value,
  onChange,
  invalid,
}: {
  items: AgentInstanceTypeItem[]
  value: string | null
  onChange: (v: string | undefined) => void
  invalid?: boolean
}) {
  const selected = items.find((it) => it.name === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item.showName || item.name}
      value={selected}
      onValueChange={(v) => onChange(v?.name ?? undefined)}
    >
      <ComboboxTrigger>
        <ComboboxInput aria-invalid={invalid} placeholder="select instance type" />
      </ComboboxTrigger>
      <ComboboxContent>
        <ComboboxList>
          {(item: AgentInstanceTypeItem) => {
            const reqs = item.baseResources?.requests ?? item.baseResources?.limits
            const cpu = reqs?.["cpu"]
            const mem = reqs?.["memory"]
            const meta = [cpu && `${cpu}c`, mem && `${mem}`].filter(Boolean).join(" / ")
            return (
              <ComboboxItem key={item.name} value={item}>
                <div className="flex flex-col">
                  <span className="font-mono text-xs">{item.showName || item.name}</span>
                  {meta && (
                    <span className="text-[10px] text-muted-foreground">{meta}</span>
                  )}
                </div>
              </ComboboxItem>
            )
          }}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
  )
}

// ─── ImagePullSecret section ────────────────────────────────────────────────

interface ImagePullSecretSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
}

function ImagePullSecretSection({ control, register }: ImagePullSecretSectionProps) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({ control, name: "imagePullSecretRows" })
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="font-mono text-[11px] tracking-wider uppercase text-muted-foreground">
          {t("envs.form.section.imagePullSecret")}
        </h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => append({ registry: "", username: "", password: "" })}
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.imagePullSecret.addRegistry")}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">{t("envs.form.imagePullSecret.hint")}</p>
      {fields.length === 0 && (
        <p className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
          {t("envs.form.imagePullSecret.empty")}
        </p>
      )}
      <div className="space-y-2">
        {fields.map((field, index) => (
          <div
            key={field.id}
            className="flex items-start gap-2 rounded-md border bg-muted/30 p-2.5"
          >
            <div className="grid flex-1 grid-cols-3 gap-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.registry")}
                </FieldLabel>
                <Input
                  placeholder="ghcr.io"
                  {...register(`imagePullSecretRows.${index}.registry` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.username")}
                </FieldLabel>
                <Input
                  placeholder="user"
                  autoComplete="off"
                  {...register(`imagePullSecretRows.${index}.username` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.imagePullSecret.password")}
                </FieldLabel>
                <Input
                  type="password"
                  placeholder="••••••"
                  autoComplete="new-password"
                  {...register(`imagePullSecretRows.${index}.password` as const)}
                />
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => remove(index)}
              className="mt-5 text-destructive"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>
    </section>
  )
}

// ─── Form ↔ API mapping ──────────────────────────────────────────────────────

function envToFormValues(env: AgentSandboxEnv | null, instanceTypeGate: boolean): FormValues {
  const defaultMode: MemberInput["resourceMode"] = instanceTypeGate ? "instanceType" : "manual"
  if (!env) {
    return {
      name: "",
      templateName: "",
      image: undefined,
      podCreationImagePolicy: "IdleImage",
      defaultStartupTimeout: undefined,
      defaultIdleTimeout: undefined,
      members: [],
      imagePullSecretRows: [],
      autoscaling: readInitialAutoscaling(undefined),
    }
  }
  const overrides = env.spec.overrides
  const members: MemberInput[] = pickLocalMembers(env).map((m) => {
    const hasInstanceType = !!m.instanceType
    const hasInline =
      !hasInstanceType &&
      !!m.inlineResources &&
      ((m.inlineResources.requests && Object.keys(m.inlineResources.requests).length > 0) ||
        (m.inlineResources.limits && Object.keys(m.inlineResources.limits).length > 0))
    const mode: MemberInput["resourceMode"] = hasInstanceType
      ? "instanceType"
      : hasInline
        ? "manual"
        : defaultMode
    let cpuCores: number | undefined
    let memoryGiB: number | undefined
    if (mode === "manual" && hasInline) {
      const reqs = m.inlineResources?.requests ?? m.inlineResources?.limits ?? {}
      const cpu = parseCpuToCore(reqs.cpu)
      const mem = parseMemoryToMiB(reqs.memory)
      cpuCores = cpu != null ? Math.max(1, Math.round(cpu)) : undefined
      memoryGiB = mem != null ? Math.max(1, Math.round(mem / 1024)) : undefined
    }
    return {
      name: m.name,
      quotaUrl: m.labels?.[QUOTA_URL_LABEL],
      replicas: m.replicas,
      resourceMode: mode,
      instanceType: m.instanceType ?? undefined,
      multiplier: m.multiplier ?? undefined,
      cpuCores,
      memoryGiB,
    }
  })
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    members,
    imagePullSecretRows: [],
    autoscaling: readInitialAutoscaling(env.spec.autoscaling),
  }
}

function readInitialAutoscaling(
  auto: AgentEnvAutoscalingSpec | undefined,
): FormValues["autoscaling"] {
  const group: AgentEnvAutoscalingGroup | undefined = auto?.groups?.[0]
  return {
    enabled: auto?.enabled ?? false,
    groupName: group?.name ?? "default",
    minReplicas: group?.minReplicas,
    maxReplicas: group?.maxReplicas,
    scaleUpMode: (group?.scaleUpPolicy?.mode ?? undefined) as
      | "Conservative"
      | "Default"
      | "Aggressive"
      | undefined,
    cooldownSeconds: group?.scaleUpPolicy?.cooldownSeconds,
    idleThresholdSeconds: group?.scaleUpPolicy?.idleThresholdSeconds,
    saturationCooldownSeconds: group?.scaleUpPolicy?.saturationCooldownSeconds,
    idleTimeoutSeconds: group?.scaleDownPolicy?.idleTimeoutSeconds,
    stabilizationSeconds: group?.scaleDownPolicy?.stabilizationSeconds,
    protectionWindowSeconds: group?.scaleDownPolicy?.protectionWindowSeconds,
  }
}

function pickLocalMembers(env: AgentSandboxEnv): NonNullable<
  NonNullable<NonNullable<typeof env.spec.clusters>[number]["members"]>
> {
  // The Reconciler is the only writer of cluster.IsLocal=true; for the UI we
  // assume the first cluster segment is the local one (single-cluster MVP).
  const first = env.spec.clusters?.[0]
  return first?.members ?? []
}

function formValuesToCreateBody(v: FormValues) {
  return {
    name: v.name,
    mode: "WarmPool" as const,
    templateRef: { name: v.templateName },
    members: v.members.map(memberInputToWire),
    overrides: buildOverrides(v),
    autoscaling: buildAutoscaling(v.autoscaling),
  }
}

function formValuesToUpdateBody(v: FormValues) {
  return {
    members: v.members.map(memberInputToWire),
    overrides: buildOverrides(v),
    autoscaling: buildAutoscaling(v.autoscaling),
  }
}

function buildOverrides(v: FormValues) {
  const o: Record<string, unknown> = {}
  if (v.image) o.image = v.image
  if (v.podCreationImagePolicy) o.podCreationImagePolicy = v.podCreationImagePolicy
  if (v.defaultStartupTimeout) o.defaultStartupTimeout = v.defaultStartupTimeout
  if (v.defaultIdleTimeout) o.defaultIdleTimeout = v.defaultIdleTimeout
  const registries = v.imagePullSecretRows
    .filter((r) => r.registry && r.username && r.password)
    .map((r) => ({ registry: r.registry!, username: r.username!, password: r.password! }))
  if (registries.length > 0) {
    o.imagePullSecret = { registries }
  }
  return Object.keys(o).length ? o : undefined
}

function buildAutoscaling(a: FormValues["autoscaling"]): AgentEnvAutoscalingSpec {
  const group: AgentEnvAutoscalingGroup = {
    name: a.groupName,
    ...(a.minReplicas !== undefined && { minReplicas: a.minReplicas }),
    ...(a.maxReplicas !== undefined && { maxReplicas: a.maxReplicas }),
    ...(hasScaleUpFields(a) && {
      scaleUpPolicy: {
        ...(a.scaleUpMode && { mode: a.scaleUpMode }),
        ...(a.cooldownSeconds !== undefined && { cooldownSeconds: a.cooldownSeconds }),
        ...(a.idleThresholdSeconds !== undefined && {
          idleThresholdSeconds: a.idleThresholdSeconds,
        }),
        ...(a.saturationCooldownSeconds !== undefined && {
          saturationCooldownSeconds: a.saturationCooldownSeconds,
        }),
      },
    }),
    ...(hasScaleDownFields(a) && {
      scaleDownPolicy: {
        ...(a.idleTimeoutSeconds !== undefined && { idleTimeoutSeconds: a.idleTimeoutSeconds }),
        ...(a.stabilizationSeconds !== undefined && {
          stabilizationSeconds: a.stabilizationSeconds,
        }),
        ...(a.protectionWindowSeconds !== undefined && {
          protectionWindowSeconds: a.protectionWindowSeconds,
        }),
      },
    }),
  }
  return {
    enabled: a.enabled,
    groups: [group],
  }
}

function hasScaleUpFields(a: FormValues["autoscaling"]): boolean {
  return (
    a.scaleUpMode !== undefined ||
    a.cooldownSeconds !== undefined ||
    a.idleThresholdSeconds !== undefined ||
    a.saturationCooldownSeconds !== undefined
  )
}

function hasScaleDownFields(a: FormValues["autoscaling"]): boolean {
  return (
    a.idleTimeoutSeconds !== undefined ||
    a.stabilizationSeconds !== undefined ||
    a.protectionWindowSeconds !== undefined
  )
}

function memberInputToWire(m: MemberInput) {
  const labels = m.quotaUrl ? { [QUOTA_URL_LABEL]: m.quotaUrl } : undefined
  const base = {
    name: m.name,
    ...(labels ? { labels } : {}),
    ...(typeof m.replicas === "number" ? { replicas: m.replicas } : {}),
  }
  if (m.resourceMode === "instanceType") {
    return {
      ...base,
      ...(m.instanceType ? { instanceType: m.instanceType } : {}),
      ...(typeof m.multiplier === "number" ? { multiplier: m.multiplier } : {}),
    }
  }
  // Manual mode: the user's deployments invariably set Requests == Limits,
  // so we emit the same Quantity strings on both sides.
  const cpu = m.cpuCores !== undefined ? String(m.cpuCores) : undefined
  const mem = m.memoryGiB !== undefined ? `${m.memoryGiB}Gi` : undefined
  const requests: Record<string, string> = {}
  if (cpu) requests.cpu = cpu
  if (mem) requests.memory = mem
  const limits: Record<string, string> = { ...requests }
  const inlineResources =
    Object.keys(requests).length > 0
      ? { requests, limits }
      : undefined
  return {
    ...base,
    ...(inlineResources ? { inlineResources } : {}),
  }
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}

function emptyMember(
  envName: string,
  index: number,
  instanceTypeGate: boolean,
): MemberInput {
  const base = envName || "member"
  return {
    name: `${base}-${index + 1}`,
    quotaUrl: undefined,
    replicas: 1,
    resourceMode: instanceTypeGate ? "instanceType" : "manual",
    instanceType: undefined,
    multiplier: 1,
    cpuCores: undefined,
    memoryGiB: undefined,
  }
}
