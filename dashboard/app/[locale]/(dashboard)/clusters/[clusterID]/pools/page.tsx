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

import { useMemo, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { parseAsString, useQueryState } from "nuqs"
import { Plus, Trash2 } from "lucide-react"
import { PageHeader } from "@/components/page-header"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createPoolColumns } from "@/components/pools/columns"
import { CreatePoolSheet } from "@/components/pools/create-pool-sheet"
import { SyncTemplateSheet } from "@/components/pools/sync-template-sheet"
import { DeletePoolDialog } from "@/components/pools/delete-dialog"
import { PoolMetricsSheet } from "@/components/prometheus/pool-metrics-sheet"
import { PoolDocsSheet, POOL_DOCS_PARAM } from "@/components/pools/pool-docs-sheet"
import { poolsQueryOptions, deletePoolImperative } from "@/lib/queries"
import type { AgentSandboxPool } from "@/lib/api/client"
import { Button } from "@/components/ui/button"
import type { MultipleHandler } from "@/components/custom/query-table/pagination"
import type { Row } from "@tanstack/react-table"
import { toast } from "sonner"
import { useTableSearchParams, type FilterColumnDef } from "@/hooks/use-table-search-params"
import { useTranslation } from "@/lib/i18n"

const poolFilterColumns: FilterColumnDef[] = [
  { id: "name", type: "string" },
  { id: "templateName", type: "string" },
  { id: "cpu", type: "number-range" },
  { id: "memory", type: "number-range" },
]

export default function PoolsPage() {
  const { t } = useTranslation()
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AgentSandboxPool | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandboxPool | null>(null)
  const [syncTarget, setSyncTarget] = useState<AgentSandboxPool | null>(null)
  const [metricsTarget, setMetricsTarget] = useState<AgentSandboxPool | null>(null)
  const [, setPoolDocsName] = useQueryState(
    POOL_DOCS_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const qc = useQueryClient()
  const tableState = useTableSearchParams(poolFilterColumns)

  const queryOptions = poolsQueryOptions()

  const columns = useMemo(
    () =>
      createPoolColumns(
        t,
        (pool) => setEditTarget(pool),
        (pool) => setDeleteTarget(pool),
        (pool) => setSyncTarget(pool),
        (pool) => setMetricsTarget(pool),
        (pool) => void setPoolDocsName(pool.name),
      ),
    [t, setPoolDocsName],
  )

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
                  <span className="text-muted-foreground ml-2">
                    ({row.original.spec?.replicas ?? 0} replicas)
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      ),
      handleSubmit: async (rows: Row<AgentSandboxPool>[]) => {
        const results = await Promise.allSettled(
          rows.map((row) => deletePoolImperative(row.original.name)),
        )
        const failed = results.filter((r) => r.status === "rejected").length
        const succeeded = results.length - failed
        if (succeeded > 0) {
          toast.success(t("pools.deletedCount", { count: succeeded }))
          void qc.invalidateQueries({ queryKey: queryOptions.queryKey })
        }
        if (failed > 0) {
          toast.error(t("pools.deleteFailedCount", { count: failed }))
        }
      },
      isDanger: true,
    },
  ]

  const toolbar = (
    <div>
      <Button
        size="sm"
        onClick={() => setCreateOpen(true)}
        className="bg-foreground text-background hover:bg-foreground/90 h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
      >
        <Plus className="h-3 w-3" />
        {t("pools.newPool")}
      </Button>
    </div>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={t("pools.title")} />

      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          columns={columns}
          idFn={(row) => row.name}
          queryOptions={queryOptions}
          toolbarConfig={{ globalSearch: { placeholder: t("pools.searchAll") } }}
          multipleHandlers={multipleHandlers}
          externalState={tableState}
          className="table-layout-fixed h-[calc(100vh-76px)]"
        >
          {toolbar}
        </QueryTable>
      </div>

      <CreatePoolSheet open={createOpen} onOpenChange={setCreateOpen} />
      <CreatePoolSheet
        pool={editTarget}
        open={!!editTarget}
        mode="edit"
        onOpenChange={(open) => {
          if (!open) setEditTarget(null)
        }}
      />
      <DeletePoolDialog
        pool={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
      <SyncTemplateSheet
        pool={syncTarget}
        onOpenChange={(open) => {
          if (!open) setSyncTarget(null)
        }}
      />
      <PoolMetricsSheet
        pool={metricsTarget}
        onOpenChange={(open) => {
          if (!open) setMetricsTarget(null)
        }}
      />
      <PoolDocsSheet />
    </div>
  )
}
