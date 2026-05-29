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
 * EnvCapacitySection — Capacity trend for a SandboxEnv.
 *
 * Renders a MetricsChart (line) showing each phase's replica count summed
 * across all member Pools over time. Running/Idle/Starting/Stopping are
 * independent parallel series (not stacked); Desired is their approximate
 * ceiling. Data comes from `env-capacity-waterfall` which returns series
 * named "<phase>/<pool>"; we aggregate phase totals client-side so the
 * route can also feed the per-pool sparkline grid without a second round-trip.
 */

import { useMemo, useState, useCallback } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle } from "lucide-react"

import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import { MetricsChart } from "@/components/prometheus/metrics-chart"
import { C } from "@/components/prometheus/colors"
import {
  envCapacityWaterfallQueryOptions,
  envCapacityWaterfallAbsoluteQueryOptions,
} from "@/lib/queries"
import {
  type TimeRangeValue,
  type RefreshInterval,
  resolveTimeRange,
  type ChartSeries,
} from "@/lib/types/prometheus"
import { mergeChartSeries } from "@/lib/prometheus/transform"

const PHASE_SERIES = [
  { name: "Running", color: C.running },
  { name: "Idle", color: C.idle },
  { name: "Starting", color: C.starting },
  { name: "Stopping", color: C.stopping },
  { name: "Desired", color: C.desired },
]

/**
 * Sum the per-pool series into a single series per phase. Input names look
 * like "<phase>/<pool>"; output names are bare phase labels.
 */
function aggregateByPhase(series: ChartSeries[]): ChartSeries[] {
  const buckets = new Map<string, Map<number, number>>()
  for (const s of series) {
    const phase = s.name.split("/")[0]
    if (!phase) continue
    let bucket = buckets.get(phase)
    if (!bucket) {
      bucket = new Map()
      buckets.set(phase, bucket)
    }
    for (const p of s.points) {
      bucket.set(p.time, (bucket.get(p.time) ?? 0) + p.value)
    }
  }
  return Array.from(buckets.entries()).map(([name, m]) => ({
    name,
    points: Array.from(m.entries())
      .sort((a, b) => a[0] - b[0])
      .map(([time, value]) => ({ time, value })),
  }))
}

export interface EnvCapacitySectionProps {
  envName: string
}

export function EnvCapacitySection({ envName }: EnvCapacitySectionProps) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const queryClient = useQueryClient()

  const [timeRange, setTimeRange] = useState<TimeRangeValue>({ type: "preset", preset: "1h" })
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(30_000)

  const { start, end, step } = useMemo(() => resolveTimeRange(timeRange), [timeRange])
  const isAbsolute = timeRange.type === "absolute"
  const preset = timeRange.type === "preset" ? timeRange.preset : "1h"
  const effectiveRefetch = refreshInterval > 0 ? refreshInterval : undefined

  const filters = useMemo(
    () => ({ cluster: clusterID, sandboxEnv: envName }),
    [clusterID, envName],
  )

  const opts = useMemo(
    () =>
      (isAbsolute
        ? envCapacityWaterfallAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : envCapacityWaterfallQueryOptions(filters, preset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof envCapacityWaterfallAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, preset, effectiveRefetch],
  )

  const { data, isLoading, isFetching, dataUpdatedAt } = useQuery(opts)
  const countdown = useRefreshCountdown(dataUpdatedAt ?? 0, effectiveRefetch)

  const handleRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["prometheus", "env-capacity-waterfall"] })
  }, [queryClient])

  const aggregated = useMemo(
    () => (data?.data?.series ? aggregateByPhase(data.data.series) : []),
    [data],
  )
  const merged = useMemo(() => mergeChartSeries(aggregated), [aggregated])
  const promUnavailable = data && !data.configured

  return (
    <section className="space-y-2">
      <GrafanaTimePicker
        value={timeRange}
        onValueChange={setTimeRange}
        refreshInterval={refreshInterval}
        onRefreshIntervalChange={setRefreshInterval}
        onRefresh={handleRefresh}
        countdown={countdown}
      />
      {promUnavailable ? (
        <div className="border-border text-muted-foreground flex items-center gap-2 rounded border border-dashed p-4 font-mono text-xs">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{t("prometheus.prometheusNotConfigured")}</span>
        </div>
      ) : (
        <MetricsChart
          title={t("envs.detail.section.capacity")}
          series={PHASE_SERIES}
          data={merged}
          isLoading={isLoading}
          isFetching={isFetching}
          valueFormatter={(v) => Math.round(v).toString()}
          yAxisLabel="replicas"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
      )}
    </section>
  )
}
