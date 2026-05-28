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
 * EnvPoolSparklinesSection — small-multiples grid of per-Pool replica
 * trends. One Card per member Pool, each with a compact Desired-vs-Running
 * line chart. Click a card to open the existing PoolMetricsSheet for a
 * full-detail view. Shares the env-capacity-waterfall payload with the
 * EnvCapacitySection so we don't double-fetch.
 */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  ResponsiveContainer,
  Tooltip,
} from "recharts"
import { AlertCircle } from "lucide-react"

import { useTranslation } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { C } from "@/components/prometheus/colors"
import { envCapacityWaterfallQueryOptions } from "@/lib/queries"
import { mergeChartSeries } from "@/lib/prometheus/transform"
import type { AgentSandboxPool } from "@/lib/api/client"
import type { ChartSeries } from "@/lib/types/prometheus"

interface PoolSeries {
  pool: string
  desired: ChartSeries | null
  running: ChartSeries | null
}

/**
 * Bucket the flat "<phase>/<pool>" series down to one entry per pool, keeping
 * only the two phases the sparkline cares about. Doing the slicing here
 * (rather than asking the BFF for a separate endpoint) lets the capacity
 * section and the sparkline grid share a single network round-trip.
 */
function groupByPool(series: ChartSeries[]): PoolSeries[] {
  const byPool = new Map<string, PoolSeries>()
  for (const s of series) {
    const slash = s.name.indexOf("/")
    if (slash < 0) continue
    const phase = s.name.slice(0, slash)
    const pool = s.name.slice(slash + 1)
    if (!byPool.has(pool)) {
      byPool.set(pool, { pool, desired: null, running: null })
    }
    const entry = byPool.get(pool)!
    if (phase === "Desired") entry.desired = { ...s, name: "Desired" }
    if (phase === "Running") entry.running = { ...s, name: "Running" }
  }
  return Array.from(byPool.values()).sort((a, b) => a.pool.localeCompare(b.pool))
}

export interface EnvPoolSparklinesSectionProps {
  envName: string
  /** Member pools from the Env's clusters[].members — used to render even pools that haven't emitted metrics yet (Pending phase). */
  pools: AgentSandboxPool[]
  onPoolClick?: (pool: AgentSandboxPool) => void
}

export function EnvPoolSparklinesSection({
  envName,
  pools,
  onPoolClick,
}: EnvPoolSparklinesSectionProps) {
  const { t } = useTranslation()
  const clusterID = useClusterID()

  const filters = useMemo(
    () => ({ cluster: clusterID, sandboxEnv: envName }),
    [clusterID, envName],
  )

  const { data } = useQuery(
    envCapacityWaterfallQueryOptions(filters, "1h", { refetchInterval: 30_000 }),
  )

  const promUnavailable = data && !data.configured
  const seriesByPool = useMemo(
    () => groupByPool(data?.data?.series ?? []),
    [data?.data?.series],
  )

  // Align rendering to the canonical member-pool set: pools without any
  // metric series yet still show up as empty placeholders so the operator
  // sees the full Env shape, not "only pools Prometheus knows about".
  const seriesMap = useMemo(() => {
    const m = new Map<string, PoolSeries>()
    for (const ps of seriesByPool) m.set(ps.pool, ps)
    return m
  }, [seriesByPool])

  if (pools.length === 0) {
    return (
      <section>
        <SectionHeader title={t("envs.detail.section.poolSparklines")} />
        <div className="border-border text-muted-foreground rounded border border-dashed p-6 text-center font-mono text-xs">
          {t("envs.detail.sparklines.empty")}
        </div>
      </section>
    )
  }

  return (
    <section>
      <SectionHeader
        title={t("envs.detail.section.poolSparklines")}
        description={t("envs.detail.sparklines.description")}
      />
      {promUnavailable ? (
        <div className="border-border text-muted-foreground flex items-center gap-2 rounded border border-dashed p-4 font-mono text-xs">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{t("prometheus.prometheusNotConfigured")}</span>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {pools.map((p) => (
            <PoolSparklineCard
              key={p.name}
              pool={p}
              series={seriesMap.get(p.name) ?? null}
              onClick={onPoolClick ? () => onPoolClick(p) : undefined}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function SectionHeader({ title, description }: { title: string; description?: string }) {
  return (
    <div className="mb-2 flex items-baseline justify-between gap-2">
      <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {title}
      </h3>
      {description ? (
        <span className="text-muted-foreground font-mono text-[10px]">{description}</span>
      ) : null}
    </div>
  )
}

function PoolSparklineCard({
  pool,
  series,
  onClick,
}: {
  pool: AgentSandboxPool
  series: PoolSeries | null
  onClick?: () => void
}) {
  const points = useMemo(() => {
    if (!series) return []
    const merged: ChartSeries[] = []
    if (series.desired) merged.push(series.desired)
    if (series.running) merged.push(series.running)
    return mergeChartSeries(merged)
  }, [series])

  const phase = pool.status?.phase ?? "Pending"
  const phaseColor =
    phase === "Ready"
      ? C.success
      : phase === "ScalingUp" || phase === "ScalingDown"
        ? C.warning
        : phase === "Degraded"
          ? C.error
          : C.idle

  return (
    <Card
      className={`group flex flex-col gap-2 p-3 ${onClick ? "hover:border-foreground/40 cursor-pointer transition-colors" : ""}`}
      onClick={onClick}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate font-mono text-xs font-semibold">{pool.name}</span>
        <Badge
          variant="outline"
          className="font-mono text-[10px]"
          style={{ borderColor: phaseColor, color: phaseColor }}
        >
          {phase}
        </Badge>
      </div>
      <div className="h-[120px] w-full">
        {points.length === 0 ? (
          <div className="text-muted-foreground flex h-full items-center justify-center font-mono text-[10px]">
            —
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={points} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
              <XAxis dataKey="time" hide />
              <YAxis hide allowDecimals={false} />
              <Tooltip
                contentStyle={{ fontSize: 11, fontFamily: "var(--font-mono)" }}
                labelFormatter={(v) => new Date(v as number).toLocaleString()}
              />
              <Line
                type="monotone"
                dataKey="Desired"
                stroke={C.prewarmed}
                strokeWidth={1.5}
                strokeDasharray="3 2"
                dot={false}
                isAnimationActive={false}
              />
              <Line
                type="monotone"
                dataKey="Running"
                stroke={C.running}
                strokeWidth={1.5}
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </Card>
  )
}
