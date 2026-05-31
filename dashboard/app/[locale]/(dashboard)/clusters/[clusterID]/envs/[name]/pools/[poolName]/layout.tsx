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
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useQuery } from "@tanstack/react-query"
import { ActivityIcon, Database, InfoIcon, Loader2, Pencil, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabsNav } from "@/components/custom/detail-tabs-nav"
import { UpsertPoolSheet } from "@/components/envs/upsert-pool-sheet"
import { DeletePoolDialog } from "@/components/envs/delete-pool-dialog"
import { envQueryOptions, envPoolQueryOptions } from "@/lib/queries"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"

interface LayoutProps {
  children: React.ReactNode
  params: Promise<{ clusterID: string; name: string; poolName: string; locale: string }>
}

/**
 * Shared shell for the SandboxPool detail sub-routes. Renders a fixed header
 * (pool name + edit/delete actions) and the tab bar once; each tab (overview /
 * metrics) is a child page rendered into {children}. Loading / not-found states
 * gate the body here so every sub-page can assume the pool is present.
 *
 * Lives under the Env detail subtree at `…/envs/{name}/pools/{poolName}`; the
 * Env layout yields its chrome to this one for pool-detail routes so the page
 * reads as a standalone resource detail (matching Sandbox / Env).
 */
export default function PoolDetailLayout({ children, params }: LayoutProps) {
  const { name, poolName } = use(params)
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  const { data, isLoading, isError } = useQuery(envPoolQueryOptions(name, poolName))
  const pool = data?.template ?? null

  const { data: envData } = useQuery(envQueryOptions(name))
  const env = envData?.env ?? null

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const envPath = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(name)}`
  const basePath = `${envPath}/pools/${encodeURIComponent(poolName)}`
  const tabs = [
    { value: "", label: t("pools.tab.overview"), icon: InfoIcon },
    { value: "metrics", label: t("pools.tab.metrics"), icon: ActivityIcon },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <DetailHeader
        icon={Database}
        title={poolName}
        copyValue={poolName}
        kind="SandboxPool"
        meta={[
          {
            label: t("pools.col.env"),
            value: (
              <Link
                href={`${envPath}/pools`}
                className="text-foreground hover:text-brand underline-offset-2 hover:underline"
              >
                {name}
              </Link>
            ),
          },
        ]}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!env || !pool}
              onClick={() => setEditOpen(true)}
              className="h-8 gap-1 text-xs"
            >
              <Pencil className="h-3.5 w-3.5" />
              {t("common.edit")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!pool}
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
        ) : isError || !pool ? (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-muted-foreground text-sm">{t("common.noResultsFound")}</p>
          </div>
        ) : (
          children
        )}
      </div>

      {/* Pool-level sheets & dialogs */}
      {env && <UpsertPoolSheet env={env} pool={pool} open={editOpen} onOpenChange={setEditOpen} />}
      <DeletePoolDialog
        envName={name}
        pool={deleteOpen ? pool : null}
        onOpenChange={setDeleteOpen}
        onDeleted={() => router.push(`${envPath}/pools`)}
      />
    </div>
  )
}
