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
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form"
import { useQuery, useQueries } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
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
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import type { AgentSandboxEnv, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { getApiClient } from "@/lib/api/client"
import { clustersAtom } from "@/lib/atoms"
import { useClusterID } from "@/hooks/use-cluster-id"
import { envQueryOptions, templatesQueryOptions, useCreateEnv, useUpdateEnv } from "@/lib/queries"
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

const injectionCredentialRowSchema = z.object({
  name: z.string().optional(),
  secretName: z.string().optional(),
  secretKey: z.string().optional(),
  exposeAs: z.string().optional(),
  placeholder: z.string().optional(),
})

const injectionRuleRowSchema = z.object({
  host: z.string().optional(),
  headerName: z.string().optional(),
  headerValue: z.string().optional(),
  mode: z.enum(["Override", "IfAbsent"]).optional(),
  substitute: z.string().optional(),
  pathPrefixes: z.string().optional(),
})

const registryRowSchema = z.object({
  registry: z.preprocess(emptyToUndef, z.string().optional()),
  username: z.preprocess(emptyToUndef, z.string().optional()),
  password: z.preprocess(emptyToUndef, z.string().optional()),
})

// splitLines turns a textarea value into a trimmed, de-duplicated list, split on
// newlines or commas. Shared by validation, payload assembly, and read-back.
function splitLines(s?: string): string[] {
  const seen = new Set<string>()
  return (s ?? "")
    .split(/[\n,]+/)
    .map((x) => x.trim())
    .filter((x) => x !== "" && !seen.has(x) && seen.add(x) !== undefined)
}

const domainRe = /^(\*|\*\.([a-z0-9-]+\.)+[a-z]{2,}|([a-z0-9-]+\.)+[a-z]{2,})$/i

function isCIDRorIP(s: string): boolean {
  if (s.includes(":")) return true // IPv6 (incl. CIDR) — accept loosely
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(\/(\d{1,2}))?$/.exec(s)
  if (!m) return false
  if (m[1]! > "255" || [m[1], m[2], m[3], m[4]].some((o) => Number(o) > 255)) return false
  if (m[6] !== undefined && Number(m[6]) > 32) return false
  return true
}

const formSchema = z
  .object({
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
    // Network policy: a 3-way mode gates the egress rules.
    networkPolicyMode: z.enum(["unrestricted", "disable", "allowlist"]),
    allowedDomains: z.preprocess(emptyToUndef, z.string().optional()),
    allowedCIDRs: z.preprocess(emptyToUndef, z.string().optional()),
    deniedCIDRs: z.preprocess(emptyToUndef, z.string().optional()),
    allowPrivateNetworks: z.boolean(),
    // Credential injection: the sidecar adds these headers on the way out, so
    // the sandbox can use a credential it can never read.
    injectionCredentialRows: z.array(injectionCredentialRowSchema),
    injectionRuleRows: z.array(injectionRuleRowSchema),
    // Auto-update rollout policy (Env-level default; per-member override lives
    // on the pool sheet). maxUnavailable is a free-form int-or-percent string.
    autoUpdate: z.boolean(),
    maxUnavailable: z.preprocess(emptyToUndef, z.string().optional()),
  })
  .superRefine((v, ctx) => {
    validateInjection(v, ctx)
    if (v.networkPolicyMode !== "allowlist") return
    for (const d of splitLines(v.allowedDomains)) {
      if (!domainRe.test(d)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["allowedDomains"],
          message: "envs.form.errors.invalidDomain",
        })
        break
      }
    }
    for (const key of ["allowedCIDRs", "deniedCIDRs"] as const) {
      for (const c of splitLines(v[key])) {
        if (!isCIDRorIP(c)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: [key],
            message: "envs.form.errors.invalidCidr",
          })
          break
        }
      }
    }
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
        {open && <UpsertEnvLoader envName={envName ?? null} onClose={() => onOpenChange(false)} />}
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
    setValue,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues,
  })

  const createMutation = useCreateEnv()
  const updateMutation = useUpdateEnv()

  // Create mode. "new" builds a fresh Env; "extend" pins the name to an Env
  // that already exists in another cluster — creating a same-named Env here
  // joins its cross-cluster federation. The extend picker fans GET /envs out
  // across every other cluster.
  const [createMode, setCreateMode] = useState<"new" | "extend">("new")
  const [extendSel, setExtendSel] = useState<{
    clusterID: string
    clusterName: string
    name: string
  } | null>(null)
  const clustersData = useAtomValue(clustersAtom)
  const currentCluster = useClusterID()
  const otherClusters = useMemo(
    () => (clustersData?.clusters ?? []).filter((c) => c.id && c.id !== currentCluster),
    [clustersData, currentCluster],
  )
  const otherEnvQueries = useQueries({
    queries: otherClusters.map((c) => ({
      ...getApiClient(c.id).queryOptions("get", "/envs", undefined, {
        select: (d: { items?: { name: string }[] }) => d.items ?? [],
      }),
      enabled: !isEdit && createMode === "extend",
    })),
  })
  const otherEnvs = useMemo(
    () =>
      otherClusters.flatMap((c, i) =>
        ((otherEnvQueries[i]?.data as { name: string }[] | undefined) ?? []).map((e) => ({
          clusterID: c.id,
          clusterName: c.name ?? c.id,
          name: e.name,
        })),
      ),
    [otherClusters, otherEnvQueries],
  )

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
            {!isEdit && otherClusters.length > 0 && (
              <Tabs
                value={createMode}
                onValueChange={(v) => {
                  setCreateMode(v as "new" | "extend")
                  setExtendSel(null)
                  setValue("name", "", { shouldValidate: false })
                }}
              >
                <TabsList className="w-full">
                  <TabsTrigger value="new" className="flex-1 text-xs">
                    {t("envs.form.tab.new")}
                  </TabsTrigger>
                  <TabsTrigger value="extend" className="flex-1 text-xs">
                    {t("envs.form.tab.extend")}
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            )}
            <Field>
              <FieldLabel htmlFor="env-name">{t("envs.form.name")}</FieldLabel>
              {!isEdit && createMode === "extend" ? (
                <Combobox
                  autoHighlight
                  value={extendSel}
                  onValueChange={(
                    v: { clusterID: string; clusterName: string; name: string } | null,
                  ) => {
                    setExtendSel(v)
                    setValue("name", v?.name ?? "", { shouldValidate: true })
                  }}
                  items={otherEnvs}
                  itemToStringLabel={(e: {
                    clusterID: string
                    clusterName: string
                    name: string
                  }) => e.name}
                >
                  <ComboboxInput
                    aria-invalid={!!errors.name}
                    placeholder={t("envs.form.extendPlaceholder")}
                    className="h-9 font-mono text-sm"
                  />
                  <ComboboxContent>
                    <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
                    <ComboboxList>
                      {(e: { clusterID: string; clusterName: string; name: string }) => (
                        <ComboboxItem key={`${e.clusterID}/${e.name}`} value={e}>
                          <span className="font-mono text-sm">{e.name}</span>
                          <span className="text-muted-foreground ml-2 text-xs">
                            {e.clusterName}
                          </span>
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              ) : (
                <Input
                  id="env-name"
                  disabled={isEdit}
                  {...register("name")}
                  placeholder="my-env"
                  maxLength={24}
                />
              )}
              {errors.name && <FieldError>{t(errors.name.message as never)}</FieldError>}
              <FieldDescription>
                {createMode === "extend"
                  ? t("envs.form.extendDescription")
                  : t("envs.form.nameDescription")}
              </FieldDescription>
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

                    <Separator />

                    <NetworkPolicySection control={control} register={register} errors={errors} />
                    <SecretInjectionSection control={control} register={register} errors={errors} />

                    <Separator />

                    <UpdateStrategySection control={control} register={register} />
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

// ─── Credential injection ───────────────────────────────────────────────────

// splitCommas parses a comma-separated free-text field into trimmed entries.
function splitCommas(v: string | undefined): string[] {
  return (v ?? "")
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
}

// buildSecretInjection folds the credential and rule rows into the wire shape.
// Rows are keyed by host so several header rows on one host become one rule
// carrying several headers, matching how the proxy evaluates them.
function buildSecretInjection(v: FormValues): Record<string, unknown> | undefined {
  const credentials = v.injectionCredentialRows
    .filter((c) => c.name && c.secretName && c.secretKey)
    .map((c) => ({
      name: c.name!,
      valueFrom: { name: c.secretName!, key: c.secretKey! },
      ...(c.exposeAs ? { exposeAs: c.exposeAs } : {}),
      ...(c.placeholder ? { placeholder: c.placeholder } : {}),
    }))

  const byHost = new Map<string, Record<string, unknown>>()
  for (const r of v.injectionRuleRows) {
    if (!r.host) continue
    let rule = byHost.get(r.host)
    if (!rule) {
      rule = { host: r.host, headers: [] as Record<string, unknown>[] }
      byHost.set(r.host, rule)
    }
    if (r.headerName && r.headerValue) {
      ;(rule.headers as Record<string, unknown>[]).push({
        name: r.headerName,
        value: r.headerValue,
        ...(r.mode && r.mode !== "Override" ? { mode: r.mode } : {}),
      })
    }
    const sub = splitCommas(r.substitute)
    if (sub.length) rule.substitute = sub
    const paths = splitCommas(r.pathPrefixes)
    if (paths.length) rule.pathPrefixes = paths
  }
  const rules = [...byHost.values()].map((r) => {
    const headers = r.headers as Record<string, unknown>[]
    if (headers.length === 0) delete r.headers
    return r
  })

  if (credentials.length === 0 && rules.length === 0) return undefined
  return { credentials, rules }
}

// validateInjection mirrors the server-side checks that would otherwise only
// surface when a sandbox is created hours later.
function validateInjection(v: FormValues, ctx: z.RefinementCtx): void {
  const names = new Set<string>()
  v.injectionCredentialRows.forEach((c, i) => {
    if (!c.name && !c.secretName && !c.secretKey) return
    if (!c.name || !c.secretName || !c.secretKey) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i],
        message: "envs.form.errors.injectionCredentialIncomplete",
      })
      return
    }
    names.add(c.name)
    if (c.placeholder && c.placeholder.length < 16) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "placeholder"],
        message: "envs.form.errors.placeholderTooShort",
      })
    }
    if (c.placeholder && !c.exposeAs) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionCredentialRows", i, "exposeAs"],
        message: "envs.form.errors.placeholderNeedsExposeAs",
      })
    }
  })

  const allowed = new Set(splitLines(v.allowedDomains))
  v.injectionRuleRows.forEach((r, i) => {
    if (!r.host) return
    if (r.host.includes("*")) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionWildcardHost",
      })
    }
    // A host outside the allowlist is dropped before the L7 path ever runs.
    if (v.networkPolicyMode === "allowlist" && allowed.size > 0 && !allowed.has(r.host)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["injectionRuleRows", i, "host"],
        message: "envs.form.errors.injectionHostNotAllowed",
      })
    }
    if (r.headerValue) {
      const refs = [...r.headerValue.matchAll(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g)].map((m) => m[1])
      if (refs.length === 0) {
        // A literal here would be a plaintext secret stored in the CR.
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["injectionRuleRows", i, "headerValue"],
          message: "envs.form.errors.injectionValueNeedsCredential",
        })
      }
      for (const ref of refs) {
        if (!names.has(ref)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ["injectionRuleRows", i, "headerValue"],
            message: "envs.form.errors.injectionUnknownCredential",
          })
          break
        }
      }
    }
  })
}

