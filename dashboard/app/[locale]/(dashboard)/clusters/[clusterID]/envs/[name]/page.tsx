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
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  Boxes,
  Check,
  Copy,
  InfoIcon,
  KeyRound,
  Loader2,
  Pencil,
  RefreshCw,
  TrendingUp,
  Trash2,
  DatabaseIcon,
} from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabs, DetailTabsContent } from "@/components/custom/detail-tabs"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import {
  AutoscalingSection,
  EnvPoolsSection,
} from "@/components/envs/env-detail-sections"
import { EnvCapacitySection } from "@/components/envs/env-capacity-section"
import { EnvEventsTimelineSection } from "@/components/envs/env-events-timeline-section"

import { UpsertEnvSheet } from "@/components/envs/upsert-env-sheet"
import { DeleteEnvDialog } from "@/components/envs/delete-env-dialog"
import { UpsertPoolSheet } from "@/components/envs/upsert-pool-sheet"
import { DeletePoolDialog } from "@/components/envs/delete-pool-dialog"
import { UpsertAutoscalingGroupSheet } from "@/components/envs/upsert-autoscaling-group-sheet"
import { DeleteAutoscalingGroupDialog } from "@/components/envs/delete-autoscaling-group-dialog"
import { PoolMetricsSheet } from "@/components/prometheus/pool-metrics-sheet"
import { envQueryOptions, useSyncEnvTemplate } from "@/lib/queries"
import type { AgentEnvAutoscalingGroup, AgentSandboxEnv, AgentSandboxPool } from "@/lib/api/client"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/**
 * Env detail page with 4-tab layout:
 *   Overview    — metadata grid + inline rendered documentation
 *   Pools       — full-height fixed table
 *   Autoscaling — scaling rules table (scrollable)
 *   Metrics & Events — capacity chart + events timeline (scrollable)
 */
export default function EnvDetailPage({ params }: PageProps) {
  const { name } = use(params)
  const { t } = useTranslation()
  const { data, isLoading, isError, error } = useQuery(envQueryOptions(name))
  const env = data?.env

  const isApiKeyRequired =
    (error as { errorCode?: string } | null)?.errorCode === "API_KEY_REQUIRED"

  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [metricsTarget, setMetricsTarget] = useState<AgentSandboxPool | null>(null)
  const [createPoolOpen, setCreatePoolOpen] = useState(false)
  const [editPoolTarget, setEditPoolTarget] = useState<AgentSandboxPool | null>(null)
  const [deletePoolTarget, setDeletePoolTarget] = useState<AgentSandboxPool | null>(null)
  const [editAutoscalingTarget, setEditAutoscalingTarget] =
    useState<AgentEnvAutoscalingGroup | null>(null)
  const [deleteAutoscalingTarget, setDeleteAutoscalingTarget] =
    useState<AgentEnvAutoscalingGroup | null>(null)

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

  const tabs = [
    { value: "overview", label: t("envs.tab.overview"), icon: InfoIcon },
    { value: "pools", label: t("envs.tab.pools"), icon: DatabaseIcon },
    { value: "autoscaling", label: t("envs.tab.autoscaling"), icon: TrendingUp },
    { value: "metrics", label: t("envs.tab.metrics"), icon: ActivityIcon },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header — always shown; title comes from URL param */}
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
          </>
        }
      />

      {/* Tab area */}
      {isLoading ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
        </div>
      ) : isApiKeyRequired ? (
        <ApiKeyRequiredNotice />
      ) : isError || !env ? (
        <div className="flex flex-1 items-center justify-center">
          <p className="text-muted-foreground text-sm">{t("envs.empty")}</p>
        </div>
      ) : (
        <DetailTabs tabs={tabs} defaultTab="overview">
          {/* Tab 1: Overview — metadata grid + inline docs, scrollable */}
          <DetailTabsContent value="overview" className="overflow-y-auto">
            <EnvOverview env={env} t={t} />
          </DetailTabsContent>

          {/* Tab 2: Pools — full-height fixed table */}
          <DetailTabsContent
            value="pools"
            className="flex min-h-0 flex-1 flex-col overflow-hidden"
          >
            <EnvPoolsSection
              env={env}
              onCreatePool={() => setCreatePoolOpen(true)}
              onEditPool={(pool) => setEditPoolTarget(pool)}
              onDeletePool={(pool) => setDeletePoolTarget(pool)}
              onViewMetrics={(pool) => setMetricsTarget(pool)}
              fixed
            />
          </DetailTabsContent>

          {/* Tab 3: Autoscaling — rules table, scrollable */}
          <DetailTabsContent value="autoscaling" className="overflow-y-auto">
            <div className="p-6">
              <AutoscalingSection
                env={env}
                onEdit={(g) => setEditAutoscalingTarget(g)}
                onDelete={(g) => setDeleteAutoscalingTarget(g)}
              />
            </div>
          </DetailTabsContent>

          {/* Tab 4: Metrics & Events — capacity chart + timeline, scrollable */}
          <DetailTabsContent value="metrics" className="overflow-y-auto">
            <div className="space-y-6 p-6">
              <EnvCapacitySection envName={env.name} />
              <Separator />
              <EnvEventsTimelineSection envName={env.name} />
            </div>
          </DetailTabsContent>
        </DetailTabs>
      )}

      {/* Sheets & dialogs */}
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

