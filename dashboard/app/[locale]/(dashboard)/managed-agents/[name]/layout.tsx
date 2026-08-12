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
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  BookOpen,
  Bot,
  Boxes,
  Cpu,
  FileCode,
  InfoIcon,
  Layers,
  Loader2,
  MessageSquare,
  Pencil,
  Trash2,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabsNav } from "@/components/custom/detail-tabs-nav"
import { StatusBadge } from "@/components/custom/status-badge"
import { DeleteManagedAgentDialog } from "@/components/managed-agents/delete-dialog"
import { MANAGED_AGENT_PHASE_COLORS } from "@/components/managed-agents/model"
import { UpsertManagedAgentSheet } from "@/components/managed-agents/upsert-sheet"
import { managedAgentQueryOptions } from "@/lib/queries"
import { standalonePath } from "@/lib/cluster-path"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

interface LayoutProps {
  children: React.ReactNode
  params: Promise<{ name: string; locale: string }>
}

/**
 * Shared shell for the agent detail sub-routes: header, actions and tab bar are
 * rendered once here, each tab is a child page rendered into {children}. Loading
 * and not-found states are gated here so every sub-page can assume the agent is
 * present.
 */
export default function ManagedAgentDetailLayout({ children, params }: LayoutProps) {
  const { name } = use(params)
  const { t } = useTranslation()
  const router = useRouter()
  const locale = useLocale()

  const { data: agent, isLoading } = useQuery(managedAgentQueryOptions(name))

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const basePath = `${standalonePath("managed-agents", locale)}/${encodeURIComponent(name)}`
  const tabs = [
    { value: "", label: t("managedAgents.tab.docs"), icon: BookOpen },
    { value: "overview", label: t("managedAgents.tab.overview"), icon: InfoIcon },
    { value: "preview", label: t("managedAgents.tab.preview"), icon: MessageSquare },
    { value: "runtime", label: t("managedAgents.tab.runtime"), icon: Cpu },
    { value: "scenarios", label: t("managedAgents.tab.scenarios"), icon: Layers },
    { value: "hands", label: t("managedAgents.tab.hands"), icon: Boxes },
    { value: "session", label: t("managedAgents.tab.session"), icon: ActivityIcon },
    { value: "yaml", label: t("managedAgents.tab.yaml"), icon: FileCode },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <DetailHeader
        icon={Bot}
        title={agent?.spec.displayName || name}
        copyValue={name}
        kind="ManagedAgent"
        badge={
          agent?.status?.phase ? (
            <StatusBadge
              status={agent.status.phase}
              colorMap={MANAGED_AGENT_PHASE_COLORS}
              defaultClass={MANAGED_AGENT_PHASE_COLORS.pending}
            />
          ) : undefined
        }
        meta={[
          { label: t("managedAgents.col.runtime"), value: agent?.spec.runtime?.default ?? "—" },
          { label: t("managedAgents.col.hands"), value: agent?.status?.hands?.envName ?? "—" },
          { label: t("managedAgents.col.endpoint"), value: agent?.status?.endpoint ?? "—" },
        ]}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!agent}
              onClick={() => setEditOpen(true)}
              className="h-8 gap-1 text-xs"
            >
              <Pencil className="h-3.5 w-3.5" />
              {t("managedAgents.action.edit")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!agent}
              onClick={() => setDeleteOpen(true)}
              className="text-destructive hover:text-destructive h-8 gap-1 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("managedAgents.action.delete")}
            </Button>
          </>
        }
      />

      <DetailTabsNav basePath={basePath} tabs={tabs} />

      <div className="flex min-h-0 flex-1 flex-col">
        {isLoading ? (
          <div className="flex flex-1 items-center justify-center">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        ) : !agent ? (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-muted-foreground text-sm">{t("managedAgents.detail.notFound")}</p>
          </div>
        ) : (
          children
        )}
      </div>

      <UpsertManagedAgentSheet agentName={name} open={editOpen} onOpenChange={setEditOpen} />
      <DeleteManagedAgentDialog
        agent={deleteOpen && agent ? agent : null}
        onOpenChange={setDeleteOpen}
        onDeleted={() => router.push(standalonePath("managed-agents", locale))}
      />
    </div>
  )
}
