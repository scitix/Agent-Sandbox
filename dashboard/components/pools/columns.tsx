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
import {
  type AgentEnvAutoscalingGroup,
  type AgentEnvObservedMember,
  type AgentSandboxPool,
} from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"
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
import { DataTableFacetedFilterProps } from "@/components/custom/query-table/faceted-filter"
import { textFilterFn } from "@/components/custom/query-table/filter-text-panel"
import { ResourceLink } from "@/components/custom/resource-link"
import { RelativeTime } from "@/components/custom/relative-time"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"
import { StatusBadge, type StatusBadgeColorMap } from "@/components/custom/status-badge"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"
import Link from "next/link"
import { useAtomValue } from "jotai"
import { clustersAtom } from "@/lib/atoms"
import type { TranslationKey } from "@/messages/_schema"

// PoolRow is a SandboxPool row optionally tagged with its owning cluster. The
// Env pools view merges local pools with foreign members surfaced from
// status.clusters[]; foreign rows set owningClusterID (a peer cluster) and
// foreignCluster=true. Plain pool tables leave both unset (→ current cluster).
export type PoolRow = AgentSandboxPool & {
  owningClusterID?: string
  foreignCluster?: boolean
}

export const POOL_PHASE_COLORS: StatusBadgeColorMap = {
  ready: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30",
  degraded: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  scalingup: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
  scalingdown: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  pending: "bg-gray-500/15 text-gray-500 dark:text-gray-400 border-gray-500/30",
}

// Autoscaling Enabled/Disabled badge colors — outline + tone, matching the
// phase-badge styling (blue = autoscaler active, gray = manual/off).
const SCALING_STATUS_COLORS = {
  enabled: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  disabled: "bg-gray-500/15 text-gray-500 dark:text-gray-400 border-gray-500/30",
} as const

// Infinity glyph used for an unset (unbounded) autoscaling max.
const UNBOUNDED = "∞"

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

// Pool name → the pool's detail page, nested under its owning Env. For a
// foreign row (owningClusterID set) the link targets that peer cluster's pools
// page. Pools with no owning Env fall back to a copy-only label.
function PoolNameCell({ pool }: { pool: AgentSandboxPool }) {
  const currentCluster = useClusterID()
  const locale = useLocale()
  const cid = (pool as PoolRow).owningClusterID || currentCluster
  const href = pool.owningEnv
    ? `${clusterPath(cid, "envs", locale)}/${encodeURIComponent(pool.owningEnv)}/pools/${encodeURIComponent(pool.name)}`
    : undefined
  return <ResourceLink value={pool.name} href={href} />
}

// OwningClusterCell renders the cluster a pool belongs to. The local cluster is
// shown muted; a peer cluster links to that cluster's SandboxEnv detail page.
function OwningClusterCell({ pool }: { pool: AgentSandboxPool }) {
  const currentCluster = useClusterID()
  const locale = useLocale()
  const clustersData = useAtomValue(clustersAtom)
  const cid = (pool as PoolRow).owningClusterID || currentCluster
  const isLocal = cid === currentCluster
  const display = clustersData?.clusters?.find((c) => c.id === cid)?.name ?? cid
  const href = pool.owningEnv
    ? `${clusterPath(cid, "envs", locale)}/${encodeURIComponent(pool.owningEnv)}`
    : undefined
  return (
    <ResourceLink
      value={display}
      href={href}
      copyable={false}
      tone={isLocal ? "muted" : "default"}
    />
  )
}

// Per-pool autoscaling lookups, all keyed off the owning Env's spec/status.
// Bundled so the column takes a single option instead of three parallel maps.
export interface PoolScalingContext {
  // Pool name → its observed autoscaler state (state, saturatedUntil).
  observedByPool?: Map<string, AgentEnvObservedMember>
  // Pool name → its scaling-group name (env.spec.clusters.members.config).
  scalingGroupByPool?: Map<string, string>
  // Scaling-group name → its autoscaling policy (env.spec.autoscaling.groups).
  groups?: Map<string, AgentEnvAutoscalingGroup>
}

// Autoscaling cell: a single Enabled/Disabled badge that links to the scaling
// group's detail page. Enabled badges show the group's min~max range inside a
// blue badge (0~∞ when unbounded); disabled show a gray "Disabled". Autoscaler
// runtime detail (observed state, saturation window, last scale-up result) is
// tucked into the badge's hover tooltip. The group name itself is not shown —
// it is reachable by clicking through.
function ScalingCell({ pool, ctx }: { pool: AgentSandboxPool; ctx: PoolScalingContext }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  const { t } = useTranslation()

  const group = ctx.scalingGroupByPool?.get(pool.name) ?? pool.scalingGroup ?? ""
  if (!group) return <span className="text-muted-foreground text-xs">—</span>

  const groupConfig = ctx.groups?.get(group)
  const observed = ctx.observedByPool?.get(pool.name)
  const enabled = groupConfig?.enabled ?? false
  const min = groupConfig?.minReplicas ?? 0
  const max = groupConfig?.maxReplicas

  const label = enabled ? `${min} ~ ${max ?? UNBOUNDED}` : t("pools.scaling.disabled")
  const color = enabled ? SCALING_STATUS_COLORS.enabled : SCALING_STATUS_COLORS.disabled

  const state = observed?.state
  const saturatedUntil = observed?.saturatedUntil
  const lastResult = pool.status?.autoscaling?.lastScaleUpAttemptResult
  const showLastResult = Boolean(lastResult && lastResult !== "Enough")
  const hasTooltip = Boolean(state || saturatedUntil || showLastResult)

  const href = pool.owningEnv
    ? `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(pool.owningEnv)}/autoscaling/${encodeURIComponent(group)}`
    : undefined

  const badge = (
    <Badge
      variant="outline"
      className={cn("font-mono text-[10px]", color, href && "cursor-pointer")}
    >
      {label}
    </Badge>
  )

  if (!hasTooltip) {
    return href ? (
      <Link href={href} title={group} className="inline-flex w-fit">
        {badge}
      </Link>
    ) : (
      badge
    )
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          href ? (
            <Link href={href} title={group} className="inline-flex w-fit" />
          ) : (
            <span className="inline-flex w-fit" />
          )
        }
      >
        {badge}
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-60 space-y-1 text-xs">
        {state && (
          <div>
            <span className="text-muted-foreground">{t("pools.scaling.tooltip.state")}: </span>
            {state}
          </div>
        )}
        {saturatedUntil && (
          <div>
            <span className="text-muted-foreground">
              {t("pools.scaling.tooltip.saturatedUntil")}:{" "}
            </span>
            <RelativeTime date={saturatedUntil} />
          </div>
        )}
        {showLastResult && (
          <div>
            <span className="text-muted-foreground">{t("pools.scaling.tooltip.lastResult")}: </span>
            {lastResult}
          </div>
        )}
      </TooltipContent>
    </Tooltip>
  )
}

