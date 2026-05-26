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
import { Field, FieldLabel } from "@/components/ui/field"
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
import type { AgentSandboxEnv } from "@/lib/api/client"
import { useAddEnvAutoscalingGroup, useUpdateEnvAutoscalingGroup } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  env: AgentSandboxEnv
  scalingGroupName: string | null // null = sheet closed
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
  saturationCooldownSeconds: optionalSeconds,
  idleTimeoutSeconds: optionalSeconds,
  stabilizationSeconds: optionalSeconds,
  protectionWindowSeconds: optionalSeconds,
})

type FormValues = z.infer<typeof formSchema>

export function EditPoolAutoscalingSheet({ env, scalingGroupName, onOpenChange }: Props) {
  const open = scalingGroupName !== null
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {open && scalingGroupName && (
          <EditAutoscalingInner
            env={env}
            scalingGroupName={scalingGroupName}
            onClose={() => onOpenChange(false)}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

function EditAutoscalingInner({
  env,
  scalingGroupName,
  onClose,
}: {
  env: AgentSandboxEnv
  scalingGroupName: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const addGroupMutation = useAddEnvAutoscalingGroup(env.name)
  const updateGroupMutation = useUpdateEnvAutoscalingGroup(env.name)

  const defaults = useMemo<FormValues>(
    () => extractGroupForForm(env, scalingGroupName),
    [env, scalingGroupName],
  )

  const {
    control,
    register,
    handleSubmit,
    formState: { isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: defaults,
  })

  const onSubmit = handleSubmit(async (values) => {
    try {
      // Enabled is per-group now: include it directly in the Add/Update
      // payload alongside policy fields. No separate SetEnabled call.
      const groupExists = (env.spec.autoscaling?.groups ?? []).some(
        (g) => g.name === scalingGroupName,
      )
      const groupBody = { enabled: values.enabled, ...buildGroupBody(values) }
      if (groupExists) {
        await runMutation(updateGroupMutation, {
          params: { path: { name: env.name, groupName: scalingGroupName } },
          body: groupBody,
        })
      } else {
        await runMutation(addGroupMutation, {
          params: { path: { name: env.name } },
          body: { name: scalingGroupName, ...groupBody },
        })
      }

      toast.success(t("envs.poolAutoscaling.toast", { group: scalingGroupName }))
      onClose()
    } catch (err: unknown) {
      toast.error(extractError(err))
    }
  })

  return (
    <Fragment>
      <SheetHeader className="px-6 py-4">
        <SheetTitle className="font-mono text-sm tracking-wider uppercase">
          {t("envs.poolAutoscaling.title", { group: scalingGroupName })}
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
          <Button type="submit" disabled={isSubmitting} className="gap-1.5">
            <Save className="h-3.5 w-3.5" />
            {t("common.save")}
          </Button>
        </div>
      </form>
    </Fragment>
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

// ─── Form ↔ API mapping ──────────────────────────────────────────────────────

function extractGroupForForm(env: AgentSandboxEnv, groupName: string): FormValues {
  const auto = env.spec.autoscaling
  const group = auto?.groups?.find((g) => g.name === groupName)
  return {
    enabled: group?.enabled ?? false,
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

// buildGroupBody projects the form values into the shared shape used by
// both Add (POST) and Update (PUT) — the request bodies have the same
// fields except for the group `name`, which Add provides via the body and
// Update via the URL path.
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

// runMutation wraps the openapi-react-query mutate() into a promise so the
// submit handler can `await` it. Required because the queries package
// exposes only the imperative .mutate(input, { onSuccess, onError })
// callback style.
function runMutation<TInput>(
  mutation: { mutate: (input: TInput, opts: { onSuccess: () => void; onError: (e: unknown) => void }) => void },
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
