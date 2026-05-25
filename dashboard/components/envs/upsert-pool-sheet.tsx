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
import { Controller, useForm, useWatch } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Save } from "lucide-react"

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

const formSchema = z
  .object({
    resourceMode,
    instanceType: z.string().optional(),
    multiplier: intGte1,
    cpuCores: intGte1,
    memoryGiB: intGte1,
    quotaUrl: z.string().optional(),
    replicas: intGte0,
    maxReplicas: intGte0,
  })
  .superRefine((m, ctx) => {
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

type FormValues = z.infer<typeof formSchema>

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function UpsertPoolSheet({ env, pool, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {open && (
          <UpsertPoolInner env={env} pool={pool} onClose={() => onOpenChange(false)} />
        )}
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
    resolver: zodResolver(formSchema),
    defaultValues,
  })

  const mode = useWatch({ control, name: "resourceMode" }) ?? "manual"
  const watchedInstance = useWatch({ control, name: "instanceType" })
  const watchedMultiplier = useWatch({ control, name: "multiplier" })
  const selectedInstanceType =
    instanceTypes.find((it) => it.name === watchedInstance) ?? null
  const preview = computeInstanceTypePreview(selectedInstanceType, watchedMultiplier)

  // Member's scaling group only matters for the autoscaling-vs-replicas
  // gate on Update. We look it up from the env spec.
  const scalingGroupName = pool ? findScalingGroupForPool(env, pool.name) : ""
  const autoscalingEnabled = env.spec.autoscaling?.enabled === true
  const groupHasAutoscaling =
    autoscalingEnabled && env.spec.autoscaling?.groups?.some((g) => g.name === scalingGroupName)
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
              <span className="font-mono text-[11px] text-muted-foreground">
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
                <div className="rounded border bg-background px-3 py-2 font-mono text-[11px] text-muted-foreground">
                  <span className="mr-1 uppercase tracking-wider">
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
                {errors.cpuCores && (
                  <FieldError>{t(errors.cpuCores.message as never)}</FieldError>
                )}
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

          <div className="grid grid-cols-2 gap-2">
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
              <FieldLabel>{t("envs.poolForm.maxReplicas")}</FieldLabel>
              <Input
                type="number"
                min={0}
                placeholder="—"
                {...register("maxReplicas")}
              />
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
  return (
    <Combobox
      items={uniqueItems}
      itemToStringLabel={(item) => item.name || item.id}
      value={selected}
      onValueChange={(v) => onChange(v?.id ?? undefined)}
      disabled={disabled}
    >
      <ComboboxInput placeholder="(none)" />
      <ComboboxContent>
        <ComboboxList>
          {(item: QuotaItem) => {
            const total = item.resources?.total ?? {}
            const used = item.resources?.used ?? {}
            const usageLine =
              Object.keys(total).length > 0
                ? Object.entries(total)
                    .map(([k, v]) => `${used[k] ?? "0"}/${v ?? "?"} ${k}`)
                    .join(" · ")
                : null
            return (
              <ComboboxItem key={item.id} value={item}>
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate font-mono text-xs leading-tight">
                    {item.name || item.id}
                  </span>
                  {usageLine && (
                    <span className="text-muted-foreground text-[10px] leading-tight">
                      {usageLine}
                    </span>
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
      quotaUrl: undefined,
      replicas: 1,
      maxReplicas: undefined,
    }
  }
  // Edit mode — extract from the matching env member.
  const member = findMember(env, pool.name)
  const hasInstanceType = !!member?.instanceType
  const inlineCpu = member?.inlineResources?.requests?.["cpu"] as string | undefined
  const inlineMem = member?.inlineResources?.requests?.["memory"] as string | undefined
  const mode: FormValues["resourceMode"] = hasInstanceType ? "instanceType" : "manual"
  const cpu = parseCpuToCore(inlineCpu)
  const mem = parseMemoryToMiB(inlineMem)
  return {
    resourceMode: mode,
    instanceType: member?.instanceType,
    multiplier: member?.multiplier ?? 1,
    cpuCores: cpu != null ? Math.max(1, Math.round(cpu)) : undefined,
    memoryGiB: mem != null ? Math.max(1, Math.round(mem / 1024)) : undefined,
    quotaUrl: member?.labels?.[QUOTA_URL_LABEL],
    replicas: pool.spec.replicas,
    maxReplicas: member?.maxReplicas,
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
  return findMember(env, name)?.scalingGroup ?? ""
}

function formValuesToCreateBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.resourceMode === "instanceType") {
    if (v.instanceType) body.instanceType = v.instanceType
    if (v.multiplier !== undefined) body.multiplier = v.multiplier
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
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  return body
}

function formValuesToUpdateBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.replicas !== undefined) body.replicas = v.replicas
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  return body
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
