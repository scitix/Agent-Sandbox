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

import { z } from "zod"
import type { components } from "@/lib/api/schema"
import { Controller, useWatch } from "react-hook-form"
import type { Control, UseFormRegister, UseFormSetValue, FieldErrors } from "react-hook-form"
import { TrendingUp } from "lucide-react"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion"
import { Switch } from "@/components/ui/switch"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { useTranslation } from "@/lib/i18n"

// ─── Schema ──────────────────────────────────────────────────────────────────
// optionalSeconds: empty string → undefined (use server default), or a non-negative integer
const optionalSeconds = z
  .union([z.literal(""), z.coerce.number().int().min(0)])
  .optional()
  .transform((v) => (v === "" ? undefined : (v as number)))

export const autoscalingFieldsSchema = {
  autoscalingEnabled: z.boolean().optional(),
  minReplicas: z
    .union([z.literal(""), z.coerce.number().int().min(0)])
    .optional()
    .transform((v) => (v === "" ? undefined : (v as number))),
  maxReplicas: z
    .union([z.literal(""), z.coerce.number().int().min(0)])
    .optional()
    .transform((v) => (v === "" ? undefined : (v as number))),
  scaleUpMode: z.enum(["Conservative", "Default", "Aggressive"]).optional(),
  cooldownSeconds: optionalSeconds,
  idleThresholdSeconds: optionalSeconds,
  idleTimeoutSeconds: optionalSeconds,
  stabilizationSeconds: optionalSeconds,
  protectionWindowSeconds: optionalSeconds,
}

// The "input" type before transform (what the form controls see)
export type AutoscalingFormInput = {
  autoscalingEnabled?: boolean
  minReplicas?: number | ""
  maxReplicas?: number | ""
  scaleUpMode?: "Conservative" | "Default" | "Aggressive"
  cooldownSeconds?: number | ""
  idleThresholdSeconds?: number | ""
  idleTimeoutSeconds?: number | ""
  stabilizationSeconds?: number | ""
  protectionWindowSeconds?: number | ""
}

// The "output" type after transform (what onSubmit sees)
export type AutoscalingFormFields = {
  autoscalingEnabled?: boolean
  minReplicas?: number
  maxReplicas?: number
  scaleUpMode?: "Conservative" | "Default" | "Aggressive"
  cooldownSeconds?: number
  idleThresholdSeconds?: number
  idleTimeoutSeconds?: number
  stabilizationSeconds?: number
  protectionWindowSeconds?: number
}

// ─── Helper ──────────────────────────────────────────────────────────────────

type PoolAutoscalingSpec = components["schemas"]["PoolAutoscalingSpec"]

// Accept either input (pre-transform, with "" strings) or output (post-transform, with numbers) type
export function buildAutoscalingSpec(
  data: AutoscalingFormInput | AutoscalingFormFields,
): PoolAutoscalingSpec {
  const toNum = (v: number | "" | undefined): number | undefined =>
    v === "" || v == null ? undefined : (v as number)

  const autoscalingEnabled = data.autoscalingEnabled
  const scaleUpMode = data.scaleUpMode
  const cooldownSeconds = toNum(data.cooldownSeconds)
  const idleThresholdSeconds = toNum(data.idleThresholdSeconds)
  const idleTimeoutSeconds = toNum(data.idleTimeoutSeconds)
  const stabilizationSeconds = toNum(data.stabilizationSeconds)
  const protectionWindowSeconds = toNum(data.protectionWindowSeconds)

  if (!autoscalingEnabled) return { enabled: false }

  const scaleUpPolicy =
    scaleUpMode != null || cooldownSeconds != null || idleThresholdSeconds != null
      ? {
        mode: (scaleUpMode ?? "Default") as "Conservative" | "Default" | "Aggressive",
        // API schema requires these as numbers; fall back to server-default-friendly 0 only if
        // the field was left empty. The server treats 0 the same as "use default".
        cooldownSeconds: cooldownSeconds ?? 0,
        idleThresholdSeconds: idleThresholdSeconds ?? 0,
      }
      : undefined

  const scaleDownPolicy =
    idleTimeoutSeconds != null || stabilizationSeconds != null || protectionWindowSeconds != null
      ? {
        idleTimeoutSeconds: idleTimeoutSeconds ?? 0,
        stabilizationSeconds: stabilizationSeconds ?? 0,
        protectionWindowSeconds: protectionWindowSeconds ?? 0,
      }
      : undefined

  return {
    enabled: true,
    scaleUpPolicy,
    scaleDownPolicy,
  }
}

// ─── Component ───────────────────────────────────────────────────────────────

