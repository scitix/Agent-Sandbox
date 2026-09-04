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

import { Fragment, useMemo, useState, type ReactNode } from "react"
import { Controller, useForm, useWatch, type Control, type FieldPath } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Check, ChevronsUpDown, Save } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { FormCloneActions } from "@/components/custom/form-clone-actions"
import { createFormClone } from "@/lib/utils/form-clone"
import { getPoolMeta, PoolTypeBadge, poolDisplayName } from "@/components/quota/pool-meta"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Separator } from "@/components/ui/separator"
import { useFeatureGates } from "@/hooks/use-feature-gates"
import type {
  AgentInstanceTypeItem,
  AgentSandboxEnv,
  AgentSandboxPool,
  QuotaItem,
} from "@/lib/api/client"
import {
  instanceTypesQueryOptions,
  quotasQueryOptions,
  useCreateEnvPool,
  useUpdateEnvPool,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import type { TranslationKey } from "@/messages/_schema"
import { cn } from "@/lib/utils"
import {
  cpuQuantity,
  formatCores,
  memoryQuantity,
  parseCpuToCore,
  parseMemoryToMiB,
  splitCpu,
  splitMemory,
  toMiB,
  toMilliCores,
  type CpuUnit,
  type MemoryUnit,
} from "@/lib/resources"

const QUOTA_URL_LABEL = "quota.scitix.ai/url"

interface Props {
  env: AgentSandboxEnv
  pool: AgentSandboxPool | null // null = create
  open: boolean
  onOpenChange: (open: boolean) => void
}

// ─── Form schema ─────────────────────────────────────────────────────────────
//
// Server derives PoolName and ScalingGroup from the resource shape + quota
// label, so this form never exposes those fields. On Update the server only
// accepts replicas / maxReplicas; resource / quota fields are read-only.

const resourceMode = z.enum(["instanceType", "manual"])

// Resource amounts are entered as a whole number plus a unit, so a sandbox can
// be sized in milli-cores / MiB as well as whole cores / GiB. Requiring whole
// numbers in every unit keeps the generated Kubernetes Quantity canonical —
// half a core is entered as 500 milli-cores, not 0.5 cores.
const cpuUnit = z.enum(["core", "milli"]) satisfies z.ZodType<CpuUnit>
const memoryUnit = z.enum(["Gi", "Mi"]) satisfies z.ZodType<MemoryUnit>

const intGte0 = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(0).optional(),
)
const intGte1 = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(1).optional(),
)

// Resource fields are immutable post-create (server rejects them on PUT) so
// the Edit form disables them — but disabled inputs still feed into RHF as
// undefined when the source pool lacked the corresponding shape (e.g. a pool
// created via instanceType has no inlineResources, leaving cpuValue/memoryValue
// blank). Required-field validation would then fail on submit even though the
// user can't fix the values, so we attach the resource-mode refinement only
// in Create mode.
const baseObject = z.object({
  resourceMode,
  instanceType: z.string().optional(),
  multiplier: intGte1,
  cpuValue: intGte1,
  cpuUnit,
  memoryValue: intGte1,
  memoryUnit,
  // Optional rounded-down request in instanceType mode. When set, the Pod
  // requests these (smaller) resources while the reservation still charges the
  // whole instanceType × multiplier envelope. Left blank → Pod uses the full
  // envelope. The fits-within (≤ envelope) check runs in the component since it
  // needs the fetched InstanceType catalog.
  overrideCpuValue: intGte1,
  overrideCpuUnit: cpuUnit,
  overrideMemoryValue: intGte1,
  overrideMemoryUnit: memoryUnit,
  quotaUrl: z.string().optional(),
  replicas: intGte0,
  minReplicas: intGte0,
  maxReplicas: intGte0,
  // Per-member auto-update override. autoUpdate defaults to inheriting the Env
  // (represented as `undefined` = inherit); maxUnavailable is int-or-percent.
  autoUpdate: z.boolean().optional(),
  maxUnavailable: z.preprocess((v) => (v === "" ? undefined : v), z.string().optional()),
})

