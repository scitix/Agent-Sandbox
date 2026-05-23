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

import { useEffect, useMemo, useState } from "react"
import { parseAsString, useQueryState } from "nuqs"

import { PageHeader } from "@/components/page-header"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createEnvColumns } from "@/components/envs/columns"
import { EnvDetailSheet } from "@/components/envs/env-detail-sheet"
import { EditAutoscalingSheet } from "@/components/envs/edit-autoscaling-sheet"
import { ENV_DETAIL_PARAM } from "@/components/envs/constants"
import { envsQueryOptions } from "@/lib/queries"
import type { AgentSandboxEnv } from "@/lib/api/client"
import { useTableSearchParams, type FilterColumnDef } from "@/hooks/use-table-search-params"
import { useTranslation } from "@/lib/i18n"

const envFilterColumns: FilterColumnDef[] = [
  { id: "name", type: "string" },
  { id: "templateRef", type: "string" },
  { id: "mode", type: "string" },
]

export default function EnvsPage() {
  const { t } = useTranslation()
  const [detailTarget, setDetailTarget] = useState<AgentSandboxEnv | null>(null)
  const [editTarget, setEditTarget] = useState<AgentSandboxEnv | null>(null)
  const [autoOpenName, setAutoOpenName] = useQueryState(
    ENV_DETAIL_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const tableState = useTableSearchParams(envFilterColumns)
  const queryOptions = envsQueryOptions()

  const columns = useMemo(
    () =>
      createEnvColumns(
        t,
        (env) => setDetailTarget(env),
        (env) => setEditTarget(env),
      ),
    [t],
  )

  // When the URL carries `?env=<name>` (reverse link from the Pool list),
  // we open the detail sheet for that env once the data has loaded. Cleared
  // when the user closes the sheet.
  useEffect(() => {
    if (!autoOpenName || detailTarget) return
    const data = queryOptions.queryKey
    // queryOptions.queryKey is the stable cache key; we let the QueryTable
    // load the list and then re-check via a lightweight effect below. To
    // avoid a second fetch here we just stash the name into a virtual env
    // stub and let the detail sheet's own useQuery fetch the full env.
    setDetailTarget({
      name: autoOpenName,
      namespace: "",
      spec: { templateRef: { name: "" }, mode: "WarmPool" },
    } as AgentSandboxEnv)
    void data // silence unused warning
  }, [autoOpenName, detailTarget, queryOptions.queryKey])

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={t("envs.title")} />

      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          columns={columns}
          idFn={(row) => row.name}
          queryOptions={queryOptions}
          externalState={tableState}
          className="table-layout-fixed h-[calc(100vh-104px)]"
        />
      </div>

      <EnvDetailSheet
        env={detailTarget}
        onOpenChange={(open: boolean) => {
          if (!open) {
            setDetailTarget(null)
            if (autoOpenName) void setAutoOpenName(null)
          }
        }}
        onEditAutoscaling={(env: AgentSandboxEnv) => {
          setDetailTarget(null)
          setEditTarget(env)
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
