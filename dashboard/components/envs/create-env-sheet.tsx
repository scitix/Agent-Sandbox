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
import { Controller, useFieldArray, useForm } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { toast } from "sonner"
import { Plus, Save, Trash2 } from "lucide-react"

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
import type {
  AgentSandboxEnv,
  AgentSandboxTemplateSummary,
  QuotaItem,
} from "@/lib/api/client"
import { templatesQueryOptions, quotasQueryOptions } from "@/lib/queries"
import { useCreateEnv, useUpdateEnv } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"

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

// Member row: name + replicas + optional InlineResources + quota selection.
const memberSchema = z.object({
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
  cpuRequest: z.preprocess(emptyToUndef, z.string().optional()),
  cpuLimit: z.preprocess(emptyToUndef, z.string().optional()),
  memoryRequest: z.preprocess(emptyToUndef, z.string().optional()),
  memoryLimit: z.preprocess(emptyToUndef, z.string().optional()),
})

// imagePullSecret registry row — write-only credentials. Empty rows are
// dropped before submit; users add rows to provide creds and remove them
// to leave the existing Secret unchanged.
const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

const formSchema = z.object({
  name: z
    .string()
    .min(1, "envs.form.errors.nameRequired")
    .max(63)
    .regex(dnsLabel, "envs.form.errors.nameDnsLabel"),
  templateName: z.string().min(1, "envs.form.errors.templateRequired"),
  // Env-level overrides — all optional.
  image: z.preprocess(emptyToUndef, z.string().optional()),
  podCreationImagePolicy: z.enum(["PoolDefaultImage", "IdleImage"]).optional(),
  imagePullSecretRows: z.array(registryRowSchema),
  defaultStartupTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  defaultIdleTimeout: z.preprocess(emptyToUndef, z.string().optional()),
  members: z.array(memberSchema),
})

type FormValues = z.infer<typeof formSchema>

interface MemberInput {
  name: string
  quotaUrl?: string
  replicas?: number
  cpuRequest?: string
  cpuLimit?: string
  memoryRequest?: string
  memoryLimit?: string
}

// ─── Sheet shell ─────────────────────────────────────────────────────────────

export function CreateEnvSheet({ env, open, onOpenChange }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        {open && <CreateEnvInner env={env ?? null} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

// ─── Inner form ──────────────────────────────────────────────────────────────

interface InnerProps {
  env: AgentSandboxEnv | null
  onClose: () => void
}

function CreateEnvInner({ env, onClose }: InnerProps) {
  const { t } = useTranslation()
  const isEdit = !!env

  const { data: templates = [] } = useQuery(templatesQueryOptions())
  const { data: quotas = [] } = useQuery(quotasQueryOptions())

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

          {/* ImagePullSecret */}
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
                onClick={() => append(emptyMember(env?.name ?? "", fields.length))}
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
                  onRemove={() => remove(index)}
                />
              ))}
            </div>
          </section>
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
  onRemove: () => void
}

function MemberRow({ index, control, register, errors, quotas, onRemove }: MemberRowProps) {
  const { t } = useTranslation()
  const memberErrors = errors.members?.[index]

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

      <div className="grid grid-cols-5 gap-2">
        <Field className="col-span-1">
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
        <Field>
          <FieldLabel className="text-[11px]">{t("envs.form.member.cpuRequest")}</FieldLabel>
          <Input
            placeholder="500m"
            {...register(`members.${index}.cpuRequest` as const)}
          />
        </Field>
        <Field>
          <FieldLabel className="text-[11px]">{t("envs.form.member.cpuLimit")}</FieldLabel>
          <Input
            placeholder="1"
            {...register(`members.${index}.cpuLimit` as const)}
          />
        </Field>
        <Field>
          <FieldLabel className="text-[11px]">{t("envs.form.member.memoryRequest")}</FieldLabel>
          <Input
            placeholder="1Gi"
            {...register(`members.${index}.memoryRequest` as const)}
          />
        </Field>
        <Field>
          <FieldLabel className="text-[11px]">{t("envs.form.member.memoryLimit")}</FieldLabel>
          <Input
            placeholder="2Gi"
            {...register(`members.${index}.memoryLimit` as const)}
          />
        </Field>
      </div>
    </div>
  )
}

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

