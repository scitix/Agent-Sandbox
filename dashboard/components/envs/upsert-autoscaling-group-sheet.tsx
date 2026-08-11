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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
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
import { FormCloneActions } from "@/components/custom/form-clone-actions"
import { createFormClone } from "@/lib/utils/form-clone"
import { Switch } from "@/components/ui/switch"
import type { AgentEnvAutoscalingGroup, AgentSandboxEnv } from "@/lib/api/client"
import { useUpdateEnvAutoscalingGroup } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  env: AgentSandboxEnv
  // Group to edit. null keeps the sheet closed (groups are created
  // automatically when a member declaring the ScalingGroup is added, so
  // there is no create mode here — only tuning an existing group).
  group: AgentEnvAutoscalingGroup | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

const optionalSeconds = z.preprocess(
  (v) => (v === "" || v === null || v === undefined ? undefined : Number(v)),
  z.number().int().min(0).optional(),
)

const formSchema = z.object({
  enabled: z.boolean(),
  minReplicas: optionalSeconds,
  maxReplicas: optionalSeconds,
  scaleUpMode: z.enum(["Conservative", "Default", "Aggressive"]).optional(),
  cooldownSeconds: optionalSeconds,
  idleThresholdSeconds: optionalSeconds,
  idleZeroQuietWindowSeconds: optionalSeconds,
  saturationCooldownSeconds: optionalSeconds,
  idleTimeoutSeconds: optionalSeconds,
  stabilizationSeconds: optionalSeconds,
  protectionWindowSeconds: optionalSeconds,
})

type FormValues = z.infer<typeof formSchema>

// Autoscaling policy is pure numbers — no credentials to strip, and no name of
// its own (the group is named by the resource key it scales).
const autoscalingGroupClone = createFormClone<FormValues>({
  kind: "EnvAutoscalingGroupFormExport",
  version: 1,
  schema: formSchema.partial(),
  filePrefix: "autoscaling-group",
})

// crdDefaults mirrors the kubebuilder `+kubebuilder:default=` markers on
// PoolScaleUpPolicy / PoolScaleDownPolicy / EnvAutoscalingGroup. Used as the
// fallback when a group field the server has not yet stamped reads back as
// undefined, so the form always shows the value the API server would apply.
// Keep this table in lockstep with api/v1alpha1/autoscaler_types.go and
// sandboxenv_types.go.
const crdDefaults = {
  enabled: true,
  minReplicas: 0,
  maxReplicas: undefined, // unset → no ceiling (Aggressive mode requires explicit value via CRD CEL)
  scaleUpMode: "Default" as const,
  cooldownSeconds: 30,
  idleThresholdSeconds: 30,
  idleZeroQuietWindowSeconds: 300,
  saturationCooldownSeconds: 60,
  idleTimeoutSeconds: 300,
  stabilizationSeconds: 60,
  protectionWindowSeconds: 10,
}

