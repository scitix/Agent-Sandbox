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

import { useState, useMemo } from "react"
import { useForm, Controller } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Loader2, Plus, X, Settings2Icon, Settings2 } from "lucide-react"
import { parse as yamlParse } from "yaml"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
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
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { toast } from "sonner"
import { useQuery } from "@tanstack/react-query"
import { useCreatePool, useUpdatePool } from "@/lib/queries/pool"
import { templatesQueryOptions, quotasQueryOptions } from "@/lib/queries"
import { k8sNameSchema } from "@/lib/utils/validation"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"
import type { AgentSandboxPool } from "@/lib/api/client"
import { useAtomValue } from "jotai"
import { isAdminAtom } from "@/lib/atoms"
import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { clusterPath } from "@/lib/cluster-path"
import { useFeatureGates } from "@/hooks/use-feature-gates"
import {
  autoscalingFieldsSchema,
  buildAutoscalingSpec,
  AutoscalingFormSection,
} from "@/components/pools/autoscaling-form-section"
import {
  ImagePullSecretDialog,
  type ImagePullSecretValue,
} from "@/components/pools/image-pull-secret-dialog"
import { useLocale } from "@/hooks/use-locale"

// ─── Schemas ──────────────────────────────────────────────────────────────────

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type TranslationFn = (key: any, params?: Record<string, string | number>) => string

function addAutoscalingRefinement(t: TranslationFn) {
  return (data: Record<string, unknown>, ctx: z.RefinementCtx) => {
    if (!data.autoscalingEnabled) return
    const min = data.minReplicas as number | undefined
    const max = data.maxReplicas as number | undefined
    const replicas = data.replicas as number
    if (min == null) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: t("pools.validation.minReplicasRequired"),
        path: ["minReplicas"],
      })
    }
    if (max == null) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: t("pools.validation.maxReplicasRequired"),
        path: ["maxReplicas"],
      })
    }
    if (min != null && max != null) {
      if (min > max) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t("pools.validation.minExceedsMax"),
          path: ["maxReplicas"],
        })
      }
      if (replicas < min) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t("pools.validation.replicasBelowMin", { replicas, min }),
          path: ["replicas"],
        })
      }
      if (replicas > max) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t("pools.validation.replicasAboveMax", { replicas, max }),
          path: ["replicas"],
        })
      }
    }
  }
}

function makeCreateSchema(t: TranslationFn) {
  return z
    .object({
      name: k8sNameSchema,
      replicas: z.coerce.number().int().min(0, "Min 0").max(100000, "Max 100000"),
      templateName: z.string().min(1, "Template name is required"),
      quotaUrl: z.string().optional(),
      overrideImage: z.string().optional(),
      resourceMultiplier: z.coerce.number().int().min(0, "Min 0").optional(),
      podCreationImagePolicy: z.enum(["IdleImage", "PoolDefaultImage"]).optional(),
      ...autoscalingFieldsSchema,
    })
    .superRefine(addAutoscalingRefinement(t))
}

function makeEditPoolSchema(t: TranslationFn) {
  return z
    .object({
      replicas: z.coerce.number().int().min(0, "Min 0").max(100000, "Max 100000"),
      podCreationImagePolicy: z.enum(["IdleImage", "PoolDefaultImage"]).optional(),
      // Image override — optional; empty string means "no change"
      overrideImage: z.string().optional(),
      ...autoscalingFieldsSchema,
    })
    .superRefine(addAutoscalingRefinement(t))
}

type CreateFormData = z.infer<ReturnType<typeof makeCreateSchema>>
// z.input<> gives us the pre-transform type (number | "" instead of number | undefined)
// which is what react-hook-form sees before Zod runs transforms on submit.
type EditPoolFormData = z.input<ReturnType<typeof makeEditPoolSchema>>

// ─── Shared Sub-components ────────────────────────────────────────────────────

/** Shared replicas input field */
function ReplicasField({
  register,
  errors,
  t,
}: {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  register: any
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  errors: any
  t: TranslationFn
}) {
  return (
    <Field data-invalid={!!errors.replicas}>
      <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("pools.form.replicas")} <span className="text-destructive">*</span>
      </FieldLabel>
      <Input
        {...register("replicas")}
        type="number"
        min={0}
        max={100000}
        className="border-border bg-background h-9 font-mono text-sm"
      />
      <FieldError errors={[errors.replicas]} className="font-mono text-xs" />
      <FieldDescription>{t("pools.form.replicasDesc")}</FieldDescription>
    </Field>
  )
}