interface SecretInjectionSectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
}

function SecretInjectionSection({ control, register, errors }: SecretInjectionSectionProps) {
  const { t } = useTranslation()
  const creds = useFieldArray({ control, name: "injectionCredentialRows" })
  const rules = useFieldArray({ control, name: "injectionRuleRows" })

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.secretInjection")}
        </h3>
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.secretInjection.hint")}</p>
      </div>

      <div className="flex items-center justify-between">
        <FieldLabel className="text-[11px]">
          {t("envs.form.secretInjection.credentials")}
        </FieldLabel>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() =>
            creds.append({ name: "", secretName: "", secretKey: "", exposeAs: "", placeholder: "" })
          }
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.secretInjection.addCredential")}
        </Button>
      </div>
      {creds.fields.length === 0 && (
        <p className="text-muted-foreground rounded-md border border-dashed px-3 py-3 text-center text-xs">
          {t("envs.form.secretInjection.empty")}
        </p>
      )}
      <div className="space-y-2">
        {creds.fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
          >
            <div className="grid flex-1 grid-cols-5 gap-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.credName")}
                </FieldLabel>
                <Input
                  placeholder="openai"
                  {...register(`injectionCredentialRows.${index}.name` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.secretName")}
                </FieldLabel>
                <Input
                  placeholder="my-secrets"
                  {...register(`injectionCredentialRows.${index}.secretName` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.secretKey")}
                </FieldLabel>
                <Input
                  placeholder="api-key"
                  {...register(`injectionCredentialRows.${index}.secretKey` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.exposeAs")}
                </FieldLabel>
                <Input
                  placeholder="OPENAI_API_KEY"
                  {...register(`injectionCredentialRows.${index}.exposeAs` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.placeholder")}
                </FieldLabel>
                <Input
                  placeholder={t("envs.form.secretInjection.placeholderHint")}
                  {...register(`injectionCredentialRows.${index}.placeholder` as const)}
                />
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => creds.remove(index)}
              className="text-destructive mt-5"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>

      <div className="flex items-center justify-between pt-1">
        <FieldLabel className="text-[11px]">{t("envs.form.secretInjection.rules")}</FieldLabel>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() =>
            rules.append({
              host: "",
              headerName: "Authorization",
              headerValue: "",
              mode: "Override",
              substitute: "",
              pathPrefixes: "",
            })
          }
          className="h-7 gap-1 font-mono text-[11px]"
        >
          <Plus className="h-3 w-3" />
          {t("envs.form.secretInjection.addRule")}
        </Button>
      </div>
      <div className="space-y-2">
        {rules.fields.map((field, index) => (
          <div
            key={field.id}
            className="bg-muted/30 flex items-start gap-2 rounded-md border p-2.5"
          >
            <div className="grid flex-1 grid-cols-3 gap-2">
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.host")}
                </FieldLabel>
                <Input
                  placeholder="api.openai.com"
                  {...register(`injectionRuleRows.${index}.host` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.headerName")}
                </FieldLabel>
                <Input
                  placeholder="Authorization"
                  {...register(`injectionRuleRows.${index}.headerName` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.headerValue")}
                </FieldLabel>
                <Input
                  placeholder="Bearer {{ openai }}"
                  {...register(`injectionRuleRows.${index}.headerValue` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.mode")}
                </FieldLabel>
                <Controller
                  control={control}
                  name={`injectionRuleRows.${index}.mode` as const}
                  render={({ field: f }) => (
                    <Select value={f.value ?? "Override"} onValueChange={f.onChange}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="Override">
                          {t("envs.form.secretInjection.modeOverride")}
                        </SelectItem>
                        <SelectItem value="IfAbsent">
                          {t("envs.form.secretInjection.modeIfAbsent")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.substitute")}
                </FieldLabel>
                <Input
                  placeholder="openai"
                  {...register(`injectionRuleRows.${index}.substitute` as const)}
                />
              </Field>
              <Field>
                <FieldLabel className="text-[11px]">
                  {t("envs.form.secretInjection.pathPrefixes")}
                </FieldLabel>
                <Input
                  placeholder="/v1/"
                  {...register(`injectionRuleRows.${index}.pathPrefixes` as const)}
                />
              </Field>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => rules.remove(index)}
              className="text-destructive mt-5"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>
      {(errors.injectionCredentialRows || errors.injectionRuleRows) && (
        <p className="text-destructive text-xs">{t("envs.form.secretInjection.hasErrors")}</p>
      )}
      <p className="text-muted-foreground text-xs">
        {t("envs.form.secretInjection.usableNotReadable")}
      </p>
    </section>
  )
}

// ─── Network policy section ─────────────────────────────────────────────────

interface NetworkPolicySectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
  errors: ReturnType<typeof useForm<FormValues>>["formState"]["errors"]
}

function NetworkPolicySection({ control, register, errors }: NetworkPolicySectionProps) {
  const { t } = useTranslation()
  const mode = useWatch({ control, name: "networkPolicyMode" })
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.networkPolicy")}
        </h3>
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.networkPolicy.hint")}</p>
      </div>

      <Field>
        <FieldLabel>{t("envs.form.networkPolicy.mode")}</FieldLabel>
        <Controller
          control={control}
          name="networkPolicyMode"
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="unrestricted">
                  {t("envs.form.networkPolicy.modeUnrestricted")}
                </SelectItem>
                <SelectItem value="disable">{t("envs.form.networkPolicy.modeDisable")}</SelectItem>
                <SelectItem value="allowlist">
                  {t("envs.form.networkPolicy.modeAllowlist")}
                </SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        <FieldDescription>{t("envs.form.networkPolicy.modeDescription")}</FieldDescription>
      </Field>

      {mode === "allowlist" && (
        <>
          <Field>
            <FieldLabel htmlFor="np-domains">
              {t("envs.form.networkPolicy.allowedDomains")}
            </FieldLabel>
            <Textarea
              id="np-domains"
              rows={3}
              placeholder={"pypi.org\n*.pythonhosted.org"}
              className="font-mono text-sm"
              {...register("allowedDomains")}
            />
            {errors.allowedDomains && (
              <FieldError>{t(errors.allowedDomains.message as never)}</FieldError>
            )}
            <FieldDescription>
              {t("envs.form.networkPolicy.allowedDomainsDescription")}
            </FieldDescription>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="np-allow-cidr">
                {t("envs.form.networkPolicy.allowedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-allow-cidr"
                rows={2}
                placeholder="8.8.8.8/32"
                className="font-mono text-sm"
                {...register("allowedCIDRs")}
              />
              {errors.allowedCIDRs && (
                <FieldError>{t(errors.allowedCIDRs.message as never)}</FieldError>
              )}
            </Field>
            <Field>
              <FieldLabel htmlFor="np-deny-cidr">
                {t("envs.form.networkPolicy.deniedCIDRs")}
              </FieldLabel>
              <Textarea
                id="np-deny-cidr"
                rows={2}
                placeholder="1.2.3.4/32"
                className="font-mono text-sm"
                {...register("deniedCIDRs")}
              />
              {errors.deniedCIDRs && (
                <FieldError>{t(errors.deniedCIDRs.message as never)}</FieldError>
              )}
            </Field>
          </div>
        </>
      )}

      {mode !== "unrestricted" && (
        <Controller
          control={control}
          name="allowPrivateNetworks"
          render={({ field }) => (
            <div className="flex items-center justify-between rounded-md border p-3">
              <div className="space-y-0.5 pr-3">
                <FieldLabel>{t("envs.form.networkPolicy.allowPrivateNetworks")}</FieldLabel>
                <FieldDescription>
                  {t("envs.form.networkPolicy.allowPrivateNetworksDescription")}
                </FieldDescription>
              </div>
              <Switch checked={field.value} onCheckedChange={field.onChange} />
            </div>
          )}
        />
      )}
    </section>
  )
}

interface UpdateStrategySectionProps {
  control: ReturnType<typeof useForm<FormValues>>["control"]
  register: ReturnType<typeof useForm<FormValues>>["register"]
}

function UpdateStrategySection({ control, register }: UpdateStrategySectionProps) {
  const { t } = useTranslation()
  const autoUpdate = useWatch({ control, name: "autoUpdate" })
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
          {t("envs.form.section.updateStrategy")}
        </h3>
        <p className="text-muted-foreground mt-1 text-xs">{t("envs.form.updateStrategy.hint")}</p>
      </div>

      <Controller
        control={control}
        name="autoUpdate"
        render={({ field }) => (
          <div className="flex items-center justify-between rounded-md border p-3">
            <div className="space-y-0.5 pr-3">
              <FieldLabel>{t("envs.form.updateStrategy.autoUpdate")}</FieldLabel>
              <FieldDescription>
                {t("envs.form.updateStrategy.autoUpdateDescription")}
              </FieldDescription>
            </div>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </div>
        )}
      />

      {autoUpdate && (
        <Field>
          <FieldLabel htmlFor="us-max-unavailable">
            {t("envs.form.updateStrategy.maxUnavailable")}
          </FieldLabel>
          <Input id="us-max-unavailable" placeholder="20%" {...register("maxUnavailable")} />
          <FieldDescription>
            {t("envs.form.updateStrategy.maxUnavailableDescription")}
          </FieldDescription>
        </Field>
      )}
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
      networkPolicyMode: "unrestricted",
      allowedDomains: undefined,
      allowedCIDRs: undefined,
      deniedCIDRs: undefined,
      allowPrivateNetworks: false,
      injectionCredentialRows: [],
      injectionRuleRows: [],
      autoUpdate: true,
      maxUnavailable: undefined,
    }
  }
  const overrides = env.spec.overrides
  const np = overrides?.networkPolicy
  const mode: FormValues["networkPolicyMode"] = np?.disableEgress
    ? "disable"
    : np?.egress
      ? "allowlist"
      : "unrestricted"
  return {
    name: env.name,
    templateName: env.spec.templateRef.name,
    image: overrides?.image,
    podCreationImagePolicy: overrides?.podCreationImagePolicy ?? "IdleImage",
    defaultStartupTimeout: overrides?.defaultStartupTimeout,
    defaultIdleTimeout: overrides?.defaultIdleTimeout,
    imagePullSecretRows: [],
    networkPolicyMode: mode,
    allowedDomains: (np?.egress?.allowedDomains ?? []).join("\n") || undefined,
    allowedCIDRs: (np?.egress?.allowedCIDRs ?? []).join("\n") || undefined,
    deniedCIDRs: (np?.egress?.deniedCIDRs ?? []).join("\n") || undefined,
    allowPrivateNetworks: np?.allowPrivateNetworks ?? false,
    injectionCredentialRows: (np?.secretInjection?.credentials ?? []).map((c) => ({
      name: c.name,
      secretName: c.valueFrom.name,
      secretKey: c.valueFrom.key,
      exposeAs: c.exposeAs ?? "",
      placeholder: c.placeholder ?? "",
    })),
    injectionRuleRows: (np?.secretInjection?.rules ?? []).flatMap((r) =>
      (r.headers && r.headers.length > 0
        ? r.headers
        : [{ name: "", value: "", mode: undefined }]
      ).map((h) => ({
        host: r.host,
        headerName: h.name,
        headerValue: h.value,
        mode: (h.mode as "Override" | "IfAbsent" | undefined) ?? "Override",
        substitute: (r.substitute ?? []).join(", "),
        pathPrefixes: (r.pathPrefixes ?? []).join(", "),
      })),
    ),
    autoUpdate: overrides?.updateStrategy?.autoUpdate ?? true,
    maxUnavailable: overrides?.updateStrategy?.maxUnavailable,
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
  const np = buildNetworkPolicy(v)
  if (np) o.networkPolicy = np
  const us = buildUpdateStrategy(v)
  if (us) o.updateStrategy = us
  return Object.keys(o).length ? o : undefined
}

// buildUpdateStrategy emits the rollout override only when it deviates from the
// inherited defaults (autoUpdate=true, maxUnavailable=20%), keeping the CR clean.
function buildUpdateStrategy(v: FormValues): Record<string, unknown> | undefined {
  const us: Record<string, unknown> = {}
  if (v.autoUpdate === false) us.autoUpdate = false
  if (v.maxUnavailable) us.maxUnavailable = v.maxUnavailable
  return Object.keys(us).length ? us : undefined
}

// buildNetworkPolicy maps the form's mode + fields onto the wire networkPolicy
// object, or undefined for "unrestricted" (omit the field).
function buildNetworkPolicy(v: FormValues): Record<string, unknown> | undefined {
  const injection = buildSecretInjection(v)
  // Injection alone is a valid configuration: it still needs the sidecar, which
  // is what "networkPolicy is set" means, but it filters nothing.
  if (v.networkPolicyMode === "unrestricted") {
    return injection ? { secretInjection: injection } : undefined
  }
  const np: Record<string, unknown> = {}
  if (injection) np.secretInjection = injection
  if (v.allowPrivateNetworks) np.allowPrivateNetworks = true
  if (v.networkPolicyMode === "disable") {
    np.disableEgress = true
    return np
  }
  // allowlist
  const egress: Record<string, string[]> = {}
  const domains = splitLines(v.allowedDomains)
  const allow = splitLines(v.allowedCIDRs)
  const deny = splitLines(v.deniedCIDRs)
  if (domains.length) egress.allowedDomains = domains
  if (allow.length) egress.allowedCIDRs = allow
  if (deny.length) egress.deniedCIDRs = deny
  np.egress = egress
  return np
}

function extractError(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { error?: string; message?: string }
    return e.error ?? e.message ?? JSON.stringify(err)
  }
  return String(err)
}
