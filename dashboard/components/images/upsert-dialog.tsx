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

import { useRef } from "react"
import { useForm, Controller, useFieldArray } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { useAtomValue } from "jotai"
import { Globe, HardDrive, Upload, Download } from "lucide-react"
import { toast } from "sonner"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Field, FieldLabel, FieldError, FieldDescription } from "@/components/ui/field"
import { clustersAtom } from "@/lib/atoms"
import { useCreateImageDataset, useUpdateImageDataset } from "@/lib/queries/images"
import type { ImageDataset } from "@/components/images/data"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { useState } from "react"

// ─── Zod schema ──────────────────────────────────────────────────────────────

const clusterDocSchema = z.object({
  clusterId: z.string().min(1),
  markdown: z.string(),
})

const schema = z.object({
  id: z
    .string()
    .min(1, "ID is required")
    .regex(/^[a-z0-9-]+$/, "Use lowercase letters, digits, hyphens"),
  name: z.string().min(1, "Name is required"),
  description: z.string().min(1, "Description is required"),
  imageCount: z.coerce.number().int().min(0, "Must be ≥ 0"),
  huggingFaceUrl: z.string().url("Must be a valid URL"),
  tags: z.string(), // comma-separated
  clusterDocs: z.array(clusterDocSchema),
})

type FormValues = z.infer<typeof schema>

// ─── Inner Form ───────────────────────────────────────────────────────────────

const FORM_ID = "upsert-image-form"