// ─── Form ↔ API mapping ──────────────────────────────────────────────────────

// ─── ImagePullSecret section ────────────────────────────────────────────────

interface ImagePullSecretSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
}

/**
 * Write-only ImagePullSecret editor. Each row collects one registry's
 * credentials. Empty rows are filtered out on submit so a fully-blank
 * section means "leave the existing ips-{envName} Secret untouched".
 * Submitting any non-empty rows upserts the Secret server-side.
 */
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
      <p className="text-xs text-muted-foreground">
        {t("envs.form.imagePullSecret.hint")}
      </p>
      {fields.length === 0 && (
        <p className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
          {t("envs.form.imagePullSecret.empty")}
        </p>
      )}
      <div className="space-y-2">
        {fields.map((field, index) => (
          <div key={field.id} className="flex items-start gap-2 rounded-md border bg-muted/30 p-2.5">
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

function envToFormValues(env: AgentSandboxEnv | null): FormValues {
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
    }
  }
  const overrides = env.spec.overrides
  const members = pickLocalMembers(env).map<MemberInput>((m) => ({
    name: m.name,
    quotaUrl: m.labels?.[QUOTA_URL_LABEL],
    replicas: m.replicas,
    cpuRequest: m.inlineResources?.requests?.cpu,
    cpuLimit: m.inlineResources?.limits?.cpu,
    memoryRequest: m.inlineResources?.requests?.memory,
    memoryLimit: m.inlineResources?.limits?.memory,
  }))
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    members,
    // ImagePullSecret credentials are write-only on the wire; never
    // hydrated from the server. The user adds rows to set/replace, leaves
    // empty to keep the existing Secret.
    imagePullSecretRows: [],
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
  }
}

function formValuesToUpdateBody(v: FormValues) {
  return {
    members: v.members.map(memberInputToWire),
    overrides: buildOverrides(v),
  }
}

function buildOverrides(v: FormValues) {
  const o: Record<string, unknown> = {}
  if (v.image) o.image = v.image
  if (v.podCreationImagePolicy) o.podCreationImagePolicy = v.podCreationImagePolicy
  if (v.defaultStartupTimeout) o.defaultStartupTimeout = v.defaultStartupTimeout
  if (v.defaultIdleTimeout) o.defaultIdleTimeout = v.defaultIdleTimeout
  // ImagePullSecret: write-only. Drop empty rows so a fully-blank form
  // means "leave the existing Secret untouched" instead of "delete it".
  const registries = v.imagePullSecretRows
    .filter((r) => r.registry && r.username && r.password)
    .map((r) => ({ registry: r.registry!, username: r.username!, password: r.password! }))
  if (registries.length > 0) {
    o.imagePullSecret = { registries }
  }
  return Object.keys(o).length ? o : undefined
}

function memberInputToWire(m: MemberInput) {
  const labels = m.quotaUrl ? { [QUOTA_URL_LABEL]: m.quotaUrl } : undefined
  const requests: Record<string, string> = {}
  const limits: Record<string, string> = {}
  if (m.cpuRequest) requests.cpu = m.cpuRequest
  if (m.memoryRequest) requests.memory = m.memoryRequest
  if (m.cpuLimit) limits.cpu = m.cpuLimit
  if (m.memoryLimit) limits.memory = m.memoryLimit
  const inlineResources =
    Object.keys(requests).length || Object.keys(limits).length
      ? {
          ...(Object.keys(requests).length ? { requests } : {}),
          ...(Object.keys(limits).length ? { limits } : {}),
        }
      : undefined
  return {
    name: m.name,
    ...(labels ? { labels } : {}),
    ...(typeof m.replicas === "number" ? { replicas: m.replicas } : {}),
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

function emptyMember(envName: string, index: number): MemberInput {
  const base = envName || "member"
  return {
    name: `${base}-${index + 1}`,
    quotaUrl: undefined,
    replicas: 1,
    cpuRequest: undefined,
    cpuLimit: undefined,
    memoryRequest: undefined,
    memoryLimit: undefined,
  }
}

