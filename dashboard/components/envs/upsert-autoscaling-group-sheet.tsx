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
import { Controller, useForm } from "react-hook-form"
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
import type { AgentEnvAutoscalingGroup, AgentSandboxEnv } from "@/lib/api/client"
import { useAddEnvAutoscalingGroup, useUpdateEnvAutoscalingGroup } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  env: AgentSandboxEnv
  group: AgentEnvAutoscalingGroup | null // null = create
  open: boolean
  onOpenChange: (open: boolean) => void
}

const optionalSeconds = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(0).optional(),
)

const formSchema = z.object({
  name: z.string().optional(),
  enabled: z.boolean(),
  minReplicas: optionalSeconds,
  maxReplicas: optionalSeconds,
  scaleUpMode: z.enum(["Conservative", "Default", "Aggressive"]).optional(),
  cooldownSeconds: optionalSeconds,
  idleThresholdSeconds: optionalSeconds,
  saturationCooldownSeconds: optionalSeconds,
  idleTimeoutSeconds: optionalSeconds,
  stabilizationSeconds: optionalSeconds,
  protectionWindowSeconds: optionalSeconds,
})

type FormValues = z.infer<typeof formSchema>

export function UpsertAutoscalingGroupSheet({ env, group, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {open && (
          <UpsertInner env={env} group={group} onClose={() => onOpenChange(false)} />
        )}
      </SheetContent>
    </Sheet>
  )
}

function UpsertInner({
  env,
  group,
  onClose,
}: {
  env: AgentSandboxEnv
  group: AgentEnvAutoscalingGroup | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const isEdit = !!group

  const addGroupMutation = useAddEnvAutoscalingGroup(env.name)
  const updateGroupMutation = useUpdateEnvAutoscalingGroup(env.name)

  // Available scaling groups for create mode: every distinct ScalingGroup
  // referenced by an EnvClusterMember minus those that already have a rule.
  // Server requires the name to match an existing member's ScalingGroup so
  // free-text would be silently rejected — Combobox enforces the constraint.
  const availableGroups = useMemo<string[]>(() => {
    if (isEdit) return []
    const taken = new Set((env.spec.autoscaling?.groups ?? []).map((g) => g.name))
    const seen = new Set<string>()
    const out: string[] = []
    for (const cluster of env.spec.clusters ?? []) {
      for (const m of cluster.members ?? []) {
        const sg = m.config?.scalingGroup
        if (!sg) continue
        if (taken.has(sg) || seen.has(sg)) continue
        seen.add(sg)
        out.push(sg)
      }
    }
    return out
  }, [env, isEdit])

  const defaults = useMemo<FormValues>(() => extractGroupForForm(group), [group])

  const {
    control,
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: defaults,
  })

  const onSubmit = handleSubmit(async (values) => {
    try {
      const groupBody = { enabled: values.enabled, ...buildGroupBody(values) }
      if (isEdit) {
        await runMutation(updateGroupMutation, {
          params: { path: { name: env.name, groupName: group!.name } },
          body: groupBody,
        })
        toast.success(t("envs.poolAutoscaling.toast", { group: group!.name }))
      } else {
        const name = values.name?.trim()
        if (!name) {
          toast.error(t("envs.upsertAutoscaling.errors.nameRequired"))
          return
        }
        await runMutation(addGroupMutation, {
          params: { path: { name: env.name } },
          body: { name, ...groupBody },
        })
        toast.success(t("envs.upsertAutoscaling.createdToast", { group: name }))
      }
      onClose()
    } catch (err: unknown) {
      toast.error(extractError(err))
    }
  })

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {isEdit
            ? t("envs.upsertAutoscaling.editTitle", { group: group!.name })
            : t("envs.upsertAutoscaling.createTitle")}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-3 overflow-y-auto px-6 py-5">
          {/* ScalingGroup picker (create only) */}
          {!isEdit && (
            <Field>
              <FieldLabel>{t("envs.upsertAutoscaling.field.scalingGroup")}</FieldLabel>
              <Controller
                control={control}
                name="name"
                render={({ field, fieldState }) => (
                  <ScalingGroupCombobox
                    items={availableGroups}
                    value={field.value ?? null}
                    onChange={field.onChange}
                    invalid={fieldState.invalid}
                  />
                )}
              />
              {availableGroups.length === 0 ? (
                <FieldError>{t("envs.upsertAutoscaling.errors.noAvailableGroups")}</FieldError>
              ) : (
                <FieldDescription>
                  {t("envs.upsertAutoscaling.field.scalingGroupHint")}
                </FieldDescription>
              )}
              {errors.name && <FieldError>{String(errors.name.message)}</FieldError>}
            </Field>
          )}

          <Controller
            control={control}
            name="enabled"
            render={({ field }) => (
              <div className="flex items-start justify-between gap-4 rounded border p-3">
                <div>
                  <div className="text-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                    {t("envs.editAutoscaling.field.enabled")}
                  </div>
                  <p className="text-muted-foreground mt-1 text-[11px]">
                    {t("envs.poolAutoscaling.enabledDescription")}
                  </p>
                </div>
                <Switch
                  checked={!!field.value}
                  onCheckedChange={(v: boolean) => field.onChange(v)}
                />
              </div>
            )}
          />

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.editAutoscaling.field.minReplicas")}
              </FieldLabel>
              <Input
                {...register("minReplicas")}
                type="number"
                min={0}
                className="h-9 font-mono text-sm"
              />
            </Field>
            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.editAutoscaling.field.maxReplicas")}
              </FieldLabel>
              <Input
                {...register("maxReplicas")}
                type="number"
                min={0}
                className="h-9 font-mono text-sm"
              />
            </Field>
          </div>

          <fieldset className="space-y-3 rounded border p-3">
            <legend className="text-foreground px-1 font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.scaleUpSection")}
            </legend>
            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.editAutoscaling.field.scaleUpMode")}
              </FieldLabel>
              <Controller
                control={control}
                name="scaleUpMode"
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
                {...register("cooldownSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.idleThresholdSeconds")}
                {...register("idleThresholdSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.saturationCooldownSeconds")}
                {...register("saturationCooldownSeconds")}
              />
            </div>
          </fieldset>

          <fieldset className="space-y-3 rounded border p-3">
            <legend className="text-foreground px-1 font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.scaleDownSection")}
            </legend>
            <div className="grid grid-cols-3 gap-3">
              <SecondsField
                label={t("envs.editAutoscaling.field.idleTimeoutSeconds")}
                {...register("idleTimeoutSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.stabilizationSeconds")}
                {...register("stabilizationSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.protectionWindowSeconds")}
                {...register("protectionWindowSeconds")}
              />
            </div>
          </fieldset>
        </div>

        <Separator />
        <div className="flex justify-end gap-2 px-6 py-3">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            disabled={isSubmitting || (!isEdit && availableGroups.length === 0)}
            className="gap-1.5"
          >
            <Save className="h-3.5 w-3.5" />
            {isEdit ? t("common.save") : t("common.create")}
          </Button>
        </div>
      </form>
    </Fragment>
  )
}