// minReplicas must not exceed maxReplicas when both are supplied. Shared by
// both Create and Edit since maxReplicas/minReplicas are editable in both.
function refineReplicaBounds(m: FormValues, ctx: z.RefinementCtx) {
  if (m.minReplicas !== undefined && m.maxReplicas !== undefined && m.minReplicas > m.maxReplicas) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: "envs.poolForm.errors.minMaxOrder",
      path: ["minReplicas"],
    })
  }
}

const createSchema = baseObject.superRefine((m, ctx) => {
  refineReplicaBounds(m, ctx)
  if (m.resourceMode === "instanceType") {
    if (!m.instanceType) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.instanceTypeRequired",
        path: ["instanceType"],
      })
    }
    if (m.multiplier === undefined) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.multiplierRequired",
        path: ["multiplier"],
      })
    }
    // Rounded-down override is all-or-nothing: a partial request would drop the
    // unspecified dimension on the server (inlineResources is used verbatim).
    const hasCpu = m.overrideCpuValue !== undefined
    const hasMem = m.overrideMemoryValue !== undefined
    if (hasCpu !== hasMem) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.overrideBothRequired",
        path: [hasCpu ? "overrideMemoryValue" : "overrideCpuValue"],
      })
    }
  } else {
    if (m.cpuValue === undefined) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.cpuRequired",
        path: ["cpuValue"],
      })
    }
    if (m.memoryValue === undefined) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.memoryRequired",
        path: ["memoryValue"],
      })
    }
  }
})

const updateSchema = baseObject.superRefine(refineReplicaBounds)

type FormValues = z.infer<typeof baseObject>

