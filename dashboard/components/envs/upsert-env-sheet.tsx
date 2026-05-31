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
import { Plus, Save, Trash2 } from "lucide-react"

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
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
import type { AgentSandboxEnv, AgentSandboxTemplateSummary } from "@/lib/api/client"
import {
  envQueryOptions,
  templatesQueryOptions,
  useCreateEnv,
  useUpdateEnv,
} from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

interface Props {
  /**
   * Name of the env to edit. Omitted/null opens the sheet in create mode.
   * The full SandboxEnv is refetched via GET so the form never relies on a
   * possibly-stale list-row projection.
   */
  envName?: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

// ─── Form schema ─────────────────────────────────────────────────────────────
//
// The env-creation form holds only env-level identity + overrides. Member
// SandboxPools (with their resource choice + replica counts) are created
// afterwards from the Env detail page via /v1/envs/{name}/sandboxpools.
// Autoscaling lives at the env level but is edited per-pool (one
// scaling-group entry at a time) from the pool table — not from this sheet.

const emptyToUndef = (val: unknown) =>
  typeof val === "string" && val.trim() === "" ? undefined : val

const dnsLabel = /^[a-z]([a-z0-9-]*[a-z0-9])?$/

const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

const formSchema = z.object({
  name: z
    .string()
    .min(1, "envs.form.errors.nameRequired")
    .max(24, "envs.form.errors.nameTooLong")
    .regex(dnsLabel, "envs.form.errors.nameDnsLabel"),
  templateName: z.string().min(1, "envs.form.errors.templateRequired"),
  image: z.preprocess(emptyToUndef, z.string().optional()),
  podCreationImagePolicy: z.enum(["PoolDefaultImage", "IdleImage"]).optional(),
  imagePullSecretRows: z.array(registryRowSchema),
  defaultStartupTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  defaultIdleTimeout: z.preprocess(emptyToUndef, z.string().optional()),
})

type FormValues = z.infer<typeof formSchema>

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function UpsertEnvSheet({ envName, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        {open && (
          <UpsertEnvLoader envName={envName ?? null} onClose={() => onOpenChange(false)} />
        )}
      </SheetContent>
    </Sheet>
  )
}

// Resolves edit mode to a full SandboxEnv (refetched via GET) before mounting
// the form, so the form's defaultValues are built from authoritative state
// rather than the trimmed list-row summary. Create mode mounts immediately.
function UpsertEnvLoader({ envName, onClose }: { envName: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    ...envQueryOptions(envName ?? ""),
    enabled: !!envName,
  })

  if (envName && isLoading) {
    return (
      <>
        <SheetHeader className="border-b px-6 py-4">
          <SheetTitle>{t("envs.form.editTitle")}</SheetTitle>
        </SheetHeader>
        <div className="text-muted-foreground flex-1 px-6 py-8 text-sm">{t("common.loading")}</div>
      </>
    )
  }

  return <UpsertEnvForm env={envName ? (data?.env ?? null) : null} onClose={onClose} />
}

interface InnerProps {
  env: AgentSandboxEnv | null
  onClose: () => void
}

function UpsertEnvForm({ env, onClose }: InnerProps) {
  const { t } = useTranslation()
  const isEdit = !!env

  const { data: templates = [] } = useQuery(templatesQueryOptions())

  const defaultValues = useMemo<FormValues>(() => envToFormValues(env), [env])

  const {
    control,
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues,
  })

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
        <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
          {/* Basics — always visible */}
          <section className="space-y-4">
            <Field>
              <FieldLabel htmlFor="env-name">{t("envs.form.name")}</FieldLabel>
              <Input
                id="env-name"
                disabled={isEdit}
                {...register("name")}
                placeholder="my-env"
                maxLength={24}
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
              <SelectedTemplateInfo templates={templates} control={control} />
            </Field>
          </section>

          {/* Advanced — collapsed by default */}
          <div className="border-border rounded-md border">
            <Accordion>
              <AccordionItem value="advanced">
                <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                  {t("common.advanced")}
                </AccordionTrigger>
                <AccordionContent className="px-3">
                  <div className="flex flex-col gap-5 pb-2">
                    {/* Env-level overrides */}
                    <section className="space-y-3">
                      <div>
                        <h4 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
                          {t("envs.form.section.overrides")}
                        </h4>
                        <p className="text-muted-foreground mt-1 text-xs">
                          {t("envs.form.overridesHint")}
                        </p>
                      </div>

                      <Field>
                        <FieldLabel htmlFor="env-image">{t("envs.form.image")}</FieldLabel>
                        <Input
                          id="env-image"
                          {...register("image")}
                          placeholder="ghcr.io/org/runtime:1.2"
                          className="font-mono text-sm"
                        />
                        <FieldDescription>{t("envs.form.imageDescription")}</FieldDescription>
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
                        <FieldDescription>
                          {t("envs.form.podCreationImagePolicyDescription")}
                        </FieldDescription>
                      </Field>

                      <div className="grid grid-cols-2 gap-3">
                        <Field>
                          <FieldLabel htmlFor="env-startup">
                            {t("envs.form.defaultStartupTimeout")}
                          </FieldLabel>
                          <Input
                            id="env-startup"
                            placeholder="5m"
                            {...register("defaultStartupTimeout")}
                          />
                          <FieldDescription>
                            {t("envs.form.defaultStartupTimeoutDescription")}
                          </FieldDescription>
                        </Field>
                        <Field>
                          <FieldLabel htmlFor="env-idle">
                            {t("envs.form.defaultIdleTimeout")}
                          </FieldLabel>
                          <Input
                            id="env-idle"
                            placeholder="30m"
                            {...register("defaultIdleTimeout")}
                          />
                          <FieldDescription>
                            {t("envs.form.defaultIdleTimeoutDescription")}
                          </FieldDescription>
                        </Field>
                      </div>
                    </section>

                    <Separator />

                    <ImagePullSecretSection control={control} register={register} />
                  </div>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
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