function ScalingGroupCombobox({
  items,
  value,
  onChange,
  invalid,
}: {
  items: string[]
  value: string | null
  onChange: (v: string | undefined) => void
  invalid?: boolean
}) {
  const selected = items.find((i) => i === value) ?? null
  return (
    <Combobox
      items={items}
      itemToStringLabel={(item) => item}
      value={selected}
      onValueChange={(v) => onChange(v ?? undefined)}
    >
      <ComboboxInput aria-invalid={invalid} placeholder="—" />
      <ComboboxContent>
        <ComboboxList>
          {(item: string) => (
            <ComboboxItem key={item} value={item}>
              <span className="font-mono text-xs">{item}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
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

function extractGroupForForm(group: AgentEnvAutoscalingGroup | null): FormValues {
  return {
    name: group?.name,
    enabled: group?.enabled ?? true,
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

function buildGroupBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.minReplicas !== undefined) body.minReplicas = v.minReplicas
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  if (
    v.scaleUpMode !== undefined ||
    v.cooldownSeconds !== undefined ||
    v.idleThresholdSeconds !== undefined ||
    v.saturationCooldownSeconds !== undefined
  ) {
    const up: Record<string, unknown> = {}
    if (v.scaleUpMode) up.mode = v.scaleUpMode
    if (v.cooldownSeconds !== undefined) up.cooldownSeconds = v.cooldownSeconds
    if (v.idleThresholdSeconds !== undefined) up.idleThresholdSeconds = v.idleThresholdSeconds
    if (v.saturationCooldownSeconds !== undefined) {
      up.saturationCooldownSeconds = v.saturationCooldownSeconds
    }
    body.scaleUpPolicy = up
  }
  if (
    v.idleTimeoutSeconds !== undefined ||
    v.stabilizationSeconds !== undefined ||
    v.protectionWindowSeconds !== undefined
  ) {
    const down: Record<string, unknown> = {}
    if (v.idleTimeoutSeconds !== undefined) down.idleTimeoutSeconds = v.idleTimeoutSeconds
    if (v.stabilizationSeconds !== undefined) down.stabilizationSeconds = v.stabilizationSeconds
    if (v.protectionWindowSeconds !== undefined) {
      down.protectionWindowSeconds = v.protectionWindowSeconds
    }
    body.scaleDownPolicy = down
  }
  return body
}

function runMutation<TInput>(
  mutation: {
    mutate: (
      input: TInput,
      opts: { onSuccess: () => void; onError: (e: unknown) => void },
    ) => void
  },
  input: TInput,
): Promise<void> {
  return new Promise((resolve, reject) => {
    mutation.mutate(input, {
      onSuccess: () => resolve(),
      onError: (err) => reject(err),
    })
  })
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