// A member Pool's form holds no credentials, so there is nothing to strip. A
// Pool has no name field either — its name is derived from the resource key — so
// the export is named after the instance type when there is one.
const poolClone = createFormClone<FormValues>({
  kind: "SandboxPoolFormExport",
  // v2 carries resource amounts as value + unit; a v1 file's bare cpuCores /
  // memoryGiB would import as a blank sizing, so it is refused outright.
  version: 2,
  schema: baseObject.partial(),
  filePrefix: "sandbox-pool",
  nameOf: (v) => v.instanceType,
})

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function UpsertPoolSheet({ env, pool, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {open && <UpsertPoolInner env={env} pool={pool} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

function UpsertPoolInner({
  env,
  pool,
  onClose,
}: {
  env: AgentSandboxEnv
  pool: AgentSandboxPool | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const isEdit = !!pool
  const gates = useFeatureGates()

  const { data: quotas = [] } = useQuery(quotasQueryOptions())
  const { data: instanceTypes = [] } = useQuery(
    instanceTypesQueryOptions({ enabled: gates.instanceType }),
  )

  const defaultValues = useMemo<FormValues>(
    () => buildDefaultValues(env, pool, gates.instanceType),
    [env, pool, gates.instanceType],
  )

  const {
    control,
    register,
    handleSubmit,
    reset,
    trigger,
    getValues,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(isEdit ? updateSchema : createSchema),
    defaultValues,
  })

  const mode = useWatch({ control, name: "resourceMode" }) ?? "manual"
  const watchedInstance = useWatch({ control, name: "instanceType" })
  const watchedMultiplier = useWatch({ control, name: "multiplier" })
  const watchedOverrideCpu = useWatch({ control, name: "overrideCpuValue" })
  const watchedOverrideCpuUnit = useWatch({ control, name: "overrideCpuUnit" }) ?? "core"
  const watchedOverrideMem = useWatch({ control, name: "overrideMemoryValue" })
  const watchedOverrideMemUnit = useWatch({ control, name: "overrideMemoryUnit" }) ?? "Gi"
  const selectedInstanceType = instanceTypes.find((it) => it.name === watchedInstance) ?? null
  const preview = computeInstanceTypePreview(selectedInstanceType, watchedMultiplier)

  // Fits-within: the rounded-down request must not exceed the envelope in any
  // dimension. Computed here (not in the zod schema) because it needs the
  // fetched InstanceType catalog. Blocks submit and surfaces inline errors.
  const envelope = computeEnvelope(selectedInstanceType, watchedMultiplier)
  const overrideCpuMilli = toMilliCores(watchedOverrideCpu, watchedOverrideCpuUnit)
  const overrideMemMiB = toMiB(watchedOverrideMem, watchedOverrideMemUnit)
  const overrideCpuExceeds =
    envelope != null &&
    overrideCpuMilli !== undefined &&
    envelope.cpuMilli != null &&
    overrideCpuMilli > envelope.cpuMilli
  const overrideMemExceeds =
    envelope != null &&
    overrideMemMiB !== undefined &&
    envelope.memMiB != null &&
    overrideMemMiB > envelope.memMiB
  const overrideExceeds = !!(overrideCpuExceeds || overrideMemExceeds)

  // Member's scaling group only matters for the autoscaling-vs-replicas
  // gate on Update. Replicas is owned by the autoscaler only when the
  // matching group itself has Enabled=true.
  const scalingGroupName = pool ? findScalingGroupForPool(env, pool.name) : ""
  const groupHasAutoscaling = (env.spec.autoscaling?.groups ?? []).some(
    (g) => g.name === scalingGroupName && g.enabled,
  )
  const replicasDisabled = isEdit && groupHasAutoscaling

  const createMutation = useCreateEnvPool(env.name)
  const updateMutation = useUpdateEnvPool(env.name)

  const onSubmit = handleSubmit(async (values) => {
    if (isEdit) {
      const body = formValuesToUpdateBody(values)
      await new Promise<void>((resolve, reject) => {
        updateMutation.mutate(
          {
            params: { path: { name: env.name, poolName: pool!.name } },
            body,
          },
          {
            onSuccess: () => {
              toast.success(t("envs.poolForm.toast.updated", { name: pool!.name }))
              onClose()
              resolve()
            },
            onError: (err) => reject(err),
          },
        )
      }).catch((err: unknown) => toast.error(extractError(err)))
    } else {
      if (overrideExceeds) {
        toast.error(t("envs.poolForm.errors.overrideExceedsEnvelope"))
        return
      }
      const body = formValuesToCreateBody(values)
      await new Promise<void>((resolve, reject) => {
        createMutation.mutate(
          { params: { path: { name: env.name } }, body },
          {
            onSuccess: () => {
              toast.success(t("envs.poolForm.toast.created"))
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
          {isEdit
            ? t("envs.poolForm.editTitle", { name: pool!.name })
            : t("envs.poolForm.createTitle", { env: env.name })}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
          {/* Quota selector */}
          <Field>
            <FieldLabel>{t("envs.poolForm.quota")}</FieldLabel>
            <Controller
              control={control}
              name="quotaUrl"
              render={({ field }) => (
                <QuotaCombobox
                  items={quotas}
                  value={field.value ?? null}
                  onChange={field.onChange}
                  disabled={isEdit}
                />
              )}
            />
            <FieldDescription>
              {isEdit ? t("envs.poolForm.quotaImmutable") : t("envs.poolForm.quotaHint")}
            </FieldDescription>
          </Field>

          {/* Resource mode */}
          {gates.instanceType && !isEdit && (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground font-mono text-[11px]">
                {t("envs.poolForm.resourceMode")}
              </span>
              <Controller
                control={control}
                name="resourceMode"
                render={({ field }) => (
                  <ModeToggle
                    value={field.value}
                    onChange={field.onChange}
                    options={[
                      { value: "instanceType", label: t("envs.poolForm.modeInstanceType") },
                      { value: "manual", label: t("envs.poolForm.modeManual") },
                    ]}
                  />
                )}
              />
            </div>
          )}

          {mode === "instanceType" ? (
            <div className="space-y-2">
              <div className="grid grid-cols-3 gap-2">
                <Field className="col-span-2">
                  <FieldLabel>{t("envs.poolForm.instanceType")}</FieldLabel>
                  <Controller
                    control={control}
                    name="instanceType"
                    render={({ field, fieldState }) => (
                      <InstanceTypeCombobox
                        items={instanceTypes}
                        value={field.value ?? null}
                        onChange={field.onChange}
                        invalid={fieldState.invalid}
                        disabled={isEdit}
                      />
                    )}
                  />
                  {errors.instanceType && (
                    <FieldError>{t(errors.instanceType.message as never)}</FieldError>
                  )}
                </Field>
                <Field>
                  <FieldLabel>{t("envs.poolForm.multiplier")}</FieldLabel>
                  <Input
                    type="number"
                    min={1}
                    max={selectedInstanceType?.maxMultiplier || undefined}
                    placeholder="1"
                    disabled={isEdit}
                    {...register("multiplier")}
                  />
                  {errors.multiplier && (
                    <FieldError>{t(errors.multiplier.message as never)}</FieldError>
                  )}
                </Field>
              </div>
              {preview && (
                <div className="bg-background text-muted-foreground rounded border px-3 py-2 font-mono text-[11px]">
                  <span className="mr-1 tracking-wider uppercase">
                    {t("envs.poolForm.preview")}
                  </span>
                  <span className="text-foreground">
                    {watchedInstance}
                    {watchedMultiplier && Number(watchedMultiplier) > 1
                      ? ` × ${watchedMultiplier}`
                      : ""}{" "}
                    ={" "}
                  </span>
                  <span className="text-foreground">{preview}</span>
                </div>
              )}

              {/* Advanced — rounded-down custom resources (collapsed by default) */}
              <div className="border-border rounded-md border">
                <Accordion>
                  <AccordionItem value="advanced">
                    <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                      {t("envs.poolForm.advancedResources")}
                    </AccordionTrigger>
                    <AccordionContent className="px-3">
                      <div className="flex flex-col gap-3 pb-2">
                        <FieldDescription>
                          {t("envs.poolForm.advancedResourcesHint")}
                        </FieldDescription>
                        <div className="grid grid-cols-1 gap-2">
                          <ResourceAmountField
                            label={t("envs.poolForm.overrideCpu")}
                            control={control}
                            valueName="overrideCpuValue"
                            unitName="overrideCpuUnit"
                            units={cpuUnitOptions(t)}
                            disabled={isEdit}
                            invalid={overrideCpuExceeds || !!errors.overrideCpuValue}
                            placeholder={envelopePlaceholder(
                              envelope?.cpuMilli,
                              watchedOverrideCpuUnit === "milli" ? 1 : 1000,
                            )}
                            error={
                              overrideCpuExceeds ? (
                                <FieldError>
                                  {t("envs.poolForm.errors.overrideExceedsCpu")}
                                </FieldError>
                              ) : (
                                errors.overrideCpuValue && (
                                  <FieldError>
                                    {t(errors.overrideCpuValue.message as never)}
                                  </FieldError>
                                )
                              )
                            }
                          />
                          <ResourceAmountField
                            label={t("envs.poolForm.overrideMemory")}
                            control={control}
                            valueName="overrideMemoryValue"
                            unitName="overrideMemoryUnit"
                            units={memoryUnitOptions(t)}
                            disabled={isEdit}
                            invalid={overrideMemExceeds || !!errors.overrideMemoryValue}
                            placeholder={envelopePlaceholder(
                              envelope?.memMiB,
                              watchedOverrideMemUnit === "Mi" ? 1 : 1024,
                            )}
                            error={
                              overrideMemExceeds ? (
                                <FieldError>
                                  {t("envs.poolForm.errors.overrideExceedsMemory")}
                                </FieldError>
                              ) : (
                                errors.overrideMemoryValue && (
                                  <FieldError>
                                    {t(errors.overrideMemoryValue.message as never)}
                                  </FieldError>
                                )
                              )
                            }
                          />
                        </div>
                        {(watchedOverrideCpu !== undefined || watchedOverrideMem !== undefined) &&
                          !overrideExceeds && (
                            <div className="bg-background text-muted-foreground rounded border px-3 py-2 font-mono text-[11px]">
                              <span className="mr-1 tracking-wider uppercase">
                                {t("envs.poolForm.actualRequest")}
                              </span>
                              <span className="text-foreground">
                                {formatActualRequest(overrideCpuMilli, overrideMemMiB, envelope)}
                              </span>
                            </div>
                          )}
                      </div>
                    </AccordionContent>
                  </AccordionItem>
                </Accordion>
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-2">
              <ResourceAmountField
                label={t("envs.poolForm.cpu")}
                control={control}
                valueName="cpuValue"
                unitName="cpuUnit"
                units={cpuUnitOptions(t)}
                disabled={isEdit}
                invalid={!!errors.cpuValue}
                placeholder="2"
                error={
                  errors.cpuValue && <FieldError>{t(errors.cpuValue.message as never)}</FieldError>
                }
              />
              <ResourceAmountField
                label={t("envs.poolForm.memory")}
                control={control}
                valueName="memoryValue"
                unitName="memoryUnit"
                units={memoryUnitOptions(t)}
                disabled={isEdit}
                invalid={!!errors.memoryValue}
                placeholder="8"
                error={
                  errors.memoryValue && (
                    <FieldError>{t(errors.memoryValue.message as never)}</FieldError>
                  )
                }
              />
            </div>
          )}

          <Separator />

          <div className="grid grid-cols-3 gap-2">
            <Field>
              <FieldLabel>{t("envs.poolForm.replicas")}</FieldLabel>
              <Input
                type="number"
                min={0}
                placeholder="1"
                disabled={replicasDisabled}
                {...register("replicas")}
              />
              {replicasDisabled && (
                <FieldDescription>{t("envs.poolForm.replicasOwnedByAutoscaler")}</FieldDescription>
              )}
            </Field>
            <Field>
              <FieldLabel>{t("envs.poolForm.minReplicas")}</FieldLabel>
              <Input type="number" min={0} placeholder="0" {...register("minReplicas")} />
              {errors.minReplicas && (
                <FieldError>{t(errors.minReplicas.message as never)}</FieldError>
              )}
            </Field>
            <Field>
              <FieldLabel>{t("envs.poolForm.maxReplicas")}</FieldLabel>
              <Input type="number" min={0} placeholder="—" {...register("maxReplicas")} />
            </Field>
            <Field>
              <FieldLabel>{t("envs.form.updateStrategy.maxUnavailable")}</FieldLabel>
              <Input
                placeholder={t("envs.poolForm.inheritPlaceholder")}
                {...register("maxUnavailable")}
              />
              <FieldDescription>
                {t("envs.form.updateStrategy.maxUnavailableDescription")}
              </FieldDescription>
            </Field>
          </div>
        </div>

        <Separator />
        <div className="flex items-center gap-2 px-6 py-3">
          <FormCloneActions
            clone={poolClone}
            getValues={getValues}
            defaults={defaultValues}
            canImport={!isEdit}
            onImport={(v) => {
              reset(v)
              void trigger()
            }}
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

// ─── Helpers ─────────────────────────────────────────────────────────────────

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
    <div className="bg-background inline-flex h-9 w-fit overflow-hidden rounded-md border font-mono text-[11px]">
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

// ResourceAmountField pairs a whole-number input with a unit toggle, so the same
// field can express 2 cores or 20 milli-cores without a second control layout.
// The unit lives in the form state (not component state) so it survives reset()
// on Edit and rides along in the JSON export.
function ResourceAmountField<U extends string>({
  label,
  control,
  valueName,
  unitName,
  units,
  disabled,
  invalid,
  placeholder,
  max,
  error,
}: {
  label: string
  control: Control<FormValues>
  valueName: FieldPath<FormValues>
  unitName: FieldPath<FormValues>
  units: Array<{ value: U; label: string }>
  disabled?: boolean
  invalid?: boolean
  placeholder?: string
  max?: number
  error?: ReactNode
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <div className="flex items-center gap-2">
        <Controller
          control={control}
          name={valueName}
          render={({ field }) => (
            <Input
              type="number"
              min={1}
              max={max}
              placeholder={placeholder}
              disabled={disabled}
              aria-invalid={invalid}
              value={field.value == null ? "" : String(field.value)}
              onChange={(e) => field.onChange(e.target.value)}
              onBlur={field.onBlur}
            />
          )}
        />
        <Controller
          control={control}
          name={unitName}
          render={({ field }) => (
            <ModeToggle
              value={(field.value as U) ?? units[0].value}
              onChange={(v) => !disabled && field.onChange(v)}
              options={units}
            />
          )}
        />
      </div>
      {error}
    </Field>
  )
}

// Usage summary line for a quota item, e.g. "41/96 sci.g32-12 · 0/16 sci.c22-2".
function quotaUsageLine(item: QuotaItem): string | null {
  const total = item.resources?.total ?? {}
  const used = item.resources?.used ?? {}
  if (Object.keys(total).length === 0) return null
  return Object.entries(total)
    .map(([k, v]) => `${used[k] ?? "0"}/${v ?? "?"} ${k}`)
    .join(" · ")
}

// Quota selector. Uses Popover + Command (not the text-input Combobox) so both
// the trigger and each option can render the pool-type badge + resource-pool
// name — a plain <input> can only show a string. Value is the quota id (its
// url), kept stable for the form; the label surfaced to the user is the pool
// name, falling back to the raw id when a provider emits no pool metadata.
function QuotaCombobox({
  items,
  value,
  onChange,
  disabled,
}: {
  items: QuotaItem[]
  value: string | null
  onChange: (v: string | undefined) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")

  const uniqueItems = useMemo(() => {
    const seen = new Set<string>()
    const out: QuotaItem[] = []
    for (const it of items) {
      if (!it.id || seen.has(it.id)) continue
      seen.add(it.id)
      out.push(it)
    }
    return out
  }, [items])

  const selected = uniqueItems.find((q) => q.id === value) ?? null
  const selectedMeta = selected ? getPoolMeta(selected) : null

  // Manual search over pool name + type + raw url (cmdk filtering disabled), so
  // the badge-rendered options remain searchable by what the user reads.
  const query = search.trim().toLowerCase()
  const visible = query
    ? uniqueItems.filter((q) => {
        const { poolName, poolType } = getPoolMeta(q)
        return `${poolName ?? ""} ${poolType ?? ""} ${q.name ?? ""} ${q.id}`
          .toLowerCase()
          .includes(query)
      })
    : uniqueItems

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setSearch("")
      }}
    >
      <PopoverTrigger
        disabled={disabled}
        render={
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            className="h-9 w-full justify-between px-2.5 font-normal"
          />
        }
      >
        {selected ? (
          <span className="flex min-w-0 items-center gap-2">
            <PoolTypeBadge type={selectedMeta?.poolType} />
            <span className="truncate font-mono text-xs">{poolDisplayName(selected)}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">{t("envs.poolForm.quotaPlaceholder")}</span>
        )}
        <ChevronsUpDown className="text-muted-foreground size-4 shrink-0" />
      </PopoverTrigger>
      <PopoverContent className="w-(--anchor-width) p-0" align="start">
        <Command shouldFilter={false}>
          <CommandInput
            value={search}
            onValueChange={setSearch}
            placeholder={t("envs.poolForm.quotaSearch")}
          />
          <CommandList>
            {visible.length === 0 ? (
              <CommandEmpty>{t("envs.poolForm.quotaEmpty")}</CommandEmpty>
            ) : (
              <CommandGroup>
                {visible.map((item) => {
                  const { poolType } = getPoolMeta(item)
                  const usageLine = quotaUsageLine(item)
                  return (
                    <CommandItem
                      key={item.id}
                      value={item.id}
                      onSelect={() => {
                        onChange(item.id)
                        setOpen(false)
                        setSearch("")
                      }}
                    >
                      <div className="flex min-w-0 flex-1 flex-col gap-1">
                        <span className="flex min-w-0 items-center gap-2">
                          <PoolTypeBadge type={poolType} />
                          <span className="truncate font-mono text-xs leading-tight">
                            {poolDisplayName(item)}
                          </span>
                        </span>
                        {usageLine && (
                          <span className="text-muted-foreground text-[10px] leading-tight">
                            {usageLine}
                          </span>
                        )}
                      </div>
                      {item.id === value && <Check className="ml-2 size-4 shrink-0" />}
                    </CommandItem>
                  )
                })}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function InstanceTypeCombobox({
  items,
  value,
  onChange,
  invalid,
  disabled,
}: {
  items: AgentInstanceTypeItem[]
  value: string | null
  onChange: (v: string | undefined) => void
  invalid?: boolean
  disabled?: boolean
}) {
  const selected = items.find((it) => it.name === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item.showName || item.name}
      value={selected}
      onValueChange={(v) => onChange(v?.name ?? undefined)}
      disabled={disabled}
    >
      <ComboboxInput aria-invalid={invalid} placeholder="select instance type" />
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
                  {meta && <span className="text-muted-foreground text-[10px]">{meta}</span>}
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
  if (mib >= 1024 && mib % 1024 === 0) return `${mib / 1024}Gi`
  return `${Math.round(mib)}Mi`
}

// ─── Unit pickers ────────────────────────────────────────────────────────────

type Translate = (key: TranslationKey, params?: Record<string, string | number>) => string

function cpuUnitOptions(t: Translate): Array<{ value: CpuUnit; label: string }> {
  return [
    { value: "core", label: t("envs.poolForm.unitCores") },
    { value: "milli", label: t("envs.poolForm.unitMilliCores") },
  ]
}

function memoryUnitOptions(t: Translate): Array<{ value: MemoryUnit; label: string }> {
  return [
    { value: "Gi", label: t("envs.poolForm.unitGiB") },
    { value: "Mi", label: t("envs.poolForm.unitMiB") },
  ]
}

// envelopePlaceholder shows the full envelope in the unit currently selected, so
// leaving the field blank visibly means "use all of it".
function envelopePlaceholder(base: number | null | undefined, perUnit: number): string {
  if (base == null) return "—"
  return String(Math.floor(base / perUnit))
}

// computeEnvelope returns the instanceType × multiplier envelope in milli-cores
// / MiB — the upper bound a rounded-down request must fit within.
function computeEnvelope(
  it: AgentInstanceTypeItem | null,
  multiplierRaw: number | string | undefined,
): { cpuMilli: number | null; memMiB: number | null } | null {
  if (!it) return null
  const reqs = it.baseResources?.requests ?? it.baseResources?.limits
  if (!reqs) return null
  const m = Number(multiplierRaw)
  const mult = Number.isFinite(m) && m >= 1 ? Math.floor(m) : 1
  const baseCpu = parseCpuToCore(reqs["cpu"] as string | undefined)
  const baseMem = parseMemoryToMiB(reqs["memory"] as string | undefined)
  return {
    cpuMilli: baseCpu != null ? Math.floor(baseCpu * mult * 1000) : null,
    memMiB: baseMem != null ? Math.floor(baseMem * mult) : null,
  }
}

// formatActualRequest renders the Pod's effective request line, falling back to
// the full envelope for any dimension the user left blank.
function formatActualRequest(
  cpuMilli: number | undefined,
  memMiB: number | undefined,
  envelope: { cpuMilli: number | null; memMiB: number | null } | null,
): string {
  const cpu = cpuMilli ?? envelope?.cpuMilli ?? undefined
  const mem = memMiB ?? envelope?.memMiB ?? undefined
  const parts: string[] = []
  if (cpu != null) {
    parts.push(cpu % 1000 === 0 ? `${formatCores(cpu / 1000)}c` : `${Math.round(cpu)}m`)
  }
  if (mem != null) parts.push(formatMemPreview(mem))
  return parts.join(" / ")
}

// ─── Form ↔ API mapping ──────────────────────────────────────────────────────

function buildDefaultValues(
  env: AgentSandboxEnv,
  pool: AgentSandboxPool | null,
  instanceTypeGate: boolean,
): FormValues {
  const defaultMode: FormValues["resourceMode"] = instanceTypeGate ? "instanceType" : "manual"
  if (!pool) {
    return {
      resourceMode: defaultMode,
      instanceType: undefined,
      multiplier: 1,
      cpuValue: undefined,
      cpuUnit: "core",
      memoryValue: undefined,
      memoryUnit: "Gi",
      overrideCpuValue: undefined,
      overrideCpuUnit: "core",
      overrideMemoryValue: undefined,
      overrideMemoryUnit: "Gi",
      quotaUrl: undefined,
      replicas: 1,
      minReplicas: undefined,
      maxReplicas: undefined,
      autoUpdate: undefined,
      maxUnavailable: undefined,
    }
  }
  // Edit mode — extract from the matching env member.
  const member = findMember(env, pool.name)
  const cfg = member?.config
  const hasInstanceType = !!cfg?.instanceType
  const inlineCpu = cfg?.inlineResources?.requests?.["cpu"] as string | undefined
  const inlineMem = cfg?.inlineResources?.requests?.["memory"] as string | undefined
  const mode: FormValues["resourceMode"] = hasInstanceType ? "instanceType" : "manual"
  // Round-tripping through milli-cores / MiB is what lets a 20m / 128Mi Pool
  // reopen at its real size instead of a rounded whole-unit approximation.
  const cpu = parseCpuToCore(inlineCpu)
  const mem = parseMemoryToMiB(inlineMem)
  const cpuAmount = cpu != null ? splitCpu(cpu) : undefined
  const memAmount = mem != null ? splitMemory(mem) : undefined
  return {
    resourceMode: mode,
    instanceType: cfg?.instanceType,
    multiplier: cfg?.multiplier ?? 1,
    cpuValue: cpuAmount?.value,
    cpuUnit: cpuAmount?.unit ?? "core",
    memoryValue: memAmount?.value,
    memoryUnit: memAmount?.unit ?? "Gi",
    // In instanceType mode the actual request lives on inlineResources; surface
    // it (read-only) in the advanced section so an already-downsized pool shows
    // its true request.
    overrideCpuValue: hasInstanceType ? cpuAmount?.value : undefined,
    overrideCpuUnit: (hasInstanceType ? cpuAmount?.unit : undefined) ?? "core",
    overrideMemoryValue: hasInstanceType ? memAmount?.value : undefined,
    overrideMemoryUnit: (hasInstanceType ? memAmount?.unit : undefined) ?? "Gi",
    quotaUrl: cfg?.labels?.[QUOTA_URL_LABEL],
    replicas: pool.spec.replicas,
    minReplicas: cfg?.minReplicas,
    maxReplicas: cfg?.maxReplicas,
    autoUpdate: cfg?.updateStrategy?.autoUpdate,
    maxUnavailable: cfg?.updateStrategy?.maxUnavailable,
  }
}

function findMember(env: AgentSandboxEnv, name: string) {
  for (const c of env.spec.clusters ?? []) {
    for (const m of c.members ?? []) {
      if (m.name === name) return m
    }
  }
  return null
}

function findScalingGroupForPool(env: AgentSandboxEnv, name: string): string {
  return findMember(env, name)?.config?.scalingGroup ?? ""
}

function formValuesToCreateBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.resourceMode === "instanceType") {
    if (v.instanceType) body.instanceType = v.instanceType
    if (v.multiplier !== undefined) body.multiplier = v.multiplier
    // Rounded-down request: send inlineResources alongside instanceType so the
    // Pod requests less than the reserved instance. Both dimensions are
    // required together (enforced by the schema) so inlineResources is always
    // complete; the reservation still charges the full envelope.
    if (v.overrideCpuValue !== undefined && v.overrideMemoryValue !== undefined) {
      const requests = {
        cpu: cpuQuantity(v.overrideCpuValue, v.overrideCpuUnit),
        memory: memoryQuantity(v.overrideMemoryValue, v.overrideMemoryUnit),
      }
      body.inlineResources = { requests, limits: { ...requests } }
    }
  } else {
    const requests: Record<string, string> = {}
    if (v.cpuValue !== undefined) requests.cpu = cpuQuantity(v.cpuValue, v.cpuUnit)
    if (v.memoryValue !== undefined) {
      requests.memory = memoryQuantity(v.memoryValue, v.memoryUnit)
    }
    body.inlineResources = { requests, limits: { ...requests } }
  }
  if (v.quotaUrl) {
    body.labels = { [QUOTA_URL_LABEL]: v.quotaUrl }
  }
  if (v.replicas !== undefined) body.replicas = v.replicas
  if (v.minReplicas !== undefined) body.minReplicas = v.minReplicas
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  const us = buildMemberUpdateStrategy(v)
  if (us) body.updateStrategy = us
  return body
}

function formValuesToUpdateBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.replicas !== undefined) body.replicas = v.replicas
  if (v.minReplicas !== undefined) body.minReplicas = v.minReplicas
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  const us = buildMemberUpdateStrategy(v)
  if (us) body.updateStrategy = us
  return body
}

// buildMemberUpdateStrategy emits a per-member rollout override only when the
// user set a field; an all-empty result inherits the Env-level strategy.
function buildMemberUpdateStrategy(v: FormValues): Record<string, unknown> | undefined {
  const us: Record<string, unknown> = {}
  if (v.autoUpdate !== undefined) us.autoUpdate = v.autoUpdate
  if (v.maxUnavailable) us.maxUnavailable = v.maxUnavailable
  return Object.keys(us).length ? us : undefined
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
