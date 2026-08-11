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
import { usePathname } from "next/navigation"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  Boxes,
  DatabaseIcon,
  InfoIcon,
  Loader2,
  Network,
  Pencil,
  TrendingUp,
  Trash2,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabsNav } from "@/components/custom/detail-tabs-nav"
import { UpsertEnvSheet } from "@/components/envs/upsert-env-sheet"
import { DeleteEnvDialog } from "@/components/envs/delete-env-dialog"
import { ExtendEnvDialog } from "@/components/envs/extend-env-dialog"
import { ApiKeyRequiredNotice } from "@/components/custom/api-key-required-notice"
import { envQueryOptions } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"

interface LayoutProps {
  children: React.ReactNode
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/**
 * Shared shell for the Env detail sub-routes. Renders the persistent header and
 * tab bar once; each tab (overview / pools / autoscaling / metrics) is a child
 * page rendered into {children}. Loading / error / API-key states gate the body
 * here so every sub-page can assume the Env is present.
 */
export default function EnvDetailLayout({ children, params }: LayoutProps) {
  const { name } = use(params)
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()
  const pathname = usePathname()

  const { data, isLoading, isError, error } = useQuery(envQueryOptions(name))
  const env = data?.env

  const isApiKeyRequired =
    (error as { errorCode?: string } | null)?.errorCode === "API_KEY_REQUIRED"

  const [editOpen, setEditOpen] = useState(false)
  const [extendOpen, setExtendOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  // A specific child detail route (`…/pools/{poolName}` or
  // `…/autoscaling/{group}`) renders its own header, so the Env shell yields
  // its chrome and just forwards the child subtree. The bare list routes
  // (`…/pools`, `…/autoscaling`) keep the Env chrome.
  const isChildDetailRoute = /\/(pools|autoscaling)\/[^/]+/.test(pathname)
  if (isChildDetailRoute) return <>{children}</>

  const basePath = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(name)}`
  const tabs = [
    { value: "", label: t("envs.tab.overview"), icon: InfoIcon },
    { value: "pools", label: t("envs.tab.pools"), icon: DatabaseIcon },
    { value: "autoscaling", label: t("envs.tab.autoscaling"), icon: TrendingUp },
    { value: "metrics", label: t("envs.tab.metrics"), icon: ActivityIcon },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header — always shown; title comes from the URL param */}
      <DetailHeader
        icon={Boxes}
        title={name}
        copyValue={name}
        kind="SandboxEnv"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!env}
              onClick={() => setEditOpen(true)}
              className="h-8 gap-1 text-xs"
            >
              <Pencil className="h-3.5 w-3.5" />
              {t("envs.action.edit")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!env}
              onClick={() => setExtendOpen(true)}
              className="h-8 gap-1 text-xs"
            >
              <Network className="h-3.5 w-3.5" />
              {t("envs.action.extend")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!env}
              onClick={() => setDeleteOpen(true)}
              className="text-destructive hover:text-destructive h-8 gap-1 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("envs.action.delete")}
            </Button>
          </>
        }
      />

      <DetailTabsNav basePath={basePath} tabs={tabs} />

      {/* Body — sub-page content, gated on load/error/api-key state */}
      <div className="flex min-h-0 flex-1 flex-col">
        {isLoading ? (
          <div className="flex flex-1 items-center justify-center">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        ) : isApiKeyRequired ? (
          <ApiKeyRequiredNotice description={t("envs.apiKeyRequired.envDocsDescription")} />
        ) : isError || !env ? (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-muted-foreground text-sm">{t("envs.empty")}</p>
          </div>
        ) : (
          children
        )}
      </div>

      {/* Env-level sheets & dialogs */}
      <UpsertEnvSheet envName={env?.name ?? null} open={editOpen} onOpenChange={setEditOpen} />
      <ExtendEnvDialog env={env ?? null} open={extendOpen} onOpenChange={setExtendOpen} />
      <DeleteEnvDialog
        env={deleteOpen && env ? { name: env.name, memberCount: env.status?.memberCount } : null}
        onOpenChange={(open) => setDeleteOpen(open)}
      />
    </div>
  )
}
