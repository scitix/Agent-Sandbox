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

import { useCallback, useRef, useEffect, useState } from "react"
import { useForm, useFieldArray, Controller, useWatch } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Loader2, Plus, Minus, X, Download, Pencil, FileCode, Eye } from "lucide-react"
import { stringify as yamlStringify, parse as yamlParse } from "yaml"
import { toast } from "sonner"

import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
  ComboboxChips,
  ComboboxChip,
  ComboboxChipsInput,
  useComboboxAnchor,
} from "@/components/ui/combobox"
import { useQuery } from "@tanstack/react-query"
import {
  useCreateTemplate,
  useUpdateTemplate,
  adminTeamsQueryOptions,
  adminUsersByTeamQueryOptions,
} from "@/lib/queries"
import { cn } from "@/lib/utils"
import type { AgentSandboxTemplate } from "@/lib/api/client"
import { k8sNameSchema } from "@/lib/utils/validation"
import { useTranslation } from "@/lib/i18n"
import { YamlDiffView } from "@/components/templates/yaml-diff-view"
import { LargeDialog } from "@/components/large-dialog"

// ─── Kubernetes-managed annotations to strip from display / submission ───────────

/** Annotations managed by Kubernetes or kubectl that should never be shown or submitted. */
const K8S_MANAGED_ANNOTATIONS = new Set(["kubectl.kubernetes.io/last-applied-configuration"])

/** Return a copy of the annotations map without any K8s-managed keys. */
function stripManagedAnnotations(
  annotations: Record<string, string> | undefined,
): Record<string, string> | undefined {
  if (!annotations) return undefined
  const filtered = Object.fromEntries(
    Object.entries(annotations).filter(([k]) => !K8S_MANAGED_ANNOTATIONS.has(k)),
  )
  return Object.keys(filtered).length > 0 ? filtered : undefined
}

/**
 * Strip server-managed read-only metadata fields before showing the diff.
 * Also normalizes the object to match what formToCrdObject produces:
 *   - Ensures apiVersion/kind are present (server YAML may omit them)
 *   - Removes null values recursively (server may serialize omitempty-missing fields as null)
 */
function stripServerManagedFields(yamlStr: string): string {
  if (!yamlStr) return yamlStr
  try {
    const parsed = yamlParse(yamlStr) as Record<string, unknown>

    // Ensure top-level K8s type fields are present — formToCrdObject always emits them.
    if (!parsed.apiVersion) parsed.apiVersion = "agents.navix.sh/v1alpha1"
    if (!parsed.kind) parsed.kind = "SandboxTemplate"

    const meta = parsed.metadata as Record<string, unknown> | undefined
    if (meta) {
      const cleaned = { ...meta }
      delete cleaned.generation
      delete cleaned.managedFields
      delete cleaned.selfLink
      parsed.metadata = cleaned
    }
    delete parsed.status

    return yamlStringify(removeNulls(parsed))
  } catch {
    return yamlStr
  }
}

/** Recursively remove null values from an object/array so the diff doesn't show
 *  noise from server fields serialized as null due to missing omitempty tags. */
function removeNulls(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(removeNulls)
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>)
        .filter(([, v]) => v !== null)
        .map(([k, v]) => [k, removeNulls(v)]),
    )
  }
  return value
}

// ─── Semver helpers ──────────────────────────────────────────────────────────────

function parseSemver(v: string): [number, number, number] | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)$/.exec(v)
  if (!m) return null
  return [Number(m[1]), Number(m[2]), Number(m[3])]
}

function patchIncrement(v: string): string {
  const p = parseSemver(v)
  if (!p) return "0.0.1"
  const prefix = v.startsWith("v") ? "v" : ""
  return `${prefix}${p[0]}.${p[1]}.${p[2] + 1}`
}

function semverGt(a: string, b: string): boolean {
  const ap = parseSemver(a),
    bp = parseSemver(b)
  if (!ap || !bp) return false
  for (let i = 0; i < 3; i++) {
    if (ap[i] > bp[i]) return true
    if (ap[i] < bp[i]) return false
  }
  return false
}

// ─── Schema ─────────────────────────────────────────────────────────────────────

const runtimeSchema = z.object({
  name: z.string().min(1, "Name required"),
  port: z.coerce.number().int().min(1).max(65535).optional().or(z.literal("")),
  protocol: z.string().optional(),
  description: z.string().optional(),
  logDir: z.string().optional(),
  readinessProbeEnabled: z.boolean().optional(),
  readinessProbePath: z.string().optional(),
  readinessProbePort: z.coerce.number().int().min(1).max(65535).optional().or(z.literal("")),
})

const ruleSchema = z.object({
  team: z.string().optional(),
  users: z.array(z.string()).optional(),
})