export interface AutoscalingFormSectionProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  control: Control<any>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  register: UseFormRegister<any>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  errors: FieldErrors<any>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  setValue: UseFormSetValue<any>
  /** Open the accordion by default (true when editing a pool with autoscaling already enabled) */
  defaultOpen?: boolean
}

export function AutoscalingFormSection({
  control,
  register,
  errors,
  setValue,
  defaultOpen,
}: AutoscalingFormSectionProps) {
  const { t } = useTranslation()
  const watchedAutoscalingEnabled = useWatch({ control, name: "autoscalingEnabled" })
  const watchedMinReplicas = useWatch({ control, name: "minReplicas" })
  const watchedMaxReplicas = useWatch({ control, name: "maxReplicas" })
  const watchedScaleUpMode = useWatch({ control, name: "scaleUpMode" })

  return (
    <div className="border-border rounded border">
      <Accordion defaultValue={defaultOpen ? ["autoscaling"] : []}>
        <AccordionItem value="autoscaling">
          <AccordionTrigger className="px-3 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <TrendingUp className="text-muted-foreground h-3.5 w-3.5" />
              <span className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("pools.form.autoscaling")}
              </span>
              {watchedAutoscalingEnabled && (
                <span className="bg-primary/10 text-primary rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold">
                  ON
                </span>
              )}
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-3">
            <div className="flex flex-col gap-4 pb-2">
              {/* Enable toggle */}
              <div className="flex items-center justify-between gap-3">
                <div className="flex flex-col gap-0.5">
                  <span className="text-foreground font-mono text-xs font-semibold">
                    {t("pools.form.autoscalingEnabled")}
                  </span>
                  <span className="text-muted-foreground font-mono text-[11px]">
                    {t("pools.form.autoscalingEnabledDesc")}
                  </span>
                </div>
                <Controller
                  control={control}
                  name="autoscalingEnabled"
                  render={({ field }) => (
                    <Switch
                      checked={field.value ?? false}
                      onCheckedChange={(checked) => {
                        field.onChange(checked)
                        if (checked) {
                          if (watchedMinReplicas === "" || watchedMinReplicas == null)
                            setValue("minReplicas", 0)
                          if (watchedMaxReplicas === "" || watchedMaxReplicas == null)
                            setValue("maxReplicas", 10)
                          if (!watchedScaleUpMode) setValue("scaleUpMode", "Default")
                        }
                      }}
                    />
                  )}
                />
              </div>

              {watchedAutoscalingEnabled && (
                <>
                  {/* Min / Max Replicas */}
                  <div className="grid grid-cols-2 gap-3">
                    <Field data-invalid={!!errors.minReplicas}>
                      <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                        {t("pools.form.minReplicas")}
                      </FieldLabel>
                      <Input
                        {...register("minReplicas")}
                        type="number"
                        min={0}
                        placeholder="0"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      <FieldError errors={[errors.minReplicas]} className="font-mono text-xs" />
                      <FieldDescription className="text-[11px]">
                        {t("pools.form.minReplicasDesc")}
                      </FieldDescription>
                    </Field>
                    <Field data-invalid={!!errors.maxReplicas}>
                      <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                        {t("pools.form.maxReplicas")}
                      </FieldLabel>
                      <Input
                        {...register("maxReplicas")}
                        type="number"
                        min={0}
                        placeholder="10"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      <FieldError errors={[errors.maxReplicas]} className="font-mono text-xs" />
                      <FieldDescription className="text-[11px]">
                        {t("pools.form.maxReplicasDesc")}
                      </FieldDescription>
                    </Field>
                  </div>

                  {/* Scale-Up Policy sub-accordion */}
                  <div className="border-border rounded border">
                    <Accordion>
                      <AccordionItem value="scaleup">
                        <AccordionTrigger className="px-3 py-2 hover:no-underline">
                          <span className="text-muted-foreground font-mono text-[11px] font-bold tracking-[0.1em] uppercase">
                            {t("pools.form.scaleUpPolicy")}
                          </span>
                        </AccordionTrigger>
                        <AccordionContent className="px-3">
                          <div className="flex flex-col gap-3 pb-2">
                            <Field>
                              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                {t("pools.form.scaleUpMode")}
                              </FieldLabel>
                              <Controller
                                control={control}
                                name="scaleUpMode"
                                render={({ field }) => (
                                  <Select
                                    value={field.value ?? "Default"}
                                    onValueChange={(val) => field.onChange(val)}
                                  >
                                    <SelectTrigger className="h-9 w-full font-mono text-sm">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectItem value="Conservative">Conservative</SelectItem>
                                      <SelectItem value="Default">Default</SelectItem>
                                      <SelectItem value="Aggressive">Aggressive</SelectItem>
                                    </SelectContent>
                                  </Select>
                                )}
                              />
                              <FieldDescription className="text-[11px]">
                                {t("pools.form.scaleUpModeDesc")}
                              </FieldDescription>
                            </Field>
                            <div className="grid grid-cols-2 gap-3">
                              <Field data-invalid={!!errors.cooldownSeconds}>
                                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                  {t("pools.form.cooldownSeconds")}
                                </FieldLabel>
                                <Input
                                  {...register("cooldownSeconds")}
                                  type="number"
                                  min={0}
                                  placeholder="30"
                                  className="border-border bg-background h-9 font-mono text-sm"
                                />
                                <FieldError
                                  errors={[errors.cooldownSeconds]}
                                  className="font-mono text-xs"
                                />
                                <FieldDescription className="text-[11px]">
                                  {t("pools.form.cooldownSecondsDesc")}
                                  {" · "}
                                  {t("pools.form.serverDefault")}
                                </FieldDescription>
                              </Field>
                              <Field data-invalid={!!errors.idleThresholdSeconds}>
                                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                  {t("pools.form.idleThresholdSeconds")}
                                </FieldLabel>
                                <Input
                                  {...register("idleThresholdSeconds")}
                                  type="number"
                                  min={0}
                                  placeholder="30"
                                  className="border-border bg-background h-9 font-mono text-sm"
                                />
                                <FieldError
                                  errors={[errors.idleThresholdSeconds]}
                                  className="font-mono text-xs"
                                />
                                <FieldDescription className="text-[11px]">
                                  {t("pools.form.idleThresholdSecondsDesc")}
                                  {" · "}
                                  {t("pools.form.serverDefault")}
                                </FieldDescription>
                              </Field>
                            </div>
                          </div>
                        </AccordionContent>
                      </AccordionItem>
                    </Accordion>
                  </div>

                  {/* Scale-Down Policy sub-accordion */}
                  <div className="border-border rounded border">
                    <Accordion>
                      <AccordionItem value="scaledown">
                        <AccordionTrigger className="px-3 py-2 hover:no-underline">
                          <span className="text-muted-foreground font-mono text-[11px] font-bold tracking-[0.1em] uppercase">
                            {t("pools.form.scaleDownPolicy")}
                          </span>
                        </AccordionTrigger>
                        <AccordionContent className="px-3">
                          <div className="flex flex-col gap-3 pb-2">
                            <div className="grid grid-cols-2 gap-3">
                              <Field data-invalid={!!errors.idleTimeoutSeconds}>
                                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                  {t("pools.form.idleTimeoutSeconds")}
                                </FieldLabel>
                                <Input
                                  {...register("idleTimeoutSeconds")}
                                  type="number"
                                  min={0}
                                  placeholder="300"
                                  className="border-border bg-background h-9 font-mono text-sm"
                                />
                                <FieldError
                                  errors={[errors.idleTimeoutSeconds]}
                                  className="font-mono text-xs"
                                />
                                <FieldDescription className="text-[11px]">
                                  {t("pools.form.idleTimeoutSecondsDesc")}
                                  {" · "}
                                  {t("pools.form.serverDefault")}
                                </FieldDescription>
                              </Field>
                              <Field data-invalid={!!errors.stabilizationSeconds}>
                                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                  {t("pools.form.stabilizationSeconds")}
                                </FieldLabel>
                                <Input
                                  {...register("stabilizationSeconds")}
                                  type="number"
                                  min={0}
                                  placeholder="60"
                                  className="border-border bg-background h-9 font-mono text-sm"
                                />
                                <FieldError
                                  errors={[errors.stabilizationSeconds]}
                                  className="font-mono text-xs"
                                />
                                <FieldDescription className="text-[11px]">
                                  {t("pools.form.stabilizationSecondsDesc")}
                                  {" · "}
                                  {t("pools.form.serverDefault")}
                                </FieldDescription>
                              </Field>
                            </div>
                            <Field data-invalid={!!errors.protectionWindowSeconds}>
                              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                                {t("pools.form.protectionWindowSeconds")}
                              </FieldLabel>
                              <Input
                                {...register("protectionWindowSeconds")}
                                type="number"
                                min={0}
                                placeholder="10"
                                className="border-border bg-background h-9 font-mono text-sm"
                              />
                              <FieldError
                                errors={[errors.protectionWindowSeconds]}
                                className="font-mono text-xs"
                              />
                              <FieldDescription className="text-[11px]">
                                {t("pools.form.protectionWindowSecondsDesc")}
                                {" · "}
                                {t("pools.form.serverDefault")}
                              </FieldDescription>
                            </Field>
                          </div>
                        </AccordionContent>
                      </AccordionItem>
                    </Accordion>
                  </div>
                </>
              )}
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  )
}
