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

/**
 * SandboxMetricsSheet — CPU and Memory monitoring for a specific Sandbox.
 *
 * Opens as a right-side Sheet from the sandbox Actions dropdown.
 * Queries pod-cpu and pod-memory BFF endpoints (admin-only).
 *
 * Smart time-range defaults:
 * - Running/Starting/Stopping sandboxes with a known start (startedAt or claimedAt) < 15 min ago:
 *   from that start to now (adjustable), auto-refresh 30s
 * - Running/Starting/Stopping sandboxes with start >= 15 min ago: last 15 minutes preset
 *   (avoid slow full-range queries), auto-refresh 30s
 * - Running/Starting/Stopping sandboxes without any start timestamp: last 1 hour preset, auto-refresh 30s
 * - Terminated sandboxes (Completed/Failed/Canceled): full lifetime view from startedAt
 *   (fallback: claimedAt) to recycledAt (fallback: terminatedAt) — fixed, no refresh
 */

import { useState, useMemo, useEffect, useCallback } from "react"
import { useTranslation } from "@/lib/i18n"
import { AlertCircle, Activity } from "lucide-react"
import { Sheet, SheetContent } from "@/components/ui/sheet"
import { SandboxSheetHeader } from "@/components/sandboxes/sandbox-sheet-header"
import { MetricsChart } from "@/components/prometheus/metrics-chart"
import { C } from "./colors"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import { usePodCpu, usePodMemory, usePodNetwork, usePodDiskIo } from "@/lib/queries"
import {
  formatBytes,
  formatCores,
  formatDuration,
} from "@/lib/prometheus/transform"
import { sandboxLifetimeBounds } from "@/lib/prometheus/sandbox-lifetime"
import {
  type TimeRangeValue,
  type RefreshInterval,
  resolveTimeRange,
  formatUnixTimestamp,
} from "@/lib/types/prometheus"
import type { AgentSandbox } from "@/lib/api/client"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { useQueryClient } from "@tanstack/react-query"
import { isTerminalStatus } from "../sandboxes/columns"

// ─── Types ────────────────────────────────────────────────────────────────

// ─── Helpers ─────────────────────────────────────────────────────────────

function formatDurationLabel(startSec: number, endSec: number): string {
  return formatDuration(endSec - startSec)
}

// ─── Inner form (only mounted when open) ─────────────────────────────────

