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

import { Fragment, useMemo, useState } from "react"
import { Controller, useForm, useWatch } from "react-hook-form"
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
import { cn } from "@/lib/utils"
import { formatCores, parseCpuToCore, parseMemoryToMiB } from "@/lib/resources"

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
// created via instanceType has no inlineResources, leaving cpuCores/memoryGiB
// blank). Required-field validation would then fail on submit even though the
// user can't fix the values, so we attach the resource-mode refinement only
// in Create mode.
const baseObject = z.object({
  resourceMode,
  instanceType: z.string().optional(),
  multiplier: intGte1,
  cpuCores: intGte1,
  memoryGiB: intGte1,
  // Optional rounded-down request in instanceType mode. When set, the Pod
  // requests these (smaller) resources while the reservation still charges the
  // whole instanceType × multiplier envelope. Left blank → Pod uses the full
  // envelope. The fits-within (≤ envelope) check runs in the component since it
  // needs the fetched InstanceType catalog.
  overrideCpuCores: intGte1,
  overrideMemoryGiB: intGte1,
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
    const hasCpu = m.overrideCpuCores !== undefined
    const hasMem = m.overrideMemoryGiB !== undefined
    if (hasCpu !== hasMem) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.overrideBothRequired",
        path: [hasCpu ? "overrideMemoryGiB" : "overrideCpuCores"],
      })
    }
  } else {
    if (m.cpuCores === undefined) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.cpuRequired",
        path: ["cpuCores"],
      })
    }
    if (m.memoryGiB === undefined) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "envs.poolForm.errors.memoryRequired",
        path: ["memoryGiB"],
      })
    }
  }
})

const updateSchema = baseObject.superRefine(refineReplicaBounds)

