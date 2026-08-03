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

import { useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { useQuery } from "@tanstack/react-query"
import { Bot, Plus, Search } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { AgentCard } from "@/components/managed-agents/agent-card"
import { DeleteManagedAgentDialog } from "@/components/managed-agents/delete-dialog"
import { UpsertManagedAgentSheet } from "@/components/managed-agents/upsert-sheet"
import type { ManagedAgent } from "@/lib/api/managed-agent-types"
import { managedAgentsQueryOptions } from "@/lib/queries"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

/**
 * Agent list. A team runs a handful of agents, each carrying more state than a
 * table row shows comfortably, so this is a card grid like the Template list —
 * search plus a create entry point, no column machinery.
 */
export default function ManagedAgentsPage() {
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  const [search, setSearch] = useState("")
  const [upsertOpen, setUpsertOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ManagedAgent | null>(null)

  const { data: agents = [] } = useQuery(managedAgentsQueryOptions())

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim()
    if (!q) return agents
    return agents.filter(
      (agent) =>
        agent.name.toLowerCase().includes(q) ||
        (agent.spec.displayName?.toLowerCase().includes(q) ?? false) ||
        (agent.spec.description?.toLowerCase().includes(q) ?? false) ||
        (agent.spec.runtime?.default?.toLowerCase().includes(q) ?? false),
    )
  }, [agents, search])

  const basePath = clusterPath(clusterID, "managed-agents", locale)
  const openDetail = (agent: ManagedAgent) =>
    router.push(`${basePath}/${encodeURIComponent(agent.name)}`)

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* Toolbar */}
        <div className="border-border border-b px-6 py-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative flex-1" style={{ minWidth: "200px", maxWidth: "360px" }}>
              <Search className="text-muted-foreground absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
              <Input
                placeholder={t("managedAgents.searchAll")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="h-9 pl-9 font-mono text-sm"
              />
            </div>
            <Button
              size="sm"
              onClick={() => {
                setEditTarget(null)
                setUpsertOpen(true)
              }}
              className="bg-foreground text-background hover:bg-foreground/90 ml-auto h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
            >
              <Plus className="h-3 w-3" />
              {t("managedAgents.newAgent")}
            </Button>
          </div>
        </div>

        {/* Cards grid */}
        <div className="px-6 py-5">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <Bot className="text-muted-foreground/40 mb-3 h-10 w-10" />
              <p className="text-muted-foreground text-sm font-medium">
                {t("managedAgents.noResults")}
              </p>
              <p className="text-muted-foreground mt-1 text-xs">
                {t("managedAgents.noResultsDesc")}
              </p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {filtered.map((agent) => (
                <AgentCard
                  key={agent.name}
                  agent={agent}
                  onClick={openDetail}
                  onEdit={(a) => {
                    setEditTarget(a.name)
                    setUpsertOpen(true)
                  }}
                  onDelete={setDeleteTarget}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      <UpsertManagedAgentSheet
        agentName={editTarget}
        open={upsertOpen}
        onOpenChange={setUpsertOpen}
        onSaved={(name) => {
          if (!editTarget) router.push(`${basePath}/${encodeURIComponent(name)}`)
        }}
      />
      <DeleteManagedAgentDialog
        agent={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
    </div>
  )
}
