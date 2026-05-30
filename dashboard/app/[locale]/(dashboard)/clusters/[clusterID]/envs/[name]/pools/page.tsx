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

import { use, useState } from "react"
import { useQuery } from "@tanstack/react-query"

import { EnvPoolsSection } from "@/components/envs/env-detail-sections"
import { UpsertPoolSheet } from "@/components/envs/upsert-pool-sheet"
import { DeletePoolDialog } from "@/components/envs/delete-pool-dialog"
import { PoolMetricsSheet } from "@/components/prometheus/pool-metrics-sheet"
import { envQueryOptions } from "@/lib/queries"
import type { AgentSandboxPool } from "@/lib/api/client"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/** Pools tab — full-height pool table + create/edit/delete/metrics dialogs. */
export default function EnvPoolsPage({ params }: PageProps) {
  const { name } = use(params)
  const { data } = useQuery(envQueryOptions(name))
  const env = data?.env

  const [createPoolOpen, setCreatePoolOpen] = useState(false)
  const [editPoolTarget, setEditPoolTarget] = useState<AgentSandboxPool | null>(null)
  const [deletePoolTarget, setDeletePoolTarget] = useState<AgentSandboxPool | null>(null)
  const [metricsTarget, setMetricsTarget] = useState<AgentSandboxPool | null>(null)

  if (!env) return null

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <EnvPoolsSection
        env={env}
        onCreatePool={() => setCreatePoolOpen(true)}
        onEditPool={(pool) => setEditPoolTarget(pool)}
        onDeletePool={(pool) => setDeletePoolTarget(pool)}
        onViewMetrics={(pool) => setMetricsTarget(pool)}
        fixed
      />

      <UpsertPoolSheet
        env={env}
        pool={null}
        open={createPoolOpen}
        onOpenChange={setCreatePoolOpen}
      />
      <UpsertPoolSheet
        env={env}
        pool={editPoolTarget}
        open={!!editPoolTarget}
        onOpenChange={(open) => {
          if (!open) setEditPoolTarget(null)
        }}
      />
      <DeletePoolDialog
        envName={env.name}
        pool={deletePoolTarget}
        onOpenChange={(open) => {
          if (!open) setDeletePoolTarget(null)
        }}
      />
      <PoolMetricsSheet
        pool={metricsTarget}
        onOpenChange={(open) => {
          if (!open) setMetricsTarget(null)
        }}
      />
    </div>
  )
}
