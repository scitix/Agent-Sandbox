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

import { type ColumnDef } from "@tanstack/react-table"
import { Globe, Trash2 } from "lucide-react"
import { type AgentboxApiKey } from "@/lib/api/client"
import { DataTableColumnHeader } from "@/components/custom/query-table/column-header"
import { CopyableText } from "@/components/custom/copyable-text"
import { RelativeTime } from "@/components/custom/relative-time"
import { Badge } from "@/components/ui/badge"
import TooltipButton from "@/components/custom/button/tooltip-button"
import type { TranslationKey } from "@/lib/i18n"

export function createAdminApiKeyColumns(
  t: (key: TranslationKey, params?: Record<string, string | number>) => string,
  onRevoke: (key: AgentboxApiKey) => void,
  onPromote?: (key: AgentboxApiKey) => void,
): ColumnDef<AgentboxApiKey>[] {
  return [
    {
      accessorKey: "keyId",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t("apiKeys.col.keyId")}
          includesStringFilterOptions={{ placeholder: t("apiKeys.searchById") }}
        />
      ),
      cell: ({ row }) => {
        const id = row.original.keyId
        return <CopyableText value={id} label={id} className="font-mono text-xs" />
      },
    },
    {
      accessorKey: "rawToken",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.rawToken")} />
      ),
      enableHiding: true,
      cell: ({ row }) => {
        const raw = row.original.rawToken
        if (!raw) return <span className="text-muted-foreground font-mono text-xs">—</span>
        const masked = raw.length > 17 ? raw.slice(0, 9) + "..." + raw.slice(-4) : raw
        return <CopyableText value={raw} label={masked} className="font-mono text-xs" />
      },
    },
    {
      accessorKey: "role",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.role")} />
      ),
      cell: ({ row }) => (
        <Badge variant="outline" className="font-mono text-xs">
          {row.original.role}
        </Badge>
      ),
    },
    {
      accessorKey: "team",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.team")} />
      ),
      cell: ({ row }) => {
        const team = row.original.team
        if (!team) return <span className="text-muted-foreground text-xs">—</span>
        return <span className="font-mono text-xs">{team}</span>
      },
    },
    {
      accessorKey: "user",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.user")} />
      ),
      cell: ({ row }) => {
        const user = row.original.user
        if (!user) return <span className="text-muted-foreground text-xs">—</span>
        return <span className="font-mono text-xs">{user}</span>
      },
    },
    {
      accessorKey: "description",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.description")} />
      ),
      enableHiding: true,
      cell: ({ row }) => {
        const desc = row.original.description
        if (!desc) return <span className="text-muted-foreground text-xs">—</span>
        return <span className="text-muted-foreground text-xs">{desc}</span>
      },
    },
    {
      accessorKey: "issuedAt",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.issuedAt")} />
      ),
      cell: ({ row }) => <RelativeTime date={row.original.issuedAt} />,
      sortingFn: (a, b) => {
        const da = new Date(a.original.issuedAt ?? 0).getTime()
        const db = new Date(b.original.issuedAt ?? 0).getTime()
        return da - db
      },
    },
    {
      accessorKey: "expiresAt",
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t("apiKeys.col.expiresAt")} />
      ),
      enableHiding: true,
      cell: ({ row }) => <RelativeTime date={row.original.expiresAt} />,
      sortingFn: (a, b) => {
        const da = new Date(a.original.expiresAt ?? 0).getTime()
        const db = new Date(b.original.expiresAt ?? 0).getTime()
        return da - db
      },
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          {onPromote && row.original.syncSource !== "global" && (
            <TooltipButton
              variant="ghost"
              size="icon-sm"
              tooltip={t("apiKeys.promoteTitle")}
              side="top"
              onClick={() => onPromote(row.original)}
              className="text-muted-foreground hover:text-blue-500 h-7 w-7"
            >
              <Globe className="h-3.5 w-3.5" />
            </TooltipButton>
          )}
          <TooltipButton
            variant="ghost"
            size="icon-sm"
            tooltip={t("apiKeys.revokeTitle")}
            side="top"
            onClick={() => onRevoke(row.original)}
            className="text-muted-foreground hover:text-destructive h-7 w-7"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </TooltipButton>
        </div>
      ),
    },
  ]
}

