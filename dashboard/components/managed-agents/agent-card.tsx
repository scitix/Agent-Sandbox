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

import { Bot, Layers, Pencil, Trash2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CopyableText } from "@/components/custom/copyable-text"
import { StatusBadge } from "@/components/custom/status-badge"
import { MANAGED_AGENT_PHASE_COLORS, handsModeOf } from "@/components/managed-agents/model"
import type { ManagedAgent } from "@/lib/api/managed-agent-types"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

interface AgentCardProps {
  agent: ManagedAgent
  onClick: (agent: ManagedAgent) => void
  onEdit: (agent: ManagedAgent) => void
  onDelete: (agent: ManagedAgent) => void
}

/**
 * One agent as a card. Agents are few and each carries more state than a table
 * row shows comfortably, so the list is a card grid — mirroring the Template
 * list rather than the Sandbox table.
 */
export function AgentCard({ agent, onClick, onEdit, onDelete }: AgentCardProps) {
  const { t } = useTranslation()
  const scenarioCount = agent.spec.scenarios?.length ?? 0
  const handsMode = handsModeOf(agent.spec.hands)
  const handsLabel =
    handsMode === "envRef"
      ? t("managedAgents.handsMode.envRef")
      : handsMode === "external"
        ? t("managedAgents.handsMode.external")
        : t("managedAgents.handsMode.auto")
  const handsTarget =
    agent.status?.hands?.envName ??
    agent.spec.hands?.envRef?.name ??
    agent.spec.hands?.external?.envName

  return (
    <div
      className={cn(
        "group flex cursor-pointer flex-col gap-3 rounded-xl border p-5",
        "bg-card hover:bg-accent/30 transition-colors duration-150",
      )}
      onClick={() => onClick(agent)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onClick(agent)
      }}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
            <Bot className="text-muted-foreground h-4 w-4" />
          </div>
          <div className="min-w-0">
            <p className="truncate font-mono text-sm font-semibold tracking-tight">
              {agent.spec.displayName || agent.name}
            </p>
            {agent.spec.displayName && (
              <p className="text-muted-foreground truncate font-mono text-[10px]">{agent.name}</p>
            )}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          <StatusBadge
            status={agent.status?.phase}
            colorMap={MANAGED_AGENT_PHASE_COLORS}
            defaultClass={MANAGED_AGENT_PHASE_COLORS.pending}
          />
          <div
            className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100"
            onClick={(e) => e.stopPropagation()}
          >
            <Button
              variant="ghost"
              size="icon-sm"
              className="h-7 w-7"
              onClick={() => onEdit(agent)}
            >
              <Pencil className="h-3.5 w-3.5" />
              <span className="sr-only">{t("managedAgents.action.edit")}</span>
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground hover:text-destructive h-7 w-7"
              onClick={() => onDelete(agent)}
            >
              <Trash2 className="h-3.5 w-3.5" />
              <span className="sr-only">{t("managedAgents.action.delete")}</span>
            </Button>
          </div>
        </div>
      </div>

      {/* Description */}
      <p className="text-muted-foreground line-clamp-2 text-xs leading-relaxed">
        {agent.spec.description || t("managedAgents.noDescription")}
      </p>

      {/* Facts */}
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline" className="font-mono text-[10px]">
          {agent.spec.runtime?.default ?? "—"}
        </Badge>
        <Badge variant="outline" className="gap-1 font-mono text-[10px]">
          <Layers className="h-3 w-3" />
          {t("managedAgents.scenarioCount", { count: scenarioCount })}
        </Badge>
        <Badge variant="outline" className="font-mono text-[10px]">
          {handsTarget ? `${handsLabel} · ${handsTarget}` : handsLabel}
        </Badge>
      </div>

      {/* Endpoint */}
      <div className="mt-auto pt-1" onClick={(e) => e.stopPropagation()}>
        {agent.status?.endpoint ? (
          <CopyableText value={agent.status.endpoint} className="text-[11px] font-normal" />
        ) : (
          <span className="text-muted-foreground font-mono text-[11px]">
            {t("managedAgents.endpointPending")}
          </span>
        )}
      </div>
    </div>
  )
}