type FormValues = z.infer<typeof baseObject>

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
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(isEdit ? updateSchema : createSchema),
    defaultValues,
  })

  const mode = useWatch({ control, name: "resourceMode" }) ?? "manual"
  const watchedInstance = useWatch({ control, name: "instanceType" })
  const watchedMultiplier = useWatch({ control, name: "multiplier" })
  const watchedOverrideCpu = useWatch({ control, name: "overrideCpuCores" })
  const watchedOverrideMem = useWatch({ control, name: "overrideMemoryGiB" })
  const selectedInstanceType = instanceTypes.find((it) => it.name === watchedInstance) ?? null
  const preview = computeInstanceTypePreview(selectedInstanceType, watchedMultiplier)

  // Fits-within: the rounded-down request must not exceed the envelope in any
  // dimension. Computed here (not in the zod schema) because it needs the
  // fetched InstanceType catalog. Blocks submit and surfaces inline errors.
  const envelope = computeEnvelope(selectedInstanceType, watchedMultiplier)
  const overrideCpuExceeds =
    envelope != null &&
    watchedOverrideCpu !== undefined &&
    envelope.cpuCores != null &&
    Number(watchedOverrideCpu) > envelope.cpuCores
  const overrideMemExceeds =
    envelope != null &&
    watchedOverrideMem !== undefined &&
    envelope.memGiB != null &&
    Number(watchedOverrideMem) > envelope.memGiB
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
                        <div className="grid grid-cols-2 gap-2">
                          <Field>
                            <FieldLabel>{t("envs.poolForm.overrideCpuCores")}</FieldLabel>
                            <Input
                              type="number"
                              min={1}
                              max={envelope?.cpuCores || undefined}
                              placeholder={
                                envelope?.cpuCores != null ? String(envelope.cpuCores) : "—"
                              }
                              disabled={isEdit}
                              aria-invalid={overrideCpuExceeds || !!errors.overrideCpuCores}
                              {...register("overrideCpuCores")}
                            />
                            {overrideCpuExceeds ? (
                              <FieldError>
                                {t("envs.poolForm.errors.overrideExceedsCpu")}
                              </FieldError>
                            ) : (
                              errors.overrideCpuCores && (
                                <FieldError>
                                  {t(errors.overrideCpuCores.message as never)}
                                </FieldError>
                              )
                            )}
                          </Field>
                          <Field>
                            <FieldLabel>{t("envs.poolForm.overrideMemoryGiB")}</FieldLabel>
                            <Input
                              type="number"
                              min={1}
                              max={envelope?.memGiB || undefined}
                              placeholder={
                                envelope?.memGiB != null ? String(envelope.memGiB) : "—"
                              }
                              disabled={isEdit}
                              aria-invalid={overrideMemExceeds || !!errors.overrideMemoryGiB}
                              {...register("overrideMemoryGiB")}
                            />
                            {overrideMemExceeds ? (
                              <FieldError>
                                {t("envs.poolForm.errors.overrideExceedsMemory")}
                              </FieldError>
                            ) : (
                              errors.overrideMemoryGiB && (
                                <FieldError>
                                  {t(errors.overrideMemoryGiB.message as never)}
                                </FieldError>
                              )
                            )}
                          </Field>
                        </div>
                        {(watchedOverrideCpu !== undefined || watchedOverrideMem !== undefined) &&
                          !overrideExceeds && (
                            <div className="bg-background text-muted-foreground rounded border px-3 py-2 font-mono text-[11px]">
                              <span className="mr-1 tracking-wider uppercase">
                                {t("envs.poolForm.actualRequest")}
                              </span>
                              <span className="text-foreground">
                                {formatActualRequest(
                                  watchedOverrideCpu,
                                  watchedOverrideMem,
                                  envelope,
                                )}
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
            <div className="grid grid-cols-2 gap-2">
              <Field>
                <FieldLabel>{t("envs.poolForm.cpuCores")}</FieldLabel>
                <Input
                  type="number"
                  min={1}
                  placeholder="2"
                  disabled={isEdit}
                  {...register("cpuCores")}
                />
                {errors.cpuCores && <FieldError>{t(errors.cpuCores.message as never)}</FieldError>}
              </Field>
              <Field>
                <FieldLabel>{t("envs.poolForm.memoryGiB")}</FieldLabel>
                <Input
                  type="number"
                  min={1}
                  placeholder="8"
                  disabled={isEdit}
                  {...register("memoryGiB")}
                />
                {errors.memoryGiB && (
                  <FieldError>{t(errors.memoryGiB.message as never)}</FieldError>
                )}
              </Field>
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
              <Input placeholder={t("envs.poolForm.inheritPlaceholder")} {...register("maxUnavailable")} />
              <FieldDescription>
                {t("envs.form.updateStrategy.maxUnavailableDescription")}
              </FieldDescription>
            </Field>
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

// computeEnvelope returns the instanceType × multiplier envelope in whole
// cores / GiB — the upper bound a rounded-down request must fit within.
function computeEnvelope(
  it: AgentInstanceTypeItem | null,
  multiplierRaw: number | string | undefined,
): { cpuCores: number | null; memGiB: number | null } | null {
  if (!it) return null
  const reqs = it.baseResources?.requests ?? it.baseResources?.limits
  if (!reqs) return null
  const m = Number(multiplierRaw)
  const mult = Number.isFinite(m) && m >= 1 ? Math.floor(m) : 1
  const baseCpu = parseCpuToCore(reqs["cpu"] as string | undefined)
  const baseMem = parseMemoryToMiB(reqs["memory"] as string | undefined)
  return {
    cpuCores: baseCpu != null ? Math.floor(baseCpu * mult) : null,
    memGiB: baseMem != null ? Math.floor((baseMem * mult) / 1024) : null,
  }
}

// formatActualRequest renders the Pod's effective request line, falling back to
// the full envelope for any dimension the user left blank.
function formatActualRequest(
  cpu: number | string | undefined,
  memGiB: number | string | undefined,
  envelope: { cpuCores: number | null; memGiB: number | null } | null,
): string {
  const cpuVal = cpu !== undefined ? Number(cpu) : (envelope?.cpuCores ?? undefined)
  const memVal = memGiB !== undefined ? Number(memGiB) : (envelope?.memGiB ?? undefined)
  const parts: string[] = []
  if (cpuVal != null && Number.isFinite(cpuVal)) parts.push(`${cpuVal}c`)
  if (memVal != null && Number.isFinite(memVal)) parts.push(`${memVal}Gi`)
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
      cpuCores: undefined,
      memoryGiB: undefined,
      overrideCpuCores: undefined,
      overrideMemoryGiB: undefined,
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
  const cpu = parseCpuToCore(inlineCpu)
  const mem = parseMemoryToMiB(inlineMem)
  const cpuCores = cpu != null ? Math.max(1, Math.round(cpu)) : undefined
  const memoryGiB = mem != null ? Math.max(1, Math.round(mem / 1024)) : undefined
  return {
    resourceMode: mode,
    instanceType: cfg?.instanceType,
    multiplier: cfg?.multiplier ?? 1,
    cpuCores,
    memoryGiB,
    // In instanceType mode the actual request lives on inlineResources; surface
    // it (read-only) in the advanced section so an already-downsized pool shows
    // its true request.
    overrideCpuCores: hasInstanceType ? cpuCores : undefined,
    overrideMemoryGiB: hasInstanceType ? memoryGiB : undefined,
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
    if (v.overrideCpuCores !== undefined && v.overrideMemoryGiB !== undefined) {
      const requests = {
        cpu: String(v.overrideCpuCores),
        memory: `${v.overrideMemoryGiB}Gi`,
      }
      body.inlineResources = { requests, limits: { ...requests } }
    }
  } else {
    const requests: Record<string, string> = {}
    if (v.cpuCores !== undefined) requests.cpu = String(v.cpuCores)
    if (v.memoryGiB !== undefined) requests.memory = `${v.memoryGiB}Gi`
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
