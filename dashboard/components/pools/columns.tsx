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

import { type ColumnDef } from "@tanstack/react-table"
import { type AgentEnvObservedMember, type AgentSandboxPool } from "@/lib/api/client"
import { MoreVertical, ArrowUpRight, Activity, AlertTriangle, Pencil, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { ResourceLink } from "@/components/custom/resource-link"
import { RelativeTime } from "@/components/custom/relative-time"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"
import { StatusBadge, type StatusBadgeColorMap } from "@/components/custom/status-badge"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import Link from "next/link"
import type { TranslationKey } from "@/messages/_schema"

export const POOL_PHASE_COLORS: StatusBadgeColorMap = {
  ready: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30",
  degraded: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  scalingup: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
  scalingdown: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  pending: "bg-gray-500/15 text-gray-500 dark:text-gray-400 border-gray-500/30",
}

function StatusLinkCell({
  value,
  color,
  poolName,
  status,
  disableLink,
  warning,
  warningTooltip,
}: {
  value?: number
  color: string
  poolName: string
  status: string
  disableLink?: boolean
  warning?: boolean
  warningTooltip?: string
}) {
  const clusterID = useClusterID()
  const locale = useLocale()

  if (value == null) return <span className="text-muted-foreground text-xs">---</span>

  const countEl =
    value === 0 || disableLink ? (
      <span className={`inline-flex min-w-8 font-mono text-sm font-semibold ${color}`}>
        {value}
      </span>
    ) : (
      (() => {
        const params = new URLSearchParams({ poolName })
        if (status) params.set("status", status)
        const href = `${clusterPath(clusterID, "sandboxes", locale)}?${params.toString()}`
        return (
          <Button
            variant="ghost"
            size="sm"
            nativeButton={false}
            className={`h-auto justify-start gap-1 px-0 py-0.5 font-mono text-sm font-semibold ${color} hover:bg-transparent hover:opacity-70`}
            render={<Link href={href} />}
          >
            {value}
            <ArrowUpRight className="h-3 w-3 opacity-70" />
          </Button>
        )
      })()
    )

  if (!warning || !warningTooltip) return countEl

  return (
    <div className="flex items-center gap-1">
      {countEl}
      <Tooltip>
        <TooltipTrigger render={<span className="inline-flex cursor-default" />}>
          <AlertTriangle className="h-3.5 w-3.5 text-amber-500 dark:text-amber-400" />
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-48 text-xs">
          {warningTooltip}
        </TooltipContent>
      </Tooltip>
    </div>
  )
}

// OwningEnvCell renders the SandboxEnv that owns this Pool. Links to the
// 3-level Env detail page at /clusters/{id}/envs/{name}. Pools awaiting
// adoption (no owning Env) show "—".
function OwningEnvCell({ envName }: { envName?: string }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  if (!envName) return <span className="text-muted-foreground text-xs">—</span>
  const href = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(envName)}`
  return <ResourceLink value={envName} href={href} tone="muted" />
}

// Pool name → the pool's detail page, nested under its owning Env. Pools not
// yet adopted by an Env (no owningEnv) fall back to a copy-only label.
function PoolNameCell({ pool }: { pool: AgentSandboxPool }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  const href = pool.owningEnv
    ? `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(pool.owningEnv)}/pools/${encodeURIComponent(pool.name)}`
    : undefined
  return <ResourceLink value={pool.name} href={href} />
}

export interface PoolColumnsOptions {
  showOwner?: boolean
  // Hide the "Env" reverse-link column. Useful when the table is already
  // scoped to a single Env (e.g. the Env detail page) where the column would
  // be redundant for every row.
  hideOwningEnv?: boolean
  // When present, adds a "Scaling" column that surfaces the per-member
  // observed state (scaling group, state, saturatedUntil, last attempt
  // result) from the owning Env's status. Pools that aren't in the map
  // render an em-dash.
  envObservedByPool?: Map<string, AgentEnvObservedMember>
  // Optional scaling-group lookup (sourced from env.spec.clusters.members).
  // Falls back to "" when missing.
  scalingGroupByPool?: Map<string, string>
  // Env-scoped row actions. Each appears in the row dropdown when set.
  onEditPool?: (pool: AgentSandboxPool) => void
  onDeletePool?: (pool: AgentSandboxPool) => void
}

export function createPoolColumns(
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
  onViewMetrics?: (pool: AgentSandboxPool) => void,
  options?: PoolColumnsOptions,
): ColumnDef<AgentSandboxPool>[] {
  const createStatusColumn = (
    id: string,
    title: string,
    accessor: (pool: AgentSandboxPool) => number | undefined,
    color: string,
    status: string,
    tooltip?: string,
    linkable = true,
  ): ColumnDef<AgentSandboxPool> => ({
    id,
    accessorFn: accessor,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={title} tooltip={tooltip} />
    ),
    cell: ({ row }) => {
      const val = accessor(row.original)
      if (!linkable) {
        if (val == null) return <span className="text-muted-foreground text-xs">---</span>
        return (
          <span className={`inline-flex min-w-8 font-mono text-sm font-semibold ${color}`}>
            {val}
          </span>
        )
      }
      return (
        <StatusLinkCell value={val} color={color} poolName={row.original.name} status={status} />
      )
    },
  })

  const ownerColumn: ColumnDef<AgentSandboxPool> = {
    id: "owner",
    header: ({ column }) => <DataTableColumnHeader column={column} title={t("pools.col.owner")} />,
    enableSorting: false,
    cell: ({ row }) => {
      const team = (row.original as AgentSandboxPool & { team?: string }).team
      const user = (row.original as AgentSandboxPool & { user?: string }).user
      if (!team && !user) return <span className="text-muted-foreground text-xs">---</span>
      return (
        <div className="flex flex-col gap-0.5">
          {team && (
            <span className="text-muted-foreground font-mono text-xs uppercase">{team}</span>
          )}
          {user && <span className="text-foreground font-mono text-xs font-semibold">{user}</span>}
        </div>
      )
    },
  }

  const scalingColumn: ColumnDef<AgentSandboxPool> = {
    id: "scaling",
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={t("pools.col.scaling")} />
    ),
    cell: ({ row }) => {
      const observed = options?.envObservedByPool?.get(row.original.name)
      const group =
        options?.scalingGroupByPool?.get(row.original.name) ?? row.original.scalingGroup ?? ""
      const state = observed?.state ?? ""
      const saturatedUntil = observed?.saturatedUntil
      const lastResult = row.original.status?.autoscaling?.lastScaleUpAttemptResult
      if (!group && !state && !saturatedUntil && !lastResult)
        return <span className="text-muted-foreground text-xs">—</span>
      return (
        <div className="flex flex-col gap-0.5 font-mono text-[11px]">
          {group && <span className="text-muted-foreground truncate">{group}</span>}
          {state && (
            <Badge variant="outline" className="w-fit font-mono text-[10px]">
              {state}
            </Badge>
          )}
          {saturatedUntil && (
            <span className="text-[10px] text-amber-600">
              {t("envs.detail.members.col.saturatedUntil")}: <RelativeTime date={saturatedUntil} />
            </span>
          )}
          {lastResult && lastResult !== "Enough" && (
            <span className="text-muted-foreground text-[10px]">{lastResult}</span>
          )}
        </div>
      )
    },
  }

  const createdAtColumn: ColumnDef<AgentSandboxPool> = {
    id: "createdAt",
    accessorFn: (row) => row.createdAt,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={t("pools.col.createdAt")} />
    ),
    enableHiding: true,
    cell: ({ row }) => <RelativeTime date={row.original.createdAt} />,
  }

  return [
    {
      accessorKey: "name",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.name")}
          tooltip={t("pools.col.nameTooltip")}
          includesStringFilterOptions={{ placeholder: t("pools.col.searchByName") }}
        />
      ),
      cell: ({ row }) => <PoolNameCell pool={row.original} />,
    },
    ...(options?.showOwner ? [ownerColumn] : []),
    {
      id: "phase",
      accessorFn: (row) => row.status?.phase ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.phase")}
          tooltip={t("pools.col.phaseTooltip")}
        />
      ),
      cell: ({ row }) => {
        const phase = row.original.status?.phase
        const unavailIdle = row.original.status?.unavailableIdleReplicas ?? 0

        const hoverContent =
          phase === "Degraded" && unavailIdle > 0 ? (
            <div className="space-y-1.5">
              <div className="text-foreground font-semibold">{t("pools.phase.degradedTitle")}</div>
              <div className="text-muted-foreground">
                {t("pools.col.unavailTooltip", { count: unavailIdle })}
              </div>
            </div>
          ) : undefined

        return (
          <StatusBadge
            status={phase ?? null}
            colorMap={POOL_PHASE_COLORS}
            hoverContent={hoverContent}
          />
        )
      },
    },
    ...(options?.hideOwningEnv
      ? []
      : [
          {
            // Reverse-link to the owning SandboxEnv. The OwnerReference is
            // stamped by the Phase 1 adopter, so every Pool created
            // post-adoption shows a link; brand-new Pools may show "—"
            // briefly before adoption runs.
            id: "owningEnv",
            accessorFn: (row) => row.owningEnv ?? "",
            header: ({ column }) => (
              <DataTableColumnHeader column={column} title={t("pools.col.env")} />
            ),
            cell: ({ row }) => <OwningEnvCell envName={row.original.owningEnv} />,
          } satisfies ColumnDef<AgentSandboxPool>,
        ]),
    ...(options?.envObservedByPool ? [scalingColumn] : []),
    {
      id: "replicas",
      accessorFn: (row) => row.spec?.replicas,
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.replicas")}
          tooltip={t("pools.col.replicasTooltip")}
        />
      ),
      cell: ({ row }) => {
        const replicas = row.original.spec?.replicas
        const idleReplicas = row.original.status?.idleReplicas
        if (replicas == null) return <span className="font-mono text-sm">---</span>
        // Autoscaling state lives on the owning SandboxEnv now; the Pool
        // spec only carries the desired replica count. The "autoscaling
        // active" tooltip will return when the dashboard surfaces Env-level
        // state — out of scope for this refactor.
        return (
          <StatusLinkCell
            value={replicas}
            color="text-foreground"
            poolName={row.original.name}
            status=""
            disableLink={idleReplicas === replicas}
          />
        )
      },
    },
    {
      id: "idleReplicas",
      accessorFn: (row) => row.status?.idleReplicas,
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.idle")}
          tooltip={t("pools.col.idleTooltip")}
        />
      ),
      cell: ({ row }) => {
        const val = row.original.status?.idleReplicas
        const unavailIdle = row.original.status?.unavailableIdleReplicas ?? 0
        if (val == null) return <span className="text-muted-foreground text-xs">---</span>
        return (
          <StatusLinkCell
            value={val}
            color="text-green-700 dark:text-green-400"
            poolName={row.original.name}
            status="Idle"
            disableLink
            warning={unavailIdle > 0}
            warningTooltip={
              unavailIdle > 0 ? t("pools.col.unavailTooltip", { count: unavailIdle }) : undefined
            }
          />
        )
      },
    },
    createStatusColumn(
      "runningReplicas",
      t("pools.col.running"),
      (row) => row.status?.runningReplicas,
      "text-blue-700 dark:text-blue-400",
      "Running",
      t("pools.col.runningTooltip"),
    ),
    createStatusColumn(
      "startingReplicas",
      t("pools.col.starting"),
      (row) => row.status?.startingReplicas,
      "text-yellow-700 dark:text-yellow-400",
      "Starting",
      t("pools.col.startingTooltip"),
    ),
    createStatusColumn(
      "stoppingReplicas",
      t("pools.col.stopping"),
      (row) => row.status?.stoppingReplicas,
      "text-purple-700 dark:text-purple-400",
      "Stopping",
      t("pools.col.stoppingTooltip"),
    ),
    createStatusColumn(
      "failedReplicas",
      t("pools.col.failed"),
      (row) => row.status?.failedReplicas,
      "text-red-700 dark:text-red-400",
      "Failed",
      t("pools.col.failedTooltip"),
    ),
    {
      id: "cpu",
      accessorFn: (row) => parseCpuToCore(row.cpu),
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.cpu")}
          tooltip={t("pools.col.cpuTooltip")}
          numberRangeFilterOptions={{
            unit: " cores",
            placeholder: { min: t("pools.col.minCpu"), max: t("pools.col.maxCpu") },
          }}
        />
      ),
      cell: ({ row }) => {
        const cpu = row.original.cpu
        const cores = parseCpuToCore(cpu)
        return cores != null ? (
          <span className="text-muted-foreground font-mono text-[12px]">
            {formatCores(cores)} cores
          </span>
        ) : (
          <span className="text-muted-foreground text-xs">---</span>
        )
      },
      filterFn: "inNumberRange",
    },
    {
      id: "memory",
      accessorFn: (row) => parseMemoryToMiB(row.memory),
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.memory")}
          tooltip={t("pools.col.memoryTooltip")}
          numberRangeFilterOptions={{
            unit: "MiB",
            placeholder: { min: t("pools.col.minMemory"), max: t("pools.col.maxMemory") },
          }}
        />
      ),
      cell: ({ row }) => {
        const memory = row.original.memory
        const gib = parseMemoryToMiB(memory)
        return gib != null ? (
          <span className="text-muted-foreground font-mono text-[12px]">{formatMiB(gib)} MiB</span>
        ) : (
          <span className="text-muted-foreground text-xs">---</span>
        )
      },
      filterFn: "inNumberRange",
    },
    createdAtColumn,
    {
      id: "actions",
      cell: ({ row }) => {
        const onEditPool = options?.onEditPool
        const onDeletePool = options?.onDeletePool
        const hasAny = onViewMetrics || onEditPool || onDeletePool
        if (!hasAny) return null
        return (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" className="text-muted-foreground h-7 w-7" />
              }
            >
              <MoreVertical className="h-4 w-4" />
              <span className="sr-only">{t("common.actions")}</span>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              {onViewMetrics && (
                <DropdownMenuItem
                  onClick={() => onViewMetrics(row.original)}
                  className="cursor-pointer font-mono text-xs"
                >
                  <Activity className="mr-2 h-3.5 w-3.5" />
                  {t("prometheus.metrics")}
                </DropdownMenuItem>
              )}
              {onEditPool && (
                <DropdownMenuItem
                  onClick={() => onEditPool(row.original)}
                  className="cursor-pointer font-mono text-xs"
                >
                  <Pencil className="mr-2 h-3.5 w-3.5" />
                  {t("envs.poolForm.editAction")}
                </DropdownMenuItem>
              )}
              {onDeletePool && (
                <DropdownMenuItem
                  onClick={() => onDeletePool(row.original)}
                  className="text-destructive cursor-pointer font-mono text-xs"
                >
                  <Trash2 className="mr-2 h-3.5 w-3.5" />
                  {t("common.delete")}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )
      },
    },
  ]
}