/**
 * Shared "Advanced Image Settings" accordion.
 * `showResourceMultiplier` is only relevant for the create form.
 */
function AdvancedImageSettingsAccordion({
  control,
  register,
  errors,
  t,
  showResourceMultiplier = false,
  effectiveCpu,
  effectiveMem,
}: {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  control: any
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  register: any
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  errors: any
  t: TranslationFn
  showResourceMultiplier?: boolean
  effectiveCpu?: number
  effectiveMem?: number
}) {
  return (
    <div className="border-border rounded border">
      <Accordion>
        <AccordionItem value="advanced-image">
          <AccordionTrigger className="px-3 py-3 hover:no-underline">
            <div className="flex items-center gap-2">
              <Settings2Icon className="text-muted-foreground h-3.5 w-3.5" />
              <span className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("pools.form.advancedImageSettings")}
              </span>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-3">
            <div className="flex flex-col gap-4 pb-2">
              {/* Pod Creation Image Policy */}
              <Field>
                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("pools.form.podCreationImagePolicy")}
                </FieldLabel>
                <Controller
                  control={control}
                  name="podCreationImagePolicy"
                  render={({ field }) => (
                    <Select
                      value={field.value ?? "IdleImage"}
                      onValueChange={(val) => field.onChange(val)}
                    >
                      <SelectTrigger className="h-9 w-full font-mono text-sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="IdleImage">
                          {t("pools.form.podCreationImagePolicyIdleImage")}
                        </SelectItem>
                        <SelectItem value="PoolDefaultImage">
                          {t("pools.form.podCreationImagePolicyPoolDefault")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
                <FieldDescription className="text-[11px]">
                  {t("pools.form.podCreationImagePolicyDesc")}
                </FieldDescription>
              </Field>

              {/* Image Override */}
              <Field data-invalid={!!errors.overrideImage}>
                <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("pools.form.imageOverride")}
                </FieldLabel>
                <Input
                  {...register("overrideImage")}
                  placeholder="e.g. myrepo/myimage:v2"
                  className="border-border bg-background h-9 font-mono text-sm"
                />
                <FieldDescription className="mt-1.5">
                  {t("pools.form.imageOverrideDesc")}
                </FieldDescription>
                <FieldError errors={[errors.overrideImage]} className="font-mono text-xs" />
              </Field>

              {/* Resource Multiplier — create form only */}
              {showResourceMultiplier && (
                <Field data-invalid={!!errors.resourceMultiplier}>
                  <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                    {t("pools.form.resourceMultiplier")}
                    <span className="text-muted-foreground ml-1.5 font-mono text-xs font-normal tracking-normal normal-case">
                      {t("pools.form.resourceMultiplierOptional")}
                    </span>
                  </FieldLabel>
                  <Input
                    {...register("resourceMultiplier")}
                    type="number"
                    min={1}
                    step={1}
                    placeholder="1"
                    className="border-border bg-background h-9 font-mono text-sm"
                  />
                  <FieldError errors={[errors.resourceMultiplier]} className="font-mono text-xs" />
                  <FieldDescription>{t("pools.form.resourceMultiplierDesc")}</FieldDescription>
                </Field>
              )}

              {/* Sandbox resources preview — create form only */}
              {showResourceMultiplier && (effectiveCpu != null || effectiveMem != null) && (
                <div className="border-border bg-muted/30 rounded border px-3 py-2">
                  <p className="text-muted-foreground font-mono text-[11px] font-semibold tracking-wide uppercase">
                    {t("pools.form.sandboxResources")}
                  </p>
                  <div className="mt-1 flex gap-4">
                    {effectiveCpu != null && (
                      <span className="font-mono text-xs">
                        CPU:{" "}
                        <span className="text-foreground font-semibold">
                          {formatCores(effectiveCpu)} cores
                        </span>
                      </span>
                    )}
                    {effectiveMem != null && (
                      <span className="font-mono text-xs">
                        Memory:{" "}
                        <span className="text-foreground font-semibold">
                          {formatMiB(effectiveMem)} MiB
                        </span>
                      </span>
                    )}
                  </div>
                </div>
              )}
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  )
}