const buildSchema = (currentVersion?: string) =>
  z.object({
    // Basic — only required on Create
    name: k8sNameSchema.optional(),
    version: z
      .string()
      .optional()
      .refine(
        (v) => !v || /^v?\d+\.\d+\.\d+$/.test(v),
        "Version must be in x.y.z or vx.y.z format (e.g. 1.2.3 or v1.2.3)",
      )
      .refine(
        (v) => {
          if (!v || !currentVersion) return true
          return semverGt(v, currentVersion)
        },
        `Version must be greater than current (${currentVersion ?? "none"})`,
      ),
    description: z.string().optional(),
    idleImage: z.string().min(1, "Idle image is required"),
    // Runtimes
    runtimes: z.array(runtimeSchema).optional(),
    // CRD YAML — the primary input
    crdYaml: z.string().min(1, "CRD YAML is required"),
    // Visibility
    rules: z.array(ruleSchema).optional(),
    visibilityPublic: z.boolean().optional(),
    // Docs — Markdown documentation stored in annotation
    docs: z.string().optional(),
  })

type FormData = z.infer<ReturnType<typeof buildSchema>>

// ─── Props ───────────────────────────────────────────────────────────────────────

export interface UpsertTemplateSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  template?: AgentSandboxTemplate | null
  onSuccess?: () => void
  readOnly?: boolean
}

// ─── Outer Shell ─────────────────────────────────────────────────────────────────

