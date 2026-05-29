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
import { Box, FileTextIcon, InfoIcon, Loader2, TerminalSquare, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DetailHeader } from "@/components/custom/detail-header"
import { DetailTabs, DetailTabsContent } from "@/components/custom/detail-tabs"
import { CopyButton } from "@/components/custom/button/copy-button"
import { RelativeTime } from "@/components/custom/relative-time"
import { SandboxLogsPanel } from "@/components/sandboxes/logs-sheet"
import { SandboxMetricsPanel } from "@/components/prometheus/sandbox-metrics-panel"

import { DeleteSandboxDialog } from "@/components/sandboxes/delete-dialog"
import { TerminalDialog, TERMINAL_SANDBOX_ID_PARAM } from "@/components/sandboxes/terminal-dialog"
import { sandboxQueryOptions } from "@/lib/queries"
import type { AgentSandbox } from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { formatDuration } from "@/lib/prometheus/transform"
import { cn } from "@/lib/utils"
import { parseAsString, useQueryState } from "nuqs"

interface PageProps {
  params: Promise<{ clusterID: string; id: string; locale: string }>
}

/**
 * Detail page for a single Sandbox.
 *
 * Layout: fixed DetailHeader (id + actions) → DetailTabs below.
 * - Overview tab: status + metadata + metrics charts (scrollable)
 * - Logs tab: streaming log panel (full-height, managed internally)
 */
export default function SandboxDetailPage({ params }: PageProps) {
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

  if (isLoading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
      </div>
    )
  }

  if (!sandbox) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-muted-foreground text-sm">{t("common.noResultsFound")}</p>
      </div>
    )
  }

  const hasMetrics = !!sandbox.podName
  const isRunning = sandbox.status === "Running"

  const tabs = [
    { value: "overview", label: t("sandboxes.tab.overview"), icon: InfoIcon },
    { value: "logs", label: t("sandboxes.logs"), icon: FileTextIcon },
  ]

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Fixed header — status + metadata live in Overview tab, not here */}
      <DetailHeader
        icon={Box}
        title={sandbox.sandboxId}
        copyValue={sandbox.sandboxId}
        kind="Sandbox"
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled={!isRunning}
              onClick={() => void setTerminalForId(sandbox.sandboxId)}
              className="h-8 gap-1 text-xs"
            >
              <TerminalSquare className="h-3.5 w-3.5" />
              {t("sandboxes.terminal")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDeleteOpen(true)}
              className="text-destructive hover:text-destructive h-8 gap-1 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {t("common.delete")}
            </Button>
          </>
        }
      />

      {/* Tab bar (fixed) + tab content panels */}
      <DetailTabs tabs={tabs} defaultTab="overview">
        {/* Overview: metadata + (optional) metrics — scrollable */}
        <DetailTabsContent value="overview" className="overflow-y-auto">
          <SandboxOverview sandbox={sandbox} t={t} />
          {hasMetrics && (
            <SandboxMetricsPanel sandbox={sandbox} />
          )}
        </DetailTabsContent>

        {/* Logs: full-height streaming panel */}
        <DetailTabsContent value="logs" className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <SandboxLogsPanel sandboxId={sandbox.sandboxId} />
        </DetailTabsContent>
      </DetailTabs>

      <TerminalDialog />
      <DeleteSandboxDialog
        sandbox={deleteOpen ? sandbox : null}
        onOpenChange={(open) => setDeleteOpen(open)}
        onDeleted={() => router.push(clusterPath(clusterID, "sandboxes", locale))}
      />
    </div>
  )
}

// ─── Overview tab content ──────────────────────────────────────────────────

const STATUS_DOT: Record<string, { dot: string; text: string }> = {
  Running: { dot: "bg-green-500", text: "text-green-700 dark:text-green-400" },
  Starting: { dot: "bg-yellow-500", text: "text-yellow-700 dark:text-yellow-400" },
  Stopping: { dot: "bg-blue-500", text: "text-blue-700 dark:text-blue-400" },
  Failed: { dot: "bg-red-500", text: "text-red-700 dark:text-red-400" },
  Completed: { dot: "bg-slate-400", text: "text-muted-foreground" },
  Canceled: { dot: "bg-orange-500", text: "text-orange-700 dark:text-orange-400" },
  Released: { dot: "bg-slate-400", text: "text-muted-foreground" },
  Pending: { dot: "bg-yellow-500", text: "text-yellow-700 dark:text-yellow-400" },
}
const DEFAULT_DOT = { dot: "bg-muted-foreground", text: "text-muted-foreground" }

