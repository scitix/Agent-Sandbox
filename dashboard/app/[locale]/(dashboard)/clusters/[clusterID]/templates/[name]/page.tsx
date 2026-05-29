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

import { use, useState } from "react"
import { useRouter } from "next/navigation"
import { useAtomValue } from "jotai"
import { TemplateDetailContent } from "@/components/templates/detail-sheet"
import { DeleteTemplateDialog } from "@/components/templates/delete-dialog"
import { UpsertTemplateSheet } from "@/components/templates/upsert-sheet"
import type { AgentSandboxTemplate, AgentSandboxTemplateSummary } from "@/lib/api/client"
import { isAdminAtom } from "@/lib/atoms"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/**
 * Detail page for a single SandboxTemplate. Replaces the old nuqs-driven
 * TemplateDetailSheet — a real route gives a shareable URL and back-stack
 * support. The page reuses TemplateDetailContent (which fetches by name) and
 * keeps Edit/Delete as modals; the title is supplied by the layout breadcrumb.
 */
export default function TemplateDetailPage({ params }: PageProps) {
  const { name } = use(params)
  const isAdmin = useAtomValue(isAdminAtom)
  const clusterID = useClusterID()
  const locale = useLocale()
  const router = useRouter()

  const [editTarget, setEditTarget] = useState<AgentSandboxTemplate | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandboxTemplateSummary | null>(null)

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <TemplateDetailContent
        templateName={name}
        onEdit={(tmpl) => setEditTarget(tmpl)}
        onDelete={(tmpl) => setDeleteTarget(tmpl)}
        isAdmin={isAdmin}
      />

      {isAdmin && (
        <UpsertTemplateSheet
          template={editTarget}
          open={!!editTarget}
          onOpenChange={(open) => {
            if (!open) setEditTarget(null)
          }}
        />
      )}
      {isAdmin && (
        <DeleteTemplateDialog
          template={deleteTarget}
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null)
          }}
          onDeleted={() => router.push(clusterPath(clusterID, "templates", locale))}
        />
      )}
    </div>
  )
}
