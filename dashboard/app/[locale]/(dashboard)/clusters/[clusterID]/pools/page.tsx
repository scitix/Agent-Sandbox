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
import { parseAsString, useQueryState } from "nuqs"
import { PageHeader } from "@/components/page-header"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createPoolColumns } from "@/components/pools/columns"
import { PoolMetricsSheet } from "@/components/prometheus/pool-metrics-sheet"
import { PoolDocsSheet, POOL_DOCS_PARAM } from "@/components/pools/pool-docs-sheet"
import { poolsQueryOptions } from "@/lib/queries"
import type { AgentSandboxPool } from "@/lib/api/client"
import { useTableSearchParams, type FilterColumnDef } from "@/hooks/use-table-search-params"
import { useTranslation } from "@/lib/i18n"

const poolFilterColumns: FilterColumnDef[] = [
  { id: "name", type: "string" },
  { id: "templateName", type: "string" },
  { id: "cpu", type: "number-range" },
  { id: "memory", type: "number-range" },
]

// Pools page is read-only — Pool lifecycle is managed through SandboxEnv.
// The table surfaces live status and a metrics / docs drill-down per row.
export default function PoolsPage() {
  const { t } = useTranslation()
  const [metricsTarget, setMetricsTarget] = useState<AgentSandboxPool | null>(null)
  const [, setPoolDocsName] = useQueryState(
    POOL_DOCS_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const tableState = useTableSearchParams(poolFilterColumns)

  const queryOptions = poolsQueryOptions()

  const columns = useMemo(
    () =>
      createPoolColumns(
        t,
        (pool) => setMetricsTarget(pool),
        (pool) => void setPoolDocsName(pool.name),
      ),
    [t, setPoolDocsName],
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
          externalState={tableState}
          className="table-layout-fixed h-[calc(100vh-76px)]"
        />
      </div>

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