/** Shared form footer with cancel + submit buttons */
function FormFooter({
  formId,
  isMutating,
  submitLabel,
  SubmitIcon,
  onCancel,
  t,
}: {
  formId: string
  isMutating: boolean
  submitLabel: string
  SubmitIcon: React.ElementType
  onCancel: () => void
  t: TranslationFn
}) {
  return (
    <div className="border-border flex items-center justify-end gap-2 border-t px-6 py-3">
      <Button
        type="button"
        variant="outline"
        onClick={onCancel}
        className="font-mono text-xs tracking-wider uppercase"
      >
        <X className="mr-1.5 h-3.5 w-3.5" />
        {t("common.cancel")}
      </Button>
      <Button
        type="submit"
        form={formId}
        disabled={isMutating}
        className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
      >
        {isMutating ? (
          <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
        ) : (
          <SubmitIcon className="mr-1.5 h-3.5 w-3.5" />
        )}
        {submitLabel}
      </Button>
    </div>
  )
}

// ─── Props ────────────────────────────────────────────────────────────────────

export interface CreatePoolSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
  pool?: AgentSandboxPool | null
  /** "create" = create new pool; "edit" = edit pool settings + optionally update image */
  mode?: "create" | "edit"
}

// ─── Create Form ──────────────────────────────────────────────────────────────

interface CreateFormProps {
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

function CreateForm({ onOpenChange, onCreated }: CreateFormProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useCreatePool()
  const clusterID = useClusterID()
  const locale = useLocale()
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false)
  const [showImagePullSecretDialog, setShowImagePullSecretDialog] = useState(false)
  const [imagePullSecret, setImagePullSecret] = useState<ImagePullSecretValue | null>(null)

  const isAdmin = useAtomValue(isAdminAtom)
  const featureGates = useFeatureGates()
  const { data: templates } = useQuery(templatesQueryOptions())
  const { data: quotas } = useQuery(
    quotasQueryOptions({ enabled: !isAdmin && featureGates.quota }),
  )

  const createSchema = useMemo(() => makeCreateSchema(t), [t])

  const {
    register,
    handleSubmit,
    reset,
    control,
    watch,
    setValue,
    formState: { errors },
  } = useForm<CreateFormData>({
    resolver: zodResolver(createSchema),
    defaultValues: {
      replicas: 1,
      quotaUrl: quotas?.[0]?.quotaUrl,
      podCreationImagePolicy: "IdleImage",
    },
  })

  const watchedTemplateName = watch("templateName")
  const watchedMultiplier = watch("resourceMultiplier")
  const selectedTemplate = templates?.find((t) => t.name === watchedTemplateName)

  const onSubmit = (data: CreateFormData) => {
    const hasImageOverride = !!data.overrideImage?.trim()
    const hasMultiplier = typeof data.resourceMultiplier === "number" && data.resourceMultiplier > 1

    mutate(
      {
        body: {
          name: data.name,
          templateName: data.templateName || undefined,
          labels: data.quotaUrl ? { "quota.scitix.ai/url": data.quotaUrl } : undefined,
          spec: {
            replicas: data.replicas,
            minReplicas:
              data.autoscalingEnabled && data.minReplicas != null
                ? (data.minReplicas as number)
                : undefined,
            maxReplicas:
              data.autoscalingEnabled && data.maxReplicas != null
                ? (data.maxReplicas as number)
                : undefined,
            autoscaling: buildAutoscalingSpec(data),
            podCreationImagePolicy: (data.podCreationImagePolicy ?? "IdleImage") as
              | "IdleImage"
              | "PoolDefaultImage",
          },
          overrides:
            hasImageOverride || hasMultiplier
              ? {
                image: hasImageOverride ? data.overrideImage!.trim() : undefined,
                resourceMultiplier: hasMultiplier ? data.resourceMultiplier : undefined,
              }
              : undefined,
          imagePullSecret:
            imagePullSecret && imagePullSecret.registries.length > 0
              ? imagePullSecret
              : undefined,
        },
      },
      {
        onSuccess: () => {
          toast.success(t("pools.createdSuccess"))
          reset()
          onOpenChange(false)
          onCreated?.()
        },
        onError: (err) => {
          // When the backend returns API_KEY_REQUIRED (422), suppress the global
          // toast (handled by SUPPRESSED_ERROR_CODES in client.ts) and instead
          // show an in-page dialog guiding the user to the API Keys page.
          const errorCode = (err as unknown as { errorCode?: string }).errorCode
          if (errorCode === "API_KEY_REQUIRED") {
            setShowApiKeyDialog(true)
          }
        },
      },
    )
  }

