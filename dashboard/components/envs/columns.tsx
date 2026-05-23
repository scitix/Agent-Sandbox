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
// Mirrors `dashboard/components/pools/columns.tsx` in style: a top-level
// factory that takes `t` (for i18n) and per-row action callbacks. Rows are
// keyed by env name (unique within namespace, which the API scopes us to).

import type { ColumnDef } from "@tanstack/react-table"
import { Eye, MoreVertical, Settings2 } from "lucide-react"

import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { RelativeTime } from "@/components/custom/relative-time"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { AgentSandboxEnv } from "@/lib/api/client"
import type { TranslationKey } from "@/messages/_schema"

type TranslationFn = (key: TranslationKey, params?: Record<string, string | number>) => string

/**
 * Returns the per-Env aggregated scaling-group status, when present.
 *
 * Phase 1 envs only have a single group (matching the single resource shape
 * across all members). We pick the first non-empty group; future multi-group
 * Envs will warrant a richer breakdown in the detail sheet.
 */
function pickAggregateGroup(env: AgentSandboxEnv) {
  const groups = env.status?.scalingGroups
  if (!groups || groups.length === 0) return undefined
  return groups[0]
}

/**
 * Returns the local-cluster status segment. The Worker writes only its own
 * segment, so this is also the segment carrying lastScaleUpTime / etc. for
 * the current cluster. Returns undefined when the segment hasn't been
 * populated yet (typical right after adoption).
 */
function pickLocalClusterStatus(env: AgentSandboxEnv) {
  const clusters = env.status?.clusters
  if (!clusters || clusters.length === 0) return undefined
  return clusters.find((c) => c.isLocal === true) ?? clusters[0]
}

export function createEnvColumns(
  t: TranslationFn,
  onViewDetail: (env: AgentSandboxEnv) => void,
  onEditAutoscaling: (env: AgentSandboxEnv) => void,
): ColumnDef<AgentSandboxEnv>[] {
  return [
    {
      accessorKey: "name",
      header: ({ column }) => <DataTableColumnHeader column={column} title={t("envs.col.name")} />,
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="sm"
          className="h-auto px-0 py-0 font-mono text-xs hover:bg-transparent"
          onClick={() => onViewDetail(row.original)}
        >
          {row.original.name}
        </Button>
      ),
    },
    {
      id: "templateRef",
      accessorFn: (row) => row.spec.templateRef.name,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.template")} />
      ),
      cell: ({ row }) => (
        <span className="text-muted-foreground font-mono text-xs">
          {row.original.spec.templateRef.name}
        </span>
      ),
    },
    {
      id: "mode",
      accessorFn: (row) => row.spec.mode,
      header: ({ column }) => <DataTableColumnHeader column={column} title={t("envs.col.mode")} />,
      cell: ({ row }) => (
        <Badge variant="outline" className="font-mono text-xs">
          {row.original.spec.mode}
        </Badge>
      ),
    },
    {
      id: "members",
      accessorFn: (row) => row.status?.localMemberCount ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.members")} />
      ),
      cell: ({ row }) => {
        const localCluster = row.original.spec.clusters?.find((c) => c.members && c.members.length > 0)
        const memberNames = localCluster?.members?.map((m) => m.name) ?? []
        if (memberNames.length === 0) return <span className="text-muted-foreground text-xs">0</span>
        const label = memberNames.length === 1 ? memberNames[0] : `${memberNames.length} pools`
        return <span className="text-muted-foreground font-mono text-xs">{label}</span>
      },
    },
    {
      id: "autoscaling",
      accessorFn: (row) => row.spec.autoscaling?.enabled === true,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.autoscaling")} />
      ),
      cell: ({ row }) => {
        const enabled = row.original.spec.autoscaling?.enabled === true
        return (
          <Badge
            variant={enabled ? "default" : "outline"}
            className="font-mono text-xs"
          >
            {enabled ? t("envs.autoscalingEnabled") : t("envs.autoscalingDisabled")}
          </Badge>
        )
      },
    },
    {
      id: "totalIdle",
      accessorFn: (row) => pickAggregateGroup(row)?.totalIdle ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.totalIdle")} />
      ),
      cell: ({ row }) => {
        const agg = pickAggregateGroup(row.original)
        return <span className="font-mono text-xs">{agg?.totalIdle ?? 0}</span>
      },
    },
    {
      id: "totalRunning",
      accessorFn: (row) => pickAggregateGroup(row)?.totalRunning ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.totalRunning")} />
      ),
      cell: ({ row }) => {
        const agg = pickAggregateGroup(row.original)
        return <span className="font-mono text-xs">{agg?.totalRunning ?? 0}</span>
      },
    },
    {
      id: "totalPending",
      accessorFn: (row) => pickAggregateGroup(row)?.totalPending ?? 0,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.totalPending")} />
      ),
      cell: ({ row }) => {
        const agg = pickAggregateGroup(row.original)
        const v = agg?.totalPending ?? 0
        return (
          <span className={`font-mono text-xs ${v > 0 ? "text-amber-600" : "text-muted-foreground"}`}>
            {v}
          </span>
        )
      },
    },
    {
      id: "lastScaleUp",
      accessorFn: (row) => pickLocalClusterStatus(row)?.lastScaleUpTime ?? null,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.col.lastScaleUp")} />
      ),
      cell: ({ row }) => {
        const ts = pickLocalClusterStatus(row.original)?.lastScaleUpTime
        if (!ts) return <span className="text-muted-foreground text-xs">—</span>
        return <RelativeTime date={ts} />
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
            <DropdownMenuItem onClick={() => onEditAutoscaling(row.original)}>
              <Settings2 className="mr-2 h-3.5 w-3.5" /> {t("envs.action.editAutoscaling")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ),
    },
  ]
}
