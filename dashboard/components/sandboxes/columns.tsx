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

import * as React from "react"
import { type ColumnDef } from "@tanstack/react-table"
import { type AgentSandbox } from "@/lib/api/client"
import { Trash2, TerminalSquare, MoreVertical, Activity, FileTextIcon } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { CopyableText } from "@/components/custom/copyable-text"
import { ResourceLink } from "@/components/custom/resource-link"
import { RelativeTime } from "@/components/custom/relative-time"
import { parseCpuToCore, parseMemoryToMiB, formatCores, formatMiB } from "@/lib/resources"
import { Button } from "../ui/button"
import TooltipButton from "@/components/custom/button/tooltip-button"
import { StatusBadge, type StatusBadgeColorMap } from "@/components/custom/status-badge"
import type { TranslationKey } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"

export const SANDBOX_STATUS_COLORS: StatusBadgeColorMap = {
  running: "bg-green-500/15 text-green-700 dark:text-green-400 border-green-500/30",
  starting: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
  activating: "bg-yellow-500/15 text-yellow-700 dark:text-yellow-400 border-yellow-500/30",
  stopping: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  recycling: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  failed: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  faulty: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
  canceled: "bg-orange-500/15 text-orange-700 dark:text-orange-400 border-orange-500/30",
}

const TERMINAL_STATUSES = new Set(["released", "failed", "completed", "canceled"])

export function isTerminalStatus(status: string): boolean {
  return TERMINAL_STATUSES.has(status?.toLowerCase())
}

const STATUS_I18N_KEYS: Record<string, TranslationKey> = {
  running: "sandboxes.col.statusRunning",
  starting: "sandboxes.col.statusStarting",
  stopping: "sandboxes.col.statusStopping",
  failed: "sandboxes.col.statusFailed",
  pending: "sandboxes.col.statusPending",
  completed: "sandboxes.col.statusCompleted",
  canceled: "sandboxes.col.statusCanceled",
}

function SandboxStatusBadge({
  sandbox,
  t,
}: {
  sandbox: AgentSandbox
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
}) {
  const isTerminal = isTerminalStatus(sandbox.status)
  const hasFailureInfo = isTerminal && !!(sandbox.failureReason || sandbox.failureMessage)
  const isRunning = sandbox.status?.toLowerCase() === "running"
  const hasImages =
    isRunning && sandbox.containerImages && Object.keys(sandbox.containerImages).length > 0
  const showHover = hasFailureInfo || !!sandbox.statusDetail || hasImages

  let hoverContent: React.ReactNode = undefined
  if (showHover) {
    if (hasFailureInfo) {
      hoverContent = (
        <div className="space-y-1.5">
          {sandbox.failureReason && (
            <div className="text-foreground font-semibold">{sandbox.failureReason}</div>
          )}
          {sandbox.failureMessage && (
            <div className="text-muted-foreground break-all">{sandbox.failureMessage}</div>
          )}
        </div>
      )
    } else if (hasImages) {
      hoverContent = (
        <div className="space-y-2">
          {Object.entries(sandbox.containerImages!).map(([name, image]) => {
            const shortImage = image.includes("/") ? (image.split("/").pop() ?? image) : image
            return (
              <div key={name} className="space-y-0.5">
                <div className="text-muted-foreground font-semibold uppercase">{name}</div>
                <div className="text-foreground font-mono break-all">{shortImage}</div>
              </div>
            )
          })}
        </div>
      )
    } else if (sandbox.statusDetail) {
      hoverContent = (
        <div className="space-y-1.5">
          <div className="text-foreground font-semibold">{sandbox.statusDetail.reason}</div>
          <div className="text-muted-foreground wrap-break-word">
            {sandbox.statusDetail.message}
          </div>
          {sandbox.statusDetail.lastUpdatedTime && (
            <div className="text-muted-foreground/60 text-xs">
              Updated: <RelativeTime date={sandbox.statusDetail.lastUpdatedTime} />
            </div>
          )}
        </div>
      )
    }
  }

  const rawStatus = sandbox.status
  const statusKey = rawStatus ? STATUS_I18N_KEYS[rawStatus.toLowerCase()] : undefined
  const statusLabel = statusKey ? t(statusKey) : (rawStatus ?? "---")

  return (
    <StatusBadge
      status={rawStatus || "---"}
      label={statusLabel || "---"}
      colorMap={SANDBOX_STATUS_COLORS}
      hoverContent={hoverContent}
    />
  )
}

