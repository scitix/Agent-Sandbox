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
import { parseAsString, useQueryState } from "nuqs"
import Link from "next/link"
import { ArrowLeft, Boxes, FileText, Pencil, RefreshCw, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import {
  AutoscalingSection,
  EnvPoolsSection,
  SpecSection,
} from "@/components/envs/env-detail-sections"
import { UpsertEnvSheet } from "@/components/envs/upsert-env-sheet"
import { DeleteEnvDialog } from "@/components/envs/delete-env-dialog"
import { UpsertPoolSheet } from "@/components/envs/upsert-pool-sheet"
import { DeletePoolDialog } from "@/components/envs/delete-pool-dialog"
import { UpsertAutoscalingGroupSheet } from "@/components/envs/upsert-autoscaling-group-sheet"
import { DeleteAutoscalingGroupDialog } from "@/components/envs/delete-autoscaling-group-dialog"
import { PoolMetricsSheet } from "@/components/prometheus/pool-metrics-sheet"
import { EnvDocsSheet, ENV_DOCS_PARAM } from "@/components/envs/env-docs-sheet"
import { envQueryOptions, useSyncEnvTemplate } from "@/lib/queries"
import type { AgentEnvAutoscalingGroup, AgentSandboxPool } from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import { useTranslation } from "@/lib/i18n"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/**
 * 3-level detail page for a single SandboxEnv. Replaces the old
 * EnvDetailSheet — having a real route gives us a shareable URL, browser
 * back-stack support, and room for richer toolbars (Edit / Delete / Sync /
 * Edit Autoscaling) that didn't fit cleanly in the sheet.
 *
 * Data lives in the same envQueryOptions everything else uses; the page
 * composes the existing detail sections so we don't duplicate layout logic.
 */
export default function EnvDetailPage({ params }: PageProps) {
  const { clusterID, name } = use(params)
  const { t } = useTranslation()
  const { data, isLoading, isError } = useQuery(envQueryOptions(name))
  const env = data?.env

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [metricsTarget, setMetricsTarget] = useState<AgentSandboxPool | null>(null)
  const [createPoolOpen, setCreatePoolOpen] = useState(false)
  const [editPoolTarget, setEditPoolTarget] = useState<AgentSandboxPool | null>(null)
  const [deletePoolTarget, setDeletePoolTarget] = useState<AgentSandboxPool | null>(null)
  const [createAutoscalingOpen, setCreateAutoscalingOpen] = useState(false)
  const [editAutoscalingTarget, setEditAutoscalingTarget] =
    useState<AgentEnvAutoscalingGroup | null>(null)
  const [deleteAutoscalingTarget, setDeleteAutoscalingTarget] =
    useState<AgentEnvAutoscalingGroup | null>(null)
  const [, setEnvDocsName] = useQueryState(
    ENV_DOCS_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const syncTemplate = useSyncEnvTemplate()

  const handleSync = () => {
    syncTemplate.mutate(
      { params: { path: { name } } },
      {
        onSuccess: () => toast.success(t("envs.detail.actions.syncTemplateToast", { name })),
        onError: (err) => toast.error(err?.error ?? String(err)),
      },
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header */}
      <div className="border-border border-b px-6 py-4">
        <div className="mb-2">
          <Button
            variant="ghost"
            size="sm"
            nativeButton={false}
            className="text-muted-foreground hover:text-foreground -ml-2 h-7 gap-1 font-mono text-[11px]"
            render={<Link href={clusterPath(clusterID, "envs")} />}
          >
            <ArrowLeft className="h-3 w-3" />
            {t("envs.detail.backToList")}
          </Button>
        </div>
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="bg-muted flex h-9 w-9 shrink-0 items-center justify-center rounded-lg">
              <Boxes className="h-4 w-4" />
            </div>
            <div className="min-w-0">
              <h1 className="font-mono text-base font-semibold truncate">{name}</h1>
              <p className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
                SandboxEnv
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!env}
              onClick={() => void setEnvDocsName(name)}
              className="h-8 gap-1 text-xs"
            >
              <FileText className="h-3.5 w-3.5" />
              {t("envs.docs")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!env || syncTemplate.isPending}
              onClick={handleSync}
              className="h-8 gap-1 text-xs"
            >
              <RefreshCw className="h-3.5 w-3.5" />
              {t("envs.detail.actions.syncTemplate")}
            </Button>
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
              onClick={() => setDeleteOpen(true)}
              className="text-destructive hover:text-destructive h-8 gap-1 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("envs.action.delete")}
            </Button>
          </div>
        </div>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto px-6 py-5">
        {isLoading ? (
          <div className="text-muted-foreground text-sm">…</div>
        ) : isError || !env ? (
          <div className="text-muted-foreground text-sm">{t("envs.empty")}</div>
        ) : (
          <div className="space-y-6">
            <SpecSection env={env} />
            <Separator />
            <EnvPoolsSection
              env={env}
              onCreatePool={() => setCreatePoolOpen(true)}
              onEditPool={(pool) => setEditPoolTarget(pool)}
              onDeletePool={(pool) => setDeletePoolTarget(pool)}
              onViewMetrics={(pool) => setMetricsTarget(pool)}
            />
            <Separator />
            <AutoscalingSection
              env={env}
              onCreate={() => setCreateAutoscalingOpen(true)}
              onEdit={(g) => setEditAutoscalingTarget(g)}
              onDelete={(g) => setDeleteAutoscalingTarget(g)}
            />
          </div>
        )}
      </div>

      {/* Edit / Delete sheets */}
      <UpsertEnvSheet env={env ?? null} open={editOpen} onOpenChange={setEditOpen} />
      <DeleteEnvDialog
        env={deleteOpen ? (env ?? null) : null}
        onOpenChange={(open) => setDeleteOpen(open)}
      />
      <PoolMetricsSheet
        pool={metricsTarget}
        onOpenChange={(open) => {
          if (!open) setMetricsTarget(null)
        }}
      />
      <EnvDocsSheet />

      {/* Pool / Autoscaling sheets — only rendered while env is loaded so the
          children can rely on a non-null env reference. */}
      {env && (
        <>
          <UpsertPoolSheet
            env={env}
            pool={null}
            open={createPoolOpen}
            onOpenChange={setCreatePoolOpen}
          />
          <UpsertPoolSheet
            env={env}
            pool={editPoolTarget}
            open={!!editPoolTarget}
            onOpenChange={(open) => {
              if (!open) setEditPoolTarget(null)
            }}
          />
          <DeletePoolDialog
            envName={env.name}
            pool={deletePoolTarget}
            onOpenChange={(open) => {
              if (!open) setDeletePoolTarget(null)
            }}
          />
          <UpsertAutoscalingGroupSheet
            env={env}
            group={null}
            open={createAutoscalingOpen}
            onOpenChange={setCreateAutoscalingOpen}
          />
          <UpsertAutoscalingGroupSheet
            env={env}
            group={editAutoscalingTarget}
            open={!!editAutoscalingTarget}
            onOpenChange={(open) => {
              if (!open) setEditAutoscalingTarget(null)
            }}
          />
          <DeleteAutoscalingGroupDialog
            envName={env.name}
            group={deleteAutoscalingTarget}
            onOpenChange={(open) => {
              if (!open) setDeleteAutoscalingTarget(null)
            }}
          />
        </>
      )}
    </div>
  )
}
