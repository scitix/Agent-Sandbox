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
import { useRouter } from "next/navigation"
import { parseAsString, useQueryState } from "nuqs"
import { Box, FileTextIcon, InfoIcon, Loader2, TerminalSquare, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabsNav } from "@/components/custom/detail-tabs-nav"
import { DeleteSandboxDialog } from "@/components/sandboxes/delete-dialog"
import { TerminalDialog, TERMINAL_SANDBOX_ID_PARAM } from "@/components/sandboxes/terminal-dialog"
import { sandboxQueryOptions } from "@/lib/queries"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

interface LayoutProps {
  children: React.ReactNode
  params: Promise<{ clusterID: string; id: string; locale: string }>
}

/**
 * Shared shell for the Sandbox detail sub-routes: a fixed header (id + actions)
 * and the tab bar. Each tab (overview / logs) is a child page rendered into
 * {children}. Loading / not-found states gate the body here.
 */
export default function SandboxDetailLayout({ children, params }: LayoutProps) {
  const { id } = use(params)
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  const { data, isLoading } = useQuery(sandboxQueryOptions(id))
  const sandbox = data?.sandbox ?? null

  const [, setTerminalForId] = useQueryState(
    TERMINAL_SANDBOX_ID_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const [deleteOpen, setDeleteOpen] = useState(false)

  const isRunning = sandbox?.status === "Running"
  const basePath = `${clusterPath(clusterID, "sandboxes", locale)}/${encodeURIComponent(id)}`
  const tabs = [
    { value: "", label: t("sandboxes.tab.overview"), icon: InfoIcon },
    { value: "logs", label: t("sandboxes.logs"), icon: FileTextIcon },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Fixed header — status + metadata live in the Overview tab, not here */}
      <DetailHeader
        icon={Box}
        title={id}
        copyValue={id}
        kind="Sandbox"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!isRunning}
              onClick={() => void setTerminalForId(id)}
              className="h-8 gap-1 text-xs"
            >
              <TerminalSquare className="h-3.5 w-3.5" />
              {t("sandboxes.terminal")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!sandbox}
              onClick={() => setDeleteOpen(true)}
              className="text-destructive hover:text-destructive h-8 gap-1 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("common.delete")}
            </Button>
          </>
        }
      />

      <DetailTabsNav basePath={basePath} tabs={tabs} />

      {/* Body — sub-page content, gated on load/not-found state */}
      <div className="flex min-h-0 flex-1 flex-col">
        {isLoading ? (
          <div className="flex flex-1 items-center justify-center">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        ) : !sandbox ? (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-muted-foreground text-sm">{t("common.noResultsFound")}</p>
          </div>
        ) : (
          children
        )}
      </div>

      <TerminalDialog />
      <DeleteSandboxDialog
        sandbox={deleteOpen ? sandbox : null}
        onOpenChange={(open) => setDeleteOpen(open)}
        onDeleted={() => router.push(clusterPath(clusterID, "sandboxes", locale))}
      />
    </div>
  )
}
