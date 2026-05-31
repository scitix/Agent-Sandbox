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

// Column definitions for the SandboxEnv table.
//
// Rows are the lightweight `SandboxEnvSummary` returned by the List endpoint
// (identity + template name + mode + replica rollups + autoscaling group
// counts + ready). The full spec/status is fetched on demand by the detail
// and edit flows via GET /envs/{name}. The table deliberately mirrors the
// `kubectl get sbe` columns (Template / Members / Running / Idle / Ready)
// and turns the reference cells into navigation: Template → its detail page,
// Members → the Env's Pools page.

import type { ColumnDef } from "@tanstack/react-table"
import { Check, Eye, MoreVertical, Pencil, Trash2, X } from "lucide-react"

import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { ResourceLink } from "@/components/custom/resource-link"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { AgentSandboxEnvSummary } from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import type { Locale } from "@/lib/i18n/config"
import { cn } from "@/lib/utils"
import type { TranslationKey } from "@/messages/_schema"

type TranslationFn = (key: TranslationKey, params?: Record<string, string | number>) => string

export function createEnvColumns(
  t: TranslationFn,
  clusterID: string,
  locale: Locale,
  onViewDetail: (env: AgentSandboxEnvSummary) => void,
  onEditEnv?: (env: AgentSandboxEnvSummary) => void,
  onDeleteEnv?: (env: AgentSandboxEnvSummary) => void,
): ColumnDef<AgentSandboxEnvSummary>[] {
  const envsBase = clusterPath(clusterID, "envs", locale)
  const templatesBase = clusterPath(clusterID, "templates", locale)
  return [
    {
      accessorKey: "name",
      header: ({ column }) => <DataTableColumnHeader column={column} title={t("envs.col.name")} />,
      cell: ({ row }) => (
        <ResourceLink value={row.original.name} onNavigate={() => onViewDetail(row.original)} />
      ),
    },
    {
      id: "templateRef",
      accessorFn: (row) => row.templateName ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.template")} />
      ),
      cell: ({ row }) => {
        const name = row.original.templateName
        if (!name) return <span className="text-muted-foreground text-xs">—</span>
        return (
          <ResourceLink
            value={name}
            href={`${templatesBase}/${encodeURIComponent(name)}`}
            tone="muted"
          />
        )
      },
    },
    {
      id: "mode",
      accessorFn: (row) => row.mode ?? "",
      header: ({ column }) => <DataTableColumnHeader column={column} title={t("envs.col.mode")} />,
      cell: ({ row }) => (
        <Badge variant="outline" className="font-mono text-xs">
          {row.original.mode}
        </Badge>
      ),
    },
    {
      id: "members",
      accessorFn: (row) => row.memberCount ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.members")} />
      ),
      cell: ({ row }) => {
        const count = row.original.memberCount ?? 0
        return (
          <ResourceLink
            value={String(count)}
            href={`${envsBase}/${encodeURIComponent(row.original.name)}/pools`}
            copyable={false}
          />
        )
      },
    },
    {
      id: "autoscaling",
      accessorFn: (row) => row.autoscalingEnabledGroupCount ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.autoscaling")} />
      ),
      cell: ({ row }) => {
        const total = row.original.scalingGroupCount ?? 0
        const enabled = row.original.autoscalingEnabledGroupCount ?? 0
        if (total === 0) return <span className="text-muted-foreground text-xs">—</span>
        return (
          <Badge
            variant={enabled > 0 ? "default" : "outline"}
            className="font-mono text-xs"
            title={t("envs.autoscalingGroupsHint", { enabled, total })}
          >
            {enabled}/{total}
          </Badge>
        )
      },
    },
    {
      id: "totalRunning",
      accessorFn: (row) => row.runningReplicas ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.totalRunning")} />
      ),
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.runningReplicas ?? 0}</span>
      ),
    },
    {
      id: "totalIdle",
      accessorFn: (row) => row.idleReplicas ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.totalIdle")} />
      ),
      cell: ({ row }) => (
        <span className="font-mono text-xs">{row.original.idleReplicas ?? 0}</span>
      ),
    },
    {
      id: "ready",
      accessorFn: (row) => (row.ready ? 1 : 0),
      header: ({ column }) => <DataTableColumnHeader column={column} title={t("envs.col.ready")} />,
      cell: ({ row }) => {
        const ready = row.original.ready ?? false
        const Icon = ready ? Check : X
        return (
          <Icon
            className={cn("h-3.5 w-3.5", ready ? "text-green-500" : "text-muted-foreground")}
            aria-label={ready ? t("common.yes") : t("common.no")}
          />
        )
      },
    },
    {
      id: "actions",
      enableHiding: false,
      header: () => null,
      cell: ({ row }) => (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon" className="h-6 w-6">
                <MoreVertical className="h-3.5 w-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => onViewDetail(row.original)}>
              <Eye className="mr-2 h-3.5 w-3.5" /> {t("envs.action.viewDetails")}
            </DropdownMenuItem>
            {onEditEnv && (
              <DropdownMenuItem onClick={() => onEditEnv(row.original)}>
                <Pencil className="mr-2 h-3.5 w-3.5" /> {t("envs.action.edit")}
              </DropdownMenuItem>
            )}
            {onDeleteEnv && (
              <DropdownMenuItem
                onClick={() => onDeleteEnv(row.original)}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="mr-2 h-3.5 w-3.5" /> {t("envs.action.delete")}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]
}
