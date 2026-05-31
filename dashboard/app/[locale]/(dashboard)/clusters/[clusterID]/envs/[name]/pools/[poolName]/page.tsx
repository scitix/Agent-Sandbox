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

import { use } from "react"
import { useQuery } from "@tanstack/react-query"

import { RelativeTime } from "@/components/custom/relative-time"
import { StatusBadge } from "@/components/custom/status-badge"
import { POOL_PHASE_COLORS } from "@/components/pools/columns"
import { envPoolQueryOptions } from "@/lib/queries"
import type { AgentSandboxPool } from "@/lib/api/client"
import { useTranslation, type TranslationKey } from "@/lib/i18n"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; poolName: string; locale: string }>
}

/**
 * Pool detail index — the Overview view. Lives at `…/pools/{poolName}` (no
 * `/overview` sub-route) so the resource's own URL doubles as its default tab.
 * Surfaces the fields returned by the Pool GET endpoint; Metrics is a sibling
 * sub-route.
 */
export default function PoolOverviewPage({ params }: PageProps) {
  const { name, poolName } = use(params)
  const { t } = useTranslation()
  const { data } = useQuery(envPoolQueryOptions(name, poolName))
  const pool = data?.template ?? null

  if (!pool) return null

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <PoolOverview pool={pool} t={t} />
    </div>
  )
}

function PoolOverview({
  pool,
  t,
}: {
  pool: AgentSandboxPool
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
}) {
  const status = pool.status
  const templateValue =
    pool.templateVersion && pool.spec.templateName
      ? `${pool.spec.templateName} · ${pool.templateVersion}`
      : (pool.spec.templateName ?? "—")

  const cells: { label: string; value: React.ReactNode }[] = [
    {
      label: t("pools.col.phase"),
      value: <StatusBadge status={status?.phase ?? null} colorMap={POOL_PHASE_COLORS} />,
    },
    { label: t("pools.col.replicas"), value: pool.spec.replicas },
    { label: t("pools.col.idle"), value: status?.idleReplicas ?? 0 },
    { label: t("pools.col.running"), value: status?.runningReplicas ?? 0 },
    { label: t("pools.col.starting"), value: status?.startingReplicas ?? 0 },
    { label: t("pools.col.stopping"), value: status?.stoppingReplicas ?? 0 },
    { label: t("pools.col.failed"), value: status?.failedReplicas ?? 0 },
    ...(status?.pendingRequests != null
      ? [{ label: t("pools.detail.pendingRequests"), value: status.pendingRequests }]
      : []),
    ...(pool.cpu ? [{ label: t("pools.col.cpu"), value: pool.cpu }] : []),
    ...(pool.memory ? [{ label: t("pools.col.memory"), value: pool.memory }] : []),
    {
      label: t("pools.col.scaling"),
      value: pool.scalingGroup || t("pools.detail.scalingGroupNone"),
    },
    { label: t("pools.col.template"), value: templateValue },
    { label: t("pools.col.namespace"), value: pool.namespace },
    ...(pool.owningEnv ? [{ label: t("pools.col.env"), value: pool.owningEnv }] : []),
    ...(pool.createdAt
      ? [{ label: t("pools.col.createdAt"), value: <RelativeTime date={pool.createdAt} /> }]
      : []),
  ]

  // Pad to complete the last 4-column row so bg-border doesn't bleed through.
  const padCount = (4 - (cells.length % 4)) % 4

  return (
    <div className="p-6">
      <div className="border-border bg-border grid grid-cols-2 gap-px overflow-hidden rounded-md border lg:grid-cols-4">
        {cells.map((cell) => (
          <div key={cell.label} className="bg-card flex flex-col gap-1.5 px-3.5 py-3">
            <span className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
              {cell.label}
            </span>
            <div className="font-mono text-xs font-medium">{cell.value}</div>
          </div>
        ))}
        {Array.from({ length: padCount }).map((_, i) => (
          <div key={`pad-${i}`} className="bg-card" />
        ))}
      </div>
    </div>
  )
}