function SandboxOverview({
  sandbox,
  t,
}: {
  sandbox: AgentSandbox
  t: (key: TranslationKey) => string
}) {
  const dot = STATUS_DOT[sandbox.status] ?? DEFAULT_DOT
  const isPulsing = sandbox.status === "Running" || sandbox.status === "Starting"
  const isFailed = sandbox.status === "Failed"

  const durationLabel = sandbox.durationSeconds != null
    ? formatDuration(sandbox.durationSeconds)
    : null

  const hasImages = !!sandbox.containerImages && Object.keys(sandbox.containerImages).length > 0
  const hasUserMeta = !!sandbox.metadata && Object.keys(sandbox.metadata).length > 0
  const multipleImages = hasImages && Object.keys(sandbox.containerImages!).length > 1

  // Regular cells for the grid
  const cells: { label: string; value: React.ReactNode }[] = [
    ...(sandbox.cpu ? [{ label: "CPU", value: sandbox.cpu }] : []),
    ...(sandbox.memory ? [{ label: "Memory", value: sandbox.memory }] : []),
    {
      label: t("sandboxes.col.status"),
      value: (
        <span className="flex items-center gap-2">
          <span className="relative flex h-2 w-2 shrink-0">
            {isPulsing && (
              <span className={cn("absolute h-full w-full animate-ping rounded-full opacity-60", dot.dot)} />
            )}
            <span className={cn("relative h-2 w-2 rounded-full", dot.dot)} />
          </span>
          <span className={cn("font-semibold", dot.text)}>{sandbox.status}</span>
        </span>
      ),
    },
    { label: t("sandboxes.col.pool"), value: sandbox.poolName },
    ...(sandbox.envName ? [{ label: t("sandboxes.col.env"), value: sandbox.envName }] : []),
    ...(durationLabel ? [{ label: t("sandboxes.col.duration"), value: durationLabel }] : []),
    { label: t("sandboxes.col.claimedAt"), value: <RelativeTime date={sandbox.claimedAt} /> },
    ...(sandbox.startedAt ? [{ label: t("sandboxes.col.startedAt"), value: <RelativeTime date={sandbox.startedAt} /> }] : []),
    ...(sandbox.terminatedAt ? [{ label: t("sandboxes.col.terminatedAt"), value: <RelativeTime date={sandbox.terminatedAt} /> }] : []),
    ...(sandbox.recycledAt ? [{ label: t("sandboxes.col.recycledAt"), value: <RelativeTime date={sandbox.recycledAt} /> }] : []),
  ]

  // Pad to complete the last 4-column row so bg-border doesn't bleed through
  const padCount = (4 - (cells.length % 4)) % 4

  return (
    <div className="p-6">
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border lg:grid-cols-4">

        {/* Regular metadata cells */}
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

        {/* Images — full-width merged cell */}
        {hasImages && (
          <div className="col-span-2 bg-card px-3.5 py-3 lg:col-span-4">
            <span className="text-muted-foreground mb-2 block font-mono text-[10px] tracking-wider uppercase">
              {multipleImages ? t("sandboxes.col.images") : "Image"}
            </span>
            <div className="space-y-1.5">
              {Object.entries(sandbox.containerImages!).map(([container, image]) => (
                <div key={container} className="flex items-center gap-2 min-w-0">
                  {multipleImages && (
                    <span className="text-muted-foreground w-20 shrink-0 truncate font-mono text-[10px] uppercase">
                      {container}
                    </span>
                  )}
                  <span className="min-w-0 truncate font-mono text-xs" title={image}>{image}</span>
                  <span className="shrink-0"><CopyButton text={image} /></span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* User-defined labels — full-width merged cell */}
        {hasUserMeta && (
          <div className="col-span-2 bg-card px-3.5 py-3 lg:col-span-4">
            <span className="text-muted-foreground mb-2 block font-mono text-[10px] tracking-wider uppercase">
              Labels
            </span>
            <div className="flex flex-wrap gap-1.5">
              {Object.entries(sandbox.metadata!).map(([k, v]) => (
                <span
                  key={k}
                  className="border-border bg-muted/40 inline-flex items-center gap-1 rounded border px-2 py-0.5 font-mono text-xs"
                >
                  <span className="text-muted-foreground">{k}</span>
                  <span className="text-muted-foreground/40">=</span>
                  <span>{v}</span>
                </span>
              ))}
            </div>
          </div>
        )}

        {/* Failure details — full-width merged cell, only when failed */}
        {isFailed && (sandbox.failureMessage || sandbox.exitCode != null) && (
          <div className="col-span-2 bg-red-500/5 px-3.5 py-3 lg:col-span-4">
            <div className="mb-1.5 flex items-center gap-3">
              <span className="font-mono text-[10px] font-medium tracking-wider uppercase text-red-600 dark:text-red-400">
                Failure
              </span>
              {sandbox.exitCode != null && (
                <span className="font-mono text-[10px] text-red-500/70">
                  exit {sandbox.exitCode}
                </span>
              )}
            </div>
            {sandbox.failureReason && (
              <p className="font-mono text-xs font-semibold text-red-700 dark:text-red-400">
                {sandbox.failureReason}
              </p>
            )}
            {sandbox.failureMessage && (
              <p className="text-muted-foreground font-mono text-xs leading-relaxed">
                {sandbox.failureMessage}
              </p>
            )}
          </div>
        )}

      </div>
    </div>
  )
}