function MetricsSheetContent({ sandbox }: { sandbox: AgentSandbox }) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const queryClient = useQueryClient()
  const isTerminated = isTerminalStatus(sandbox.status)
  const bounds = useMemo(() => sandboxLifetimeBounds(sandbox), [sandbox])
  const hasLifetime = bounds.start !== undefined && bounds.end !== undefined

  // Default time range:
  // - Terminated with lifetime: full lifetime view (absolute, startedAt → recycledAt)
  // - Running with a known start < 15 min ago: from start to now (absolute, adjustable)
  // - Running with start >= 15 min ago: last 15 minutes preset (avoid full-range query)
  // - Otherwise: last 1 hour preset
  const defaultTimeRange = useMemo<TimeRangeValue>(
    () => {
      if (isTerminated && hasLifetime) {
        return { type: "absolute", start: bounds.start!, end: bounds.end! }
      }
      if (!isTerminated && bounds.start !== undefined) {
        const startSec = bounds.start
        const nowSec = Math.floor(Date.now() / 1000)
        const FIFTEEN_MINUTES = 15 * 60
        if (nowSec - startSec < FIFTEEN_MINUTES) {
          return { type: "absolute", start: startSec, end: nowSec }
        }
        return { type: "preset", preset: "15m" }
      }
      return { type: "preset", preset: "1h" }
    }, // eslint-disable-next-line react-hooks/exhaustive-deps
    [sandbox.sandboxId],
  )

  const [timeRange, setTimeRange] = useState<TimeRangeValue>(defaultTimeRange)
  // Terminated sandboxes: no auto-refresh; active: default 30s
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(isTerminated ? 0 : 30_000)

  // Reset state when sandbox changes
  useEffect(() => {
    setTimeRange(defaultTimeRange)
    setRefreshInterval(isTerminated ? 0 : 30_000)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sandbox.sandboxId])

  // Compute effective time range
  const { start, end } = useMemo(() => resolveTimeRange(timeRange), [timeRange])

  // Both admin and tenant use sandboxId-based queries.
  const resourceParams = {
    sandboxId: sandbox.sandboxId,
    cluster: clusterID,
    start,
    end,
  }

  // Terminated sandboxes have static ranges → no auto-refresh
  const effectiveRefetch = isTerminated
    ? undefined
    : refreshInterval > 0
      ? refreshInterval
      : undefined

  // Data queries — always called (React hooks rules)
  const {
    data: cpuData,
    isLoading: cpuLoading,
    isFetching: cpuFetching,
    dataUpdatedAt: cpuUpdatedAt,
  } = usePodCpu(resourceParams, effectiveRefetch)
  const {
    data: memData,
    isLoading: memLoading,
    isFetching: memFetching,
  } = usePodMemory(resourceParams, effectiveRefetch)
  const {
    data: netData,
    isLoading: netLoading,
    isFetching: netFetching,
  } = usePodNetwork(resourceParams, effectiveRefetch)
  const {
    data: diskData,
    isLoading: diskLoading,
    isFetching: diskFetching,
  } = usePodDiskIo(resourceParams, effectiveRefetch)

  // Auto-refresh countdown (only when auto-refresh is active)
  const countdown = useRefreshCountdown(cpuUpdatedAt ?? 0, effectiveRefetch)

  const prometheusUnavailable = (cpuData && !cpuData.configured) || (memData && !memData.configured)

  // Manual refresh
  const handleRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "pod-cpu"] })
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "pod-memory"] })
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "pod-network"] })
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "pod-disk-io"] })
  }, [queryClient])

  // Fixed label for terminated sandboxes
  const fixedLabel = useMemo(() => {
    if (!isTerminated || !hasLifetime) return undefined
    const start = bounds.start!
    const end = bounds.end!
    const rangeLabel = `${formatUnixTimestamp(start)} ${t("prometheus.timeRangeTo")} ${formatUnixTimestamp(end)}`
    const duration = formatDurationLabel(start, end)
    return `${rangeLabel} (${duration})`
  }, [isTerminated, hasLifetime, bounds.start, bounds.end, t])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-4 py-6">
      {/* Time range controls */}
      <GrafanaTimePicker
        value={timeRange}
        onValueChange={setTimeRange}
        refreshInterval={refreshInterval}
        onRefreshIntervalChange={setRefreshInterval}
        onRefresh={handleRefresh}
        countdown={countdown}
        disabled={isTerminated}
        fixedLabel={fixedLabel}
      />

      {/* Prometheus not configured */}
      {prometheusUnavailable ? (
        <div className="border-border text-muted-foreground flex items-center gap-2 rounded border border-dashed p-4 font-mono text-xs">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{t("prometheus.prometheusNotConfigured")}</span>
        </div>
      ) : (
        <div className="space-y-5">
          {/* CPU chart */}
          <MetricsChart
            title={t("prometheus.cpuUsage")}
            series={[{ name: "CPU", color: C.orange }]}
            response={cpuData}
            isLoading={cpuLoading}
            isFetching={cpuFetching}
            valueFormatter={formatCores}
            yAxisLabel="cores"
            height={180}
            emptyMessage={t("prometheus.noCpuData")}
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />

          {/* Memory chart */}
          <MetricsChart
            title={t("prometheus.memoryWorkingSet")}
            series={[{ name: "Memory", color: C.indigo }]}
            response={memData}
            isLoading={memLoading}
            isFetching={memFetching}
            valueFormatter={formatBytes}
            height={180}
            emptyMessage={t("prometheus.noMemoryData")}
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />

          {/* Network chart */}
          <MetricsChart
            title={t("prometheus.networkBandwidth")}
            series={[
              { name: "RX", color: C.rx },
              { name: "TX", color: C.tx },
            ]}
            response={netData}
            isLoading={netLoading}
            isFetching={netFetching}
            valueFormatter={formatBytes}
            yAxisLabel="B/s"
            height={180}
            emptyMessage={t("prometheus.noNetworkData")}
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />

          {/* Disk I/O chart */}
          <MetricsChart
            title={t("prometheus.diskIO")}
            series={[
              { name: "Read", color: C.indigo },
              { name: "Write", color: C.orange },
            ]}
            response={diskData}
            isLoading={diskLoading}
            isFetching={diskFetching}
            valueFormatter={formatBytes}
            yAxisLabel="B/s"
            height={180}
            emptyMessage={t("prometheus.noDiskData")}
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />
        </div>
      )}
    </div>
  )
}

// ─── Shell (controls open/close) ─────────────────────────────────────────

export interface SandboxMetricsSheetProps {
  sandbox: AgentSandbox | null
  onOpenChange: (open: boolean) => void
}

export function SandboxMetricsSheet({ sandbox, onOpenChange }: SandboxMetricsSheetProps) {
  const { t } = useTranslation()
  const open = sandbox !== null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex flex-col gap-0 p-0 sm:max-w-xl data-[side=right]:sm:max-w-3xl"
      >
        <SandboxSheetHeader
          icon={Activity}
          title={t("prometheus.metrics")}
          sandboxId={sandbox?.sandboxId}
        />

        {/* Only mount inner form when open — ensures hooks reset properly */}
        {open && sandbox && <MetricsSheetContent sandbox={sandbox} />}
      </SheetContent>
    </Sheet>
  )
}
