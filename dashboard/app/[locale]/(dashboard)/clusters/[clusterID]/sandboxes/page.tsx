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
import { useRouter } from "next/navigation"
import { Plus, Trash2 } from "lucide-react"
import { parseAsString, useQueryState } from "nuqs"
import { useTranslation } from "@/lib/i18n"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createSandboxColumns } from "@/components/sandboxes/columns"
import { TerminalDialog, TERMINAL_SANDBOX_ID_PARAM } from "@/components/sandboxes/terminal-dialog"
import { CreateSandboxDialog } from "@/components/sandboxes/create-dialog"
import { DeleteSandboxDialog } from "@/components/sandboxes/delete-dialog"
import { sandboxesQueryOptions, deleteSandboxImperative } from "@/lib/queries"
import type { AgentSandbox } from "@/lib/api/client"
import { Button } from "@/components/ui/button"
import type { DataTableToolbarConfig } from "@/components/custom/query-table/toolbar"
import type { MultipleHandler } from "@/components/custom/query-table/pagination"
import type { Row } from "@tanstack/react-table"
import { toast } from "sonner"
import { useTableSearchParams, type FilterColumnDef } from "@/hooks/use-table-search-params"
import { useAtomValue } from "jotai"
import { isActualAdminAtom } from "@/lib/atoms"
import type { TranslationKey } from "@/lib/i18n"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useExternalLogsConfigured } from "@/hooks/use-external-logs"

const sandboxFilterColumns: FilterColumnDef[] = [
  { id: "sandboxId", type: "string" },
  { id: "envName", type: "faceted" },
  { id: "poolName", type: "faceted" },
  { id: "status", type: "faceted" },
  { id: "cpu", type: "number-range" },
  { id: "memory", type: "number-range" },
]

export default function SandboxesPage() {
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<AgentSandbox | null>(null)
  const [, setTerminalForId] = useQueryState(
    TERMINAL_SANDBOX_ID_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const qc = useQueryClient()
  const tableState = useTableSearchParams(sandboxFilterColumns)

  const queryOptions = sandboxesQueryOptions({ limit: 0 })
  const isActualAdmin = useAtomValue(isActualAdminAtom)
  const isExternalLogsConfigured = useExternalLogsConfigured()

  // The view actions deep-link into the consolidated detail page's tabs;
  // Terminal stays an in-place dialog and Delete an in-place confirm.
  const goToDetail = (sandbox: AgentSandbox, tab?: string) => {
    const base = `${clusterPath(clusterID, "sandboxes", locale)}/${encodeURIComponent(sandbox.sandboxId)}`
    router.push(tab ? `${base}?tab=${tab}` : base)
  }

  const columns = useMemo(
    () =>
      createSandboxColumns(
        t,
        (sandbox) => setDeleteTarget(sandbox),
        (sandbox) => void setTerminalForId(sandbox.sandboxId),
        (sandbox) => goToDetail(sandbox, "logs"),
        {
          isActualAdmin,
          isExternalLogsConfigured,
        },
        (sandbox) => goToDetail(sandbox, "metrics"),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, setTerminalForId, isActualAdmin, isExternalLogsConfigured, clusterID, locale],
  )

  const multipleHandlers: MultipleHandler<AgentSandbox>[] = [
    {
      icon: <Trash2 className="h-3.5 w-3.5" />,
      title: (rows: Row<AgentSandbox>[]) => t("sandboxes.deleteCount", { count: rows.length }),
      description: (rows: Row<AgentSandbox>[]) => (
        <div className="space-y-2">
          <p>{t("sandboxes.deleteCountDescription")}</p>
          <div className="max-h-48 overflow-auto">
            <ul className="space-y-1">
              {rows.map((row) => (
                <li key={row.original.sandboxId} className="text-foreground font-mono text-xs">
                  {row.original.sandboxId}
                  <span className="text-muted-foreground ml-2">({row.original.poolName})</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
      ),
      handleSubmit: async (rows: Row<AgentSandbox>[]) => {
        const results = await Promise.allSettled(
          rows.map((row) => deleteSandboxImperative(row.original.sandboxId)),
        )
        const failed = results.filter((r) => r.status === "rejected").length
        const succeeded = results.length - failed
        if (succeeded > 0) {
          toast.success(
            t("sandboxes.deletedSuccess", {
              id: `${succeeded} sandbox${succeeded !== 1 ? "es" : ""}`,
            }),
          )
          void qc.invalidateQueries({ queryKey: queryOptions.queryKey })
        }
        if (failed > 0) {
          toast.error(`Failed to delete ${failed} sandbox${failed !== 1 ? "es" : ""}`)
        }
      },
      isDanger: true,
    },
  ]

  const toolbarConfig: DataTableToolbarConfig = {
    globalSearch: { placeholder: t("common.searchAll") },
    filterOptions: [
      {
        columnKey: "status",
        title: t("sandboxes.col.status"),
        renderer: (value: string) => {
          const key = value.toLowerCase()
          const map: Record<string, TranslationKey> = {
            running: "sandboxes.col.statusRunning",
            starting: "sandboxes.col.statusStarting",
            stopping: "sandboxes.col.statusStopping",
            failed: "sandboxes.col.statusFailed",
            pending: "sandboxes.col.statusPending",
            completed: "sandboxes.col.statusCompleted",
            canceled: "sandboxes.col.statusCanceled",
          }
          return map[key] ? t(map[key]) : value
        },
      },
      { columnKey: "envName", title: t("sandboxes.col.env") },
      { columnKey: "poolName", title: t("sandboxes.col.pool") },
    ] as const,
    getHeader: (key) => {
      const headers: Record<string, string> = {
        sandboxId: t("sandboxes.col.id"),
        envName: t("sandboxes.col.env"),
        poolName: t("sandboxes.col.pool"),
        status: t("sandboxes.col.status"),
        claimedAt: t("sandboxes.col.claimedAt"),
        startedAt: t("sandboxes.col.startedAt"),
        terminatedAt: t("sandboxes.col.terminatedAt"),
        recycledAt: t("sandboxes.col.recycledAt"),
        podName: t("sandboxes.col.podName"),
        images: t("sandboxes.col.images"),
        nodeName: t("sandboxes.col.nodeName"),
        containerId: t("sandboxes.col.containerId"),
        durationSeconds: t("sandboxes.col.duration"),
      }
      return headers[key] || key
    },
    hiddenColumns: ["poolName", "podName", "recycledAt", "images", "nodeName", "containerId", "durationSeconds"],
  }

  const toolbarActions = (
    <div className="flex shrink-0 items-center gap-2">
      <Button
        size="sm"
        onClick={() => setCreateOpen(true)}
        className="bg-foreground text-background hover:bg-foreground/90 h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
      >
        <Plus className="h-3 w-3" />
        {t("sandboxes.newSandbox")}
      </Button>
    </div>
  )

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          idFn={(row) => row.sandboxId}
          columns={columns}
          queryOptions={queryOptions}
          toolbarConfig={toolbarConfig}
          multipleHandlers={multipleHandlers}
          externalState={tableState}
          className="table-layout-fixed h-full"
        >
          {toolbarActions}
        </QueryTable>
      </div>

      <CreateSandboxDialog open={createOpen} onOpenChange={setCreateOpen} />
      <DeleteSandboxDialog
        sandbox={deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
      <TerminalDialog />
    </div>
  )
}
