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

import { use, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { ExternalLink, HardDrive, Pencil, Trash2, Copy, Check } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { UpsertImageDialog } from "@/components/images/upsert-dialog"
import type { ImageDataset } from "@/lib/api/hub-client"
import { imagesCatalogQueryOptions, useDeleteImageDataset } from "@/lib/queries/images"
import { isActualAdminAtom } from "@/lib/atoms"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

interface PageProps {
  params: Promise<{ clusterID: string; id: string; locale: string }>
}

function CopyPageButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" onClick={handleCopy}>
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      {copied ? t("common.copied") : t("common.copyPage")}
    </Button>
  )
}

/**
 * Detail page for a single image dataset. Replaces the old nuqs-driven
 * UsageSheet — a real route gives a shareable URL and back-stack support. The
 * dataset list is a client-side catalog, so the page looks the item up by id
 * rather than issuing a per-item fetch; the title comes from the breadcrumb.
 */
export default function DatasetDetailPage({ params }: PageProps) {
  const { id } = use(params)
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()
  const router = useRouter()
  const isAdmin = useAtomValue(isActualAdminAtom)

  const { data: allDatasets = [] } = useQuery(imagesCatalogQueryOptions())
  const dataset = useMemo(() => allDatasets.find((d) => d.id === id) ?? null, [allDatasets, id])

  const [editTarget, setEditTarget] = useState<ImageDataset | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const deleteMutation = useDeleteImageDataset()

  const handleDelete = async () => {
    if (!dataset) return
    try {
      await deleteMutation.mutateAsync(dataset.id)
      toast.success(`"${dataset.name}" deleted`)
      router.push(clusterPath(clusterID, "datasets", locale))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  if (!dataset) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-muted-foreground text-sm">{t("datasets.noResults")}</p>
      </div>
    )
  }

  const docContent = dataset.clusterDocs?.[clusterID]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <DetailHeader
        icon={HardDrive}
        title={dataset.name}
        copyValue={dataset.name}
        kind={t("datasets.imageCount", { count: dataset.imageCount ?? 0 })}
        meta={[
          { label: t("datasets.filterByCategory"), value: dataset.category },
          { label: t("datasets.filterBySource"), value: dataset.source },
        ]}
        actions={
          <>
            {docContent && <CopyPageButton content={docContent} />}
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              onClick={() => window.open(dataset.huggingFaceUrl, "_blank", "noopener,noreferrer")}
            >
              <ExternalLink className="h-3 w-3" />
              HuggingFace
            </Button>
            {isAdmin && (
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1.5 text-xs"
                onClick={() => setEditTarget(dataset)}
              >
                <Pencil className="h-3 w-3" />
                {t("common.edit")}
              </Button>
            )}
            {isAdmin && (
              <Button
                variant="outline"
                size="sm"
                className="text-destructive hover:text-destructive h-8 gap-1.5 text-xs"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 className="h-3 w-3" />
                {t("common.delete")}
              </Button>
            )}
          </>
        }
      />

      <div className="flex-1 overflow-y-auto px-6 py-5">
        {docContent ? (
          <MarkdownRenderer content={docContent} />
        ) : (
          <p className="text-muted-foreground text-sm">{t("datasets.usageNoClusterDocs")}</p>
        )}
      </div>

      <UpsertImageDialog
        open={!!editTarget}
        onOpenChange={(open) => {
          if (!open) setEditTarget(null)
        }}
        dataset={editTarget}
      />

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Delete Dataset</DialogTitle>
            <DialogDescription>
              This will permanently remove <strong>{dataset.name}</strong> from the catalog. This
              action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
