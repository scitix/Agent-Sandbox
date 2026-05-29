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
import { useQuery } from "@tanstack/react-query"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { Search, HardDrive, Plus, Pencil, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { useTranslation } from "@/lib/i18n"
import { isActualAdminAtom } from "@/lib/atoms"
import { imagesCatalogQueryOptions, useDeleteImageDataset } from "@/lib/queries/images"
import type { ImageDataset } from "@/components/images/data"
import { UpsertImageDialog } from "@/components/images/upsert-dialog"
import { cn } from "@/lib/utils"

// ---------------------------------------------------------------------------
// Image Card
// ---------------------------------------------------------------------------

function ImageCard({
  dataset,
  onViewUsage,
  onEdit,
  onDelete,
  isAdmin,
}: {
  dataset: ImageDataset
  onViewUsage: (d: ImageDataset) => void
  onEdit?: (d: ImageDataset) => void
  onDelete?: (d: ImageDataset) => void
  isAdmin: boolean
}) {
  const { t } = useTranslation()

  return (
    <div
      className={cn(
        "group flex cursor-pointer flex-col gap-4 rounded-xl border p-5",
        "bg-card hover:bg-accent/30 transition-colors duration-150",
      )}
      onClick={() => onViewUsage(dataset)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onViewUsage(dataset)
      }}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
            <HardDrive className="text-muted-foreground h-4 w-4" />
          </div>
          <div className="min-w-0">
            <p className="truncate font-mono text-sm font-semibold tracking-tight">
              {dataset.name}
            </p>
            <p className="text-muted-foreground font-mono text-xs">
              {t("datasets.imageCount", { count: dataset.imageCount })}
            </p>
          </div>
        </div>
        {/* Admin actions — only visible on hover */}
        {isAdmin && (
          <div
            className="flex shrink-0 gap-1 opacity-0 transition-opacity group-hover:opacity-100"
            onClick={(e) => e.stopPropagation()}
          >
            <Button
              variant="ghost"
              size="icon-sm"
              className="h-7 w-7"
              onClick={() => onEdit?.(dataset)}
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground hover:text-destructive h-7 w-7"
              onClick={() => onDelete?.(dataset)}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
      </div>

      {/* Description */}
      <p className="text-muted-foreground line-clamp-3 text-xs leading-relaxed">
        {dataset.description}
      </p>

      {/* Tags */}
      {dataset.tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {dataset.tags.map((tag) => (
            <span
              key={tag}
              className="bg-secondary text-muted-foreground rounded px-2 py-0.5 font-mono text-xs"
            >
              {tag}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Delete Confirm Dialog
// ---------------------------------------------------------------------------

function DeleteImageDialog({
  dataset,
  onOpenChange,
}: {
  dataset: ImageDataset | null
  onOpenChange: (open: boolean) => void
}) {
  const deleteMutation = useDeleteImageDataset()

  if (!dataset) return null

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(dataset.id)
      toast.success(`"${dataset.name}" deleted`)
      onOpenChange(false)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <Dialog open={!!dataset} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Delete Dataset</DialogTitle>
          <DialogDescription>
            This will permanently remove <strong>{dataset.name}</strong> from the catalog. This
            action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteMutation.isPending}>
            Delete
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function ImagesPage() {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()
  const router = useRouter()
  const isAdmin = useAtomValue(isActualAdminAtom)

  const [search, setSearch] = useState("")
  const [upsertOpen, setUpsertOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<ImageDataset | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ImageDataset | null>(null)

  const { data: allDatasets = [] } = useQuery(imagesCatalogQueryOptions())

  // Only show datasets that have docs for the current cluster
  const availableDatasets = useMemo(
    () => allDatasets.filter((d) => clusterID in d.clusterDocs),
    [allDatasets, clusterID],
  )

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim()
    if (!q) return availableDatasets
    return availableDatasets.filter(
      (d) =>
        d.name.toLowerCase().includes(q) ||
        d.description.toLowerCase().includes(q) ||
        d.tags.some((tag) => tag.toLowerCase().includes(q)),
    )
  }, [availableDatasets, search])

  const handleViewUsage = (dataset: ImageDataset) => {
    router.push(`${clusterPath(clusterID, "datasets", locale)}/${encodeURIComponent(dataset.id)}`)
  }

  const handleEdit = (dataset: ImageDataset) => {
    setEditTarget(dataset)
    setUpsertOpen(true)
  }

  const handleCreate = () => {
    setEditTarget(null)
    setUpsertOpen(true)
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* Toolbar */}
        <div className="border-border border-b px-6 py-3">
          <div className="flex flex-wrap items-center gap-3">
            {/* Search */}
            <div className="relative flex-1" style={{ minWidth: "200px", maxWidth: "360px" }}>
              <Search className="text-muted-foreground absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
              <Input
                placeholder={t("datasets.searchPlaceholder")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 pl-9 font-mono text-sm"
              />
            </div>

            {/* Admin: new dataset */}
            {isAdmin && (
              <Button
                size="sm"
                onClick={handleCreate}
                className="bg-foreground text-background hover:bg-foreground/90 ml-auto h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
              >
                <Plus className="h-3 w-3" />
                New Dataset
              </Button>
            )}
          </div>
        </div>

        {/* Cards grid */}
        <div className="px-6 py-5">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <HardDrive className="text-muted-foreground/40 mb-3 h-10 w-10" />
              <p className="text-muted-foreground text-sm font-medium">{t("datasets.noResults")}</p>
              <p className="text-muted-foreground mt-1 text-xs">{t("datasets.noResultsDesc")}</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {filtered.map((dataset) => (
                <ImageCard
                  key={dataset.id}
                  dataset={dataset}
                  onViewUsage={handleViewUsage}
                  onEdit={handleEdit}
                  onDelete={setDeleteTarget}
                  isAdmin={isAdmin}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      <UpsertImageDialog
        open={upsertOpen}
        onOpenChange={(open) => {
          setUpsertOpen(open)
          if (!open) setEditTarget(null)
        }}
        dataset={editTarget}
      />

      <DeleteImageDialog
        dataset={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
    </div>
  )
}