export function UpsertTemplateSheet({
  open,
  onOpenChange,
  template,
  onSuccess,
  readOnly,
}: UpsertTemplateSheetProps) {
  const isEdit = !!template
  const { t } = useTranslation()

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl data-[side=right]:sm:max-w-4xl"
      >
        <SheetHeader className="border-border border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
            {readOnly ? (
              <>
                <Eye className="h-4 w-4" />
                {t("templates.templateDetail")}
                <span className="text-muted-foreground ml-1 font-normal normal-case">
                  — {template?.name}
                </span>
              </>
            ) : isEdit ? (
              <>
                <Pencil className="h-4 w-4" />
                {t("templates.editTitle")}
                <span className="text-muted-foreground ml-1 font-normal normal-case">
                  — {template.name}
                </span>
              </>
            ) : (
              <>
                <FileCode className="h-4 w-4" />
                {t("templates.newTemplate")}
              </>
            )}
          </SheetTitle>
        </SheetHeader>
        {open && (
          <UpsertTemplateForm
            template={template}
            onOpenChange={onOpenChange}
            onSuccess={onSuccess}
            readOnly={readOnly}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

// ─── helpers ──────────────────────────────────────────────────────────────────

/** Build a canonical CRD YAML object from the structured form values. */
function formToCrdObject(
  data: Partial<FormData>,
  name: string | undefined,
  isEdit: boolean,
  existingTemplate?: AgentSandboxTemplate | null,
): Record<string, unknown> {
  // Parse existing crdYaml to extract spec.template and labels (user-visible fields we don't
  // manage via form inputs) — but we never spread unknown metadata fields implicitly.
  let base: Record<string, unknown> = {}
  if (data.crdYaml && data.crdYaml.trim()) {
    try {
      base = yamlParse(data.crdYaml) as Record<string, unknown>
    } catch {
      // ignore parse error during partial updates
    }
  }

  const runtimes =
    data.runtimes && data.runtimes.length > 0
      ? data.runtimes
          .filter((r) => r.name)
          .map((r) => ({
            name: r.name,
            port: r.port ? Number(r.port) : undefined,
            protocol: r.protocol || undefined,
            description: r.description || undefined,
            logDir: r.logDir || undefined,
            readinessProbe: r.readinessProbeEnabled
              ? {
                  httpGet: {
                    port: r.readinessProbePort
                      ? Number(r.readinessProbePort)
                      : r.port
                        ? Number(r.port)
                        : 80,
                    path: r.readinessProbePath || "/",
                  },
                }
              : undefined,
          }))
      : undefined

  let visibility: Record<string, unknown> | undefined = undefined
  if (!data.visibilityPublic && data.rules && data.rules.length > 0) {
    const cleanRules = data.rules
      .map((r) => ({ team: r.team || undefined, users: r.users?.length ? r.users : undefined }))
      .filter((r) => r.team || (r.users && r.users.length > 0))
    if (cleanRules.length > 0) visibility = { rules: cleanRules }
  }

  // Preserve spec.template (pod template spec) from the current YAML — it is not
  // managed by the structured form fields and must round-trip unchanged.
  const specBase = (base.spec as Record<string, unknown>) ?? {}
  const spec: Record<string, unknown> = {
    version: data.version || undefined,
    description: data.description || undefined,
    idleImage: data.idleImage || undefined,
    runtimes: runtimes && runtimes.length > 0 ? runtimes : undefined,
    visibility: visibility ?? undefined,
    // Preserve fields not covered by form inputs (e.g. template, reservation)
    ...(specBase.template !== undefined ? { template: specBase.template } : {}),
    ...(specBase.reservation !== undefined ? { reservation: specBase.reservation } : {}),
  }
  Object.keys(spec).forEach((k) => spec[k] === undefined && delete spec[k])

  // Preserve user-managed labels from current YAML (not from server response directly,
  // so no server-managed fields leak in).
  const baseLabels = (base.metadata as Record<string, unknown> | undefined)?.labels as
    | Record<string, string>
    | undefined

  // Annotations: start from the current YAML's annotations (already cleaned on initial load),
  // then inject/remove the docs annotation.
  const baseAnnotations = (base.metadata as Record<string, unknown> | undefined)?.annotations as
    | Record<string, string>
    | undefined
  const cleanAnnotations = stripManagedAnnotations(baseAnnotations)
  const mergedAnnotations = { ...cleanAnnotations }
  if (data.docs?.trim()) {
    mergedAnnotations["agentbox.navix.sh/docs"] = data.docs.trim()
  } else {
    delete mergedAnnotations["agentbox.navix.sh/docs"]
  }
  delete mergedAnnotations["agentbox.navix.sh/pool-docs"]
  const finalAnnotations = Object.keys(mergedAnnotations).length > 0 ? mergedAnnotations : undefined

  // In edit mode, extract optimistic-lock tokens from the original server crdYaml so
  // the PUT request carries the correct resourceVersion (→ 409 on conflict).
  let existingMeta: Record<string, unknown> | undefined
  if (isEdit && existingTemplate?.crdYaml) {
    try {
      const parsed = yamlParse(existingTemplate.crdYaml) as Record<string, unknown>
      existingMeta = parsed.metadata as Record<string, unknown> | undefined
    } catch {
      // ignore
    }
  }

  return {
    apiVersion: "agents.navix.sh/v1alpha1",
    kind: "SandboxTemplate",
    metadata: {
      name: isEdit
        ? (existingTemplate?.name ?? name)
        : (name ??
          ((base.metadata as Record<string, unknown> | undefined)?.name as string | undefined)),
      ...(baseLabels && Object.keys(baseLabels).length > 0 ? { labels: baseLabels } : {}),
      ...(finalAnnotations ? { annotations: finalAnnotations } : {}),
      // Carry K8s system fields so the API server can validate the optimistic lock.
      ...(existingMeta?.resourceVersion ? { resourceVersion: existingMeta.resourceVersion } : {}),
      ...(existingMeta?.uid ? { uid: existingMeta.uid } : {}),
      ...(existingMeta?.creationTimestamp
        ? { creationTimestamp: existingMeta.creationTimestamp }
        : {}),
    },
    spec,
  }
}

/** Parse CRD YAML and extract the individual form fields. */
function crdToFormFields(yamlStr: string): Partial<FormData> & { parseError?: string } {
  try {
    const parsed = yamlParse(yamlStr) as Record<string, unknown>
    const spec = (parsed.spec as Record<string, unknown>) ?? {}
    const parsedRuntimes = spec.runtimes as
      | Array<{
          name: string
          port?: number
          protocol?: string
          description?: string
          logDir?: string
          readinessProbe?: { httpGet?: { port?: number; path?: string } }
        }>
      | undefined
    const parsedVisibility = spec.visibility as
      | { rules: Array<{ team?: string; users?: string[] }> }
      | undefined
    const metadata = parsed.metadata as Record<string, unknown> | undefined
    const annotations = metadata?.annotations as Record<string, string> | undefined

    return {
      name: ((metadata?.name as string) ?? "") || undefined,
      version: (spec.version as string) ?? "",
      description: (spec.description as string) ?? "",
      idleImage: (spec.idleImage as string) ?? "",
      runtimes:
        parsedRuntimes?.map((r) => ({
          name: r.name,
          port: r.port ?? ("" as unknown as number),
          protocol: r.protocol ?? "TCP",
          description: r.description ?? "",
          logDir: r.logDir ?? "",
          readinessProbeEnabled: !!r.readinessProbe?.httpGet,
          readinessProbePath: r.readinessProbe?.httpGet?.path ?? "/",
          readinessProbePort: r.readinessProbe?.httpGet?.port ?? ("" as unknown as number),
        })) ?? [],
      rules:
        parsedVisibility?.rules?.map((rule) => ({
          team: rule.team ?? "",
          users: rule.users ?? [],
        })) ?? [],
      visibilityPublic: !parsedVisibility || (parsedVisibility.rules?.length ?? 0) === 0,
      docs: annotations?.["agentbox.navix.sh/docs"] ?? "",
    }
  } catch (e) {
    return { parseError: (e as Error).message }
  }
}

// ─── Inner Form ───────────────────────────────────────────────────────────────────

interface UpsertTemplateFormProps {
  template?: AgentSandboxTemplate | null
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
  readOnly?: boolean
}

function UpsertTemplateForm({
  template,
  onOpenChange,
  onSuccess,
  readOnly,
}: UpsertTemplateFormProps) {
  const isEdit = !!template
  const { mutate: createMutate, isPending: isCreating } = useCreateTemplate()
  const { mutate: updateMutate, isPending: isUpdating } = useUpdateTemplate()
  const isMutating = isCreating || isUpdating
  const { t } = useTranslation()

  // Track which direction the last sync went to avoid loops.
  const syncingRef = useRef<"form-to-yaml" | "yaml-to-form" | null>(null)

  // Current version used for semver comparison in Edit mode
  const currentVersion = isEdit ? (template?.version ?? "") : undefined

  // In edit mode the server-provided crdYaml is the canonical "before" YAML.
  // Strip server-managed read-only fields so they don't appear as noise in the diff.
  const buildDefaultCrdYaml = (): string => stripServerManagedFields(template?.crdYaml ?? "")

  // Build default values from the existing template (Edit) or empty (Create)
  const getDefaultValues = (): FormData => {
    if (!template)
      return {
        runtimes: [],
        rules: [],
        visibilityPublic: true,
        idleImage: "",
        crdYaml: "",
        docs: "",
      }
    const fromYaml = crdToFormFields(template.crdYaml ?? "")
    return {
      name: template.name,
      version: fromYaml.version ?? "",
      description: fromYaml.description ?? "",
      idleImage: fromYaml.idleImage ?? "",
      runtimes: fromYaml.runtimes ?? [],
      crdYaml: template.crdYaml ?? "",
      docs: fromYaml.docs ?? "",
      rules: fromYaml.rules ?? [],
      visibilityPublic: fromYaml.visibilityPublic ?? true,
    }
  }

  const {
    register,
    handleSubmit,
    reset,
    control,
    watch,
    setValue,
    trigger,
    formState: { errors },
  } = useForm<FormData>({
    resolver: zodResolver(buildSchema(currentVersion)),
    defaultValues: getDefaultValues(),
  })

  const {
    fields: runtimeFields,
    append: appendRuntime,
    remove: removeRuntime,
    replace: replaceRuntimes,
  } = useFieldArray({ control, name: "runtimes" })

  const {
    fields: ruleFields,
    append: appendRule,
    remove: removeRule,
    replace: replaceRules,
  } = useFieldArray({ control, name: "rules" })

  const visibilityPublic = watch("visibilityPublic")

  // ─── Bidirectional sync: form → YAML ───────────────────────────────────────
  // When any form field changes, rebuild the YAML from the current form state.
  const syncFormToYaml = useCallback(() => {
    if (syncingRef.current === "yaml-to-form") return
    syncingRef.current = "form-to-yaml"
    const data = control._formValues as Partial<FormData>
    const crdObj = formToCrdObject(data, data.name, isEdit, template)
    setValue("crdYaml", yamlStringify(crdObj), { shouldValidate: false })
    setTimeout(() => {
      syncingRef.current = null
    }, 50)
  }, [control, isEdit, template, setValue])

  // Watch form fields that should drive YAML updates.
  const watchedFields = watch([
    "name",
    "version",
    "description",
    "idleImage",
    "runtimes",
    "rules",
    "visibilityPublic",
    "docs",
  ])

  // Use a stable ref for the previous watchedFields value to avoid unnecessary syncs
  const prevWatchedRef = useRef<string>("")
  useEffect(() => {
    const serialized = JSON.stringify(watchedFields)
    if (serialized !== prevWatchedRef.current) {
      prevWatchedRef.current = serialized
      syncFormToYaml()
    }
  }, [watchedFields, syncFormToYaml])

  // ─── Bidirectional sync: YAML → form ───────────────────────────────────────
  const handleYamlChange = useCallback(
    (newYaml: string) => {
      if (syncingRef.current === "form-to-yaml") return
      syncingRef.current = "yaml-to-form"
      const parsed = crdToFormFields(newYaml)
      if (!parsed.parseError) {
        if (!isEdit && parsed.name) setValue("name", parsed.name, { shouldValidate: false })
        if (parsed.version !== undefined)
          setValue("version", parsed.version, { shouldValidate: false })
        if (parsed.description !== undefined)
          setValue("description", parsed.description, { shouldValidate: false })
        if (parsed.idleImage !== undefined)
          setValue("idleImage", parsed.idleImage, { shouldValidate: false })
        if (parsed.runtimes !== undefined)
          replaceRuntimes(parsed.runtimes as FormData["runtimes"] & { id: string }[])
        if (parsed.visibilityPublic !== undefined)
          setValue("visibilityPublic", parsed.visibilityPublic, { shouldValidate: false })
        if (parsed.docs !== undefined) setValue("docs", parsed.docs, { shouldValidate: false })
        if (parsed.rules !== undefined) {
          if (parsed.visibilityPublic) {
            replaceRules([])
          } else {
            replaceRules(parsed.rules as FormData["rules"] & { id: string }[])
          }
        }
      }
      setTimeout(() => {
        syncingRef.current = null
      }, 50)
    },
    [isEdit, setValue, replaceRuntimes, replaceRules],
  )

  // ─── Diff preview dialog (edit mode only) ────────────────────────────────────

  // originalYaml is the clean server-side representation built at form init time.
  // It is used as the "before" side of the diff when the user clicks Save.
  const originalYaml = isEdit ? buildDefaultCrdYaml() : ""
  const [pendingYaml, setPendingYaml] = useState<string | null>(null)

  // ─── Submit ───────────────────────────────────────────────────────────────────

  const doSubmit = async (crdYaml: string) => {
    // Convert the YAML the user edited to a JSON string for the API.
    let crdJson: string
    try {
      crdJson = JSON.stringify(yamlParse(crdYaml))
    } catch (e) {
      toast.error(`Invalid YAML: ${(e as Error).message}`)
      return
    }
    try {
      if (isEdit) {
        await new Promise<void>((resolve, reject) => {
          updateMutate(
            { name: template!.name, crdJson },
            {
              onSuccess: () => {
                toast.success(t("templates.updatedSuccess"))
                resolve()
              },
              onError: reject,
            },
          )
        })
      } else {
        await new Promise<void>((resolve, reject) => {
          createMutate(
            { crdJson },
            {
              onSuccess: () => {
                toast.success(t("templates.createdSuccess"))
                resolve()
              },
              onError: reject,
            },
          )
        })
      }
      reset()
      onOpenChange(false)
      onSuccess?.()
    } catch {
      // Error toast handled by bff afterResponse hook
    }
  }

  const onSubmit = async (data: FormData) => {
    if (isEdit) {
      // Show diff preview before committing
      setPendingYaml(data.crdYaml)
    } else {
      await doSubmit(data.crdYaml)
    }
  }

  // ─── Export YAML ────────────────────────────────────────────────────────────

  const handleExport = async () => {
    if (!readOnly) {
      const fieldsToValidate = (Object.keys(control._fields) as (keyof FormData)[]).filter(
        (k) => k !== "version",
      )
      const ok = await trigger(fieldsToValidate)
      if (!ok) {
        toast.error("Please fix form errors before exporting.")
        return
      }
    }

    const data = control._formValues as FormData
    const yamlStr = data.crdYaml || ""
    if (!yamlStr.trim()) {
      toast.error("No YAML to export.")
      return
    }

    const blob = new Blob([yamlStr], { type: "text/yaml" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    const name = isEdit ? template!.name : data.name || "template"
    a.download = `${name}.yaml`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* Toolbar */}
      <div className="border-border flex items-center gap-2 border-b px-6 py-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleExport}
          className="h-7 font-mono text-xs tracking-wider uppercase"
        >
          <Download className="mr-1.5 h-3 w-3" />
          {t("templates.exportYaml")}
        </Button>
      </div>

      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-1 flex-col overflow-hidden">
        <fieldset disabled={readOnly} className="contents">
          <div className="flex flex-1 flex-col gap-6 overflow-y-auto px-6 py-4">
            {/* ── Section 1: Basic Info ── */}
            <section className="flex flex-col gap-3">
              <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("templates.form.basicInfo")}
              </h3>

              {(!isEdit || readOnly) && (
                <Field data-invalid={!!errors.name}>
                  <FieldLabel className="text-muted-foreground font-mono text-xs tracking-[0.12em] uppercase">
                    {t("templates.form.name")}{" "}
                    {!readOnly && <span className="text-destructive">*</span>}
                  </FieldLabel>
                  <Input
                    {...register("name")}
                    placeholder="my-template"
                    className="border-border bg-background h-9 font-mono text-sm"
                  />
                  <FieldError errors={[errors.name]} className="font-mono text-xs" />
                  <FieldDescription>
                    Unique name for this template. Must follow Kubernetes naming rules (lowercase
                    alphanumeric and hyphens).
                  </FieldDescription>
                </Field>
              )}

              <div className="grid grid-cols-2 gap-3">
                <Field data-invalid={!!errors.version}>
                  <FieldLabel className="text-muted-foreground font-mono text-xs tracking-[0.12em] uppercase">
                    {t("templates.form.version")}
                  </FieldLabel>
                  <div className="flex items-center gap-2">
                    <Input
                      {...register("version")}
                      placeholder="1.0.0"
                      className="border-border bg-background h-9 flex-1 font-mono text-sm"
                    />
                    {isEdit && !readOnly && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() =>
                          setValue("version", patchIncrement(currentVersion ?? "0.0.0"))
                        }
                        className="h-9 shrink-0 font-mono text-xs tracking-wider"
                        title={t("templates.form.patchIncrement")}
                      >
                        ↑ patch
                      </Button>
                    )}
                  </div>
                  <FieldError errors={[errors.version]} className="font-mono text-xs" />
                  <FieldDescription>{t("templates.form.versionDesc")}</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel className="text-muted-foreground font-mono text-xs tracking-[0.12em] uppercase">
                    {t("templates.form.description")}
                  </FieldLabel>
                  <Input
                    {...register("description")}
                    placeholder="Optional description"
                    className="border-border bg-background h-9 font-mono text-sm"
                  />
                  <FieldDescription>{t("templates.form.descriptionOptional")}</FieldDescription>
                </Field>
              </div>

              <Field data-invalid={!!errors.idleImage}>
                <FieldLabel className="text-muted-foreground font-mono text-xs tracking-[0.12em] uppercase">
                  {t("templates.form.idleImage")} <span className="text-destructive">*</span>
                </FieldLabel>
                <Input
                  {...register("idleImage")}
                  placeholder="docker.io/myorg/idle:latest"
                  className="border-border bg-background h-9 font-mono text-sm"
                />
                <FieldError errors={[errors.idleImage]} className="font-mono text-xs" />
                <FieldDescription>{t("templates.form.idleImageDesc")}</FieldDescription>
              </Field>
            </section>

            <hr className="border-border" />

            {/* ── Section 2: Runtimes ── */}
            <section className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("templates.form.runtimes")}
                </h3>
                {!readOnly && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      appendRuntime({
                        name: "",
                        port: "" as unknown as number,
                        protocol: "TCP",
                        description: "",
                        logDir: "",
                      })
                    }
                    className="h-6 font-mono text-xs tracking-wider uppercase"
                  >
                    <Plus className="mr-1 h-3 w-3" />
                    Add
                  </Button>
                )}
              </div>
              <FieldDescription>{t("templates.form.runtimesDesc")}</FieldDescription>
              {runtimeFields.map((field, idx) => (
                <div key={field.id} className="flex flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <Input
                      {...register(`runtimes.${idx}.name`)}
                      placeholder="name"
                      className={cn(
                        "border-border bg-background h-8 flex-1 font-mono text-xs",
                        errors.runtimes?.[idx]?.name && "border-destructive",
                      )}
                    />
                    <Input
                      {...register(`runtimes.${idx}.port`)}
                      type="number"
                      placeholder="port"
                      className={cn(
                        "border-border bg-background h-8 w-20 font-mono text-xs",
                        errors.runtimes?.[idx]?.port && "border-destructive",
                      )}
                    />
                    <Controller
                      control={control}
                      name={`runtimes.${idx}.protocol`}
                      render={({ field: f }) => (
                        <Combobox
                          value={f.value ?? "TCP"}
                          onValueChange={(val) => f.onChange(val || "TCP")}
                          items={["TCP", "UDP", "SCTP"]}
                        >
                          <ComboboxInput
                            placeholder="Protocol"
                            className="h-8 w-24 font-mono text-xs"
                          />
                          <ComboboxContent>
                            <ComboboxList>
                              {(p) => (
                                <ComboboxItem key={p} value={p} className="font-mono text-xs">
                                  {p}
                                </ComboboxItem>
                              )}
                            </ComboboxList>
                          </ComboboxContent>
                        </Combobox>
                      )}
                    />
                    {!readOnly && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => removeRuntime(idx)}
                        className="text-muted-foreground hover:text-destructive h-8 w-8 shrink-0"
                      >
                        <Minus className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <Input
                      {...register(`runtimes.${idx}.description`)}
                      placeholder="description (optional)"
                      className="border-border bg-background h-8 flex-1 font-mono text-xs"
                    />
                    <Input
                      {...register(`runtimes.${idx}.logDir`)}
                      placeholder="log dir (optional)"
                      className="border-border bg-background h-8 flex-1 font-mono text-xs"
                    />
                    {!readOnly && <div className="h-8 w-8 shrink-0" />}
                  </div>
                  {/* Readiness probe row */}
                  <div className="flex items-center gap-2">
                    <Switch
                      id={`probe-${idx}`}
                      size="sm"
                      checked={watch(`runtimes.${idx}.readinessProbeEnabled`) ?? false}
                      onCheckedChange={(v) => setValue(`runtimes.${idx}.readinessProbeEnabled`, v)}
                    />
                    <Label
                      htmlFor={`probe-${idx}`}
                      className="text-muted-foreground font-mono text-xs"
                    >
                      {t("templates.form.readinessProbe")}
                    </Label>
                    {watch(`runtimes.${idx}.readinessProbeEnabled`) && (
                      <>
                        <Input
                          {...register(`runtimes.${idx}.readinessProbePath`)}
                          placeholder="/healthz"
                          className="border-border bg-background h-8 flex-1 font-mono text-xs"
                        />
                        <Input
                          {...register(`runtimes.${idx}.readinessProbePort`)}
                          type="number"
                          placeholder="port (default: same as runtime)"
                          className="border-border bg-background h-8 w-32 font-mono text-xs"
                        />
                      </>
                    )}
                  </div>
                  <FieldError
                    errors={[
                      errors.runtimes?.[idx]?.name,
                      errors.runtimes?.[idx]?.port,
                      errors.runtimes?.[idx]?.readinessProbePort,
                    ]}
                    className="font-mono text-xs"
                  />
                </div>
              ))}
              {runtimeFields.length === 0 && (
                <p className="text-muted-foreground font-mono text-xs">
                  {t("templates.form.noRuntimesConfigured")}
                </p>
              )}
            </section>

            <hr className="border-border" />

            {/* ── Section 3: Visibility ── */}
            <section className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                  {t("templates.form.visibility")}
                </h3>
                <div className="flex items-center gap-2">
                  <Label
                    htmlFor="visibility-switch"
                    className="text-muted-foreground font-mono text-xs tracking-wider uppercase"
                  >
                    {visibilityPublic ? t("templates.form.public") : t("templates.form.restricted")}
                  </Label>
                  <Switch
                    id="visibility-switch"
                    size="sm"
                    checked={visibilityPublic ?? true}
                    onCheckedChange={(checked: boolean) => {
                      setValue("visibilityPublic", checked)
                      if (checked) replaceRules([])
                    }}
                  />
                </div>
              </div>
              <FieldDescription>{t("templates.form.visibilityDesc")}</FieldDescription>

              {!visibilityPublic && (
                <>
                  {!readOnly && (
                    <div className="flex items-center justify-end">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => appendRule({ team: "", users: [] })}
                        className="h-6 font-mono text-xs tracking-wider uppercase"
                      >
                        <Plus className="mr-1 h-3 w-3" />
                        {t("templates.form.addRule")}
                      </Button>
                    </div>
                  )}
                  {ruleFields.map((field, idx) => (
                    <RuleRow
                      key={field.id}
                      control={control}
                      index={idx}
                      onRemove={() => removeRule(idx)}
                      readOnly={readOnly}
                    />
                  ))}
                  {ruleFields.length === 0 && (
                    <p className="text-muted-foreground font-mono text-xs">
                      {t("templates.form.noRulesConfigured")}
                    </p>
                  )}
                </>
              )}
            </section>

            <hr className="border-border" />

            {/* ── Section 4: Documentation ── */}
            <section className="flex flex-col gap-3">
              <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("templates.form.docs")}
              </h3>
              <Field>
                <Textarea
                  {...register("docs")}
                  placeholder={t("templates.form.docsPlaceholder")}
                  className="border-border bg-background min-h-32 resize-y font-mono text-xs"
                  readOnly={readOnly}
                />
                <FieldDescription>
                  {t("templates.form.docsDesc")}
                  <span className="mt-1 flex flex-wrap gap-1">
                    {["${AGBX_POOL_NAME}", "${AGBX_CLUSTER_ID}", "${AGBX_API_KEY}"].map((v) => (
                      <code
                        key={v}
                        className="bg-secondary rounded px-1 py-0.5 font-mono text-[10px]"
                      >
                        {v}
                      </code>
                    ))}
                  </span>
                </FieldDescription>
              </Field>
            </section>

            <hr className="border-border" />

            {/* ── Section 5: CRD YAML (primary input) ── */}
            <section className="flex flex-col gap-3">
              <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("templates.form.crdYaml")} <span className="text-destructive">*</span>
              </h3>
              <Field data-invalid={!!errors.crdYaml}>
                <Controller
                  control={control}
                  name="crdYaml"
                  render={({ field }) => (
                    <Textarea
                      value={field.value}
                      onChange={(e) => {
                        field.onChange(e.target.value)
                        handleYamlChange(e.target.value)
                      }}
                      placeholder={t("templates.form.crdYamlPlaceholder")}
                      className="border-border bg-background min-h-120 resize-y font-mono text-xs"
                      readOnly={readOnly}
                    />
                  )}
                />
                <FieldError errors={[errors.crdYaml]} className="font-mono text-xs" />
                <FieldDescription>{t("templates.form.crdYamlDesc")}</FieldDescription>
              </Field>
            </section>
          </div>
        </fieldset>

        {/* Footer */}
        {!readOnly && (
          <div className="border-border flex items-center justify-end gap-2 border-t px-6 py-3">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                reset()
                onOpenChange(false)
              }}
              className="font-mono text-xs tracking-wider uppercase"
            >
              <X className="mr-1.5 h-3.5 w-3.5" />
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              disabled={isMutating}
              className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
            >
              {isMutating ? (
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              ) : isEdit ? (
                <Pencil className="mr-1.5 h-3.5 w-3.5" />
              ) : (
                <Plus className="mr-1.5 h-3.5 w-3.5" />
              )}
              {isEdit ? t("templates.saveChanges") : t("common.create")}
            </Button>
          </div>
        )}
      </form>

      {/* Diff preview dialog — shown before saving in edit mode */}
      <ConfirmDiffDialog
        open={pendingYaml !== null}
        oldYaml={originalYaml}
        newYaml={pendingYaml ?? ""}
        isPending={isMutating}
        onConfirm={async () => {
          if (pendingYaml !== null) await doSubmit(pendingYaml)
          setPendingYaml(null)
        }}
        onCancel={() => setPendingYaml(null)}
      />
    </div>
  )
}

