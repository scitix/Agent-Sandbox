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

import { useState, useMemo, useCallback } from "react"
import { useTranslation } from "@/lib/i18n"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useAtom, useAtomValue } from "jotai"
import {
  Activity,
  TrendingUp,
  Zap,
  AlertTriangle,
  CheckCircle2,
  Clock,
  Users,
  Server,
  ArrowUpRight,
} from "lucide-react"
import { impersonationAtom, authAtom, isActualAdminAtom } from "@/lib/atoms"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import { MetricsChart } from "@/components/prometheus/metrics-chart"
import { C } from "./colors"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import {
  useReplicasOverview,
  useStartRate,
  usePeakConcurrent,
  useUserSummary,
  sandboxUserStatsQueryOptions,
  sandboxUserStatsAbsoluteQueryOptions,
  replicasTrendQueryOptions,
  replicasTrendAbsoluteQueryOptions,
  claimDurationQueryOptions,
  claimDurationAbsoluteQueryOptions,
  startingDurationQueryOptions,
  startingDurationAbsoluteQueryOptions,
  runningDurationQueryOptions,
  runningDurationAbsoluteQueryOptions,
  recycleDurationQueryOptions,
  recycleDurationAbsoluteQueryOptions,
  sandboxCreateRateQueryOptions,
  sandboxCreateRateAbsoluteQueryOptions,
  sandboxDeleteRateQueryOptions,
  sandboxDeleteRateAbsoluteQueryOptions,
  scheduleSuccessRateQueryOptions,
  scheduleSuccessRateAbsoluteQueryOptions,
  recycleSuccessRateQueryOptions,
  recycleSuccessRateAbsoluteQueryOptions,
  scheduleDispatchLatencyQueryOptions,
  scheduleDispatchLatencyAbsoluteQueryOptions,
} from "@/lib/queries"
import { adminTeamsQueryOptions, adminUsersByTeamQueryOptions } from "@/lib/queries"
import { formatRate, formatDuration } from "@/lib/prometheus/transform"
import type { SandboxFilters } from "@/lib/types/prometheus"
import { type TimeRangeValue, type RefreshInterval, resolveTimeRange } from "@/lib/types/prometheus"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { StatCard } from "./stat-card"
import { AdminMetricsSection } from "./prometheus-admin-section"

// ─── StatCard ──────────────────────────────────────────────────────────────

// ─── Props ─────────────────────────────────────────────────────────────────────

export interface PrometheusSectionProps {
  clusterID: string
  /** When true, show Team/User filter Comboboxes and user summary tables (admin page only) */
  showAdminControls?: boolean
  /**
   * When true (overview page), scope Prometheus queries to the current user.
   * - Actual admin (not impersonating): use admin's own team/user from JWT.
   * - Admin impersonating: use the impersonated team/user.
   * - Tenant: JWT team/user are always enforced server-side, this flag is a no-op.
   */
  scopeToCurrentUser?: boolean
}

// ─── PrometheusSection ─────────────────────────────────────────────────────────