function UpsertImageForm({
  onOpenChange,
  dataset,
}: {
  onOpenChange: (open: boolean) => void
  dataset?: ImageDataset | null
}) {
  const { t } = useTranslation()
  const clustersData = useAtomValue(clustersAtom)
  const clusters = clustersData.clusters

  const isEditing = !!dataset

  // Always include ALL clusters as tabs. For each cluster, pre-fill from dataset
  // if editing, otherwise default to empty string.
  const defaultClusterDocs = clusters.map((c) => ({
    clusterId: c.id,
    markdown: dataset?.clusterDocs[c.id] ?? "",
  }))

  const {
    register,
    control,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
    watch,
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      id: dataset?.id ?? "",
      name: dataset?.name ?? "",
      description: dataset?.description ?? "",
      imageCount: dataset?.imageCount ?? 0,
      huggingFaceUrl: dataset?.huggingFaceUrl ?? "https://huggingface.co/datasets/",
      tags: dataset?.tags.join(", ") ?? "",
      clusterDocs: defaultClusterDocs,
    },
  })

  const { fields } = useFieldArray({ control, name: "clusterDocs" })
  const [activeClusterIdx, setActiveClusterIdx] = useState<number>(0)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const createMutation = useCreateImageDataset()
  const updateMutation = useUpdateImageDataset()

  const onSubmit = handleSubmit(async (values) => {
    const clusterDocs: Record<string, string> = {}
    for (const entry of values.clusterDocs) {
      if (entry.markdown.trim()) {
        clusterDocs[entry.clusterId] = entry.markdown
      }
    }

    const payload: ImageDataset = {
      id: values.id,
      name: values.name,
      description: values.description,
      imageCount: values.imageCount,
      category: dataset?.category ?? "Benchmark",
      source: "HuggingFace",
      huggingFaceUrl: values.huggingFaceUrl,
      tags: values.tags
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
      clusterDocs,
    }

    try {
      if (isEditing) {
        await updateMutation.mutateAsync(payload)
        toast.success(`Dataset "${values.name}" updated`)
      } else {
        await createMutation.mutateAsync(payload)
        toast.success(`Dataset "${values.name}" created`)
      }
      onOpenChange(false)
    } catch (e) {
      toast.error((e as Error).message)
    }
  })

  // ── Export JSON ─────────────────────────────────────────────────────────────
  const handleExport = () => {
    const values = watch()
    const clusterDocs: Record<string, string> = {}
    for (const entry of values.clusterDocs) {
      if (entry.markdown.trim()) {
        clusterDocs[entry.clusterId] = entry.markdown
      }
    }
    const exportData: ImageDataset = {
      id: values.id,
      name: values.name,
      description: values.description,
      imageCount: values.imageCount,
      category: dataset?.category ?? "Benchmark",
      source: "HuggingFace",
      huggingFaceUrl: values.huggingFaceUrl,
      tags: values.tags
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
      clusterDocs,
    }
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: "application/json" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = `${values.id || "dataset"}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  // ── Import JSON ─────────────────────────────────────────────────────────────
  const handleImport = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => {
      try {
        const parsed = JSON.parse(ev.target?.result as string) as Partial<ImageDataset>
        const docEntries = clusters.map((c) => ({
          clusterId: c.id,
          markdown: parsed.clusterDocs?.[c.id] ?? "",
        }))
        reset({
          id: parsed.id ?? "",
          name: parsed.name ?? "",
          description: parsed.description ?? "",
          imageCount: parsed.imageCount ?? 0,
          huggingFaceUrl: parsed.huggingFaceUrl ?? "https://huggingface.co/datasets/",
          tags: (parsed.tags ?? []).join(", "),
          clusterDocs: docEntries,
        })
        toast.success("JSON imported successfully")
      } catch {
        toast.error("Invalid JSON file")
      }
    }
    reader.readAsText(file)
    // Reset input so the same file can be re-imported
    e.target.value = ""
  }

  const safeActiveIdx = Math.min(activeClusterIdx, Math.max(0, fields.length - 1))

  return (
    <>
      {/* ── Scrollable body ─────────────────────────────────────── */}
      <div className="flex flex-1 flex-col gap-5 overflow-y-auto px-6 py-5">
        {/* Hidden file input for JSON import */}
        <input
          ref={fileInputRef}
          type="file"
          accept=".json,application/json"
          className="hidden"
          onChange={handleImport}
        />

        <form id={FORM_ID} onSubmit={onSubmit} className="contents">
          {/* Basic Info */}
          <div className="grid grid-cols-2 gap-4">
            <Field data-invalid={!!errors.id}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                ID <span className="text-destructive">*</span>
              </FieldLabel>
              <Input
                id="id"
                {...register("id")}
                disabled={isEditing}
                placeholder="swe-bench-verified"
                className="border-border bg-background h-9 font-mono text-sm"
              />
              <FieldError errors={[errors.id]} className="font-mono text-xs" />
              <FieldDescription>Lowercase letters, digits, hyphens only.</FieldDescription>
            </Field>

            <Field data-invalid={!!errors.name}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                Name <span className="text-destructive">*</span>
              </FieldLabel>
              <Input
                id="name"
                {...register("name")}
                placeholder="SWE-Bench Verified"
                className="border-border bg-background h-9 text-sm"
              />
              <FieldError errors={[errors.name]} className="font-mono text-xs" />
            </Field>
          </div>

          <Field data-invalid={!!errors.description}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              Description <span className="text-destructive">*</span>
            </FieldLabel>
            <Textarea
              id="description"
              {...register("description")}
              rows={3}
              placeholder="A curated subset of SWE-bench..."
              className="resize-none text-sm"
            />
            <FieldError errors={[errors.description]} className="font-mono text-xs" />
          </Field>

          <div className="grid grid-cols-2 gap-4">
            <Field data-invalid={!!errors.imageCount}>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                Image Count
              </FieldLabel>
              <Input
                id="imageCount"
                type="number"
                min={0}
                {...register("imageCount")}
                className="border-border bg-background h-9 text-sm"
              />
              <FieldError errors={[errors.imageCount]} className="font-mono text-xs" />
            </Field>

            <Field>
              <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                Tags
              </FieldLabel>
              <Input
                id="tags"
                {...register("tags")}
                placeholder="SWE-Agent, Benchmark"
                className="border-border bg-background h-9 text-sm"
              />
              <FieldDescription>Comma-separated.</FieldDescription>
            </Field>
          </div>

          <Field data-invalid={!!errors.huggingFaceUrl}>
            <FieldLabel className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
              HuggingFace URL <span className="text-destructive">*</span>
            </FieldLabel>
            <Input
              id="huggingFaceUrl"
              {...register("huggingFaceUrl")}
              placeholder="https://huggingface.co/datasets/..."
              className="border-border bg-background h-9 font-mono text-sm"
            />
            <FieldError errors={[errors.huggingFaceUrl]} className="font-mono text-xs" />
          </Field>

          {/* ── Per-cluster docs ─────────────────────────────────── */}
          <div className="space-y-3">
            {/* Section header */}
            <div className="flex items-center gap-1.5">
              <Globe className="text-muted-foreground h-3.5 w-3.5" />
              <span className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
                Per-Cluster Usage Docs
              </span>
              <span className="text-muted-foreground text-xs font-normal">(Markdown)</span>
            </div>

            <p className="text-muted-foreground text-xs">
              Clusters with non-empty docs will display this image. Leave a tab empty to hide the
              image from that cluster.
            </p>

            {fields.length === 0 ? (
              <div className="border-border rounded-lg border border-dashed px-4 py-6 text-center">
                <p className="text-muted-foreground text-sm">No clusters configured.</p>
              </div>
            ) : (
              <div className="border-border rounded-lg border">
                {/* Tab strip */}
                <div className="border-border flex flex-wrap gap-0 border-b">
                  {fields.map((field, idx) => {
                    const cluster = clusters.find((c) => c.id === field.clusterId)
                    const hasContent = !!watch(`clusterDocs.${idx}.markdown`)?.trim()
                    const isActive = safeActiveIdx === idx
                    return (
                      <button
                        key={field.id}
                        type="button"
                        onClick={() => setActiveClusterIdx(idx)}
                        className={cn(
                          "flex items-center gap-1.5 border-b-2 px-3 py-2 font-mono text-xs transition-colors",
                          isActive
                            ? "border-foreground text-foreground -mb-px"
                            : "text-muted-foreground hover:text-foreground border-transparent",
                        )}
                      >
                        {cluster?.name ?? field.clusterId}
                        <span
                          className={cn(
                            "h-1.5 w-1.5 rounded-full",
                            hasContent ? "bg-emerald-500" : "bg-muted-foreground/40",
                          )}
                        />
                      </button>
                    )
                  })}
                </div>

                {/* Active panel */}
                {fields.map((field, idx) => {
                  if (idx !== safeActiveIdx) return null
                  return (
                    <div key={field.id} className="flex flex-col gap-2 p-3">
                      <Badge variant="outline" className="w-fit font-mono text-xs">
                        {field.clusterId}
                      </Badge>
                      <Controller
                        control={control}
                        name={`clusterDocs.${idx}.markdown`}
                        render={({ field: f }) => (
                          <Textarea
                            {...f}
                            rows={16}
                            placeholder={`## ${clusters.find((c) => c.id === field.clusterId)?.name ?? field.clusterId}\n\nUsage instructions in Markdown...`}
                            className="resize-y font-mono text-xs leading-relaxed"
                          />
                        )}
                      />
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </form>
      </div>

      {/* ── Sticky footer ────────────────────────────────────────── */}
      <div className="border-border flex shrink-0 items-center gap-2 border-t px-6 py-3">
        {/* Import / Export */}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 gap-1.5 text-xs"
          onClick={() => fileInputRef.current?.click()}
        >
          <Upload className="h-3 w-3" />
          Import JSON
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 gap-1.5 text-xs"
          onClick={handleExport}
        >
          <Download className="h-3 w-3" />
          Export JSON
        </Button>

        <div className="ml-auto flex items-center gap-2">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            form={FORM_ID}
            disabled={isSubmitting || createMutation.isPending || updateMutation.isPending}
            className="bg-foreground text-background hover:bg-foreground/90"
          >
            {isEditing ? t("common.save") : t("common.create")}
          </Button>
        </div>
      </div>
    </>
  )
}

// ─── Sheet shell ─────────────────────────────────────────────────────────────

interface UpsertImageDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  dataset?: ImageDataset | null
}

export function UpsertImageDialog({ open, onOpenChange, dataset }: UpsertImageDialogProps) {
  const isEditing = !!dataset
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-2xl"
      >
        <SheetHeader className="border-border border-b px-6 py-4">
          <div className="flex items-center gap-3">
            <div className="bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg">
              <HardDrive className="h-4 w-4" />
            </div>
            <SheetTitle className="font-mono font-semibold tracking-tight">
              {isEditing ? `Edit — ${dataset.name}` : "New Image Dataset"}
            </SheetTitle>
          </div>
        </SheetHeader>

        {open && <UpsertImageForm onOpenChange={onOpenChange} dataset={dataset} />}
      </SheetContent>
    </Sheet>
  )
}