// ─── ConfirmDiffDialog ────────────────────────────────────────────────────────

interface ConfirmDiffDialogProps {
  open: boolean
  oldYaml: string
  newYaml: string
  isPending: boolean
  onConfirm: () => void
  onCancel: () => void
}

function ConfirmDiffDialog({
  open,
  oldYaml,
  newYaml,
  isPending,
  onConfirm,
  onCancel,
}: ConfirmDiffDialogProps) {
  const { t } = useTranslation()
  const normalizeYaml = (y: string) => {
    try {
      return yamlStringify(yamlParse(y))
    } catch {
      return y
    }
  }
  const normalizedOld = normalizeYaml(oldYaml)
  const normalizedNew = normalizeYaml(newYaml)
  const hasDiffNormalized = normalizedOld !== normalizedNew

  return (
    <LargeDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onCancel()
      }}
      title={t("templates.confirmSave")}
      description={t("templates.confirmSaveDesc")}
      actions={
        <>
          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
            disabled={isPending}
            className="h-7 font-mono text-xs tracking-wider uppercase"
          >
            <X className="mr-1.5 h-3 w-3" />
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            disabled={isPending}
            onClick={onConfirm}
            className="bg-foreground text-background hover:bg-foreground/90 h-7 font-mono text-xs tracking-wider uppercase"
          >
            {isPending ? (
              <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
            ) : (
              <Pencil className="mr-1.5 h-3 w-3" />
            )}
            {t("templates.confirmAndSave")}
          </Button>
        </>
      }
    >
      {hasDiffNormalized ? (
        <YamlDiffView oldYaml={normalizedOld} newYaml={normalizedNew} />
      ) : (
        <p className="text-muted-foreground py-8 text-center font-mono text-sm">
          {t("templates.noDiffDetected")}
        </p>
      )}
    </LargeDialog>
  )
}