export function PrometheusSection({
  clusterID,
  showAdminControls = false,
  scopeToCurrentUser = false,
}: PrometheusSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [, setImpersonation] = useAtom(impersonationAtom)
  const auth = useAtomValue(authAtom)
  const isActualAdmin = useAtomValue(isActualAdminAtom)
  const impersonation = useAtomValue(impersonationAtom)

  // Resolve the "effective" user context for overview page scoping.
  // If admin is impersonating → use impersonated team/user.
  // If admin is NOT impersonating → use their own team/user from JWT.
  // Tenant users: server enforces scope anyway; pass through undefined (no extra filter).
  const effectiveScopeTeam = useMemo(() => {
    if (!scopeToCurrentUser) return undefined
    if (impersonation?.team && impersonation?.user) return impersonation.team
    if (isActualAdmin) return auth?.team ?? undefined
    return undefined
  }, [scopeToCurrentUser, impersonation, isActualAdmin, auth])

  const effectiveScopeUser = useMemo(() => {
    if (!scopeToCurrentUser) return undefined
    if (impersonation?.team && impersonation?.user) return impersonation.user
    if (isActualAdmin) return auth?.user ?? undefined
    return undefined
  }, [scopeToCurrentUser, impersonation, isActualAdmin, auth])

  const [timeRange, setTimeRange] = useState<TimeRangeValue>({ type: "preset", preset: "1h" })
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(0)
  const [filterTeam, setFilterTeam] = useState<string | null>(null)
  const [filterUser, setFilterUser] = useState<string | null>(null)

  // Admin filter data (only fetched when showAdminControls)
  const { data: teams } = useQuery({
    ...adminTeamsQueryOptions(),
    enabled: showAdminControls,
  })
  const { data: users } = useQuery({
    ...adminUsersByTeamQueryOptions(filterTeam ?? undefined),
    enabled: showAdminControls && !!filterTeam,
  })

  const filters: SandboxFilters = useMemo(
    () => ({
      cluster: clusterID,
      // scopeToCurrentUser: pin to effective user; otherwise use the admin filter dropdowns
      team: scopeToCurrentUser ? effectiveScopeTeam : (filterTeam ?? undefined),
      user: scopeToCurrentUser ? effectiveScopeUser : (filterUser ?? undefined),
    }),
    [clusterID, scopeToCurrentUser, effectiveScopeTeam, effectiveScopeUser, filterTeam, filterUser],
  )

  // Resolve concrete start/end/step for the current time range
  const { start, end, step } = useMemo(() => resolveTimeRange(timeRange), [timeRange])
  const isAbsolute = timeRange.type === "absolute"
  const resolvedPreset = timeRange.type === "preset" ? timeRange.preset : "1h"

  // When using an absolute time range, pass the end time to instant queries so they
  // show point-in-time data at the end of the selected range rather than "now".
  const instantEndTime = isAbsolute ? end : undefined

  // The effective refresh interval for hooks: 0 = don't pass (disabled), else pass through
  const effectiveRefetch = refreshInterval > 0 ? refreshInterval : undefined

  // Build dynamic queryOptions based on timeRange type (preset vs absolute).
  // React Query re-fetches automatically when the key changes.
  // Cast to a common queryOptions type to satisfy useQuery's overloads when
  // the union of preset-key (string[]) and absolute-key (number[]) types differ.
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
  const claimOpts = useMemo(
    () =>
      (isAbsolute
        ? claimDurationAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : claimDurationQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof claimDurationAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )
  const startingOpts = useMemo(
    () =>
      (isAbsolute
        ? startingDurationAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : startingDurationQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof startingDurationAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )
  const runningOpts = useMemo(
    () =>
      (isAbsolute
        ? runningDurationAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : runningDurationQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof runningDurationAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )
  const recycleOpts = useMemo(
    () =>
      (isAbsolute
        ? recycleDurationAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : recycleDurationQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof recycleDurationAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const sandboxCreateRateOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxCreateRateAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : sandboxCreateRateQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof sandboxCreateRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const sandboxDeleteRateOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxDeleteRateAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : sandboxDeleteRateQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof sandboxDeleteRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const scheduleSuccessRateOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleSuccessRateAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : scheduleSuccessRateQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof scheduleSuccessRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const recycleSuccessRateOpts = useMemo(
    () =>
      (isAbsolute
        ? recycleSuccessRateAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : recycleSuccessRateQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof recycleSuccessRateAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const scheduleDispatchLatencyOpts = useMemo(
    () =>
      (isAbsolute
        ? scheduleDispatchLatencyAbsoluteQueryOptions(filters, start, end, step, {
          refetchInterval: effectiveRefetch,
        })
        : scheduleDispatchLatencyQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof scheduleDispatchLatencyAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, step, resolvedPreset, effectiveRefetch],
  )

  const userStatsOpts = useMemo(
    () =>
      (isAbsolute
        ? sandboxUserStatsAbsoluteQueryOptions(filters, start, end, {
          refetchInterval: effectiveRefetch,
        })
        : sandboxUserStatsQueryOptions(filters, resolvedPreset, {
          refetchInterval: effectiveRefetch,
        })) as ReturnType<typeof sandboxUserStatsAbsoluteQueryOptions>,
    [isAbsolute, filters, start, end, resolvedPreset, effectiveRefetch],
  )

  // All hooks called unconditionally (React rules of hooks)
  const {
    data: overviewData,
    isFetching: overviewFetching,
    dataUpdatedAt,
  } = useReplicasOverview(filters, effectiveRefetch, instantEndTime)
  const { isFetching: startRateFetching, data: startRateData } = useStartRate(
    filters,
    effectiveRefetch,
    instantEndTime,
  )
  const { isFetching: peak1hFetching, data: peak1h } = usePeakConcurrent(
    filters,
    1,
    effectiveRefetch,
    instantEndTime,
  )
  const { isFetching: peak24hFetching, data: peak24h } = usePeakConcurrent(
    filters,
    24,
    effectiveRefetch,
    instantEndTime,
  )
  const {
    data: trendData,
    isLoading: trendLoading,
    isFetching: trendFetching,
  } = useQuery(trendOpts)
  const {
    data: claimData,
    isLoading: claimLoading,
    isFetching: claimFetching,
  } = useQuery(claimOpts)
  const {
    data: startingData,
    isLoading: startingLoading,
    isFetching: startingFetching,
  } = useQuery(startingOpts)
  const {
    data: runningData,
    isLoading: runningLoading,
    isFetching: runningFetching,
  } = useQuery(runningOpts)
  const {
    data: recycleData,
    isLoading: recycleLoading,
    isFetching: recycleFetching,
  } = useQuery(recycleOpts)
  const {
    data: sandboxCreateRateData,
    isLoading: sandboxCreateRateLoading,
    isFetching: sandboxCreateRateFetching,
  } = useQuery(sandboxCreateRateOpts)
  const {
    data: sandboxDeleteRateData,
    isLoading: sandboxDeleteRateLoading,
    isFetching: sandboxDeleteRateFetching,
  } = useQuery(sandboxDeleteRateOpts)
  const {
    data: scheduleSuccessRateData,
    isLoading: scheduleSuccessRateLoading,
    isFetching: scheduleSuccessRateFetching,
  } = useQuery(scheduleSuccessRateOpts)
  const {
    data: recycleSuccessRateData,
    isLoading: recycleSuccessRateLoading,
    isFetching: recycleSuccessRateFetching,
  } = useQuery(recycleSuccessRateOpts)
  const {
    data: scheduleDispatchLatencyData,
    isLoading: scheduleDispatchLatencyLoading,
    isFetching: scheduleDispatchLatencyFetching,
  } = useQuery(scheduleDispatchLatencyOpts)
  const {
    data: userStatsData,
    isLoading: userStatsLoading,
    isFetching: userStatsFetching,
  } = useQuery(userStatsOpts)
  const { data: userSummaryData, isFetching: userSummaryFetching } = useUserSummary(
    filters,
    effectiveRefetch,
  )

  const isAnyFetching =
    overviewFetching ||
    startRateFetching ||
    peak1hFetching ||
    peak24hFetching ||
    trendFetching ||
    claimFetching ||
    startingFetching ||
    runningFetching ||
    recycleFetching ||
    sandboxCreateRateFetching ||
    sandboxDeleteRateFetching ||
    scheduleSuccessRateFetching ||
    recycleSuccessRateFetching ||
    scheduleDispatchLatencyFetching ||
    userStatsFetching ||
    userSummaryFetching

  // Manual refresh: invalidate all prometheus queries
  const handleRefresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["prometheus"] })
  }, [queryClient])

  const handleImpersonate = useCallback(
    (team: string, user: string) => {
      setImpersonation({ team, user })
      void queryClient.invalidateQueries()
    },
    [setImpersonation, queryClient],
  )

  // Countdown timer
  const countdown = useRefreshCountdown(
    dataUpdatedAt,
    refreshInterval > 0 ? refreshInterval : undefined,
  )

  // If Prometheus is not configured, hide the entire section
  if (overviewData && !overviewData.configured) {
    return null
  }

  // For the shared Start Rate card, show only the successful create rate
  const startRate = startRateData?.data?.success ?? null
  const byUser = userSummaryData?.data?.byUser ?? []
  const userStats = userStatsData?.data

  return (
    <div className="flex flex-col gap-6">
      {/* Section header with optional admin filters + time range picker */}
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-muted-foreground flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Activity className="h-3.5 w-3.5" />
          {t("prometheus.sandboxMetrics")}
        </h2>

        {showAdminControls && (
          <div className="ml-2 flex items-center gap-2">
            {/* Team filter */}
            <Combobox
              value={filterTeam}
              onValueChange={(val) => {
                setFilterTeam(val)
                setFilterUser(null) // reset user when team changes
              }}
              items={teams ?? []}
            >
              <ComboboxInput
                placeholder={t("prometheus.allTeams")}
                className="h-8 w-45 font-mono text-xs"
                showClear
              />
              <ComboboxContent>
                <ComboboxEmpty>{t("prometheus.noTeams")}</ComboboxEmpty>
                <ComboboxList>
                  {(team) => (
                    <ComboboxItem key={team} value={team}>
                      {team}
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>

            {/* User filter (only active when team is selected) */}
            <Combobox value={filterUser} onValueChange={setFilterUser} items={users ?? []}>
              <ComboboxInput
                placeholder={
                  filterTeam ? t("prometheus.allUsers") : t("prometheus.selectTeamFirst")
                }
                className="h-8 w-45 font-mono text-xs"
                disabled={!filterTeam}
                showClear
              />
              <ComboboxContent>
                <ComboboxEmpty>{t("prometheus.noUsers")}</ComboboxEmpty>
                <ComboboxList>
                  {(user) => (
                    <ComboboxItem key={user} value={user}>
                      {user}
                    </ComboboxItem>
                  )}
                </ComboboxList>
              </ComboboxContent>
            </Combobox>
          </div>
        )}

        <div className="ml-auto">
          <GrafanaTimePicker
            value={timeRange}
            onValueChange={setTimeRange}
            refreshInterval={refreshInterval}
            onRefreshIntervalChange={setRefreshInterval}
            onRefresh={handleRefresh}
            countdown={countdown}
            isFetching={isAnyFetching}
          />
        </div>
      </div>

      {/* Lifecycle stat cards — cumulative + instant */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
        <StatCard
          label={t("prometheus.created")}
          value={userStats?.createSuccess != null ? userStats.createSuccess.toLocaleString() : "—"}
          sub={t("prometheus.createdSub")}
          icon={TrendingUp}
          color="text-green-600 dark:text-green-400"
          tooltip={t("prometheus.createdTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
        <StatCard
          label={t("prometheus.completed")}
          value={
            userStats?.deleteCompleted != null ? userStats.deleteCompleted.toLocaleString() : "—"
          }
          sub={t("prometheus.completedSub")}
          icon={CheckCircle2}
          color="text-blue-600 dark:text-blue-400"
          tooltip={t("prometheus.completedTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
        <StatCard
          label={t("prometheus.prewarmed")}
          value={userStats?.prewarmed != null ? Math.round(userStats.prewarmed) : "—"}
          sub={t("prometheus.prewarmedSub")}
          icon={Server}
          color="text-indigo-600 dark:text-indigo-400"
          tooltip={t("prometheus.prewarmedTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
        <StatCard
          label={t("prometheus.running")}
          value={userStats?.running != null ? Math.round(userStats.running) : "—"}
          sub={t("prometheus.runningSub")}
          icon={Activity}
          color="text-brand"
          tooltip={t("prometheus.runningTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
        <StatCard
          label={t("prometheus.released")}
          value={
            userStats?.deleteReleased != null ? userStats.deleteReleased.toLocaleString() : "—"
          }
          sub={t("prometheus.releasedSub")}
          icon={Clock}
          color="text-orange-600 dark:text-orange-400"
          tooltip={t("prometheus.releasedTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
        <StatCard
          label={t("prometheus.failedLifecycle")}
          value={userStats?.deleteFailed != null ? userStats.deleteFailed.toLocaleString() : "—"}
          sub={t("prometheus.failedLifecycleSub")}
          icon={AlertTriangle}
          color="text-red-600 dark:text-red-400"
          tooltip={t("prometheus.failedLifecycleTooltip")}
          isLoading={userStatsLoading && !userStats}
        />
      </div>

      {/* Rate & peak cards */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3">
        <StatCard
          label={t("prometheus.startRate")}
          value={startRate !== null ? formatRate(startRate) : "—"}
          sub={t("prometheus.startRateDesc")}
          icon={TrendingUp}
          color="text-brand"
          tooltip={t("prometheus.startRateTooltip")}
        />
        <StatCard
          label={t("prometheus.peak1h")}
          value={peak1h?.data?.peak != null ? Math.round(peak1h.data.peak) : "—"}
          sub={t("prometheus.peak1hDesc")}
          icon={Zap}
          color="text-yellow-600 dark:text-yellow-400"
          tooltip={t("prometheus.peak1hTooltip")}
        />
        <StatCard
          label={t("prometheus.peak24h")}
          value={peak24h?.data?.peak != null ? Math.round(peak24h.data.peak) : "—"}
          sub={t("prometheus.peak24hDesc")}
          icon={Zap}
          color="text-orange-600 dark:text-orange-400"
          tooltip={t("prometheus.peak24hTooltip")}
        />
      </div>

      {/* Time series charts */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <MetricsChart
          title={t("prometheus.replicaTrend")}
          description={t("prometheus.replicaTrendTooltip")}
          series={[
            { name: "Prewarmed", color: C.prewarmed },
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
          title={t("prometheus.claimDuration")}
          description={t("prometheus.claimDurationTooltip")}
          series={[
            { name: "P99", color: C.p99 },
            { name: "P95", color: C.p95 },
            { name: "P90", color: C.p90 },
            { name: "P50", color: C.p50 },
          ]}
          response={claimData}
          isLoading={claimLoading}
          isFetching={claimFetching}
          valueFormatter={(v) => formatDuration(v)}
          yAxisLabel="seconds"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.startingDuration")}
          description={t("prometheus.startingDurationTooltip")}
          series={[
            { name: "P99", color: C.p99 },
            { name: "P95", color: C.p95 },
            { name: "P90", color: C.p90 },
            { name: "P50", color: C.p50 },
          ]}
          response={startingData}
          isLoading={startingLoading}
          isFetching={startingFetching}
          valueFormatter={(v) => formatDuration(v)}
          yAxisLabel="seconds"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.runningDuration")}
          description={t("prometheus.runningDurationTooltip")}
          series={[
            { name: "P99", color: C.p99 },
            { name: "P95", color: C.p95 },
            { name: "P90", color: C.p90 },
            { name: "P50", color: C.p50 },
          ]}
          response={runningData}
          isLoading={runningLoading}
          isFetching={runningFetching}
          valueFormatter={(v) => formatDuration(v)}
          yAxisLabel="seconds"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.recycleDuration")}
          description={t("prometheus.recycleDurationTooltip")}
          series={[
            { name: "P99", color: C.p99 },
            { name: "P95", color: C.p95 },
            { name: "P90", color: C.p90 },
            { name: "P50", color: C.p50 },
          ]}
          response={recycleData}
          isLoading={recycleLoading}
          isFetching={recycleFetching}
          valueFormatter={(v) => formatDuration(v)}
          yAxisLabel="seconds"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.scheduleDispatchLatency")}
          description={t("prometheus.scheduleDispatchLatencyTooltip")}
          series={[
            { name: "P99", color: C.p99 },
            { name: "P95", color: C.p95 },
            { name: "P90", color: C.p90 },
            { name: "P50", color: C.p50 },
          ]}
          response={scheduleDispatchLatencyData}
          isLoading={scheduleDispatchLatencyLoading}
          isFetching={scheduleDispatchLatencyFetching}
          valueFormatter={(v) => formatDuration(v)}
          yAxisLabel="seconds"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.createRate")}
          description={t("prometheus.createRateTooltip")}
          series={[
            { name: "Create Success", color: C.success },
            { name: "Create No Idle", color: C.orange },
            { name: "Create Error", color: C.error },
          ]}
          response={sandboxCreateRateData}
          isLoading={sandboxCreateRateLoading}
          isFetching={sandboxCreateRateFetching}
          valueFormatter={(v) => formatRate(v)}
          yAxisLabel="ops/s"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.deleteRate")}
          description={t("prometheus.deleteRateTooltip")}
          series={[
            { name: "Completed", color: C.completed },
            { name: "Canceled", color: C.canceled },
            { name: "Released", color: C.released },
            { name: "Failed", color: C.error },
          ]}
          response={sandboxDeleteRateData}
          isLoading={sandboxDeleteRateLoading}
          isFetching={sandboxDeleteRateFetching}
          valueFormatter={(v) => formatRate(v)}
          yAxisLabel="ops/s"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.scheduleSuccessRate")}
          description={t("prometheus.scheduleSuccessRateTooltip")}
          series={[
            { name: "Success", color: C.success },
            { name: "Conflict", color: C.warning },
            { name: "Error", color: C.error },
          ]}
          response={scheduleSuccessRateData}
          isLoading={scheduleSuccessRateLoading}
          isFetching={scheduleSuccessRateFetching}
          valueFormatter={(v) => formatRate(v)}
          yAxisLabel="ops/s"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
        <MetricsChart
          title={t("prometheus.recycleSuccessRate")}
          description={t("prometheus.recycleSuccessRateTooltip")}
          series={[
            { name: "Success", color: C.success },
            { name: "Conflict", color: C.warning },
            { name: "Error", color: C.error },
          ]}
          response={recycleSuccessRateData}
          isLoading={recycleSuccessRateLoading}
          isFetching={recycleSuccessRateFetching}
          valueFormatter={(v) => formatRate(v)}
          yAxisLabel="ops/s"
          xStart={start}
          xEnd={end}
          onTimeRangeSelect={setTimeRange}
        />
      </div>

      {/* User Summary Table (admin only) */}
      {showAdminControls && byUser.length > 0 && (
        <div>
          <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
            <Users className="h-3.5 w-3.5" />
            {t("prometheus.sandboxReplicasByUser")}
          </h2>
          <div className="border-border overflow-hidden rounded-xl border">
            <table className="w-full">
              <thead>
                <tr className="border-border bg-secondary border-b">
                  <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.team")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.user")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.prewarmed")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.starting")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.running")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.stopping")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("prometheus.failed")}
                  </th>
                  <th className="w-10 px-2 py-2" />
                </tr>
              </thead>
              <tbody>
                {byUser.map((row, i) => (
                  <tr
                    key={`${row.team}-${row.user ?? i}`}
                    className="border-border hover:bg-secondary/50 border-b last:border-0"
                  >
                    <td className="text-muted-foreground px-4 py-2.5 font-mono text-sm">
                      {row.team}
                    </td>
                    <td className="text-foreground px-4 py-2.5 font-mono text-sm">
                      {row.user ?? "—"}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm text-indigo-600 dark:text-indigo-400">
                      {row.prewarmed}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm text-yellow-600 dark:text-yellow-400">
                      {row.starting}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm font-semibold text-green-600 dark:text-green-400">
                      {row.running}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm text-orange-600 dark:text-orange-400">
                      {row.stopping}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm text-red-600 dark:text-red-400">
                      {row.failed}
                    </td>
                    <td className="px-2 py-2.5 text-center">
                      {row.user && row.team && (
                        <button
                          onClick={() => handleImpersonate(row.team, row.user!)}
                          className="text-muted-foreground hover:text-foreground rounded p-0.5 transition-colors"
                          title={t("prometheus.impersonateUser", {
                            team: row.team,
                            user: row.user,
                          })}
                        >
                          <ArrowUpRight className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Admin-only: HTTP & Sandbox Activity charts */}
      {showAdminControls && (
        <AdminMetricsSection
          filters={filters}
          isAbsolute={isAbsolute}
          start={start}
          end={end}
          step={step}
          resolvedPreset={resolvedPreset}
          effectiveRefetch={effectiveRefetch}
          onTimeRangeSelect={setTimeRange}
        />
      )}
    </div>
  )
}