export function UpsertAutoscalingGroupSheet({ env, group, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {open && group && (
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
  group: AgentEnvAutoscalingGroup
  onClose: () => void
}) {
  const { t } = useTranslation()

  const updateGroupMutation = useUpdateEnvAutoscalingGroup(env.name)

  const defaults = useMemo<FormValues>(() => extractGroupForForm(group), [group])

  const {
    control,
    register,
    handleSubmit,
    reset,
    trigger,
    getValues,
    formState: { isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: defaults,
  })

  const onSubmit = handleSubmit(async (values) => {
    try {
      const groupBody = { enabled: values.enabled, ...buildGroupBody(values) }
      {
        await runMutation(updateGroupMutation, {
          params: { path: { name: env.name, groupName: group!.name } },
          body: groupBody,
        })
        toast.success(t("envs.poolAutoscaling.toast", { group: group.name }))
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
          {t("envs.upsertAutoscaling.editTitle", { group: group.name })}
        </SheetTitle>
      </SheetHeader>
      <Separator />

      <form onSubmit={onSubmit} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-3 overflow-y-auto px-6 py-5">
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
            <SecondsField
              label={t("envs.editAutoscaling.field.minReplicas")}
              description={t("envs.editAutoscaling.field.minReplicasDesc")}
              {...register("minReplicas")}
            />
            <SecondsField
              label={t("envs.editAutoscaling.field.maxReplicas")}
              description={t("envs.editAutoscaling.field.maxReplicasDesc")}
              {...register("maxReplicas")}
            />
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
              <FieldDescription>{t("envs.editAutoscaling.field.scaleUpModeDesc")}</FieldDescription>
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <SecondsField
                label={t("envs.editAutoscaling.field.cooldownSeconds")}
                description={t("envs.editAutoscaling.field.cooldownSecondsDesc")}
                {...register("cooldownSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.idleThresholdSeconds")}
                description={t("envs.editAutoscaling.field.idleThresholdSecondsDesc")}
                {...register("idleThresholdSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.idleZeroQuietWindowSeconds")}
                description={t("envs.editAutoscaling.field.idleZeroQuietWindowSecondsDesc")}
                {...register("idleZeroQuietWindowSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.saturationCooldownSeconds")}
                description={t("envs.editAutoscaling.field.saturationCooldownSecondsDesc")}
                {...register("saturationCooldownSeconds")}
              />
            </div>
          </fieldset>

          <fieldset className="space-y-3 rounded border p-3">
            <legend className="text-foreground px-1 font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.scaleDownSection")}
            </legend>
            <div className="grid grid-cols-2 gap-3">
              <SecondsField
                label={t("envs.editAutoscaling.field.idleTimeoutSeconds")}
                description={t("envs.editAutoscaling.field.idleTimeoutSecondsDesc")}
                {...register("idleTimeoutSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.stabilizationSeconds")}
                description={t("envs.editAutoscaling.field.stabilizationSecondsDesc")}
                {...register("stabilizationSeconds")}
              />
              <SecondsField
                label={t("envs.editAutoscaling.field.protectionWindowSeconds")}
                description={t("envs.editAutoscaling.field.protectionWindowSecondsDesc")}
                {...register("protectionWindowSeconds")}
              />
            </div>
          </fieldset>
        </div>

        <Separator />
        <div className="flex items-center gap-2 px-6 py-3">
          <FormCloneActions
            clone={autoscalingGroupClone}
            getValues={getValues}
            defaults={defaults}
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
              {t("common.save")}
            </Button>
          </div>
        </div>
      </form>
    </Fragment>
  )
}

const SecondsField = ((
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  { label, description, ...rest }: { label: string; description?: string } & any,
) => (
  <Field>
    <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-[0.12em] uppercase">
      {label}
    </FieldLabel>
    <Input {...rest} type="number" min={0} className="h-9 font-mono text-sm" />
    {description && (
      <FieldDescription className="text-[10px] leading-snug">{description}</FieldDescription>
    )}
  </Field>
)) as (props: { label: string; description?: string; name: string }) => React.ReactElement

// extractGroupForForm produces the react-hook-form defaultValues for the
// group being edited. Every field is seeded with the current CR value;
// fields the server filled in via kubebuilder defaults come back as concrete
// numbers and round-trip through the form without surprise.
function extractGroupForForm(group: AgentEnvAutoscalingGroup): FormValues {
  return {
    enabled: group.enabled ?? crdDefaults.enabled,
    minReplicas: group.minReplicas ?? crdDefaults.minReplicas,
    maxReplicas: group.maxReplicas,
    scaleUpMode: (group.scaleUpPolicy?.mode ?? crdDefaults.scaleUpMode) as
      | "Conservative"
      | "Default"
      | "Aggressive",
    cooldownSeconds: group.scaleUpPolicy?.cooldownSeconds ?? crdDefaults.cooldownSeconds,
    idleThresholdSeconds:
      group.scaleUpPolicy?.idleThresholdSeconds ?? crdDefaults.idleThresholdSeconds,
    idleZeroQuietWindowSeconds:
      group.scaleUpPolicy?.idleZeroQuietWindowSeconds ?? crdDefaults.idleZeroQuietWindowSeconds,
    saturationCooldownSeconds:
      group.scaleUpPolicy?.saturationCooldownSeconds ?? crdDefaults.saturationCooldownSeconds,
    idleTimeoutSeconds: group.scaleDownPolicy?.idleTimeoutSeconds ?? crdDefaults.idleTimeoutSeconds,
    stabilizationSeconds:
      group.scaleDownPolicy?.stabilizationSeconds ?? crdDefaults.stabilizationSeconds,
    protectionWindowSeconds:
      group.scaleDownPolicy?.protectionWindowSeconds ?? crdDefaults.protectionWindowSeconds,
  }
}

// buildGroupBody translates the form values into the wire patch.
//
//   - When enabled=false, only `enabled` plus the bounds are sent.
//     Omitting scaleUpPolicy / scaleDownPolicy follows the server's
//     "nil = unchanged" semantic: if the user toggles the group off,
//     the previously-configured policy values are preserved so toggling
//     it back on restores the same behaviour.
//   - When enabled=true, every leaf is included so the user's choices
//     replace the live policy verbatim.
function buildGroupBody(v: FormValues) {
  const body: Record<string, unknown> = {}
  if (v.minReplicas !== undefined) body.minReplicas = v.minReplicas
  if (v.maxReplicas !== undefined) body.maxReplicas = v.maxReplicas
  if (!v.enabled) {
    return body
  }
  const up: Record<string, unknown> = {}
  if (v.scaleUpMode) up.mode = v.scaleUpMode
  if (v.cooldownSeconds !== undefined) up.cooldownSeconds = v.cooldownSeconds
  if (v.idleThresholdSeconds !== undefined) up.idleThresholdSeconds = v.idleThresholdSeconds
  if (v.idleZeroQuietWindowSeconds !== undefined) {
    up.idleZeroQuietWindowSeconds = v.idleZeroQuietWindowSeconds
  }
  if (v.saturationCooldownSeconds !== undefined) {
    up.saturationCooldownSeconds = v.saturationCooldownSeconds
  }
  body.scaleUpPolicy = up

  const down: Record<string, unknown> = {}
  if (v.idleTimeoutSeconds !== undefined) down.idleTimeoutSeconds = v.idleTimeoutSeconds
  if (v.stabilizationSeconds !== undefined) down.stabilizationSeconds = v.stabilizationSeconds
  if (v.protectionWindowSeconds !== undefined) {
    down.protectionWindowSeconds = v.protectionWindowSeconds
  }
  body.scaleDownPolicy = down
  return body
}

function runMutation<TInput>(
  mutation: {
    mutate: (input: TInput, opts: { onSuccess: () => void; onError: (e: unknown) => void }) => void
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