// ─── Combobox ────────────────────────────────────────────────────────────────

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
      <ComboboxInput aria-invalid={invalid} placeholder="select template" />
      <ComboboxContent>
        <ComboboxList>
          {(item: AgentSandboxTemplateSummary) => (
            <ComboboxItem key={item.name} value={item}>
              <div className="flex w-full min-w-0 flex-col gap-0.5">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs">{item.name}</span>
                  {item.version && (
                    <span className="border-border bg-muted text-muted-foreground rounded border px-1 font-mono text-[10px]">
                      {item.version}
                    </span>
                  )}
                </div>
                {item.description && (
                  <span className="text-muted-foreground truncate text-[10px]">
                    {item.description}
                  </span>
                )}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty />
      </ComboboxContent>
    </Combobox>
  )
}

function SelectedTemplateInfo({
  templates,
  control,
}: {
  templates: AgentSandboxTemplateSummary[]
  control: ReturnType<typeof useForm<FormValues>>["control"]
}) {
  const { t } = useTranslation()
  const name = useWatch({ control, name: "templateName" })
  const tpl = templates.find((it) => it.name === name)
  if (!tpl) {
    return <FieldDescription>{t("envs.form.templateDescription")}</FieldDescription>
  }
  return (
    <div className="border-border bg-muted/30 mt-1 space-y-1 rounded border border-dashed px-2 py-1.5">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px]">
        <span className="text-foreground">{tpl.name}</span>
        {tpl.version && (
          <span className="border-border bg-background text-muted-foreground rounded border px-1 text-[10px]">
            {tpl.version}
          </span>
        )}
        {(tpl.cpu || tpl.memory) && (
          <span className="text-muted-foreground text-[10px]">
            {[tpl.cpu && `${tpl.cpu}c`, tpl.memory].filter(Boolean).join(" / ")}
          </span>
        )}
      </div>
      {tpl.description ? (
        <p className="text-muted-foreground text-[11px] leading-snug">{tpl.description}</p>
      ) : (
        <p className="text-muted-foreground text-[11px] italic">
          {t("envs.form.templateNoDescription")}
        </p>
      )}
    </div>
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
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
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
      <p className="text-muted-foreground text-xs">{t("envs.form.imagePullSecret.hint")}</p>
      {fields.length === 0 && (
        <p className="text-muted-foreground rounded-md border border-dashed px-3 py-3 text-center text-xs">
          {t("envs.form.imagePullSecret.empty")}
        </p>
      )}
      <div className="space-y-2">
        {fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
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
              className="text-destructive mt-5"
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

function envToFormValues(env: AgentSandboxEnv | null): FormValues {
  if (!env) {
    return {
      name: "",
      templateName: "",
      image: undefined,
      podCreationImagePolicy: "IdleImage",
      defaultStartupTimeout: undefined,
      defaultIdleTimeout: undefined,
      imagePullSecretRows: [],
    }
  }
  const overrides = env.spec.overrides
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    imagePullSecretRows: [],
  }
}

function formValuesToCreateBody(v: FormValues) {
  return {
    name: v.name,
    mode: "WarmPool" as const,
    templateRef: { name: v.templateName },
    overrides: buildOverrides(v),
  }
}

function formValuesToUpdateBody(v: FormValues) {
  return {
    overrides: buildOverrides(v),
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

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
