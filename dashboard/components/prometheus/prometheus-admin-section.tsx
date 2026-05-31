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
 * PrometheusSection — shared Prometheus metrics component.
 *
 * Used on both the admin page (with admin controls) and the overview page (scoped to current user).
 *
 * Props:
 *   clusterID        — cluster label for Prometheus queries
 *   showAdminControls — admin page: show Team/User filter Comboboxes + user summary tables
 */

import { useMemo } from "react"
import { useTranslation } from "@/lib/i18n"
import { useQuery } from "@tanstack/react-query"
import {
  Activity,
  TrendingUp,
  Zap,
  AlertTriangle,
  Clock,
  ArrowUpRight,
  Globe,
  Cpu,
} from "lucide-react"
import { MetricsChart } from "@/components/prometheus/metrics-chart"
import { C } from "./colors"
import {
  httpRequestRateQueryOptions,
  httpRequestRateAbsoluteQueryOptions,
  httpRequestDurationNativeQueryOptions,
  httpRequestDurationNativeAbsoluteQueryOptions,
  httpRequestDurationE2bQueryOptions,
  httpRequestDurationE2bAbsoluteQueryOptions,
  sandboxCumulativeStatsQueryOptions,
  sandboxCumulativeStatsAbsoluteQueryOptions,
  envoyUpstreamRateQueryOptions,
  envoyUpstreamRateAbsoluteQueryOptions,
  envoyErrorRateQueryOptions,
  envoyErrorRateAbsoluteQueryOptions,
  envoyLatencyQueryOptions,
  envoyLatencyAbsoluteQueryOptions,
  sandboxCreateErrorRateQueryOptions,
  sandboxCreateErrorRateAbsoluteQueryOptions,
  sandboxDeleteFailRateQueryOptions,
  sandboxDeleteFailRateAbsoluteQueryOptions,
  envoyBandwidthQueryOptions,
  envoyBandwidthAbsoluteQueryOptions,
  envoyBandwidthRateQueryOptions,
  envoyBandwidthRateAbsoluteQueryOptions,
  scheduleCasOutcomeQueryOptions,
  scheduleCasOutcomeAbsoluteQueryOptions,
  scheduleRefreshRateQueryOptions,
  scheduleRefreshRateAbsoluteQueryOptions,
  scheduleInternalCountersQueryOptions,
  scheduleInternalCountersAbsoluteQueryOptions,
} from "@/lib/queries"
import {
  formatRate,
  formatDuration,
  formatMilliseconds,
  formatBytes,
} from "@/lib/prometheus/transform"
import type { SandboxFilters, TimeRangePreset } from "@/lib/types/prometheus"
import { type TimeRangeValue } from "@/lib/types/prometheus"
import { StatCard } from "./stat-card"

// ─── Admin-only metrics section ────────────────────────────────────────────────

interface AdminMetricsSectionProps {
  filters: SandboxFilters
  isAbsolute: boolean
  start: number
  end: number
  step: string
  resolvedPreset: TimeRangePreset
  effectiveRefetch?: number
  onTimeRangeSelect?: (range: TimeRangeValue) => void
}

