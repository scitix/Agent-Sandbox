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

// @docs-example — This file is referenced as the canonical form pattern example in CLAUDE.md.
// If you rename, move, or significantly restructure this file, update the CLAUDE.md references too.
// Key sections for docs (line numbers may shift — keep in sync):
//   Schema definition:       see `const schema = z.object({...})`
//   Inner form + useQuery:   see `function CreateSandboxForm`
//   Object Combobox pattern: see the `poolName` Controller block
//   Plain input pattern:     see the `image` Field block
//   Sheet shell:             see `export function CreateSandboxSheet`

"use client"

import { Fragment, useMemo, useState } from "react"
import { useForm, useFieldArray, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { useAtomValue } from "jotai"
import { Loader2, Plus, Save, Trash2, X } from "lucide-react"
import { toast } from "sonner"
import { useQuery } from "@tanstack/react-query"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { ApiKeyRequiredNotice } from "@/components/custom/api-key-required-notice"
import { NetworkPolicyFields } from "@/components/custom/network-policy-fields"
import { useCreateSandbox, createSandboxViaE2B } from "@/lib/queries/sandbox"
import { pickUsableApiKey } from "@/lib/queries/apikey"
import { envsQueryOptions, globalApiKeysQueryOptions } from "@/lib/queries"
import { clustersAtom, impersonationAtom } from "@/lib/atoms"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { SHORT_DURATION_RE } from "@/lib/utils/duration"
import { buildE2BCreateBody } from "@/lib/utils/e2b-sandbox"
import type { AgentSandboxEnvSummary } from "@/lib/api/client"

// ─── Helpers ──────────────────────────────────────────────────────────────────

// The env-wide idle rollup the create form cares about. GET /envs returns the
// SandboxEnvSummary shape, which carries this aggregate as a flat scalar; the
// nested status.scalingGroups[].totalIdle only exists on the full SandboxEnv
// from GET /envs/{name}, so it is absent here and must not be read.
function totalIdleFor(env: AgentSandboxEnvSummary): number {
  return env.idleReplicas ?? 0
}

// ─── Schema ───────────────────────────────────────────────────────────────────

const keyValueRow = z.object({
  key: z.string().optional(),
  value: z.string().optional(),
})

const schema = z.object({
  poolName: z.string().min(1, "sandboxes.form.errors.envRequired"),
  image: z.string().optional(),
  startupTimeout: z.string().regex(SHORT_DURATION_RE, "sandboxes.form.errors.duration").optional(),
  idleTimeout: z.string().regex(SHORT_DURATION_RE, "sandboxes.form.errors.duration").optional(),
  // E2B-only below. Hidden when a cluster has no E2B gateway, because the native
  // create endpoint ignores every one of them.
  envVarRows: z.array(keyValueRow),
  metadataRows: z.array(keyValueRow),
  autoPause: z.boolean(),
  secure: z.boolean(),
  networkPolicyMode: z.enum(["unrestricted", "disable", "allowlist"]),
  allowedDomains: z.string().optional(),
  allowedCIDRs: z.string().optional(),
  deniedCIDRs: z.string().optional(),
})

type FormData = z.infer<typeof schema>

const defaultValues: FormData = {
  poolName: "",
  image: undefined,
  startupTimeout: undefined,
  idleTimeout: undefined,
  envVarRows: [],
  metadataRows: [],
  autoPause: false,
  secure: false,
  networkPolicyMode: "unrestricted",
  allowedDomains: undefined,
  allowedCIDRs: undefined,
  deniedCIDRs: undefined,
}

// ─── Inner form ───────────────────────────────────────────────────────────────
// Rendered only while the sheet is open, so its queries only run then.

interface CreateSandboxFormProps {
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

function CreateSandboxForm({ onOpenChange, onCreated }: CreateSandboxFormProps) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const clusters = useAtomValue(clustersAtom).clusters
  const { mutate, isPending: isMutating } = useCreateSandbox()
  const [createTimeout, setCreateTimeout] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  // Creation goes through E2B when the cluster publishes an E2B gateway; reads
  // stay native either way. Without one there is nothing to proxy to, so the
  // native endpoint keeps serving and the E2B-only fields are hidden.
  const e2bEnabled = !!clusters.find((c) => c.id === clusterID)?.gateway?.e2bURL

  // The user switcher must decide whose namespace the sandbox lands in.
  const impersonation = useAtomValue(impersonationAtom)

  const { data: envs } = useQuery(envsQueryOptions())
  const { data: apiKeys, isLoading: keysLoading } = useQuery({
    ...globalApiKeysQueryOptions(),
    enabled: e2bEnabled,
  })
  const apiKey = useMemo(() => pickUsableApiKey(apiKeys), [apiKeys])

  const {
    register,
    handleSubmit,
    reset,
    control,
    formState: { errors },
  } = useForm<FormData>({ resolver: zodResolver(schema), defaultValues })

  const envVars = useFieldArray({ control, name: "envVarRows" })
  const metadata = useFieldArray({ control, name: "metadataRows" })

  const onSubmit = async (data: FormData) => {
    setCreateTimeout(false)

    if (!e2bEnabled) {
      // Native path — only the four legacy fields exist there.
      mutate(
        {
          body: {
            poolName: data.poolName,
            image: data.image || undefined,
            startupTimeout: data.startupTimeout || undefined,
            idleTimeout: data.idleTimeout || undefined,
          },
        },
        {
          onSuccess: () => {
            toast.success(t("sandboxes.createdSuccess"))
            reset(defaultValues)
            onOpenChange(false)
            onCreated?.()
          },
          onError: (error: unknown) => {
            const err = error as Error & { errorCode?: string }
            if (err?.errorCode === "SANDBOX_CREATE_TIMEOUT") setCreateTimeout(true)
          },
        },
      )
      return
    }

    if (!apiKey?.rawToken) return
    setSubmitting(true)
    try {
      await createSandboxViaE2B(buildE2BCreateBody(data), {
        impersonate: impersonation,
        clusterID,
        apiKey: apiKey.rawToken,
      })
      toast.success(t("sandboxes.createdSuccess"))
      reset(defaultValues)
      onOpenChange(false)
      onCreated?.()
    } catch (error: unknown) {
      const err = error as Error & { errorCode?: string }
      // The middleware already toasted anything else; this one gets an in-sheet
      // banner because the sandbox may still be coming up.
      if (err?.errorCode === "SANDBOX_CREATE_TIMEOUT") setCreateTimeout(true)
    } finally {
      setSubmitting(false)
    }
  }

  // No key with a recoverable token → creation via E2B cannot authenticate at
  // all, so guide to the API Keys page instead of showing a form that will 401.
  if (e2bEnabled && !keysLoading && !apiKey) {
    return <ApiKeyRequiredNotice description={t("sandboxes.apiKeyRequiredDescription")} />
  }

  const busy = isMutating || submitting

  return (
    <Fragment>
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-1 flex-col overflow-hidden">
        <div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
          <section className="space-y-4">
            <Field data-invalid={!!errors.poolName}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("sandboxes.form.envName")} <span className="text-destructive">*</span>
              </FieldLabel>
              <Controller
                control={control}
                name="poolName"
                render={({ field, fieldState }) => {
                  const selectedEnv = envs?.find((e) => e.name === field.value)
                  return (
                    <Combobox
                      autoHighlight
                      value={selectedEnv ?? null}
                      onValueChange={(val) => field.onChange(val?.name ?? "")}
                      items={envs}
                      itemToStringLabel={(e) => e.name}
                    >
                      <ComboboxInput
                        aria-invalid={fieldState.invalid}
                        placeholder={t("common.search")}
                        className="h-9 font-mono text-sm"
                      />
                      <ComboboxContent>
                        <ComboboxEmpty>{t("common.noResultsFound")}</ComboboxEmpty>
                        <ComboboxList>
                          {(e: AgentSandboxEnvSummary) => {
                            const idle = totalIdleFor(e)
                            return (
                              <ComboboxItem key={e.name} value={e}>
                                <span>{e.name}</span>
                                <span
                                  className={cn(
                                    "ml-auto font-mono text-xs",
                                    idle > 0
                                      ? "text-green-700 dark:text-green-400"
                                      : "text-muted-foreground",
                                  )}
                                >
                                  {t("sandboxes.form.envIdle", { count: idle })}
                                </span>
                              </ComboboxItem>
                            )
                          }}
                        </ComboboxList>
                      </ComboboxContent>
                    </Combobox>
                  )
                }}
              />
              {errors.poolName && <FieldError>{t(errors.poolName.message as never)}</FieldError>}
              <FieldDescription>{t("sandboxes.form.selectEnv")}</FieldDescription>
            </Field>

            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("sandboxes.form.image")}
              </FieldLabel>
              <Input
                {...register("image")}
                placeholder="docker.io/myorg/myimage:latest"
                className="border-border bg-background h-9 font-mono text-sm"
              />
              <FieldDescription>{t("sandboxes.form.optionalImage")}</FieldDescription>
            </Field>
          </section>

          <div className="border-border divide-border divide-y rounded-md border">
            <Accordion>
              <AccordionItem value="timeouts">
                <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                  {t("sandboxes.form.section.timeouts")}
                </AccordionTrigger>
                <AccordionContent className="px-3">
                  <div className="flex flex-col gap-4 pb-2">
                    <Field data-invalid={!!errors.idleTimeout}>
                      <FieldLabel>{t("sandboxes.form.idleTimeout")}</FieldLabel>
                      <Input
                        {...register("idleTimeout")}
                        placeholder="e.g. 30s, 5m, 1h"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      {errors.idleTimeout && (
                        <FieldError>{t(errors.idleTimeout.message as never)}</FieldError>
                      )}
                      <FieldDescription>{t("sandboxes.form.idleTimeoutDesc")}</FieldDescription>
                    </Field>
                    <Field data-invalid={!!errors.startupTimeout}>
                      <FieldLabel>{t("sandboxes.form.startupTimeout")}</FieldLabel>
                      <Input
                        {...register("startupTimeout")}
                        placeholder="e.g. 30s, 2m"
                        className="border-border bg-background h-9 font-mono text-sm"
                      />
                      {errors.startupTimeout && (
                        <FieldError>{t(errors.startupTimeout.message as never)}</FieldError>
                      )}
                      <FieldDescription>{t("sandboxes.form.startupTimeoutDesc")}</FieldDescription>
                    </Field>
                  </div>
                </AccordionContent>
              </AccordionItem>

              {e2bEnabled && (
                <>
                  <AccordionItem value="network">
                    <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                      {t("envs.form.section.networkPolicy")}
                    </AccordionTrigger>
                    <AccordionContent className="px-3">
                      {/* No "allow private networks" switch: E2B's network config
                          has no field for it, so a per-sandbox request cannot
                          carry it — declare it on the SandboxEnv instead. */}
                      <NetworkPolicyFields
                        control={control}
                        register={register}
                        errors={errors}
                        showPrivateNetworks={false}
                      />
                    </AccordionContent>
                  </AccordionItem>

                  <AccordionItem value="runtime">
                    <AccordionTrigger className="text-muted-foreground px-3 py-2 font-mono text-[11px] font-bold tracking-[0.12em] uppercase hover:no-underline">
                      {t("sandboxes.form.section.runtime")}
                    </AccordionTrigger>
                    <AccordionContent className="px-3">
                      <div className="flex flex-col gap-4 pb-2">
                        <KeyValueRows
                          label={t("sandboxes.form.envVars")}
                          description={t("sandboxes.form.envVarsDesc")}
                          keyPlaceholder="TOKEN"
                          rows={envVars.fields}
                          onAppend={() => envVars.append({ key: "", value: "" })}
                          onRemove={envVars.remove}
                          register={register}
                          name="envVarRows"
                          addLabel={t("sandboxes.form.addRow")}
                        />

                        <Separator />

                        <KeyValueRows
                          label={t("sandboxes.form.metadata")}
                          description={t("sandboxes.form.metadataDesc")}
                          keyPlaceholder="owner"
                          rows={metadata.fields}
                          onAppend={() => metadata.append({ key: "", value: "" })}
                          onRemove={metadata.remove}
                          register={register}
                          name="metadataRows"
                          addLabel={t("sandboxes.form.addRow")}
                        />

                        <Separator />

                        <Controller
                          control={control}
                          name="autoPause"
                          render={({ field }) => (
                            <div className="flex items-center justify-between rounded-md border p-3">
                              <div className="space-y-0.5 pr-3">
                                <FieldLabel>{t("sandboxes.form.autoPause")}</FieldLabel>
                                <FieldDescription>
                                  {t("sandboxes.form.autoPauseDesc")}
                                </FieldDescription>
                              </div>
                              <Switch checked={field.value} onCheckedChange={field.onChange} />
                            </div>
                          )}
                        />
                        <Controller
                          control={control}
                          name="secure"
                          render={({ field }) => (
                            <div className="flex items-center justify-between rounded-md border p-3">
                              <div className="space-y-0.5 pr-3">
                                <FieldLabel>{t("sandboxes.form.secure")}</FieldLabel>
                                <FieldDescription>
                                  {t("sandboxes.form.secureDesc")}
                                </FieldDescription>
                              </div>
                              <Switch checked={field.value} onCheckedChange={field.onChange} />
                            </div>
                          )}
                        />
                      </div>
                    </AccordionContent>
                  </AccordionItem>
                </>
              )}
            </Accordion>
          </div>

          {createTimeout && (
            <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200">
              {t("sandboxes.createTimeoutHint")}
            </div>
          )}
        </div>

        <Separator />
        <div className="flex items-center gap-2 px-6 py-3">
          {e2bEnabled && apiKey && (
            <p className="text-muted-foreground truncate font-mono text-[11px]">
              {t("sandboxes.form.usingApiKey", { keyId: apiKey.keyId })}
            </p>
          )}
          <div className="ml-auto flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                reset(defaultValues)
                onOpenChange(false)
              }}
            >
              <X className="h-3.5 w-3.5" />
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={busy} className="gap-1.5">
              {busy ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Save className="h-3.5 w-3.5" />
              )}
              {t("common.create")}
            </Button>
          </div>
        </div>
      </form>
    </Fragment>
  )
}

