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

import { use } from "react"
import { useQuery } from "@tanstack/react-query"

import { CopyButton } from "@/components/custom/button/copy-button"
import { RelativeTime } from "@/components/custom/relative-time"
import { SandboxMetricsPanel } from "@/components/prometheus/sandbox-metrics-panel"
import { sandboxQueryOptions } from "@/lib/queries"
import type { AgentSandbox } from "@/lib/api/client"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { formatDuration } from "@/lib/prometheus/transform"
import { cn } from "@/lib/utils"

interface PageProps {
  params: Promise<{ clusterID: string; id: string; locale: string }>
}

/**
 * Sandbox detail index — the Overview view. It lives at `…/sandboxes/{id}` (no
 * `/overview` sub-route) so the resource's own URL doubles as its default tab
 * and every breadcrumb ancestor links to a real page. Logs is a sibling
 * sub-route.
 */
export default function SandboxOverviewPage({ params }: PageProps) {
  const { id } = use(params)
  const { t } = useTranslation()
  const { data } = useQuery(sandboxQueryOptions(id))
  const sandbox = data?.sandbox ?? null

  if (!sandbox) return null

  const hasMetrics = !!sandbox.podName

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <SandboxOverview sandbox={sandbox} t={t} />
      {hasMetrics && <SandboxMetricsPanel sandbox={sandbox} />}
    </div>
  )
}

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

  const durationLabel =
    sandbox.durationSeconds != null ? formatDuration(sandbox.durationSeconds) : null

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
              <span
                className={cn(
                  "absolute h-full w-full animate-ping rounded-full opacity-60",
                  dot.dot,
                )}
              />
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
    ...(sandbox.startedAt
      ? [{ label: t("sandboxes.col.startedAt"), value: <RelativeTime date={sandbox.startedAt} /> }]
      : []),
    ...(sandbox.terminatedAt
      ? [
          {
            label: t("sandboxes.col.terminatedAt"),
            value: <RelativeTime date={sandbox.terminatedAt} />,
          },
        ]
      : []),
    ...(sandbox.recycledAt
      ? [
          {
            label: t("sandboxes.col.recycledAt"),
            value: <RelativeTime date={sandbox.recycledAt} />,
          },
        ]
      : []),
  ]

  // Pad to complete the last 4-column row so bg-border doesn't bleed through
  const padCount = (4 - (cells.length % 4)) % 4

  return (
    <div className="p-6">
      <div className="border-border bg-border grid grid-cols-2 gap-px overflow-hidden rounded-md border lg:grid-cols-4">
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
          <div className="bg-card col-span-2 px-3.5 py-3 lg:col-span-4">
            <span className="text-muted-foreground mb-2 block font-mono text-[10px] tracking-wider uppercase">
              {multipleImages ? t("sandboxes.col.images") : "Image"}
            </span>
            <div className="space-y-1.5">
              {Object.entries(sandbox.containerImages!).map(([container, image]) => (
                <div key={container} className="flex min-w-0 items-center gap-2">
                  {multipleImages && (
                    <span className="text-muted-foreground w-20 shrink-0 truncate font-mono text-[10px] uppercase">
                      {container}
                    </span>
                  )}
                  <span className="min-w-0 truncate font-mono text-xs" title={image}>
                    {image}
                  </span>
                  <span className="shrink-0">
                    <CopyButton text={image} />
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* User-defined labels — full-width merged cell */}
        {hasUserMeta && (
          <div className="bg-card col-span-2 px-3.5 py-3 lg:col-span-4">
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
              <span className="font-mono text-[10px] font-medium tracking-wider text-red-600 uppercase dark:text-red-400">
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