export function AdminMetricsSection({
  filters,
  isAbsolute,
  start,
  end,
  step,
  resolvedPreset,
  effectiveRefetch,
  onTimeRangeSelect,
}: AdminMetricsSectionProps) {
  const { t } = useTranslation()

  // ── queryOptions memos ──────────────────────────────────────────────────────

  const httpRateOpts = useMemo(
    () =>
      (isAbsolute
        ? httpRequestRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : httpRequestRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof httpRequestRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const httpDurationNativeOpts = useMemo(
    () =>
      (isAbsolute
        ? httpRequestDurationNativeAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : httpRequestDurationNativeQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof httpRequestDurationNativeAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const httpDurationE2bOpts = useMemo(
    () =>
      (isAbsolute
        ? httpRequestDurationE2bAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : httpRequestDurationE2bQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof httpRequestDurationE2bAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const cumulativeOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxCumulativeStatsAbsoluteQueryOptions(filters, start, end, {
            refetchInterval: effectiveRefetch,
          })
        : sandboxCumulativeStatsQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof sandboxCumulativeStatsAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, resolvedPreset, effectiveRefetch],
  )

  const envoyUpstreamRateOpts = useMemo(
    () =>
      (isAbsolute
        ? envoyUpstreamRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : envoyUpstreamRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof envoyUpstreamRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const envoyErrorRateOpts = useMemo(
    () =>
      (isAbsolute
        ? envoyErrorRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : envoyErrorRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof envoyErrorRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const envoyLatencyOpts = useMemo(
    () =>
      (isAbsolute
        ? envoyLatencyAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : envoyLatencyQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof envoyLatencyAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const sandboxCreateErrorRateOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxCreateErrorRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : sandboxCreateErrorRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof sandboxCreateErrorRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const sandboxDeleteFailRateOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxDeleteFailRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : sandboxDeleteFailRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof sandboxDeleteFailRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const envoyBandwidthOpts = useMemo(
    () =>
      (isAbsolute
        ? envoyBandwidthAbsoluteQueryOptions(filters, start, end, {
            refetchInterval: effectiveRefetch,
          })
        : envoyBandwidthQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof envoyBandwidthAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, resolvedPreset, effectiveRefetch],
  )

  const envoyBandwidthRateOpts = useMemo(
    () =>
      (isAbsolute
        ? envoyBandwidthRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : envoyBandwidthRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof envoyBandwidthRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const scheduleCasOutcomeOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleCasOutcomeAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : scheduleCasOutcomeQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof scheduleCasOutcomeAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const scheduleRefreshRateOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleRefreshRateAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : scheduleRefreshRateQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof scheduleRefreshRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const scheduleInternalCountersOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleInternalCountersAbsoluteQueryOptions(filters, start, end, step, {
            refetchInterval: effectiveRefetch,
          })
        : scheduleInternalCountersQueryOptions(filters, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof scheduleInternalCountersAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  // ── Queries ─────────────────────────────────────────────────────────────────

  const {
    data: httpRateData,
    isLoading: httpRateLoading,
    isFetching: httpRateFetching,
  } = useQuery(httpRateOpts)
  const {
    data: httpDurationNativeData,
    isLoading: httpDurationNativeLoading,
    isFetching: httpDurationNativeFetching,
  } = useQuery(httpDurationNativeOpts)
  const {
    data: httpDurationE2bData,
    isLoading: httpDurationE2bLoading,
    isFetching: httpDurationE2bFetching,
  } = useQuery(httpDurationE2bOpts)
  const { data: cumulativeData, isLoading: cumulativeLoading } = useQuery(cumulativeOpts)
  const {
    data: envoyUpstreamRateData,
    isLoading: envoyUpstreamRateLoading,
    isFetching: envoyUpstreamRateFetching,
  } = useQuery(envoyUpstreamRateOpts)
  const {
    data: envoyErrorRateData,
    isLoading: envoyErrorRateLoading,
    isFetching: envoyErrorRateFetching,
  } = useQuery(envoyErrorRateOpts)
  const {
    data: envoyLatencyData,
    isLoading: envoyLatencyLoading,
    isFetching: envoyLatencyFetching,
  } = useQuery(envoyLatencyOpts)
  const {
    data: sandboxCreateErrorRateData,
    isLoading: sandboxCreateErrorRateLoading,
    isFetching: sandboxCreateErrorRateFetching,
  } = useQuery(sandboxCreateErrorRateOpts)
  const {
    data: sandboxDeleteFailRateData,
    isLoading: sandboxDeleteFailRateLoading,
    isFetching: sandboxDeleteFailRateFetching,
  } = useQuery(sandboxDeleteFailRateOpts)
  const { data: envoyBandwidthData, isLoading: envoyBandwidthLoading } =
    useQuery(envoyBandwidthOpts)
  const {
    data: envoyBandwidthRateData,
    isLoading: envoyBandwidthRateLoading,
    isFetching: envoyBandwidthRateFetching,
  } = useQuery(envoyBandwidthRateOpts)
  const {
    data: scheduleCasOutcomeData,
    isLoading: scheduleCasOutcomeLoading,
    isFetching: scheduleCasOutcomeFetching,
  } = useQuery(scheduleCasOutcomeOpts)
  const {
    data: scheduleRefreshRateData,
    isLoading: scheduleRefreshRateLoading,
    isFetching: scheduleRefreshRateFetching,
  } = useQuery(scheduleRefreshRateOpts)
  const {
    data: scheduleInternalCountersData,
    isLoading: scheduleInternalCountersLoading,
    isFetching: scheduleInternalCountersFetching,
  } = useQuery(scheduleInternalCountersOpts)

  // ── Derived values ───────────────────────────────────────────────────────

  const cumulative = cumulativeData?.data
  const envoyBandwidth = envoyBandwidthData?.data

  // Format a fraction (0–1) as a percentage string, e.g. 0.034 → "3.40%"
  const formatPercent = (v: number) => `${(v * 100).toFixed(2)}%`

  return (
    <div className="flex flex-col gap-6">
      {/* ── Cumulative stats cards (admin-specific, no overlap with user view) ── */}
      <div>
        <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <TrendingUp className="h-3.5 w-3.5" />
          {t("prometheus.cumulativeStats")}
        </h2>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-4">
          {/* Total create attempts (success + no_idle + error) */}
          <StatCard
            label={t("prometheus.createTotal")}
            value={cumulative?.createTotal != null ? cumulative.createTotal.toLocaleString() : "—"}
            sub={t("prometheus.createTotalSub")}
            icon={TrendingUp}
            color="text-green-600 dark:text-green-400"
            tooltip={t("prometheus.createTotalTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* Total deletions (all stop_reasons) */}
          <StatCard
            label={t("prometheus.totalDeleted")}
            value={cumulative?.deleteTotal != null ? cumulative.deleteTotal.toLocaleString() : "—"}
            sub={t("prometheus.sandboxDeletions")}
            icon={Clock}
            color="text-orange-600 dark:text-orange-400"
            tooltip={t("prometheus.totalDeletedTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* Native API requests */}
          <StatCard
            label={t("prometheus.nativeApi")}
            value={cumulative?.httpNative != null ? cumulative.httpNative.toLocaleString() : "—"}
            sub={t("prometheus.httpRequests")}
            icon={Activity}
            color="text-blue-600 dark:text-blue-400"
            tooltip={t("prometheus.nativeApiTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* E2B API requests */}
          <StatCard
            label={t("prometheus.e2bApi")}
            value={cumulative?.httpE2b != null ? cumulative.httpE2b.toLocaleString() : "—"}
            sub={t("prometheus.httpRequests")}
            icon={Activity}
            color="text-indigo-600 dark:text-indigo-400"
            tooltip={t("prometheus.e2bApiTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* Envoy upstream total */}
          <StatCard
            label={t("prometheus.envoyUpstream")}
            value={
              cumulative?.envoyUpstreamTotal != null
                ? cumulative.envoyUpstreamTotal.toLocaleString()
                : "—"
            }
            sub={t("prometheus.envoyUpstreamSub")}
            icon={Globe}
            color="text-cyan-600 dark:text-cyan-400"
            tooltip={t("prometheus.envoyUpstreamTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* Peak Envoy QPS */}
          <StatCard
            label={t("prometheus.peakEnvoyQps")}
            value={cumulative?.peakEnvoyRps != null ? cumulative.peakEnvoyRps.toFixed(2) : "—"}
            sub={t("prometheus.peakEnvoyQpsSub")}
            icon={Zap}
            color="text-yellow-600 dark:text-yellow-400"
            tooltip={t("prometheus.peakEnvoyQpsTooltip")}
            isLoading={cumulativeLoading && !cumulative}
          />
          {/* Envoy TX bytes */}
          <StatCard
            label={t("prometheus.envoyTxBytes")}
            value={envoyBandwidth?.txBytes != null ? formatBytes(envoyBandwidth.txBytes) : "—"}
            sub={t("prometheus.envoyTxBytesSub")}
            icon={ArrowUpRight}
            color="text-emerald-600 dark:text-emerald-400"
            tooltip={t("prometheus.envoyTxBytesTooltip")}
            isLoading={envoyBandwidthLoading && !envoyBandwidth}
          />
          {/* Envoy RX bytes */}
          <StatCard
            label={t("prometheus.envoyRxBytes")}
            value={envoyBandwidth?.rxBytes != null ? formatBytes(envoyBandwidth.rxBytes) : "—"}
            sub={t("prometheus.envoyRxBytesSub")}
            icon={Globe}
            color="text-violet-600 dark:text-violet-400"
            tooltip={t("prometheus.envoyRxBytesTooltip")}
            isLoading={envoyBandwidthLoading && !envoyBandwidth}
          />
        </div>
      </div>

      {/* ── Error rate analysis (admin-only visibility) ── */}
      <div>
        <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <AlertTriangle className="h-3.5 w-3.5" />
          {t("prometheus.errorRateAnalysis")}
        </h2>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <MetricsChart
            title={t("prometheus.sandboxCreateErrorRate")}
            description={t("prometheus.sandboxCreateErrorRateTooltip")}
            series={[
              { name: "Success %", color: C.success },
              { name: "No Idle %", color: C.orange },
              { name: "Error %", color: C.error },
            ]}
            initialDisabledSeries={["Success %"]} // Start with "No Idle" and "Error" hidden to emphasize overall success rate
            response={sandboxCreateErrorRateData}
            isLoading={sandboxCreateErrorRateLoading}
            isFetching={sandboxCreateErrorRateFetching}
            valueFormatter={(v) => formatPercent(v)}
            yAxisLabel="%"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.sandboxDeleteFailRate")}
            description={t("prometheus.sandboxDeleteFailRateTooltip")}
            series={[
              { name: "Completed %", color: C.success },
              { name: "Failed %", color: C.error },
              { name: "Released %", color: C.released },
              { name: "Canceled %", color: C.canceled },
            ]}
            initialDisabledSeries={["Completed %"]}
            response={sandboxDeleteFailRateData}
            isLoading={sandboxDeleteFailRateLoading}
            isFetching={sandboxDeleteFailRateFetching}
            valueFormatter={(v) => formatPercent(v)}
            yAxisLabel="%"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
        </div>
      </div>

      {/* ── HTTP activity charts ── */}
      <div>
        <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Activity className="h-3.5 w-3.5" />
          {t("prometheus.httpActivity")}
        </h2>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <MetricsChart
            title={t("prometheus.httpRequestRateNative")}
            description={t("prometheus.httpRequestRateNativeTooltip")}
            series={[{ name: "native", color: C.indigo }]}
            response={httpRateData}
            isLoading={httpRateLoading}
            isFetching={httpRateFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="req/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.httpRequestRateE2b")}
            description={t("prometheus.httpRequestRateE2bTooltip")}
            series={[{ name: "e2b", color: C.orange }]}
            response={httpRateData}
            isLoading={httpRateLoading}
            isFetching={httpRateFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="req/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.httpLatencyNative")}
            description={t("prometheus.httpLatencyNativeTooltip")}
            series={[
              { name: "P99", color: C.p99 },
              { name: "P95", color: C.p95 },
              { name: "P90", color: C.p90 },
              { name: "P50", color: C.p50 },
            ]}
            response={httpDurationNativeData}
            isLoading={httpDurationNativeLoading}
            isFetching={httpDurationNativeFetching}
            valueFormatter={(v) => formatDuration(v)}
            yAxisLabel="seconds"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.httpLatencyE2b")}
            description={t("prometheus.httpLatencyE2bTooltip")}
            series={[
              { name: "P99", color: C.purple },
              { name: "P95", color: C.pink },
              { name: "P90", color: C.teal },
              { name: "P50", color: C.lime },
            ]}
            response={httpDurationE2bData}
            isLoading={httpDurationE2bLoading}
            isFetching={httpDurationE2bFetching}
            valueFormatter={(v) => formatDuration(v)}
            yAxisLabel="seconds"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
        </div>
      </div>

      {/* ── Envoy Gateway activity ── */}
      <div>
        <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Globe className="h-3.5 w-3.5" />
          {t("prometheus.envoyActivity")}
        </h2>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <MetricsChart
            title={t("prometheus.envoyUpstreamRate")}
            description={t("prometheus.envoyUpstreamRateTooltip")}
            series={[
              { name: "2xx", color: C.success },
              { name: "4xx", color: C.warning },
              { name: "5xx", color: C.error },
            ]}
            response={envoyUpstreamRateData}
            isLoading={envoyUpstreamRateLoading}
            isFetching={envoyUpstreamRateFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="req/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.envoyErrorRate")}
            description={t("prometheus.envoyErrorRateTooltip")}
            series={[
              { name: "4xx%", color: C.warning },
              { name: "5xx%", color: C.canceled },
            ]}
            response={envoyErrorRateData}
            isLoading={envoyErrorRateLoading}
            isFetching={envoyErrorRateFetching}
            valueFormatter={(v) => formatPercent(v)}
            yAxisLabel="%"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.envoyLatency")}
            description={t("prometheus.envoyLatencyTooltip")}
            series={[
              { name: "P99", color: C.p99 },
              { name: "P95", color: C.p95 },
              { name: "P90", color: C.p90 },
              { name: "P50", color: C.p50 },
            ]}
            response={envoyLatencyData}
            isLoading={envoyLatencyLoading}
            isFetching={envoyLatencyFetching}
            valueFormatter={(v) => formatMilliseconds(v)}
            yAxisLabel="ms"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.envoyBandwidthRate")}
            description={t("prometheus.envoyBandwidthRateTooltip")}
            series={[
              { name: "TX", color: C.tx },
              { name: "RX", color: C.rx },
            ]}
            response={envoyBandwidthRateData}
            isLoading={envoyBandwidthRateLoading}
            isFetching={envoyBandwidthRateFetching}
            valueFormatter={(v) => formatBytes(v)}
            yAxisLabel="bytes/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
        </div>
      </div>

      {/* ── Scheduler Activity ── */}
      <div>
        <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Cpu className="h-3.5 w-3.5" />
          {t("prometheus.schedulerActivity")}
        </h2>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <MetricsChart
            title={t("prometheus.scheduleCasOutcome")}
            description={t("prometheus.scheduleCasOutcomeTooltip")}
            series={[
              { name: "Success", color: C.success },
              { name: "Retriable", color: C.warning },
              { name: "Hard", color: C.error },
            ]}
            response={scheduleCasOutcomeData}
            isLoading={scheduleCasOutcomeLoading}
            isFetching={scheduleCasOutcomeFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="ops/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.scheduleRefreshRate")}
            description={t("prometheus.scheduleRefreshRateTooltip")}
            series={[
              { name: "OK", color: C.success },
              { name: "Throttled", color: C.warning },
              { name: "Error", color: C.error },
            ]}
            response={scheduleRefreshRateData}
            isLoading={scheduleRefreshRateLoading}
            isFetching={scheduleRefreshRateFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="ops/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
          <MetricsChart
            title={t("prometheus.scheduleInternalCounters")}
            description={t("prometheus.scheduleInternalCountersTooltip")}
            series={[
              { name: "TTL Expired", color: C.orange },
              { name: "Scale-Down Skip", color: C.indigo },
              { name: "Queue Evicted", color: C.error },
            ]}
            response={scheduleInternalCountersData}
            isLoading={scheduleInternalCountersLoading}
            isFetching={scheduleInternalCountersFetching}
            valueFormatter={(v) => formatRate(v)}
            yAxisLabel="ops/s"
            xStart={start}
            xEnd={end}
            onTimeRangeSelect={onTimeRangeSelect}
          />
        </div>
      </div>
    </div>
  )
}