// ─── RuleRow ─────────────────────────────────────────────────────────────────────

interface RuleRowProps {
  control: ReturnType<typeof useForm<FormData>>["control"]
  index: number
  onRemove: () => void
  readOnly?: boolean
}

function RuleRow({ control, index, onRemove, readOnly }: RuleRowProps) {
  const teamValue = useWatch({ control, name: `rules.${index}.team` })
  const { data: teams } = useQuery({ ...adminTeamsQueryOptions(), enabled: !readOnly })
  const { data: users } = useQuery({
    ...adminUsersByTeamQueryOptions(teamValue),
    enabled: !readOnly && !!teamValue,
  })
  const chipsAnchor = useComboboxAnchor()
  const { t } = useTranslation()

  return (
    <div className="border-border bg-background flex flex-col gap-2 border p-3">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground font-mono text-xs uppercase">
          {t("templates.form.rule")} {index + 1}
        </span>
        {!readOnly && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={onRemove}
            className="text-muted-foreground hover:text-destructive h-6 w-6"
          >
            <Minus className="h-3 w-3" />
          </Button>
        )}
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field>
          <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("templates.form.team")}
          </FieldLabel>
          <Controller
            control={control}
            name={`rules.${index}.team`}
            render={({ field }) => (
              <Combobox
                value={field.value ?? null}
                onValueChange={(val) => field.onChange(val)}
                items={teams}
              >
                <ComboboxInput
                  placeholder={t("auth.selectTeam")}
                  showClear
                  className="h-9 font-mono text-sm"
                />
                <ComboboxContent>
                  <ComboboxEmpty>{t("auth.noTeamsFound")}</ComboboxEmpty>
                  <ComboboxList>
                    {(team: string) => (
                      <ComboboxItem key={team} value={team} className="font-mono text-xs">
                        {team}
                      </ComboboxItem>
                    )}
                  </ComboboxList>
                </ComboboxContent>
              </Combobox>
            )}
          />
        </Field>

        <Field>
          <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("templates.form.users")}
          </FieldLabel>
          <Controller
            control={control}
            name={`rules.${index}.users`}
            render={({ field }) => (
              <Combobox
                value={field.value ?? []}
                onValueChange={(val) => field.onChange(val)}
                items={users}
                multiple
              >
                <ComboboxChips ref={chipsAnchor} className="min-h-9">
                  {(field.value ?? []).map((u: string) => (
                    <ComboboxChip key={u} className="font-mono text-xs">
                      {u}
                    </ComboboxChip>
                  ))}
                  <ComboboxChipsInput
                    placeholder={
                      (field.value ?? []).length === 0
                        ? t("templates.form.anyUser")
                        : t("templates.form.addUser")
                    }
                    className="font-mono text-xs"
                  />
                </ComboboxChips>
                <ComboboxContent anchor={chipsAnchor}>
                  <ComboboxEmpty>
                    {teamValue ? t("auth.noUsersFound") : t("templates.form.selectATeamFirst")}
                  </ComboboxEmpty>
                  <ComboboxList>
                    {(u: string) => (
                      <ComboboxItem key={u} value={u} className="font-mono text-xs">
                        {u}
                      </ComboboxItem>
                    )}
                  </ComboboxList>
                </ComboboxContent>
              </Combobox>
            )}
          />
        </Field>
      </div>
    </div>
  )
}
