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

import { useMemo } from "react"
import { type ColumnDef, type Row } from "@tanstack/react-table"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { MoreVertical, Pencil, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { DataTable } from "@/components/custom/query-table/table-without-query"
import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { ResourceLink } from "@/components/custom/resource-link"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import type { MultipleHandler } from "@/components/custom/query-table/pagination"
import { createPoolColumns, poolNumberFilterOptions, type PoolRow } from "@/components/pools/columns"
import {
  deleteEnvAutoscalingGroupImperative,
  deleteEnvPoolImperative,
  envPoolsQueryOptions,
} from "@/lib/queries"
import type {
  AgentEnvAutoscalingGroup,
  AgentEnvObservedMember,
  AgentSandboxEnv,
  AgentSandboxPool,
} from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"

// ─── Spec ────────────────────────────────────────────────────────────────────

export function SpecSection({ env }: { env: AgentSandboxEnv }) {
  const { t } = useTranslation()
  const local = env.spec.clusters?.find((c) => c.members && c.members.length > 0)
  return (
    <section>
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("envs.detail.section.spec")}
      </h3>
      <dl className="border-border bg-muted/20 divide-border grid grid-cols-2 divide-x divide-y rounded border text-xs">
        <Row label={t("envs.detail.field.template")} value={env.spec.templateRef.name} />
        <Row label={t("envs.detail.field.mode")} value={env.spec.mode} />
        <Row
          label={t("envs.detail.field.defaults")}
          value={
            env.spec.defaults
              ? `${env.spec.defaults.instanceType ?? "—"} × ${env.spec.defaults.multiplier ?? 1}`
              : "—"
          }
        />
        <Row label={t("envs.detail.field.localCluster")} value={local?.clusterID ?? "—"} />
      </dl>
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <dt className="text-muted-foreground w-32 shrink-0 font-mono text-xs uppercase">{label}</dt>
      <dd className="truncate font-mono text-xs">{value}</dd>
    </div>
  )
}

// ─── Pools (full table — replaces the standalone /pools page for Env-owned pools) ──