// ─── Key/value row editor ─────────────────────────────────────────────────────

function KeyValueRows({
  label,
  description,
  keyPlaceholder,
  rows,
  onAppend,
  onRemove,
  register,
  name,
  addLabel,
}: {
  label: string
  description: string
  keyPlaceholder: string
  rows: { id: string }[]
  onAppend: () => void
  onRemove: (index: number) => void
  register: ReturnType<typeof useForm<FormData>>["register"]
  name: "envVarRows" | "metadataRows"
  addLabel: string
}) {
  return (
    <section className="space-y-2">
      <div>
        <FieldLabel>{label}</FieldLabel>
        <FieldDescription>{description}</FieldDescription>
      </div>
      {rows.map((row, index) => (
        <div key={row.id} className="flex items-center gap-2">
          <Input
            {...register(`${name}.${index}.key` as const)}
            placeholder={keyPlaceholder}
            className="h-9 font-mono text-sm"
          />
          <Input
            {...register(`${name}.${index}.value` as const)}
            placeholder="value"
            className="h-9 font-mono text-sm"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-9 w-9 shrink-0"
            onClick={() => onRemove(index)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" className="h-7 text-xs" onClick={onAppend}>
        <Plus className="h-3.5 w-3.5" />
        {addLabel}
      </Button>
    </section>
  )
}

// ─── Sheet shell ──────────────────────────────────────────────────────────────

interface CreateSandboxSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

export function CreateSandboxSheet({ open, onOpenChange, onCreated }: CreateSandboxSheetProps) {
  const { t } = useTranslation()
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        <SheetHeader className="px-6 py-4">
          <SheetTitle className="font-mono text-sm tracking-wider uppercase">
            {t("sandboxes.createTitle")}
          </SheetTitle>
        </SheetHeader>
        <Separator />
        {open && <CreateSandboxForm onOpenChange={onOpenChange} onCreated={onCreated} />}
      </SheetContent>
    </Sheet>
  )
}
