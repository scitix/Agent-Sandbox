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
 * EnvCapacitySection — Capacity waterfall for a SandboxEnv.
 *
 * Renders a stacked-area Recharts chart showing each member Pool's current
 * Running (claimed) and Idle (pre-warmed) replica counts over time, summed
 * by phase across the Env's pools. A "Desired" line is overlaid as the
 * target ceiling. Data comes from `env-capacity-waterfall` which returns
 * series named "<phase>/<pool>"; we aggregate phase totals client-side so
 * the route can also feed the per-pool sparkline grid without a second
 * round-trip.
 */

import { useMemo, useState, useCallback } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  Line,
} from "recharts"
import { AlertCircle, Loader2 } from "lucide-react"

import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
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

const STACK_PHASES = [
  { key: "Running", color: C.running },
  { key: "Idle", color: C.idle },
  { key: "Starting", color: C.starting },
  { key: "Stopping", color: C.stopping },
] as const

/**
 * Sum the per-pool series into a single series per phase. Input names look
 * like "<phase>/<pool>"; output names are bare phase labels so the stacked
 * AreaChart can ride the standard Recharts dataKey contract.
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
    [data?.data?.series],
  )
  const merged = useMemo(() => mergeChartSeries(aggregated), [aggregated])
  const hasData = merged.length > 0
  const promUnavailable = data && !data.configured

  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.detail.section.capacity")}
        </h3>
        {isFetching && !isLoading ? (
          <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        ) : null}
      </div>

      <div className="space-y-3">
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
        ) : !hasData && !isLoading ? (
          <div className="border-border text-muted-foreground flex items-center justify-center rounded border border-dashed p-8 font-mono text-xs">
            {t("prometheus.noData")}
          </div>
        ) : (
          <div className="border-border bg-background rounded border p-3">
            <ResponsiveContainer width="100%" height={280}>
              <AreaChart data={merged} margin={{ top: 8, right: 12, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  dataKey="time"
                  type="number"
                  domain={[start * 1000, end * 1000]}
                  tickFormatter={(v) => new Date(v).toLocaleTimeString()}
                  tick={{ fontSize: 11 }}
                  stroke="currentColor"
                  className="text-muted-foreground"
                />
                <YAxis
                  tick={{ fontSize: 11 }}
                  allowDecimals={false}
                  stroke="currentColor"
                  className="text-muted-foreground"
                />
                <Tooltip
                  contentStyle={{
                    fontSize: 12,
                    fontFamily: "var(--font-mono)",
                  }}
                  labelFormatter={(v) => new Date(v as number).toLocaleString()}
                />
                <Legend wrapperStyle={{ fontSize: 11 }} />
                {STACK_PHASES.map((p) => (
                  <Area
                    key={p.key}
                    type="monotone"
                    dataKey={p.key}
                    stackId="capacity"
                    name={p.key}
                    stroke={p.color}
                    fill={p.color}
                    fillOpacity={0.45}
                    isAnimationActive={false}
                  />
                ))}
                <Line
                  type="monotone"
                  dataKey="Desired"
                  name="Desired"
                  stroke={C.prewarmed}
                  strokeWidth={2}
                  strokeDasharray="4 2"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </div>
    </section>
  )
}
