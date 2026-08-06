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

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useMemo, useState } from "react"
import { useAtomValue } from "jotai"
import { LayoutDashboard, Box, RefreshCw, TrendingUp, Users, UserCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import {
  createDistributionQueryOptions,
  createDistributionAbsoluteQueryOptions,
  platformUsersCountQueryOptions,
} from "@/lib/queries"
import { PrometheusSection } from "@/components/prometheus/prometheus-section"
import { StatCard as OverallStatCard } from "@/components/prometheus/stat-card"
import { DistributionPieChart } from "@/components/prometheus/distribution-pie-chart"
import { TopUsersBarChart } from "@/components/prometheus/top-users-bar-chart"
import { ClusterScopeSelect } from "@/components/cluster-scope-select"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import { LiveBadge } from "@/components/live-badge"
import { useClusterScopeSearchParams } from "@/hooks/use-cluster-scope-search-params"
import { useTimeRangeSearchParams } from "@/hooks/use-time-range-search-params"
import { useScopedLiveCount } from "@/hooks/use-scoped-live-count"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { isActualAdminAtom, impersonationAtom, authAtom, clusterIDAtom } from "@/lib/atoms"
import { getApiClient } from "@/lib/api/client"
import { resolveTimeRange, type RefreshInterval } from "@/lib/types/prometheus"
import { replicasOverviewQueryOptions } from "@/lib/queries/prometheus"
import { useTranslation } from "@/lib/i18n"

function StatCard({
  label,
  value,
  sub,
  icon: Icon,
  color,
}: {
  label: string
  value: number | string
  sub?: string
  icon: React.ComponentType<{ className?: string }>
  color: string
}) {
  return (
    <div className="border-border bg-card border p-4">
      <div className="mb-3 flex items-start justify-between">
        <span className="text-muted-foreground font-mono text-xs font-bold tracking-[0.15em] uppercase">
          {label}
        </span>
        <Icon className={`h-4 w-4 ${color}`} />
      </div>
      <div className={`font-mono text-3xl font-bold ${color}`}>{value}</div>
      {sub && <div className="text-muted-foreground mt-1 font-mono text-xs">{sub}</div>}
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const colorMap: Record<string, string> = {
    Running: "bg-green-500/10 text-green-700 dark:text-green-400 border-green-500/20",
    Starting: "bg-yellow-500/10 text-yellow-700 dark:text-yellow-400 border-yellow-500/20",
    Stopping: "bg-blue-500/10 text-blue-700 dark:text-blue-400 border-blue-500/20",
    Failed: "bg-red-500/10 text-red-700 dark:text-red-400 border-red-500/20",
    Idle: "bg-secondary text-muted-foreground border-border",
  }
  const cls = colorMap[status] ?? "bg-secondary text-muted-foreground border-border"
  return (
    <span
      className={`inline-flex items-center border px-1.5 py-0.5 font-mono text-xs font-bold tracking-wider uppercase ${cls}`}
    >
      {status}
    </span>
  )
}

export default function OverviewPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const auth = useAtomValue(authAtom)
  const boundClusterID = useAtomValue(clusterIDAtom)
  const isActualAdmin = useAtomValue(isActualAdminAtom)
  const impersonation = useAtomValue(impersonationAtom)
  const isApiKey = auth?.authMethod === "apikey"

  // Cluster scope selector (shared header control) — apikey sessions are locked
  // to their bound cluster regardless of the URL searchParam value.
  const [scope, setScope] = useClusterScopeSearchParams()
  const effectiveClusterScope = isApiKey ? boundClusterID : scope
  // The "personal usage" section always needs one concrete cluster (Prometheus
  // per-cluster filters, K8s fallback stats) — default to the bound/login
  // cluster when the scope selector is set to "all clusters".
  const personalClusterID =
    effectiveClusterScope === "all" ? boundClusterID : effectiveClusterScope

  const liveCount = useScopedLiveCount(scope)

  // Build a display label for the page header sub-title
  const scopeLabel = useMemo(() => {
    if (impersonation?.user && impersonation?.team) {
      return `${impersonation.team} / ${impersonation.user}`
    }
    if (isActualAdmin && (auth?.user || auth?.team)) {
      return auth.user ?? auth.team ?? null
    }
    return null
  }, [impersonation, isActualAdmin, auth])

  // ─── Shared time range (persisted to URL, also read by <PrometheusSection>) ──
  const [timeRange, setTimeRange] = useTimeRangeSearchParams()
  const { start, end } = useMemo(() => resolveTimeRange(timeRange), [timeRange])
  const isAbsolute = timeRange.type === "absolute"
  const resolvedPreset = timeRange.type === "preset" ? timeRange.preset : "1h"
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(0)
  const effectiveRefetch = refreshInterval > 0 ? refreshInterval : undefined

  // ─── Personal usage: probe + fallback stats (unchanged from the old page) ────
  const prometheusFilters = useMemo(
    () => ({ cluster: personalClusterID }),
    [personalClusterID],
  )
  const { data: promOverview } = useQuery(replicasOverviewQueryOptions(prometheusFilters))
  const promResolved = promOverview !== undefined
  const prometheusConfigured = promOverview?.configured === true

  const statsOptions = useMemo(
    () => getApiClient(personalClusterID).queryOptions("get", "/statistics/sandboxes", undefined),
    [personalClusterID],
  )
  const sandboxOptions = useMemo(
    () =>
      getApiClient(personalClusterID).queryOptions(
        "get",
        "/sandboxes",
        { params: { query: { limit: 10, offset: 0 } } },
        { select: (data: { items: { sandboxId: string; poolName: string; status: string; claimedAt?: string }[] }) => data.items ?? [] },
      ),
    [personalClusterID],
  )

  const { data: statsData, isFetching: statsFetching } = useQuery(statsOptions)
  const { data: sandboxes, isFetching: sandboxFetching } = useQuery(sandboxOptions)

  const stats = statsData?.statistics ?? null
  const loading = !statsData && statsFetching

  const handleRefreshPersonal = () => {
    void qc.refetchQueries({ queryKey: statsOptions.queryKey })
    void qc.refetchQueries({ queryKey: sandboxOptions.queryKey })
  }

  const runningCount = stats?.byStatus?.["Running"] ?? 0
  const activatingCount = stats?.byStatus?.["Starting"] ?? 0
  const faultyCount = stats?.byStatus?.["Failed"] ?? 0

  const showFallback = promResolved && !prometheusConfigured

  // ─── Overall usage: platform-wide creation distribution + user counts ────────
  const distributionOpts = useMemo(
    () =>
      (isAbsolute
        ? createDistributionAbsoluteQueryOptions(effectiveClusterScope, start, end, {
            refetchInterval: effectiveRefetch,
          })
        : createDistributionQueryOptions(effectiveClusterScope, resolvedPreset, {
            refetchInterval: effectiveRefetch,
          })) as ReturnType<typeof createDistributionAbsoluteQueryOptions>,
    [isAbsolute, effectiveClusterScope, start, end, resolvedPreset, effectiveRefetch],
  )
  const {
    data: distributionData,
    isFetching: distributionFetching,
    dataUpdatedAt,
  } = useQuery(distributionOpts)
  const platformUsersOpts = platformUsersCountQueryOptions()
  const { data: platformUsersData, isFetching: platformUsersFetching } =
    useQuery(platformUsersOpts)

  const distResolved = distributionData !== undefined
  const distConfigured = distributionData?.configured !== false
  const dist = distributionData?.data

  const byUserSlices = useMemo(
    () => (dist?.byUser ?? []).map((r) => ({ name: `${r.team}/${r.user}`, value: r.count })),
    [dist],
  )
  const byTeamSlices = useMemo(
    () => (dist?.byTeam ?? []).map((r) => ({ name: r.team, value: r.count })),
    [dist],
  )
  const byClusterSlices = useMemo(
    () => (dist?.byCluster ?? []).map((r) => ({ name: r.clusterID, value: r.count })),
    [dist],
  )
  const [distTab, setDistTab] = useState<"byUser" | "byTeam">("byUser")

  const overallIsFetching = distributionFetching || platformUsersFetching
  const overallCountdown = useRefreshCountdown(
    dataUpdatedAt,
    refreshInterval > 0 ? refreshInterval : undefined,
  )
  const handleRefreshOverall = () => {
    void qc.invalidateQueries({ queryKey: distributionOpts.queryKey })
    void qc.invalidateQueries({ queryKey: platformUsersOpts.queryKey })
  }

  return (
    <div className="overflow-y-auto">
      {/* Page header: title + cluster scope + time range + live badge */}
      <div className="border-border flex flex-wrap items-center gap-3 border-b px-6 py-3">
        <span className="text-foreground flex items-center gap-1.5 font-mono text-sm font-semibold tracking-wide uppercase">
          <LayoutDashboard className="h-4 w-4" />
          {t("overview.title")}
        </span>
        {scopeLabel && (
          <span className="text-muted-foreground font-mono text-xs">
            {t("overview.viewingAs", { user: scopeLabel })}
          </span>
        )}
        <div className="ml-auto flex items-center gap-3">
          <ClusterScopeSelect value={scope} onValueChange={setScope} />
          <GrafanaTimePicker
            value={timeRange}
            onValueChange={setTimeRange}
            refreshInterval={refreshInterval}
            onRefreshIntervalChange={setRefreshInterval}
            onRefresh={handleRefreshOverall}
            countdown={overallCountdown}
            isFetching={overallIsFetching}
          />
          <LiveBadge count={liveCount.count} />
        </div>
      </div>

      <div className="flex flex-col gap-10 p-6">
        {/* ── Personal usage — unchanged cards/table, only the section title moved ── */}
        <div className="flex flex-col gap-8">
          {showFallback && (
            <div className="flex items-center justify-end">
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={handleRefreshPersonal}
                className="text-muted-foreground h-7 w-7"
                disabled={statsFetching || sandboxFetching}
              >
                <RefreshCw
                  className={`h-3.5 w-3.5 ${statsFetching || sandboxFetching ? "animate-spin" : ""}`}
                />
              </Button>
            </div>
          )}

          {!promResolved || loading ? (
            <div className="flex h-64 items-center justify-center">
              <div className="flex flex-col items-center gap-2">
                <div className="bg-brand h-1 w-24 animate-pulse" />
                <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
                  {t("common.loading")}
                </span>
              </div>
            </div>
          ) : (
            <>
              {showFallback && (
                <>
                  <div>
                    <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
                      <Box className="h-3.5 w-3.5" />
                      {scopeLabel
                        ? `${t("sandboxes.title")} — ${scopeLabel}`
                        : t("overview.mySandboxes")}
                    </h2>
                    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                      <StatCard
                        label="Total"
                        value={stats?.total ?? 0}
                        icon={Box}
                        color="text-foreground"
                      />
                      <StatCard
                        label={t("status.running")}
                        value={runningCount}
                        icon={Box}
                        color="text-green-600 dark:text-green-400"
                      />
                      <StatCard
                        label={t("status.starting")}
                        value={activatingCount}
                        icon={Box}
                        color="text-yellow-600 dark:text-yellow-400"
                      />
                      <StatCard
                        label={t("status.failed")}
                        value={faultyCount}
                        icon={Box}
                        color="text-red-600 dark:text-red-400"
                      />
                    </div>
                  </div>

                  {sandboxes && sandboxes.length > 0 && (
                    <div>
                      <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
                        <Box className="h-3.5 w-3.5" />
                        {t("overview.recentSandboxes")}
                      </h2>
                      <div className="border-border overflow-hidden border">
                        <table className="w-full">
                          <thead>
                            <tr className="border-border bg-secondary border-b">
                              <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                                {t("sandboxes.col.id")}
                              </th>
                              <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                                {t("sandboxes.col.pool")}
                              </th>
                              <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                                {t("sandboxes.col.status")}
                              </th>
                              <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                                {t("sandboxes.col.claimedAt")}
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {sandboxes.map((sb) => (
                              <tr
                                key={sb.sandboxId}
                                className="border-border hover:bg-secondary/50 border-b last:border-0"
                              >
                                <td className="text-foreground max-w-50 truncate px-4 py-2.5 font-mono text-xs">
                                  {sb.sandboxId}
                                </td>
                                <td className="text-muted-foreground px-4 py-2.5 font-mono text-sm">
                                  {sb.poolName}
                                </td>
                                <td className="px-4 py-2.5">
                                  <StatusBadge status={sb.status} />
                                </td>
                                <td className="text-muted-foreground px-4 py-2.5 font-mono text-xs">
                                  {sb.claimedAt ? new Date(sb.claimedAt).toLocaleString() : "—"}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                  )}
                </>
              )}

              <PrometheusSection
                clusterID={personalClusterID}
                scopeToCurrentUser
                title="overview.personalUsage"
              />
            </>
          )}
        </div>

        {/* ── Overall usage — platform-wide, not team-isolated ── */}
        {distResolved && !distConfigured ? null : (
          <div className="flex flex-col gap-4">
            <h2 className="text-muted-foreground flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
              <TrendingUp className="h-3.5 w-3.5" />
              {t("overview.overallUsage")}
            </h2>

            <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
              <OverallStatCard
                label={t("overview.totalCreated")}
                value={dist?.totalCreated != null ? dist.totalCreated.toLocaleString() : "—"}
                sub={t("overview.totalCreatedSub")}
                icon={TrendingUp}
                color="text-brand"
                isLoading={!distResolved}
              />
              <OverallStatCard
                label={t("overview.activeUsers")}
                value={dist?.activeUsers != null ? dist.activeUsers.length.toLocaleString() : "—"}
                sub={t("overview.activeUsersSub")}
                icon={UserCheck}
                color="text-green-600 dark:text-green-400"
                isLoading={!distResolved}
              />
              <OverallStatCard
                label={t("overview.platformUsers")}
                value={
                  platformUsersData?.totalUsers != null
                    ? platformUsersData.totalUsers.toLocaleString()
                    : "—"
                }
                sub={t("overview.platformUsersSub")}
                icon={Users}
                color="text-indigo-600 dark:text-indigo-400"
                isLoading={platformUsersData === undefined}
              />
            </div>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <div>
                <Tabs
                  value={distTab}
                  onValueChange={(v) => setDistTab(v as "byUser" | "byTeam")}
                >
                  <TabsList variant="line" className="mb-2">
                    <TabsTrigger value="byUser">{t("overview.byUser")}</TabsTrigger>
                    <TabsTrigger value="byTeam">{t("overview.byTeam")}</TabsTrigger>
                  </TabsList>
                  <TabsContent value="byUser">
                    <DistributionPieChart
                      title={t("overview.byUser")}
                      data={byUserSlices}
                      isLoading={!distResolved}
                      emptyLabel={t("overview.noData")}
                    />
                  </TabsContent>
                  <TabsContent value="byTeam">
                    <DistributionPieChart
                      title={t("overview.byTeam")}
                      data={byTeamSlices}
                      isLoading={!distResolved}
                      emptyLabel={t("overview.noData")}
                    />
                  </TabsContent>
                </Tabs>
              </div>

              <TopUsersBarChart
                title={t("overview.topUsers")}
                rows={dist?.byUser ?? []}
                isLoading={!distResolved}
                emptyLabel={t("overview.noData")}
                otherLabel={t("overview.otherUsers")}
              />
            </div>

            {!isApiKey && scope === "all" && (
              <DistributionPieChart
                title={t("overview.byCluster")}
                data={byClusterSlices}
                isLoading={!distResolved}
                emptyLabel={t("overview.noData")}
              />
            )}
          </div>
        )}
      </div>
    </div>
  )
}
