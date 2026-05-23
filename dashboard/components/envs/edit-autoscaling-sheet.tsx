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

import { useMemo } from "react"
import { Controller, useForm, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Save } from "lucide-react"

import { Button } from "@/components/ui/button"
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
import { Switch } from "@/components/ui/switch"
import type {
  AgentEnvAutoscalingGroup,
  AgentEnvAutoscalingSpec,
  AgentSandboxEnv,
} from "@/lib/api/client"
import { useUpdateEnvAutoscaling } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  env: AgentSandboxEnv | null
  onOpenChange: (open: boolean) => void
}

/**
 * EditAutoscalingSheet edits the first scaling group's policy on a
 * SandboxEnv. The Env may have multiple groups in its spec (future
 * multi-resource support) but the MVP only exposes groups[0] — the
 * adopter-created Env always has exactly one group.
 *
 * Submitting issues a PATCH that replaces the entire `autoscaling` block;
 * the form preserves untouched groups (groups[1:]) verbatim so multi-group
 * data isn't lost on save.
 */
export function EditAutoscalingSheet({ env, onOpenChange }: Props) {
  const isOpen = !!env
  return (
    <Sheet open={isOpen} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-xl"
      >
        {env && <EditAutoscalingInner env={env} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

// ─── Form schema ─────────────────────────────────────────────────────────────

/**
 * Coerces an empty-string input to `undefined` so the form can distinguish
 * "user cleared this field — use server default" from "user typed 0".
 */
const optionalSeconds = z
  .union([z.literal(""), z.coerce.number().int().min(0)])
  .optional()
  .transform((v) => (v === "" ? undefined : (v as number)))

const optionalReplicas = z
  .union([z.literal(""), z.coerce.number().int().min(0)])
  .optional()
  .transform((v) => (v === "" ? undefined : (v as number)))

const formSchema = z.object({
  enabled: z.boolean().optional(),
  groupName: z.string().min(1),
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

type FormInput = z.input<typeof formSchema>

/**
 * Reads the existing group (or returns a stub with the env's name) so the
 * form's defaultValues populate consistently.
 */
function readInitial(env: AgentSandboxEnv): FormInput {
  const auto = env.spec.autoscaling
  const group = auto?.groups?.[0]
  return {
    enabled: auto?.enabled ?? false,
    groupName: group?.name ?? "default",
    minReplicas: group?.minReplicas ?? "",
    maxReplicas: group?.maxReplicas ?? "",
    scaleUpMode: (group?.scaleUpPolicy?.mode ?? undefined) as FormInput["scaleUpMode"],
    cooldownSeconds: group?.scaleUpPolicy?.cooldownSeconds ?? "",
    idleThresholdSeconds: group?.scaleUpPolicy?.idleThresholdSeconds ?? "",
    saturationCooldownSeconds: group?.scaleUpPolicy?.saturationCooldownSeconds ?? "",
    idleTimeoutSeconds: group?.scaleDownPolicy?.idleTimeoutSeconds ?? "",
    stabilizationSeconds: group?.scaleDownPolicy?.stabilizationSeconds ?? "",
    protectionWindowSeconds: group?.scaleDownPolicy?.protectionWindowSeconds ?? "",
  }
}

function EditAutoscalingInner({ env, onClose }: { env: AgentSandboxEnv; onClose: () => void }) {
  const { t } = useTranslation()
  const { mutate, isPending } = useUpdateEnvAutoscaling()
  const defaultValues = useMemo(() => readInitial(env), [env])

  const {
    register,
    handleSubmit,
    control,
    formState: { errors },
  } = useForm<FormInput>({
    resolver: zodResolver(formSchema),
    defaultValues,
  })

  const enabled = useWatch({ control, name: "enabled" }) ?? false

  const onSubmit = (raw: FormInput) => {
    const data = formSchema.parse(raw)
    const existingGroups = env.spec.autoscaling?.groups ?? []
    // Replace groups[0] but preserve any subsequent groups so a multi-group
    // env (future) doesn't lose state when editing the first one.
    const updatedGroup: AgentEnvAutoscalingGroup = {
      name: data.groupName,
      ...(data.minReplicas !== undefined && { minReplicas: data.minReplicas }),
      ...(data.maxReplicas !== undefined && { maxReplicas: data.maxReplicas }),
      ...(hasScaleUpFields(data) && {
        scaleUpPolicy: {
          ...(data.scaleUpMode && { mode: data.scaleUpMode }),
          ...(data.cooldownSeconds !== undefined && { cooldownSeconds: data.cooldownSeconds }),
          ...(data.idleThresholdSeconds !== undefined && {
            idleThresholdSeconds: data.idleThresholdSeconds,
          }),
          ...(data.saturationCooldownSeconds !== undefined && {
            saturationCooldownSeconds: data.saturationCooldownSeconds,
          }),
        },
      }),
      ...(hasScaleDownFields(data) && {
        scaleDownPolicy: {
          ...(data.idleTimeoutSeconds !== undefined && { idleTimeoutSeconds: data.idleTimeoutSeconds }),
          ...(data.stabilizationSeconds !== undefined && {
            stabilizationSeconds: data.stabilizationSeconds,
          }),
          ...(data.protectionWindowSeconds !== undefined && {
            protectionWindowSeconds: data.protectionWindowSeconds,
          }),
        },
      }),
    }
    const groups = existingGroups.length > 0 ? [updatedGroup, ...existingGroups.slice(1)] : [updatedGroup]
    const body: { autoscaling: AgentEnvAutoscalingSpec } = {
      autoscaling: {
        enabled: data.enabled ?? false,
        groups,
      },
    }
    mutate(
      { params: { path: { name: env.name } }, body },
      {
        onSuccess: () => {
          toast.success(t("envs.editAutoscaling.savedSuccess", { name: env.name }))
          onClose()
        },
      },
    )
  }

  return (
    <>
      <SheetHeader className="border-border border-b px-5 py-4">
        <SheetTitle className="font-mono text-base font-semibold">
          {t("envs.editAutoscaling.title", { name: env.name })}
        </SheetTitle>
        <p className="text-muted-foreground mt-1 text-xs">
          {t("envs.editAutoscaling.description")}
        </p>
      </SheetHeader>

      <form
        id="edit-autoscaling-form"
        onSubmit={handleSubmit(onSubmit)}
        className="flex flex-1 flex-col overflow-hidden"
      >
        <div className="flex-1 space-y-5 overflow-y-auto px-5 py-5">
          {/* Group identifier (read-only for MVP) */}
          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("envs.editAutoscaling.group")}
            </FieldLabel>
            <Input
              {...register("groupName")}
              readOnly
              className="border-border bg-muted/30 h-9 font-mono text-xs"
            />
            <FieldDescription className="text-[10px]">
              Derived from the member resource shape. Read-only.
            </FieldDescription>
          </Field>

          {/* Enabled toggle */}
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

          {/* min/max replicas */}
          <div className="grid grid-cols-2 gap-4">
            <Field data-invalid={!!errors.minReplicas}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.editAutoscaling.field.minReplicas")}
              </FieldLabel>
              <Input
                {...register("minReplicas")}
                type="number"
                min={0}
                disabled={!enabled}
                className="border-border bg-background h-9 font-mono text-sm"
              />
              <FieldError errors={[errors.minReplicas]} className="font-mono text-xs" />
            </Field>
            <Field data-invalid={!!errors.maxReplicas}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.editAutoscaling.field.maxReplicas")}
              </FieldLabel>
              <Input
                {...register("maxReplicas")}
                type="number"
                min={0}
                disabled={!enabled}
                className="border-border bg-background h-9 font-mono text-sm"
              />
              <FieldError errors={[errors.maxReplicas]} className="font-mono text-xs" />
            </Field>
          </div>

          {/* Scale-up section */}
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
                name="scaleUpMode"
                render={({ field }) => (
                  <Select
                    value={field.value ?? ""}
                    onValueChange={(v: string | null) => field.onChange(v === "" || v === null ? undefined : v)}
                  >
                    <SelectTrigger className="border-border bg-background h-9 font-mono text-sm">
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
              <FieldDescription className="text-[10px]">
                {t("envs.editAutoscaling.field.scaleUpModeDesc")}
              </FieldDescription>
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

          {/* Scale-down section */}
          <fieldset disabled={!enabled} className="space-y-3 rounded border p-3">
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

        <div className="border-border flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
          <Button type="button" variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={isPending} className="gap-1">
            <Save className="h-3.5 w-3.5" /> {t("common.save")}
          </Button>
        </div>
      </form>
    </>
  )
}

// SecondsField is a thin numeric input wrapper used for the seconds knobs in
// the scale-up / scale-down sub-sections — they all share the same shape
// (label + number input + empty="" → undefined transform via register).
const SecondsField = ((
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  { label, ...rest }: { label: string } & any,
) => (
  <Field>
    <FieldLabel className="text-muted-foreground font-mono text-[10px] font-bold tracking-[0.12em] uppercase">
      {label}
    </FieldLabel>
    <Input
      {...rest}
      type="number"
      min={0}
      className="border-border bg-background h-9 font-mono text-sm"
    />
  </Field>
)) as (props: { label: string; name: string }) => React.ReactElement

// Helpers
type FormData = z.output<typeof formSchema>
function hasScaleUpFields(d: FormData): boolean {
  return (
    d.scaleUpMode !== undefined ||
    d.cooldownSeconds !== undefined ||
    d.idleThresholdSeconds !== undefined ||
    d.saturationCooldownSeconds !== undefined
  )
}
function hasScaleDownFields(d: FormData): boolean {
  return (
    d.idleTimeoutSeconds !== undefined ||
    d.stabilizationSeconds !== undefined ||
    d.protectionWindowSeconds !== undefined
  )
}