// ─── API Key Required notice ───────────────────────────────────────────────────

function ApiKeyRequiredNotice() {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-6 text-center">
      <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
        <KeyRound className="text-muted-foreground h-6 w-6" />
      </div>
      <div className="space-y-1">
        <p className="text-sm font-semibold">{t("envs.apiKeyRequired.title")}</p>
        <p className="text-muted-foreground max-w-md text-xs">
          {t("envs.apiKeyRequired.envDocsDescription")}
        </p>
      </div>
      <Button
        size="sm"
        render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}
      >
        {t("envs.apiKeyRequired.goToApiKeys")}
      </Button>
    </div>
  )
}

// ─── Overview tab content ──────────────────────────────────────────────────────

function DocsCopyButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button
      variant="outline"
      size="sm"
      className="h-7 gap-1.5 text-xs"
      onClick={handleCopy}
    >
      <span className={cn("flex items-center gap-1.5", copied && "text-green-600 dark:text-green-400")}>
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        {copied ? t("common.copied") : t("common.copyPage")}
      </span>
    </Button>
  )
}

function EnvOverview({
  env,
  t,
}: {
  env: AgentSandboxEnv
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
}) {
  let memberCount = 0
  let localCluster: string | undefined
  for (const cluster of env.spec.clusters ?? []) {
    if (cluster.members && cluster.members.length > 0 && !localCluster) {
      localCluster = cluster.clusterID
    }
    for (const _m of cluster.members ?? []) {
      memberCount++
    }
  }

  const cells: { label: string; value: React.ReactNode }[] = [
    { label: t("envs.detail.field.template"), value: env.spec.templateRef.name },
    { label: t("envs.detail.field.mode"), value: env.spec.mode },
    { label: t("envs.detail.field.members"), value: memberCount },
    { label: t("envs.detail.field.localCluster"), value: localCluster ?? "—" },
    ...(env.spec.defaults
      ? [
        {
          label: t("envs.detail.field.defaults"),
          value: `${env.spec.defaults.instanceType ?? "—"} × ${env.spec.defaults.multiplier ?? 1}`,
        },
      ]
      : []),
  ]

  const padCount = (4 - (cells.length % 4)) % 4
  const hasDocs = !!env.envDocs

  return (
    <div className="space-y-6 p-6">
      {/* Metadata grid */}
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border lg:grid-cols-4">
        {cells.map((cell) => (
          <div key={cell.label} className="bg-card flex flex-col gap-1.5 px-3.5 py-3">
            <span className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
              {cell.label}
            </span>
            <div className="font-mono text-xs font-medium">{cell.value}</div>
          </div>
        ))}
        {Array.from({ length: padCount }).map((_, i) => (
          <div key={`pad-${i}`} className="bg-card" />
        ))}
      </div>

      {/* Inline docs */}
      <section>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.envDocsSheet.title")}
          </h3>
          {hasDocs && <DocsCopyButton content={env.envDocs!} />}
        </div>
        {hasDocs ? (
          <MarkdownRenderer content={env.envDocs!} />
        ) : (
          <p className="text-muted-foreground text-sm">{t("envs.noEnvDocs")}</p>
        )}
      </section>
    </div>
  )
}