export function EnvPoolsSection({
  env,
  onCreatePool,
  onEditPool,
  onDeletePool,
  onViewMetrics,
  fixed = false,
}: {
  env: AgentSandboxEnv
  onCreatePool: () => void
  onEditPool: (pool: AgentSandboxPool) => void
  onDeletePool: (pool: AgentSandboxPool) => void
  onViewMetrics: (pool: AgentSandboxPool) => void
  fixed?: boolean
}) {
  const { t } = useTranslation()
  const currentCluster = useClusterID()
  const poolsQuery = useQuery(envPoolsQueryOptions(env.name))

  // Build scalingGroup + observed lookups from the Env spec/status so the
  // pool table can surface autoscaler-derived info that the bare /sandboxpools
  // response doesn't carry.
  const { observedByPool, scalingGroupByPool, autoscalingGroups, memberCount } = useMemo(() => {
    // Local-only: keyed by pool name, which is unique within a cluster. Foreign
    // members are attached per-row instead (their names can collide with local
    // ones across clusters), so this map stays unambiguous for local rows.
    const observed = new Map<string, AgentEnvObservedMember>()
    const groups = new Map<string, string>()
    for (const cluster of env.status?.clusters ?? []) {
      if (cluster.isLocal === false && cluster.clusterID !== currentCluster) continue
      for (const om of cluster.observedMembers ?? []) {
        observed.set(om.name, om)
      }
    }
    let count = 0
    for (const cluster of env.spec.clusters ?? []) {
      for (const m of cluster.members ?? []) {
        if (m.config?.scalingGroup) groups.set(m.name, m.config.scalingGroup)
        count++
      }
    }
    const ag = new Map<string, AgentEnvAutoscalingGroup>(
      (env.spec.autoscaling?.groups ?? []).map((g) => [g.name, g]),
    )
    return {
      observedByPool: observed,
      scalingGroupByPool: groups,
      autoscalingGroups: ag,
      memberCount: count,
    }
  }, [env, currentCluster])

  // Merge local pools with foreign members surfaced in status.clusters[] so a
  // single table shows the whole cross-cluster picture. Local rows sort first;
  // foreign rows carry only the counts available in status (idle/running/
  // desired), tagged with their owning cluster for the "owning cluster" column.
  // Each row also carries its Env observed-member view so the autoscaling
  // columns (group / on-off / headroom) render uniformly across clusters.
  const rows = useMemo<PoolRow[]>(() => {
    const localRows: PoolRow[] = (poolsQuery.data ?? []).map((p) => ({
      ...p,
      owningClusterID: currentCluster,
      foreignCluster: false,
      observed: observedByPool.get(p.name),
    }))
    const foreignRows: PoolRow[] = []
    for (const cluster of env.status?.clusters ?? []) {
      if (cluster.isLocal === true || cluster.clusterID === currentCluster) continue
      for (const om of cluster.observedMembers ?? []) {
        foreignRows.push({
          name: om.name,
          owningEnv: env.name,
          owningClusterID: cluster.clusterID,
          foreignCluster: true,
          spec: { replicas: om.desiredReplicas },
          status: { idleReplicas: om.idleCount, runningReplicas: om.runningCount },
          observed: om,
        } as PoolRow)
      }
    }
    return [...localRows, ...foreignRows]
  }, [poolsQuery.data, env, currentCluster, observedByPool])

  const columns = useMemo(
    () =>
      createPoolColumns(t, onViewMetrics, {
        hideOwningEnv: true,
        owningCluster: true,
        scaling: {
          observedByPool,
          scalingGroupByPool,
          groups: autoscalingGroups,
        },
        onEditPool,
        onDeletePool,
      }),
    [
      t,
      onViewMetrics,
      observedByPool,
      scalingGroupByPool,
      autoscalingGroups,
      onEditPool,
      onDeletePool,
    ],
  )

  const qc = useQueryClient()

  const multipleHandlers: MultipleHandler<AgentSandboxPool>[] = [
    {
      icon: <Trash2 className="h-3.5 w-3.5" />,
      title: (rows: Row<AgentSandboxPool>[]) => t("pools.deleteCount", { count: rows.length }),
      description: (rows: Row<AgentSandboxPool>[]) => (
        <div className="space-y-2">
          <p>{t("pools.deleteCountDescription")}</p>
          <div className="max-h-48 overflow-auto">
            <ul className="space-y-1">
              {rows.map((row) => (
                <li key={row.original.name} className="text-foreground font-mono text-xs">
                  {row.original.name}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ),
      handleSubmit: async (rows: Row<AgentSandboxPool>[]) => {
        // Only local pools can be deleted from here; peer-cluster rows are
        // read-only (their pools live on another cluster's API).
        const local = rows.filter((row) => !(row.original as PoolRow).foreignCluster)
        if (local.length === 0) return
        const results = await Promise.allSettled(
          local.map((row) => deleteEnvPoolImperative(env.name, row.original.name)),
        )
        const failed = results.filter((r) => r.status === "rejected").length
        const succeeded = results.length - failed
        if (succeeded > 0) {
          toast.success(t("pools.deletedCount", { count: succeeded }))
          void qc.invalidateQueries({
            queryKey: [
              "get",
              "/envs/{name}/sandboxpools",
              { params: { path: { name: env.name } } },
            ],
          })
        }
        if (failed > 0) {
          toast.error(t("pools.deleteFailedCount", { count: failed }))
        }
      },
      isDanger: true,
    },
  ]

  const createButton = (
    <Button onClick={onCreatePool} size="sm" className="h-9 gap-1 px-2 text-xs" variant="secondary">
      <Plus className="h-3 w-3" /> {t("envs.poolForm.createAction")}
    </Button>
  )

  const table = (
    <DataTable<AgentSandboxPool>
      columns={columns}
      data={rows}
      idFn={(row) => `${(row as PoolRow).owningClusterID ?? currentCluster}/${row.name}`}
      isLoading={poolsQuery.isLoading}
      dataUpdatedAt={poolsQuery.dataUpdatedAt}
      refetch={poolsQuery.refetch}
      multipleHandlers={multipleHandlers}
      toolbarConfig={{
        filterOptions: poolNumberFilterOptions(t),
        globalSearch: { placeholder: t("pools.searchAll") },
      }}
      className={fixed ? "table-layout-fixed h-full" : undefined}
    >
      {createButton}
    </DataTable>
  )

  if (fixed) return table

  return (
    <section>
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.detail.section.pools")}
          </h3>
          <span className="text-muted-foreground font-mono text-[10px]">
            {t("envs.detail.pools.memberCount", { count: memberCount })}
          </span>
        </div>
      </div>
      {table}
    </section>
  )
}

// ─── Autoscaling table ───────────────────────────────────────────────────────

// Scaling-group name → its standalone autoscaling-group detail page, listing
// the group's policy and every member Pool in the group.
function GroupNameCell({ envName, group }: { envName: string; group: string }) {
  const clusterID = useClusterID()
  const locale = useLocale()
  const href = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(envName)}/autoscaling/${encodeURIComponent(group)}`
  return <ResourceLink value={group} href={href} />
}

export function AutoscalingSection({
  env,
  onEdit,
  onDelete,
  fixed = false,
}: {
  env: AgentSandboxEnv
  onEdit: (group: AgentEnvAutoscalingGroup) => void
  onDelete: (group: AgentEnvAutoscalingGroup) => void
  fixed?: boolean
}) {
  const { t } = useTranslation()
  const groups = env.spec.autoscaling?.groups ?? []

  const columns = useMemo<ColumnDef<AgentEnvAutoscalingGroup>[]>(
    () => [
      {
        accessorKey: "name",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("envs.autoscaling.col.group")} />
        ),
        cell: ({ row }) => <GroupNameCell envName={env.name} group={row.original.name} />,
      },
      {
        id: "enabled",
        accessorFn: (row) => row.enabled,
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("envs.autoscaling.col.enabled")} />
        ),
        cell: ({ row }) => (
          <Badge
            variant={row.original.enabled ? "default" : "outline"}
            className="font-mono text-[10px]"
          >
            {row.original.enabled
              ? t("envs.detail.autoscaling.enabled")
              : t("envs.detail.autoscaling.disabled")}
          </Badge>
        ),
      },
      {
        id: "replicas",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("envs.autoscaling.col.replicas")} />
        ),
        cell: ({ row }) => {
          const min = row.original.minReplicas
          const max = row.original.maxReplicas
          const label = `${min ?? "—"} ~ ${max ?? "—"}`
          return <span className="text-muted-foreground font-mono text-xs">{label}</span>
        },
      },
      {
        id: "scaleUp",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("envs.autoscaling.col.scaleUp")} />
        ),
        cell: ({ row }) => <ScaleUpCell group={row.original} />,
      },
      {
        id: "scaleDown",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("envs.autoscaling.col.scaleDown")} />
        ),
        cell: ({ row }) => <ScaleDownCell group={row.original} />,
      },
      {
        id: "actions",
        cell: ({ row }) => (
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
              <DropdownMenuItem
                onClick={() => onEdit(row.original)}
                className="cursor-pointer font-mono text-xs"
              >
                <Pencil className="mr-2 h-3.5 w-3.5" />
                {t("envs.upsertAutoscaling.editAction")}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => onDelete(row.original)}
                className="text-destructive cursor-pointer font-mono text-xs"
              >
                <Trash2 className="mr-2 h-3.5 w-3.5" />
                {t("envs.upsertAutoscaling.deleteAction")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ),
      },
    ],
    [t, env.name, onEdit, onDelete],
  )

  const qc = useQueryClient()
  const multipleHandlers: MultipleHandler<AgentEnvAutoscalingGroup>[] = [
    {
      icon: <Trash2 className="h-3.5 w-3.5" />,
      title: (rows: Row<AgentEnvAutoscalingGroup>[]) =>
        t("envs.autoscaling.deleteCount", { count: rows.length }),
      description: (rows: Row<AgentEnvAutoscalingGroup>[]) => (
        <div className="space-y-2">
          <p>{t("envs.autoscaling.deleteCountDescription")}</p>
          <div className="max-h-48 overflow-auto">
            <ul className="space-y-1">
              {rows.map((row) => (
                <li key={row.original.name} className="text-foreground font-mono text-xs">
                  {row.original.name}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ),
      handleSubmit: async (rows: Row<AgentEnvAutoscalingGroup>[]) => {
        const results = await Promise.allSettled(
          rows.map((row) => deleteEnvAutoscalingGroupImperative(env.name, row.original.name)),
        )
        const failed = results.filter((r) => r.status === "rejected").length
        const succeeded = results.length - failed
        if (succeeded > 0) {
          toast.success(t("envs.autoscaling.deletedCount", { count: succeeded }))
          void qc.invalidateQueries({
            queryKey: ["get", "/envs/{name}", { params: { path: { name: env.name } } }],
          })
        }
        if (failed > 0) {
          toast.error(t("envs.autoscaling.deleteFailedCount", { count: failed }))
        }
      },
      isDanger: true,
    },
  ]

  if (fixed) {
    return (
      <DataTable
        data={groups}
        dataUpdatedAt={0}
        isLoading={false}
        columns={columns}
        idFn={(row: AgentEnvAutoscalingGroup) => row.name}
        multipleHandlers={multipleHandlers}
        toolbarConfig={{ globalSearch: { placeholder: t("common.search") } }}
        className="table-layout-fixed h-full"
      />
    )
  }

  return (
    <section>
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.detail.section.autoscaling")}
        </h3>
      </div>
      <DataTable
        data={groups}
        dataUpdatedAt={0}
        isLoading={false}
        columns={columns}
        idFn={(row: AgentEnvAutoscalingGroup) => row.name}
        multipleHandlers={multipleHandlers}
        toolbarConfig={{ globalSearch: { placeholder: t("common.search") } }}
      />
    </section>
  )
}

export function ScaleUpCell({ group }: { group: AgentEnvAutoscalingGroup }) {
  const { t } = useTranslation()
  const mode = group.scaleUpPolicy?.mode
  const cd = group.scaleUpPolicy?.cooldownSeconds
  const idle = group.scaleUpPolicy?.idleThresholdSeconds
  return (
    <div className="font-mono text-[11px] leading-tight">
      {mode && <div className="text-foreground">{mode}</div>}
      <div className="text-muted-foreground">
        {t("envs.detail.autoscaling.cooldown")}: {fmt(cd, "s")}
      </div>
      <div className="text-muted-foreground">
        {t("envs.detail.autoscaling.idleThreshold")}: {fmt(idle, "s")}
      </div>
    </div>
  )
}

export function ScaleDownCell({ group }: { group: AgentEnvAutoscalingGroup }) {
  const { t } = useTranslation()
  const idle = group.scaleDownPolicy?.idleTimeoutSeconds
  const stab = group.scaleDownPolicy?.stabilizationSeconds
  return (
    <div className="font-mono text-[11px] leading-tight">
      <div className="text-muted-foreground">
        {t("envs.detail.autoscaling.idleTimeout")}: {fmt(idle, "s")}
      </div>
      <div className="text-muted-foreground">
        {t("envs.detail.autoscaling.stabilization")}: {fmt(stab, "s")}
      </div>
    </div>
  )
}

function fmt(value: number | undefined, suffix: string): string {
  if (value === undefined || value === null) return "—"
  return `${value}${suffix}`
}
