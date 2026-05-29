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
import { Search, Plus, Layers } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { TemplateCard } from "@/components/templates/template-card"
import { CreateTemplateDialog } from "@/components/templates/create-dialog"
import { DeleteTemplateDialog } from "@/components/templates/delete-dialog"
import type { AgentSandboxTemplateSummary } from "@/lib/api/client"
import { isAdminAtom } from "@/lib/atoms"
import { templatesQueryOptions } from "@/lib/queries"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

export default function TemplatesPage() {
  const isAdmin = useAtomValue(isAdminAtom)
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  const [search, setSearch] = useState("")
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandboxTemplateSummary | null>(null)

  const { data: templates = [] } = useQuery(templatesQueryOptions())

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim()
    if (!q) return templates
    return templates.filter(
      (tpl) =>
        tpl.name.toLowerCase().includes(q) ||
        (tpl.description?.toLowerCase().includes(q) ?? false) ||
        (tpl.runtimeNames?.some((r) => r.toLowerCase().includes(q)) ?? false),
    )
  }, [templates, search])

  const handleCardClick = (tpl: AgentSandboxTemplateSummary) => {
    router.push(`${clusterPath(clusterID, "templates", locale)}/${encodeURIComponent(tpl.name)}`)
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* Toolbar */}
        <div className="border-border border-b px-6 py-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative flex-1" style={{ minWidth: "200px", maxWidth: "360px" }}>
              <Search className="text-muted-foreground absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
              <Input
                placeholder={t("templates.searchPlaceholder")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 pl-9 font-mono text-sm"
              />
            </div>

            {isAdmin && (
              <Button
                size="sm"
                onClick={() => setCreateOpen(true)}
                className="bg-foreground text-background hover:bg-foreground/90 ml-auto h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
              >
                <Plus className="h-3 w-3" />
                {t("templates.newTemplate")}
              </Button>
            )}
          </div>
        </div>

        {/* Cards grid */}
        <div className="px-6 py-5">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <Layers className="text-muted-foreground/40 mb-3 h-10 w-10" />
              <p className="text-muted-foreground text-sm font-medium">
                {t("templates.noResults")}
              </p>
              <p className="text-muted-foreground mt-1 text-xs">{t("templates.noResultsDesc")}</p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {filtered.map((tpl) => (
                <TemplateCard
                  key={tpl.name}
                  template={tpl}
                  onClick={handleCardClick}
                  onEdit={isAdmin ? handleCardClick : undefined}
                  onDelete={isAdmin ? setDeleteTarget : undefined}
                  isAdmin={isAdmin}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Admin dialogs */}
      {isAdmin && <CreateTemplateDialog open={createOpen} onOpenChange={setCreateOpen} />}
      {isAdmin && (
        <DeleteTemplateDialog
          template={deleteTarget}
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null)
          }}
        />
      )}
    </div>
  )
}