  const cpuCores = selectedTemplate?.cpu ? parseCpuToCore(selectedTemplate.cpu) : undefined
  const memMiB = selectedTemplate?.memory ? parseMemoryToMiB(selectedTemplate.memory) : undefined

  const multiplier =
    typeof watchedMultiplier === "string" && parseInt(watchedMultiplier) > 1
      ? parseInt(watchedMultiplier)
      : typeof watchedMultiplier === "number" && watchedMultiplier > 1
        ? watchedMultiplier
        : 1
  const effectiveCpu = cpuCores != null ? cpuCores * multiplier : undefined
  const effectiveMem = memMiB != null ? memMiB * multiplier : undefined

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <form
        id="create-pool-form"
        onSubmit={handleSubmit(onSubmit)}
        className="flex flex-1 flex-col overflow-hidden"
      >
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-6 py-4">
          <Field data-invalid={!!errors.name}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("pools.form.poolName")} <span className="text-destructive">*</span>
            </FieldLabel>
            <Input
              {...register("name")}
              placeholder="my-pool"
              className="border-border bg-background h-9 font-mono text-sm"
            />
            <FieldError errors={[errors.name]} className="font-mono text-xs" />
            <FieldDescription>{t("pools.form.nameDesc")}</FieldDescription>
          </Field>

          <ReplicasField register={register} errors={errors} t={t} />

          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("pools.form.template")} <span className="text-destructive">*</span>
            </FieldLabel>
            <Controller
              control={control}
              name="templateName"
              render={({ field, fieldState }) => {
                const selectedTmpl = templates?.find((t) => t.name === field.value) ?? null
                return (
                  <Combobox
                    autoHighlight
                    value={selectedTmpl}
                    onValueChange={(val) => {
                      const name = val?.name ?? ""
                      field.onChange(name)
                    }}
                    items={templates}
                    itemToStringLabel={(t) => t.name}
                  >
                    <ComboboxInput
                      aria-invalid={fieldState.invalid}
                      placeholder="Search template..."
                      className="h-9 font-mono text-sm"
                    />
                    <ComboboxContent>
                      <ComboboxEmpty>No templates found</ComboboxEmpty>
                      <ComboboxList>
                        {(t) => (
                          <ComboboxItem key={t.name} value={t} className="flex-col items-start">
                            <div className="flex w-full items-center gap-2">
                              <p className="font-mono text-sm font-medium">
                                {t.name}
                                {t.version && (
                                  <span className="bg-muted text-muted-foreground ml-1 rounded px-1 py-0.5 font-mono text-[10px]">
                                    {t.version}
                                  </span>
                                )}
                              </p>
                              <div className="ml-auto flex shrink-0 items-center">
                                {(t.cpu || t.memory) && (
                                  <span className="text-muted-foreground font-mono text-[10px]">
                                    {[t.cpu && `${t.cpu} CPU`, t.memory && `${t.memory}`]
                                      .filter(Boolean)
                                      .join(" · ")}
                                  </span>
                                )}
                              </div>
                            </div>
                            {t.description && (
                              <span className="text-muted-foreground mt-0.5 line-clamp-1 font-mono text-[11px] font-normal">
                                {t.description}
                              </span>
                            )}
                          </ComboboxItem>
                        )}
                      </ComboboxList>
                    </ComboboxContent>
                  </Combobox>
                )
              }}
            />
            <FieldError errors={[errors.templateName]} className="font-mono text-xs" />
            <FieldDescription>{t("pools.form.templateDesc")}</FieldDescription>
          </Field>

          {selectedTemplate && (
            <AdvancedImageSettingsAccordion
              control={control}
              register={register}
              errors={errors}
              t={t}
              showResourceMultiplier
              effectiveCpu={effectiveCpu}
              effectiveMem={effectiveMem}
            />
          )}

          {/* Image Pull Secret */}
          <Field>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              {t("pools.form.imagePullSecret")}
            </FieldLabel>
            <div className="border-border flex items-center justify-between gap-2 rounded border px-3 py-2">
              {imagePullSecret && imagePullSecret.registries.length > 0 ? (
                <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                  <span className="text-muted-foreground font-mono text-[11px]">
                    {t("pools.form.imagePullSecretConfigured", {
                      count: imagePullSecret.registries.length,
                    })}
                  </span>
                  {imagePullSecret.registries.map((r, idx) => (
                    <span
                      key={idx}
                      className="bg-muted text-foreground max-w-[180px] truncate rounded px-1.5 py-0.5 font-mono text-[10px]"
                      title={r.registry}
                    >
                      {r.registry}
                    </span>
                  ))}
                </div>
              ) : (
                <span className="text-muted-foreground font-mono text-[11px]">
                  {t("pools.form.imagePullSecretConfigure")}
                </span>
              )}
              <div className="flex shrink-0 items-center gap-1">
                {imagePullSecret && (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={() => setImagePullSecret(null)}
                    className="h-7 font-mono text-[11px] tracking-wider uppercase"
                  >
                    {t("pools.form.imagePullSecretClear")}
                  </Button>
                )}
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setShowImagePullSecretDialog(true)}
                  className="h-7 font-mono text-[11px] tracking-wider uppercase"
                >
                  {imagePullSecret
                    ? t("pools.form.imagePullSecretEdit")
                    : t("pools.form.imagePullSecretConfigure")}
                </Button>
              </div>
            </div>
            <FieldDescription>{t("pools.form.imagePullSecretDesc")}</FieldDescription>
          </Field>

          {featureGates.quota && quotas && quotas.length > 0 && (
            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                Quota
              </FieldLabel>
              <Controller
                control={control}
                name="quotaUrl"
                render={({ field }) => (
                  <Combobox
                    value={field.value ?? null}
                    onValueChange={(val) => field.onChange(val === null ? undefined : val)}
                    items={quotas}
                  >
                    <ComboboxInput
                      placeholder="Select quota…"
                      className="h-9 font-mono text-[11px]"
                      showTrigger
                      showClear={false}
                    />
                    <ComboboxContent>
                      <ComboboxEmpty>No quotas found.</ComboboxEmpty>
                      <ComboboxList>
                        {quotas.map((q) => {
                          const usageLine =
                            Object.keys(q.resources ?? {}).length > 0
                              ? Object.entries(q.resources!)
                                .map(([k, v]) => `${q.used?.[k] ?? "0"}/${v ?? "?"} ${k}`)
                                .join(" · ")
                              : null
                          return (
                            <ComboboxItem key={q.quotaUrl} value={q.quotaUrl}>
                              <div className="flex min-w-0 flex-1 flex-col">
                                <span className="truncate font-mono text-[11px] leading-tight">
                                  {q.label}
                                </span>
                                {usageLine && (
                                  <span className="text-muted-foreground text-xs leading-tight">
                                    {usageLine}
                                  </span>
                                )}
                              </div>
                            </ComboboxItem>
                          )
                        })}
                      </ComboboxList>
                    </ComboboxContent>
                  </Combobox>
                )}
              />
              <FieldError errors={[errors.quotaUrl]} className="font-mono text-xs" />
              <FieldDescription>{t("pools.form.quotaDesc")}</FieldDescription>
            </Field>
          )}

          {/* Autoscaling */}
          <AutoscalingFormSection
            control={control}
            register={register}
            errors={errors}
            setValue={setValue}
          />
        </div>
      </form>

      <FormFooter
        formId="create-pool-form"
        isMutating={isMutating}
        submitLabel={t("common.create")}
        SubmitIcon={Plus}
        onCancel={() => {
          reset()
          onOpenChange(false)
        }}
        t={t}
      />

      {/* API Key Required Dialog */}
      <Dialog open={showApiKeyDialog} onOpenChange={setShowApiKeyDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pools.apiKeyRequired.title")}</DialogTitle>
            <DialogDescription>{t("pools.apiKeyRequired.description")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowApiKeyDialog(false)}>
              {t("common.cancel")}
            </Button>
            <Button render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}>
              {t("pools.apiKeyRequired.goToApiKeys")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Image Pull Secret Dialog */}
      <ImagePullSecretDialog
        open={showImagePullSecretDialog}
        onOpenChange={setShowImagePullSecretDialog}
        value={imagePullSecret}
        onSave={setImagePullSecret}
      />
    </div>
  )
}

