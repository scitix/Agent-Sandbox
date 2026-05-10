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

import { Pencil, Trash2, Layers, Cpu, MemoryStick } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import type { AgentSandboxTemplateSummary } from "@/lib/api/client"
import { cn } from "@/lib/utils"
import { useTranslation } from "@/lib/i18n"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"

interface TemplateCardProps {
  template: AgentSandboxTemplateSummary
  onClick: (template: AgentSandboxTemplateSummary) => void
  onEdit?: (template: AgentSandboxTemplateSummary) => void
  onDelete?: (template: AgentSandboxTemplateSummary) => void
  isAdmin: boolean
}

export function TemplateCard({ template, onClick, onEdit, onDelete, isAdmin }: TemplateCardProps) {
  const { t } = useTranslation()
  const cpuCores = parseCpuToCore(template.cpu)
  const memoryMiB = parseMemoryToMiB(template.memory)

  return (
    <div
      className={cn(
        "group flex cursor-pointer flex-col gap-3 rounded-xl border p-5",
        "bg-card hover:bg-accent/30 transition-colors duration-150",
      )}
      onClick={() => onClick(template)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onClick(template)
      }}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
            <Layers className="text-muted-foreground h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <p className="truncate font-mono text-sm font-semibold tracking-tight">
                {template.name}
              </p>
            </div>
          </div>
        </div>

        {/* Admin actions — only visible on hover */}
        {isAdmin && (
          <div
            className="flex shrink-0 gap-1 opacity-0 transition-opacity group-hover:opacity-100"
            onClick={(e) => e.stopPropagation()}
          >
            {onEdit && (
              <Button
                variant="ghost"
                size="icon-sm"
                className="h-7 w-7"
                onClick={() => onEdit(template)}
              >
                <Pencil className="h-3.5 w-3.5" />
              </Button>
            )}
            {onDelete && template.syncSource === "global" && (
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground hover:text-destructive h-7 w-7"
                onClick={() => onDelete(template)}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            )}
          </div>
        )}
      </div>

      {/* Description */}
      <p className="text-muted-foreground line-clamp-2 text-xs leading-relaxed">
        {template.description || t("templates.noResultsDesc")}
      </p>

      {/* Footer: runtimes + resources */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="mt-0.5 flex items-center gap-1.5">
          {template.version && (
            <Badge variant="outline" className="font-mono text-[10px]">
              {template.version}
            </Badge>
          )}
        </div>

        {/* Resources */}
        {(cpuCores != null || memoryMiB != null) && (
          <span className="text-muted-foreground ml-auto flex items-center gap-2 font-mono text-[10px]">
            {cpuCores != null && (
              <span className="flex items-center gap-0.5">
                <Cpu className="h-3 w-3" />
                {formatCores(cpuCores)}
              </span>
            )}
            {memoryMiB != null && (
              <span className="flex items-center gap-0.5">
                <MemoryStick className="h-3 w-3" />
                {formatMiB(memoryMiB)} MiB
              </span>
            )}
          </span>
        )}
      </div>
    </div>
  )
}
