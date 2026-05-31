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
import { useRouter } from "next/navigation"
import { useQueryClient } from "@tanstack/react-query"
import type { Row } from "@tanstack/react-table"
import { Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import type { MultipleHandler } from "@/components/custom/query-table/pagination"
import { createEnvColumns } from "@/components/envs/columns"
import { UpsertEnvSheet } from "@/components/envs/upsert-env-sheet"
import { DeleteEnvDialog } from "@/components/envs/delete-env-dialog"
import { envsQueryOptions, deleteEnvImperative } from "@/lib/queries"
import type { AgentSandboxEnvSummary } from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTableSearchParams, type FilterColumnDef } from "@/hooks/use-table-search-params"
import { useTranslation } from "@/lib/i18n"

const envFilterColumns: FilterColumnDef[] = [
  { id: "name", type: "string" },
  { id: "templateRef", type: "string" },
  { id: "mode", type: "string" },
]

export default function EnvsPage() {
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const [createOpen, setCreateOpen] = useState(false)
  const [editEnvTarget, setEditEnvTarget] = useState<AgentSandboxEnvSummary | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandboxEnvSummary | null>(null)
  const tableState = useTableSearchParams(envFilterColumns)
  const queryOptions = envsQueryOptions()
  const qc = useQueryClient()

  // Name clicks navigate to the 3-level detail page so the URL is
  // shareable and the browser back-stack works as users expect.
  const goToDetail = (env: AgentSandboxEnvSummary) => {
    router.push(`${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(env.name)}`)
  }

  const columns = useMemo(
    () =>
      createEnvColumns(
        t,
        clusterID,
        locale,
        goToDetail,
        (env) => setEditEnvTarget(env),
        (env) => setDeleteTarget(env),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, clusterID, locale],
  )

  const multipleHandlers: MultipleHandler<AgentSandboxEnvSummary>[] = [
    {
      icon: <Trash2 className="h-3.5 w-3.5" />,
      title: (rows: Row<AgentSandboxEnvSummary>[]) =>
        t("envs.deleteCount", { count: rows.length }),
      description: (rows: Row<AgentSandboxEnvSummary>[]) => (
        <div className="space-y-2">
          <p>{t("envs.deleteCountDescription")}</p>
          <div className="max-h-48 overflow-auto">
            <ul className="space-y-1">
              {rows.map((row) => (
                <li key={row.original.name} className="text-foreground font-mono text-xs">
                  {row.original.name}
                  {(row.original.memberCount ?? 0) > 0 && (
                    <span className="text-muted-foreground ml-2">
                      ({row.original.memberCount} pools)
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
      ),
      handleSubmit: async (rows: Row<AgentSandboxEnvSummary>[]) => {
        const results = await Promise.allSettled(
          rows.map((row) => deleteEnvImperative(row.original.name)),
        )
        const failed = results.filter((r) => r.status === "rejected").length
        const succeeded = results.length - failed
        if (succeeded > 0) {
          toast.success(t("envs.deletedCount", { count: succeeded }))
          void qc.invalidateQueries({ queryKey: queryOptions.queryKey })
        }
        if (failed > 0) {
          toast.error(t("envs.deleteFailedCount", { count: failed }))
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
        {t("envs.newEnv")}
      </Button>
    </div>
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          columns={columns}
          idFn={(row) => row.name}
          queryOptions={queryOptions}
          multipleHandlers={multipleHandlers}
          toolbarConfig={{ globalSearch: { placeholder: t("envs.searchAll") } }}
          externalState={tableState}
          className="table-layout-fixed h-full"
        >
          {toolbar}
        </QueryTable>
      </div>

      <UpsertEnvSheet open={createOpen} onOpenChange={setCreateOpen} />
      <UpsertEnvSheet
        envName={editEnvTarget?.name ?? null}
        open={!!editEnvTarget}
        onOpenChange={(open) => {
          if (!open) setEditEnvTarget(null)
        }}
      />
      <DeleteEnvDialog
        env={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
    </div>
  )
}
