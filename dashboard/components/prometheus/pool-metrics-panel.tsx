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
 * PoolMetricsPanel — Replica Trend + schedule queue monitoring for a single
 * SandboxPool. Rendered as the Metrics tab of the pool detail page.
 *
 * Smart time-range defaults:
 * - Pool created < 1 day ago: absolute range from createdAt to now (adjustable), auto-refresh 30s
 * - Pool created >= 1 day ago: last 1 day preset, auto-refresh 30s
 * - No createdAt: last 1 hour preset, auto-refresh 30s
 */

import { useState, useMemo, useCallback, useEffect } from "react"
import { AlertCircle } from "lucide-react"
import { useQuery, useQueryClient } from "@tanstack/react-query"

import { useTranslation } from "@/lib/i18n"
import { MetricsChart } from "@/components/prometheus/metrics-chart"
import { C } from "./colors"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import {
  replicasTrendQueryOptions,
  replicasTrendAbsoluteQueryOptions,
  scheduleReadyQueueQueryOptions,
  scheduleReadyQueueAbsoluteQueryOptions,
} from "@/lib/queries"
import { type TimeRangeValue, type RefreshInterval, resolveTimeRange } from "@/lib/types/prometheus"
import type { AgentSandboxPool } from "@/lib/api/client"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"

function toUnixSeconds(iso: string): number {
  return Math.floor(new Date(iso).getTime() / 1000)
}

export function PoolMetricsPanel({ pool }: { pool: AgentSandboxPool }) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const queryClient = useQueryClient()

  // Smart time range default:
  // - Pool created < 1 day ago: absolute range from createdAt to now
  // - Pool created >= 1 day ago: last 1 day preset
  // - No createdAt: last 1 hour preset
  const defaultTimeRange = useMemo<TimeRangeValue>(() => {
    if (pool.createdAt) {
      const startSec = toUnixSeconds(pool.createdAt)
      const nowSec = Math.floor(Date.now() / 1000)
      const ONE_DAY = 24 * 60 * 60
      if (nowSec - startSec < ONE_DAY) {
        return { type: "absolute", start: startSec, end: nowSec }
      }
      return { type: "preset", preset: "1d" }
    }
    return { type: "preset", preset: "1h" }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pool.name])

  const [timeRange, setTimeRange] = useState<TimeRangeValue>(defaultTimeRange)
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(30_000)

  // Reset state when pool changes
  useEffect(() => {
    setTimeRange(defaultTimeRange)
    setRefreshInterval(30_000)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pool.name])

  const { start, end, step } = useMemo(() => resolveTimeRange(timeRange), [timeRange])
  const isAbsolute = timeRange.type === "absolute"
  const resolvedPreset = timeRange.type === "preset" ? timeRange.preset : "1h"
  const effectiveRefetch = refreshInterval > 0 ? refreshInterval : undefined

  const filters = useMemo(
    () => ({
      cluster: clusterID,
      pool: pool.name,
    }),
    [clusterID, pool.name],
  )

  const trendOpts = useMemo(
    () =>
      (isAbsolute
        ? replicasTrendAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : replicasTrendQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof replicasTrendAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const queueOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleReadyQueueAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : scheduleReadyQueueQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof scheduleReadyQueueAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const {
    data: trendData,
    isLoading: trendLoading,
    isFetching: trendFetching,
    dataUpdatedAt,
  } = useQuery(trendOpts)

  const {
    data: queueData,
    isLoading: queueLoading,
    isFetching: queueFetching,
  } = useQuery(queueOpts)

  const countdown = useRefreshCountdown(dataUpdatedAt ?? 0, effectiveRefetch)
  const prometheusUnavailable = trendData && !trendData.configured

  const handleRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "replicas-trend"] })
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "schedule-ready-queue"] })
  }, [queryClient])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto p-6">
      {/* Time range controls */}
      <GrafanaTimePicker
        value={timeRange}
        onValueChange={setTimeRange}
        refreshInterval={refreshInterval}
        onRefreshIntervalChange={setRefreshInterval}
        onRefresh={handleRefresh}
        countdown={countdown}
      />

      {/* Prometheus not configured */}
      {prometheusUnavailable ? (
        <div className="border-border text-muted-foreground flex items-center gap-2 rounded border border-dashed p-4 font-mono text-xs">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{t("prometheus.prometheusNotConfigured")}</span>
        </div>
      ) : (
        <>
          <MetricsChart
            title={t("prometheus.replicaTrend")}
            description={t("prometheus.replicaTrendTooltip")}
            series={[
              { name: "Desired", color: C.desired },
              { name: "Running", color: C.running },
              { name: "Starting", color: C.starting },
              { name: "Stopping", color: C.stopping },
              { name: "Idle", color: C.idle },
            ]}
            initialDisabledSeries={["Starting", "Stopping", "Idle"]}
            response={trendData}
            isLoading={trendLoading}
            isFetching={trendFetching}
            valueFormatter={(v) => Math.round(v).toString()}
            yAxisLabel="replicas"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />
          <MetricsChart
            title={t("prometheus.scheduleQueueSize")}
            description={t("prometheus.scheduleQueueSizeTooltip")}
            series={[
              { name: "Ready Queue", color: C.success },
              { name: "Reservations", color: C.warning },
            ]}
            response={queueData}
            isLoading={queueLoading}
            isFetching={queueFetching}
            valueFormatter={(v) => Math.round(v).toString()}
            yAxisLabel="pods"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={setTimeRange}
          />
        </>
      )}
    </div>
  )
}
