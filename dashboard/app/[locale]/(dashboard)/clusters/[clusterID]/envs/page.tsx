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
import { Plus } from "lucide-react"

import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createEnvColumns } from "@/components/envs/columns"
import { EditAutoscalingSheet } from "@/components/envs/edit-autoscaling-sheet"
import { CreateEnvSheet } from "@/components/envs/create-env-sheet"
import { DeleteEnvDialog } from "@/components/envs/delete-env-dialog"
import { envsQueryOptions } from "@/lib/queries"
import type { AgentSandboxEnv } from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
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
  const [editTarget, setEditTarget] = useState<AgentSandboxEnv | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [editEnvTarget, setEditEnvTarget] = useState<AgentSandboxEnv | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandboxEnv | null>(null)
  const tableState = useTableSearchParams(envFilterColumns)
  const queryOptions = envsQueryOptions()

  // Name clicks navigate to the 3-level detail page so the URL is
  // shareable and the browser back-stack works as users expect.
  const goToDetail = (env: AgentSandboxEnv) => {
    router.push(`${clusterPath(clusterID, "envs")}/${encodeURIComponent(env.name)}`)
  }

  const columns = useMemo(
    () =>
      createEnvColumns(
        t,
        goToDetail,
        (env) => setEditTarget(env),
        (env) => setEditEnvTarget(env),
        (env) => setDeleteTarget(env),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, clusterID],
  )

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
      <PageHeader title={t("envs.title")} />

      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          columns={columns}
          idFn={(row) => row.name}
          queryOptions={queryOptions}
          toolbarConfig={{ globalSearch: { placeholder: t("envs.searchAll") } }}
          externalState={tableState}
          className="table-layout-fixed h-[calc(100vh-104px)]"
        >
          {toolbar}
        </QueryTable>
      </div>

      <CreateEnvSheet open={createOpen} onOpenChange={setCreateOpen} />
      <CreateEnvSheet
        env={editEnvTarget}
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
      <EditAutoscalingSheet
        env={editTarget}
        onOpenChange={(open: boolean) => {
          if (!open) setEditTarget(null)
        }}
      />
    </div>
  )
}