export interface PoolColumnsOptions {
  showOwner?: boolean
  // Hide the "Env" reverse-link column. Useful when the table is already
  // scoped to a single Env (e.g. the Env detail page) where the column would
  // be redundant for every row.
  hideOwningEnv?: boolean
  // When present, adds the "Autoscaling" column driven by the owning Env's
  // spec/status. Omit it (e.g. on a page already scoped to one scaling group)
  // to hide the column entirely.
  scaling?: PoolScalingContext
  // Env-scoped row actions. Each appears in the row dropdown when set.
  onEditPool?: (pool: AgentSandboxPool) => void
  onDeletePool?: (pool: AgentSandboxPool) => void
  // When true, adds an "owning cluster" column and makes name/cluster links
  // cluster-aware. Used by the Env pools view that merges foreign members.
  owningCluster?: boolean
}

/**
 * Filter dimensions for the pool table (the name text search plus the CPU /
 * memory number ranges). Spread into a page's `toolbarConfig.filterOptions` so
 * they surface both in the toolbar Filters menu and on the matching column
 * header (keyed by column id) — a magnifier for text, a funnel for the rest.
 */
export function poolNumberFilterOptions(
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
): DataTableFacetedFilterProps[] {
  return [
    {
      columnKey: "name",
      variant: "text",
      title: t("pools.col.name"),
      placeholder: t("pools.col.searchByName"),
    },
    {
      columnKey: "cpu",
      variant: "number_range",
      title: t("pools.col.cpu"),
      unit: " cores",
      placeholder: { min: t("pools.col.minCpu"), max: t("pools.col.maxCpu") },
    },
    {
      columnKey: "memory",
      variant: "number_range",
      title: t("pools.col.memory"),
      unit: "MiB",
      placeholder: { min: t("pools.col.minMemory"), max: t("pools.col.maxMemory") },
    },
  ]
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

  const scalingCtx = options?.scaling
  const scalingColumn: ColumnDef<AgentSandboxPool> = {
    id: "scaling",
    enableSorting: false,
    header: ({ column }) => (
      <DataTableColumnHeader column={column} title={t("pools.col.scaling")} />
    ),
    cell: ({ row }) => <ScalingCell pool={row.original} ctx={scalingCtx ?? {}} />,
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
        />
      ),
      cell: ({ row }) => <PoolNameCell pool={row.original} />,
      filterFn: textFilterFn,
    },
    ...(options?.owningCluster
      ? [
          {
            id: "owningCluster",
            accessorFn: (row: AgentSandboxPool) => (row as PoolRow).owningClusterID ?? "",
            header: ({ column }) => (
              <DataTableColumnHeader column={column} title={t("pools.col.owningCluster")} />
            ),
            enableSorting: false,
            cell: ({ row }) => <OwningClusterCell pool={row.original} />,
          } satisfies ColumnDef<AgentSandboxPool>,
        ]
      : []),
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
    ...(options?.scaling ? [scalingColumn] : []),
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
      // Plain count — desired replicas isn't a filterable sandbox status, so it
      // carries no drill-down link; the Running/Idle/etc. columns provide those.
      cell: ({ row }) => {
        const replicas = row.original.spec?.replicas
        if (replicas == null) return <span className="text-muted-foreground text-xs">---</span>
        return (
          <span className="text-foreground inline-flex min-w-8 font-mono text-sm font-semibold">
            {replicas}
          </span>
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
      id: "rollout",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("envs.detail.member.col.rollout")} />
      ),
      cell: ({ row }) => {
        const st = row.original.status
        const target = st?.updateRevision ?? ""
        if (!target) return <span className="text-muted-foreground text-xs">---</span>
        const rolling = (st?.currentRevision ?? "") !== target
        if (!rolling) {
          return (
            <Badge variant="outline" className="font-mono text-[10px]">
              {t("envs.detail.member.upToDate")}
            </Badge>
          )
        }
        return (
          <Badge className="font-mono text-[10px]">
            {t("envs.detail.member.rolloutInProgress", {
              updated: st?.updatedReplicas ?? 0,
              total: row.original.spec?.replicas ?? 0,
            })}
          </Badge>
        )
      },
    },
    {
      id: "cpu",
      accessorFn: (row) => parseCpuToCore(row.cpu),
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("pools.col.cpu")}
          tooltip={t("pools.col.cpuTooltip")}
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
        // Foreign (peer-cluster) rows are read-only here — edit/delete/metrics
        // target the local cluster and cannot act on another cluster's pool.
        if ((row.original as PoolRow).foreignCluster) return null
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