// ─── Edit Pool Form ───────────────────────────────────────────────────────────
// Merges the former UpdatePoolForm (replicas / autoscaling) and UpdateImageForm
// (image override) into a single form.  The image section lives inside a
// collapsible accordion so it stays out of the way when the user only wants to
// tweak replicas or autoscaling.

interface EditPoolFormProps {
  pool: AgentSandboxPool
  onOpenChange: (open: boolean) => void
}

function EditPoolForm({ pool, onOpenChange }: EditPoolFormProps) {
  const { t } = useTranslation()
  const { mutate, isPending: isMutating } = useUpdatePool()

  const currentAutoscaling = pool.spec?.autoscaling
  const currentScaleUp = currentAutoscaling?.scaleUpPolicy
  const currentScaleDown = currentAutoscaling?.scaleDownPolicy

  const cpuCores = pool.cpu ? parseCpuToCore(pool.cpu) : undefined
  const memMiB = pool.memory ? parseMemoryToMiB(pool.memory) : undefined

  // Extract the current effective containers[0].image from specYaml (PodTemplateSpec).
  const currentImage = (() => {
    if (!pool.specYaml) return undefined
    try {
      const parsed = yamlParse(pool.specYaml) as Record<string, unknown>
      const template = parsed?.template as Record<string, unknown> | undefined
      const spec = template?.spec as Record<string, unknown> | undefined
      const containers = spec?.containers as Array<{ image?: string }> | undefined
      return containers?.[0]?.image || undefined
    } catch {
      return undefined
    }
  })()

  const editPoolSchema = useMemo(() => makeEditPoolSchema(t), [t])

  const {
    register,
    handleSubmit,
    reset,
    control,
    setValue,
    formState: { errors },
  } = useForm<EditPoolFormData>({
    resolver: zodResolver(editPoolSchema),
    // "values" (not "defaultValues") so it re-syncs when pool prop changes
    values: {
      replicas: pool.spec?.replicas ?? 1,
      podCreationImagePolicy:
        (pool.spec?.podCreationImagePolicy as "IdleImage" | "PoolDefaultImage" | undefined) ??
        "IdleImage",
      overrideImage: "",
      autoscalingEnabled: currentAutoscaling?.enabled ?? false,
      minReplicas: pool.spec?.minReplicas ?? "",
      maxReplicas: pool.spec?.maxReplicas ?? "",
      scaleUpMode:
        (currentScaleUp?.mode as "Conservative" | "Default" | "Aggressive" | undefined) ??
        undefined,
      cooldownSeconds: currentScaleUp?.cooldownSeconds ?? "",
      idleThresholdSeconds: currentScaleUp?.idleThresholdSeconds ?? "",
      idleTimeoutSeconds: currentScaleDown?.idleTimeoutSeconds ?? "",
      stabilizationSeconds: currentScaleDown?.stabilizationSeconds ?? "",
      protectionWindowSeconds: currentScaleDown?.protectionWindowSeconds ?? "",
    },
  })

  const onSubmit = (data: EditPoolFormData) => {
    const resolved = data as unknown as z.output<typeof editPoolSchema>
    const hasImageOverride = !!resolved.overrideImage?.trim()

    const body: Record<string, unknown> = {
      replicas: resolved.replicas,
      minReplicas: resolved.autoscalingEnabled ? resolved.minReplicas : undefined,
      maxReplicas: resolved.autoscalingEnabled ? resolved.maxReplicas : undefined,
      autoscaling: buildAutoscalingSpec(data),
      podCreationImagePolicy: resolved.podCreationImagePolicy,
    }

    if (hasImageOverride) {
      body.overrides = { image: resolved.overrideImage!.trim() }
    }

    mutate(
      {
        params: { path: { name: pool.name } },
        body,
      },
      {
        onSuccess: () => {
          toast.success(t("pools.updatedSuccess", { name: pool.name }))
          onOpenChange(false)
        },
        onError: (err) => {
          const msg = err instanceof Error ? err.message : String(err)
          if (msg.includes("409") || msg.toLowerCase().includes("conflict")) {
            toast.error(t("pools.conflict409"), { duration: 10000 })
          }
        },
      },
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <form
        id="edit-pool-form"
        onSubmit={handleSubmit(onSubmit)}
        className="flex flex-1 flex-col overflow-hidden"
      >
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-6 py-4">
          {/* Read-only pool info — rich header with resources + current image */}
          <div className="border-border bg-muted/30 divide-border divide-y rounded border">
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="text-muted-foreground w-24 shrink-0 font-mono text-xs uppercase">
                {t("pools.form.pool")}
              </span>
              <span className="truncate font-mono text-xs font-semibold">{pool.name}</span>
            </div>
            {pool.spec?.templateName && (
              <div className="flex items-center gap-3 px-3 py-2">
                <span className="text-muted-foreground w-24 shrink-0 font-mono text-xs uppercase">
                  {t("pools.form.template")}
                </span>
                <span className="truncate font-mono text-xs">{pool.spec.templateName}</span>
              </div>
            )}
            {(cpuCores != null || memMiB != null) && (
              <div className="flex items-center gap-3 px-3 py-2">
                <span className="text-muted-foreground w-24 shrink-0 font-mono text-xs uppercase">
                  {t("pools.form.resources")}
                </span>
                <div className="flex gap-3">
                  {cpuCores != null && (
                    <span className="font-mono text-xs">{formatCores(cpuCores)} cores</span>
                  )}
                  {memMiB != null && (
                    <span className="font-mono text-xs">{formatMiB(memMiB)} MiB</span>
                  )}
                </div>
              </div>
            )}
            {currentImage && (
              <div className="flex items-start gap-3 px-3 py-2">
                <span className="text-muted-foreground w-24 shrink-0 font-mono text-xs uppercase">
                  {t("pools.form.current")}
                </span>
                <span className="font-mono text-xs break-all">{currentImage}</span>
              </div>
            )}
          </div>

          {/* Replicas */}
          <ReplicasField register={register} errors={errors} t={t} />

          {/* Advanced Image Settings — collapsed by default */}
          <AdvancedImageSettingsAccordion
            control={control}
            register={register}
            errors={errors}
            t={t}
          />

          {/* Autoscaling */}
          <AutoscalingFormSection
            control={control}
            register={register}
            errors={errors}
            defaultOpen={currentAutoscaling?.enabled === true}
            setValue={setValue}
          />
        </div>
      </form>

      <FormFooter
        formId="edit-pool-form"
        isMutating={isMutating}
        submitLabel={t("pools.editPool")}
        SubmitIcon={Settings2}
        onCancel={() => {
          reset()
          onOpenChange(false)
        }}
        t={t}
      />
    </div>
  )
}

// ─── Sheet Shell ──────────────────────────────────────────────────────────────

export function CreatePoolSheet({
  open,
  onOpenChange,
  onCreated,
  pool,
  mode,
}: CreatePoolSheetProps) {
  const { t } = useTranslation()

  // Infer mode from props for backward compatibility
  const resolvedMode = mode ?? (pool ? "edit" : "create")

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-lg data-[side=right]:sm:max-w-3xl"
      >
        <SheetHeader className="border-border border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
            {resolvedMode === "edit" && pool ? (
              <>
                <Settings2 className="h-4 w-4" />
                {t("pools.editPool")}
                <span className="text-muted-foreground ml-1 font-normal normal-case">
                  — {pool.name}
                </span>
              </>
            ) : (
              <>
                <Plus className="h-4 w-4" />
                {t("pools.createPool")}
              </>
            )}
          </SheetTitle>
        </SheetHeader>

        {resolvedMode === "edit" && pool
          ? open && <EditPoolForm pool={pool} onOpenChange={onOpenChange} />
          : open && <CreateForm onOpenChange={onOpenChange} onCreated={onCreated} />}
      </SheetContent>
    </Sheet>
  )
}