// Sandbox id → detail page. Shows the short (last-segment) id but links to the
// full consolidated detail route, and copies the full id.
function SandboxIdCell({ id }: { id: string }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  const shortId = id.length > 12 ? id.slice(-12) : id
  const href = `${clusterPath(clusterID, "sandboxes", locale)}/${encodeURIComponent(id)}`
  return <ResourceLink value={id} label={shortId} href={href} />
}

function EnvNameCell({ envName }: { envName: string | undefined }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  if (!envName) return <span className="text-muted-foreground text-xs">---</span>
  const href = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(envName)}`
  return <ResourceLink value={envName} href={href} />
}

// Pool name → the pool's detail page under its Env. Links only when the owning
// env name is known; otherwise renders a copy-only label.
function PoolNameCell({
  poolName,
  envName,
}: {
  poolName: string | undefined
  envName: string | undefined
}) {
  const clusterID = useClusterID()
  const locale = useLocale()
  if (!poolName) return <span className="text-muted-foreground text-xs">---</span>
  const href = envName
    ? `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(envName)}/pools/${encodeURIComponent(poolName)}`
    : undefined
  return <ResourceLink value={poolName} href={href} tone="muted" />
}

export function createSandboxColumns(
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
  onDelete: (sandbox: AgentSandbox) => void,
  onOpenTerminal?: (sandbox: AgentSandbox) => void,
  onViewLogs?: (sandbox: AgentSandbox) => void,
  options?: { isActualAdmin?: boolean; isExternalLogsConfigured?: boolean },
  onViewMetrics?: (sandbox: AgentSandbox) => void,
  actionsWithDropdownMenu = false,
): ColumnDef<AgentSandbox>[] {
  const podNameColumn: ColumnDef<AgentSandbox> = {
    accessorKey: "podName",
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={t("sandboxes.col.podName")}
        tooltip={t("sandboxes.col.podNameTooltip")}
      />
    ),
    cell: ({ row }) => {
      const podName = row.original.podName
      if (!podName) return <span className="text-muted-foreground text-xs">---</span>
      return <CopyableText value={podName} label={podName} className="font-mono text-xs" />
    },
  }

  const actionsColumn: ColumnDef<AgentSandbox> = {
    id: "actions",
    enableSorting: false,
    enableHiding: false,
    cell: ({ row }) => {
      const isTerminal = isTerminalStatus(row.original.status)
      const isCanceled = row.original.status?.toLowerCase() === "canceled"
      const isLogsDisabled = options?.isExternalLogsConfigured ? isCanceled : isTerminal
      const isCompleted = isTerminal
      const isRunning = row.original.status === "Running"

      if (actionsWithDropdownMenu) {
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
            <DropdownMenuContent align="end" className="w-40">
              {onOpenTerminal && (
                <DropdownMenuItem
                  onClick={() => onOpenTerminal(row.original)}
                  disabled={!isRunning}
                  className="cursor-pointer font-mono text-xs"
                >
                  <TerminalSquare className="mr-2 h-3.5 w-3.5" />
                  {t("sandboxes.terminal")}
                </DropdownMenuItem>
              )}
              {onViewLogs && (
                <DropdownMenuItem
                  onClick={() => onViewLogs(row.original)}
                  disabled={isLogsDisabled}
                  className="cursor-pointer font-mono text-xs"
                >
                  <FileTextIcon className="mr-2 h-3.5 w-3.5" />
                  {t("sandboxes.logs")}
                </DropdownMenuItem>
              )}
              {onViewMetrics && (
                <DropdownMenuItem
                  onClick={() => onViewMetrics(row.original)}
                  disabled={!row.original.podName}
                  className="cursor-pointer font-mono text-xs"
                >
                  <Activity className="mr-2 h-3.5 w-3.5" />
                  {t("sandboxes.metrics")}
                </DropdownMenuItem>
              )}
              {!isCompleted && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onClick={() => onDelete(row.original)}
                    className="text-destructive focus:text-destructive cursor-pointer font-mono text-xs"
                  >
                    <Trash2 className="mr-2 h-3.5 w-3.5" />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )
      }

      // Inline icon-button row (default)
      return (
        <div className="flex items-center gap-0.5">
          {onOpenTerminal && (
            <TooltipButton
              variant="ghost"
              size="icon-sm"
              tooltip={t("sandboxes.terminal")}
              side="top"
              onClick={() => onOpenTerminal(row.original)}
              disabled={!isRunning}
              className="text-muted-foreground h-7 w-7 disabled:opacity-30"
            >
              <TerminalSquare className="h-3.5 w-3.5" />
            </TooltipButton>
          )}
          {onViewLogs && (
            <TooltipButton
              variant="ghost"
              size="icon-sm"
              tooltip={t("sandboxes.logs")}
              side="top"
              onClick={() => onViewLogs(row.original)}
              disabled={isLogsDisabled}
              className="text-muted-foreground h-7 w-7 disabled:opacity-30"
            >
              <FileTextIcon className="h-3.5 w-3.5" />
            </TooltipButton>
          )}
          {onViewMetrics && (
            <TooltipButton
              variant="ghost"
              size="icon-sm"
              tooltip={t("sandboxes.metrics")}
              side="top"
              onClick={() => onViewMetrics(row.original)}
              disabled={!row.original.podName}
              className="text-muted-foreground h-7 w-7 disabled:opacity-30"
            >
              <Activity className="h-3.5 w-3.5" />
            </TooltipButton>
          )}
          {!isCompleted ? (
            <TooltipButton
              variant="ghost"
              size="icon-sm"
              tooltip={t("common.delete")}
              side="top"
              onClick={() => onDelete(row.original)}
              className="text-muted-foreground hover:text-destructive h-7 w-7"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </TooltipButton>
          ) : (
            // Placeholder to keep column width consistent when sandbox is in terminal state
            <div className="h-7 w-7" />
          )}
        </div>
      )
    },
  }

  const imagesColumn: ColumnDef<AgentSandbox> = {
    id: "images",
    accessorFn: (row) => {
      const imgs = row.containerImages
      if (!imgs) return ""
      return Object.values(imgs).join(", ")
    },
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={t("sandboxes.col.images")}
        tooltip={t("sandboxes.col.imagesTooltip")}
      />
    ),
    enableHiding: true,
    cell: ({ row }) => {
      const images = row.original.containerImages
      if (!images || Object.keys(images).length === 0)
        return <span className="text-muted-foreground text-xs">---</span>
      return (
        <div className="flex flex-col gap-0.5">
          {Object.entries(images).map(([name, image]) => {
            const shortImage = image.includes("/") ? (image.split("/").pop() ?? image) : image
            return (
              <CopyableText
                value={image}
                label={shortImage}
                className="font-mono text-xs"
                key={name}
              />
            )
          })}
        </div>
      )
    },
  }

  const nodeNameColumn: ColumnDef<AgentSandbox> = {
    accessorKey: "nodeName",
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={t("sandboxes.col.nodeName")}
        tooltip={t("sandboxes.col.nodeNameTooltip")}
      />
    ),
    enableHiding: true,
    cell: ({ row }) => {
      const nodeName = row.original.nodeName
      if (!nodeName) return <span className="text-muted-foreground text-xs">---</span>
      return <CopyableText value={nodeName} label={nodeName} className="font-mono text-xs" />
    },
  }

  const containerIdColumn: ColumnDef<AgentSandbox> = {
    accessorKey: "containerId",
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={t("sandboxes.col.containerId")}
        tooltip={t("sandboxes.col.containerIdTooltip")}
      />
    ),
    enableHiding: true,
    cell: ({ row }) => {
      const id = row.original.containerId
      if (!id) return <span className="text-muted-foreground text-xs">---</span>
      const shortId = id.length > 20 ? "…" + id.slice(-20) : id
      return <CopyableText value={id} label={shortId} className="font-mono text-xs" />
    },
  }

  const durationColumn: ColumnDef<AgentSandbox> = {
    accessorKey: "durationSeconds",
    header: ({ column }) => (
      <DataTableColumnHeader
        column={column}
        title={t("sandboxes.col.duration")}
        tooltip={t("sandboxes.col.durationTooltip")}
      />
    ),
    enableHiding: true,
    sortingFn: (a, b) => {
      const da = a.original.durationSeconds ?? -1
      const db = b.original.durationSeconds ?? -1
      return da - db
    },
    cell: ({ row }) => {
      const secs = row.original.durationSeconds
      if (secs == null) return <span className="text-muted-foreground text-xs">---</span>
      const h = Math.floor(secs / 3600)
      const m = Math.floor((secs % 3600) / 60)
      const s = secs % 60
      const parts: string[] = []
      if (h > 0) parts.push(`${h}h`)
      if (m > 0 || h > 0) parts.push(`${m}m`)
      parts.push(`${s}s`)
      return <span className="font-mono text-xs tabular-nums">{parts.join(" ")}</span>
    },
  }

  return [
    ...(options?.isActualAdmin ? [podNameColumn] : []),
    {
      accessorKey: "sandboxId",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.id")}
          tooltip={t("sandboxes.col.idTooltip")}
          includesStringFilterOptions={{ placeholder: t("sandboxes.searchById") }}
        />
      ),
      cell: ({ row }) => <SandboxIdCell id={row.original.sandboxId} />,
    },
    {
      accessorKey: "envName",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.env")}
          tooltip={t("sandboxes.col.envTooltip")}
        />
      ),
      cell: ({ row }) => <EnvNameCell envName={row.original.envName ?? undefined} />,
      filterFn: (row, _columnId, filterValue: string[]) => {
        if (!filterValue || filterValue.length === 0) return true
        return filterValue.includes(row.original.envName ?? "")
      },
    },
    {
      accessorKey: "poolName",
      enableHiding: true,
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.pool")}
          tooltip={t("sandboxes.col.poolTooltip")}
        />
      ),
      cell: ({ row }) => (
        <PoolNameCell
          poolName={row.original.poolName}
          envName={row.original.envName ?? undefined}
        />
      ),
      filterFn: (row, _columnId, filterValue: string[]) => {
        if (!filterValue || filterValue.length === 0) return true
        return filterValue.includes(row.original.poolName)
      },
    },
    {
      accessorKey: "status",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.status")}
          tooltip={t("sandboxes.col.statusTooltip")}
        />
      ),
      cell: ({ row }) => <SandboxStatusBadge sandbox={row.original} t={t} />,
      filterFn: (row, _columnId, filterValue: string[]) => {
        if (!filterValue || filterValue.length === 0) return true
        return filterValue.includes(row.original.status)
      },
    },
    {
      id: "cpu",
      accessorFn: (row) => parseCpuToCore(row.cpu),
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title="CPU"
          tooltip={t("sandboxes.col.cpuTooltip")}
          numberRangeFilterOptions={{
            unit: " cores",
            placeholder: { min: t("sandboxes.col.minCpu"), max: t("sandboxes.col.maxCpu") },
          }}
        />
      ),
      cell: ({ row }) => {
        const cpu = row.original.cpu
        const cores = parseCpuToCore(cpu)
        return cores != null ? (
          <span className="text-muted-foreground font-mono text-[12px]">{formatCores(cores)}</span>
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
          title="Memory"
          tooltip={t("sandboxes.col.memoryTooltip")}
          numberRangeFilterOptions={{
            unit: "MiB",
            placeholder: { min: t("sandboxes.col.minMemory"), max: t("sandboxes.col.maxMemory") },
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
    {
      accessorKey: "claimedAt",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.claimedAt")}
          tooltip={t("sandboxes.col.claimedAtTooltip")}
        />
      ),
      cell: ({ row }) => <RelativeTime date={row.original.claimedAt} />,
      sortingFn: (a, b) => {
        const da = new Date(a.original.claimedAt ?? 0).getTime()
        const db = new Date(b.original.claimedAt ?? 0).getTime()
        return da - db
      },
    },
    {
      accessorKey: "startedAt",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.startedAt")}
          tooltip={t("sandboxes.col.startedAtTooltip")}
        />
      ),
      cell: ({ row }) => <RelativeTime date={row.original.startedAt} />,
      sortingFn: (a, b) => {
        const da = new Date(a.original.startedAt ?? 0).getTime()
        const db = new Date(b.original.startedAt ?? 0).getTime()
        return da - db
      },
    },
    {
      accessorKey: "terminatedAt",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.terminatedAt")}
          tooltip={t("sandboxes.col.terminatedAtTooltip")}
        />
      ),
      cell: ({ row }) => <RelativeTime date={row.original.terminatedAt} />,
    },
    {
      accessorKey: "recycledAt",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("sandboxes.col.recycledAt")}
          tooltip={t("sandboxes.col.recycledAtTooltip")}
        />
      ),
      cell: ({ row }) => <RelativeTime date={row.original.recycledAt} />,
    },
    imagesColumn,
    nodeNameColumn,
    containerIdColumn,
    durationColumn,
    actionsColumn,
  ]
}
