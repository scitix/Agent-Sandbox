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
import { useAtomValue } from "jotai"
import { KeyRound, Plus, Upload } from "lucide-react"
import { CreateApiKeyDialog } from "@/components/api-keys/create-dialog"
import { RevokeApiKeyDialog } from "@/components/api-keys/revoke-dialog"
import { ImportSecretDialog } from "@/components/api-keys/import-secret-dialog"
import { PromoteApiKeyDialog } from "@/components/api-keys/promote-dialog"
import { createAdminApiKeyColumns } from "@/components/api-keys/admin-columns"
import type { AgentboxApiKey } from "@/lib/api/client"
import { isAdminAtom } from "@/lib/atoms"
import { apiKeysQueryOptions } from "@/lib/queries"
import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { QueryTable } from "@/components/custom/query-table/table-with-query"

export default function AdminApiKeysPage() {
  const isAdmin = useAtomValue(isAdminAtom)
  const { t } = useTranslation()

  const [createOpen, setCreateOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<AgentboxApiKey | null>(null)
  const [promoteTarget, setPromoteTarget] = useState<AgentboxApiKey | null>(null)

  const columns = useMemo(
    () =>
      createAdminApiKeyColumns(
        t,
        (key) => setRevokeTarget(key),
        (key) => setPromoteTarget(key),
      ),
    [t],
  )

  if (!isAdmin) {
    return (
      <div className="flex flex-1 flex-col overflow-auto">
        <div className="flex flex-1 items-center justify-center">
          <div className="text-center">
            <KeyRound className="text-muted-foreground mx-auto mb-3 h-10 w-10" />
            <p className="text-muted-foreground font-mono text-sm tracking-wider uppercase">
              {t("admin.accessRequired")}
            </p>
            <p className="text-muted-foreground mt-1 text-xs">{t("admin.onlyAdmins")}</p>
          </div>
        </div>
      </div>
    )
  }

  const toolbarConfig = {
    globalSearch: { placeholder: t("apiKeys.searchById") },
    filterOptions: [
      {
        columnKey: "keyId",
        variant: "text",
        title: t("apiKeys.col.keyId"),
        placeholder: t("apiKeys.searchById"),
      },
      { columnKey: "role", title: t("apiKeys.col.role") },
      { columnKey: "team", title: t("apiKeys.col.team") },
    ] as const,
    getHeader: (key: string) => {
      const headers: Record<string, string> = {
        keyId: t("apiKeys.col.keyId"),
        rawToken: t("apiKeys.col.rawToken"),
        role: t("apiKeys.col.role"),
        team: t("apiKeys.col.team"),
        user: t("apiKeys.col.user"),
        description: t("apiKeys.col.description"),
        quotaURL: t("apiKeys.col.quotaURL"),
        issuedAt: t("apiKeys.col.issuedAt"),
        expiresAt: t("apiKeys.col.expiresAt"),
      }
      return headers[key] || key
    },
    hiddenColumns: ["quotaURL", "rawToken"],
  }

  const toolbarActions = (
    <div className="flex shrink-0 items-center gap-2">
      <Button
        variant="outline"
        size="sm"
        onClick={() => setImportOpen(true)}
        className="h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
      >
        <Upload className="h-3 w-3" />
        {t("common.import")}
      </Button>
      <Button
        size="sm"
        onClick={() => setCreateOpen(true)}
        className="bg-foreground text-background hover:bg-foreground/90 h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
      >
        <Plus className="h-3 w-3" />
        {t("apiKeys.createTitle")}
      </Button>
    </div>
  )

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col">
        <QueryTable
          queryOptions={apiKeysQueryOptions()}
          columns={columns}
          idFn={(row) => row.keyId}
          toolbarConfig={toolbarConfig}
          className="table-layout-fixed h-full"
        >
          {toolbarActions}
        </QueryTable>
      </div>

      <CreateApiKeyDialog open={createOpen} onOpenChange={setCreateOpen} />
      <ImportSecretDialog open={importOpen} onOpenChange={setImportOpen} />
      <RevokeApiKeyDialog
        apiKey={revokeTarget}
        onOpenChange={(open) => {
          if (!open) setRevokeTarget(null)
        }}
      />
      <PromoteApiKeyDialog
        apiKey={promoteTarget}
        onOpenChange={(open) => {
          if (!open) setPromoteTarget(null)
        }}
      />
    </div>
  )
}
