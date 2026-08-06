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

import { useEffect, useMemo, useState } from "react"
import { useAtomValue } from "jotai"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import {
  BarChart3,
  RefreshCw,
  Users,
  Box,
  Bell,
  Send,
  ShieldAlert,
  ShieldOff,
} from "lucide-react"
import { isAdminAtom, clusterIDAtom, clustersAtom } from "@/lib/atoms"
import {
  replicasOverviewQueryOptions,
  platformUsersCountQueryOptions,
  adminUsersSummaryQueryOptions,
  notificationConfigQueryOptions,
  notificationHistoryQueryOptions,
  useUpdateNotificationConfig,
  useTriggerDailyReport,
  useArmIdleAlert,
  useDisarmIdleAlert,
} from "@/lib/queries"
import { PrometheusSection } from "@/components/prometheus/prometheus-section"
import { StatCard } from "@/components/prometheus/stat-card"
import { ClusterScopeSelect } from "@/components/cluster-scope-select"
import { GrafanaTimePicker } from "@/components/prometheus/grafana-time-picker"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterScopeSearchParams } from "@/hooks/use-cluster-scope-search-params"
import { useTimeRangeSearchParams } from "@/hooks/use-time-range-search-params"
import { useAdminSandboxStats } from "@/hooks/use-admin-sandbox-stats"
import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { type RefreshInterval } from "@/lib/types/prometheus"
import { useTranslation } from "@/lib/i18n"
import type { GlobalNotificationConfig } from "@/lib/queries/notifications"

export default function AdminPage() {
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const qc = useQueryClient()
  const isAdmin = useAtomValue(isAdminAtom)
  const boundClusterID = useAtomValue(clusterIDAtom)

  useEffect(() => {
    if (!isAdmin) {
      router.replace(clusterPath(clusterID, "sandboxes", locale))
    }
  }, [isAdmin, router, clusterID, locale])

  // ─── Cluster scope + time range (shared header controls) ────────────────────
  const [scope, setScope] = useClusterScopeSearchParams()
  const [timeRange, setTimeRange] = useTimeRangeSearchParams()
  const [refreshInterval, setRefreshInterval] = useState<RefreshInterval>(0)
  const personalClusterID = scope === "all" ? boundClusterID : scope

  // Probe Prometheus config first — gates whether the K8s-fallback stats are shown
  const prometheusFilters = useMemo(() => ({ cluster: personalClusterID }), [personalClusterID])
  const { data: promOverview, dataUpdatedAt } = useQuery(replicasOverviewQueryOptions(prometheusFilters))
  const promResolved = promOverview !== undefined
  const prometheusConfigured = promOverview?.configured === true
  const showFallback = promResolved && !prometheusConfigured

  const sandboxStats = useAdminSandboxStats(scope)
  const countdown = useRefreshCountdown(dataUpdatedAt, refreshInterval > 0 ? refreshInterval : undefined)

  const handleRefresh = () => {
    void qc.refetchQueries({ queryKey: replicasOverviewQueryOptions(prometheusFilters).queryKey })
    sandboxStats.refetchAll()
  }

  if (!isAdmin) return null

  const runningCount = sandboxStats.byStatus["Running"] ?? 0
  const failedCount = sandboxStats.byStatus["Failed"] ?? 0
  const startingCount = sandboxStats.byStatus["Starting"] ?? 0

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      {/* Page header: title + cluster scope + time range */}
      <div className="border-border flex flex-wrap items-center gap-3 border-b px-6 py-3">
        <span className="text-foreground flex items-center gap-1.5 font-mono text-sm font-semibold tracking-wide uppercase">
          <BarChart3 className="h-4 w-4" />
          {t("admin.title")}
        </span>
        <div className="ml-auto flex items-center gap-3">
          <ClusterScopeSelect value={scope} onValueChange={setScope} />
          <GrafanaTimePicker
            value={timeRange}
            onValueChange={setTimeRange}
            refreshInterval={refreshInterval}
            onRefreshIntervalChange={setRefreshInterval}
            onRefresh={handleRefresh}
            countdown={countdown}
            isFetching={sandboxStats.isFetching}
          />
        </div>
      </div>

      {!promResolved ? (
        <div className="flex flex-1 items-center justify-center">
          <div className="flex flex-col items-center gap-2">
            <div className="bg-brand h-1 w-24 animate-pulse" />
            <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
              {t("common.loading")}
            </span>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-10 p-6">
          {/* ── K8s fallback stats — only when Prometheus is not configured ── */}
          {showFallback && (
            <div className="flex flex-col gap-8">
              <div className="flex items-center justify-end">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={handleRefresh}
                  className="text-muted-foreground h-7 w-7"
                  disabled={sandboxStats.isFetching}
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${sandboxStats.isFetching ? "animate-spin" : ""}`} />
                </Button>
              </div>

              <div>
                <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
                  <Box className="h-3.5 w-3.5" />
                  {t("admin.sandboxes")}
                </h2>
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                  <StatCard label="Total" value={sandboxStats.total} icon={Box} color="text-foreground" />
                  <StatCard
                    label={t("status.running")}
                    value={runningCount}
                    icon={Box}
                    color="text-green-600 dark:text-green-400"
                  />
                  <StatCard
                    label={t("status.starting")}
                    value={startingCount}
                    icon={Box}
                    color="text-yellow-600 dark:text-yellow-400"
                  />
                  <StatCard
                    label={t("status.failed")}
                    value={failedCount}
                    icon={Box}
                    color="text-red-600 dark:text-red-400"
                  />
                </div>
              </div>

              {sandboxStats.rows.length > 0 && (
                <div>
                  <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
                    <Users className="h-3.5 w-3.5" />
                    {t("admin.byNamespace")}
                  </h2>
                  <div className="border-border overflow-hidden overflow-x-auto rounded-xl border">
                    <table className="w-full">
                      <thead>
                        <tr className="border-border bg-secondary border-b">
                          {sandboxStats.isMultiCluster && (
                            <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                              {t("admin.cluster")}
                            </th>
                          )}
                          <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                            {t("pools.col.namespace")}
                          </th>
                          <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                            {t("admin.sandboxes")}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {sandboxStats.rows.map((row) => (
                          <tr
                            key={`${row.clusterID}/${row.namespace}`}
                            className="border-border hover:bg-secondary/50 border-b last:border-0"
                          >
                            {sandboxStats.isMultiCluster && (
                              <td className="text-muted-foreground px-4 py-2.5 font-mono text-sm">
                                {row.clusterID}
                              </td>
                            )}
                            <td className="text-foreground px-4 py-2.5 font-mono text-sm">{row.namespace}</td>
                            <td className="px-4 py-2.5 text-right font-mono text-sm">{row.count}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* ── Platform users ── */}
          <PlatformUsersSection />

          {/* ── Notification console ── */}
          <NotificationConsoleSection />

          {/* Prometheus Metrics (hidden if not configured) */}
          <PrometheusSection clusterID={personalClusterID} showAdminControls />
        </div>
      )}
    </div>
  )
}

// ─── Platform users ─────────────────────────────────────────────────────────────

function PlatformUsersSection() {
  const { t } = useTranslation()
  const { data: platformUsers, isFetching: usersFetching } = useQuery(platformUsersCountQueryOptions())
  const { data: usersSummary, isFetching: summaryFetching } = useQuery(adminUsersSummaryQueryOptions())

  const byTeam = usersSummary?.byTeam ?? []

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-muted-foreground flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
        <Users className="h-3.5 w-3.5" />
        {t("overview.platformUsers")}
      </h2>

      <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
        <StatCard
          label={t("admin.platformUsers")}
          value={platformUsers?.totalUsers != null ? platformUsers.totalUsers.toLocaleString() : "—"}
          sub={t("admin.platformUsersSub")}
          icon={Users}
          color="text-indigo-600 dark:text-indigo-400"
          isLoading={platformUsers === undefined && usersFetching}
        />
      </div>

      {byTeam.length > 0 && (
        <div>
          <h3 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
            {t("admin.usersByTeam")}
          </h3>
          <div className="border-border overflow-hidden rounded-xl border">
            <table className="w-full">
              <thead>
                <tr className="border-border bg-secondary border-b">
                  <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                    {t("admin.colTeam")}
                  </th>
                  <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                    {t("admin.colUsers")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {byTeam.map((row) => (
                  <tr key={row.team} className="border-border hover:bg-secondary/50 border-b last:border-0">
                    <td className="text-foreground px-4 py-2.5 font-mono text-sm">{row.team}</td>
                    <td className="px-4 py-2.5 text-right font-mono text-sm">{row.users}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      {summaryFetching && byTeam.length === 0 && (
        <span className="text-muted-foreground font-mono text-xs">{t("common.loading")}</span>
      )}
    </div>
  )
}

// ─── Notification console ───────────────────────────────────────────────────────

function NotificationConsoleSection() {
  const { t } = useTranslation()
  const { data: config } = useQuery(notificationConfigQueryOptions())

  if (!config) {
    return (
      <div className="flex flex-col gap-4">
        <h2 className="text-muted-foreground flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Bell className="h-3.5 w-3.5" />
          {t("admin.notificationConsole")}
        </h2>
        <span className="text-muted-foreground font-mono text-xs">{t("common.loading")}</span>
      </div>
    )
  }

  // Remount on server-config change so the local draft always starts from the
  // authoritative value instead of syncing it in via an effect.
  return <NotificationConsoleForm key={JSON.stringify(config)} initialConfig={config} />
}

function NotificationConsoleForm({ initialConfig }: { initialConfig: GlobalNotificationConfig }) {
  const { t } = useTranslation()
  const clustersData = useAtomValue(clustersAtom)
  const { data: history } = useQuery(notificationHistoryQueryOptions())

  const [form, setForm] = useState<GlobalNotificationConfig>(initialConfig)

  const updateConfig = useUpdateNotificationConfig()
  const triggerDailyReport = useTriggerDailyReport()
  const armIdleAlert = useArmIdleAlert()
  const disarmIdleAlert = useDisarmIdleAlert()

  const handleSave = () => {
    updateConfig.mutate(
      { body: form },
      {
        onSuccess: (data) => {
          setForm(data)
          toast.success(t("admin.configSaved"))
        },
        onError: () => toast.error(t("admin.configSaveFailed")),
      },
    )
  }

  const handleTriggerDailyReport = () => {
    triggerDailyReport.mutate(undefined, {
      onSuccess: (data) => {
        if (data.result === "success") {
          toast.success(t("admin.reportTriggered"))
        } else {
          toast.error(data.detail ?? t("admin.reportTriggerFailed"))
        }
      },
      onError: () => toast.error(t("admin.reportTriggerFailed")),
    })
  }

  const handleToggleArmed = () => {
    if (form.idleAlert.armed) {
      disarmIdleAlert.mutate(undefined, {
        onSuccess: (data) => {
          setForm({ ...form, idleAlert: data })
          toast.success(t("admin.idleAlertDisarmed"))
        },
      })
    } else {
      armIdleAlert.mutate(undefined, {
        onSuccess: (data) => {
          setForm({ ...form, idleAlert: data })
          toast.success(t("admin.idleAlertArmed"))
        },
      })
    }
  }

  const toggleWatchedCluster = (id: string, checked: boolean) => {
    const watchedClusters = checked
      ? [...form.idleAlert.watchedClusters, id]
      : form.idleAlert.watchedClusters.filter((c) => c !== id)
    setForm({ ...form, idleAlert: { ...form.idleAlert, watchedClusters } })
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-muted-foreground flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          <Bell className="h-3.5 w-3.5" />
          {t("admin.notificationConsole")}
        </h2>
        <div className="ml-auto">
          <Button size="sm" onClick={handleSave} disabled={updateConfig.isPending}>
            {t("common.save")}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Daily report */}
        <div className="border-border bg-card flex flex-col gap-3 rounded-xl border p-4">
          <div className="flex items-center justify-between">
            <span className="text-foreground font-mono text-sm font-semibold">{t("admin.dailyReport")}</span>
            <Switch
              checked={form.dailyReport.enabled}
              onCheckedChange={(checked) =>
                setForm({ ...form, dailyReport: { ...form.dailyReport, enabled: checked } })
              }
            />
          </div>
          <p className="text-muted-foreground font-mono text-xs">{t("admin.dailyReportDesc")}</p>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
              {t("admin.sendHourCST")}
            </span>
            <Input
              type="number"
              min={0}
              max={23}
              value={form.dailyReport.sendHourCST}
              onChange={(e) =>
                setForm({
                  ...form,
                  dailyReport: { ...form.dailyReport, sendHourCST: Number(e.target.value) },
                })
              }
              className="w-20"
            />
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleTriggerDailyReport}
            disabled={triggerDailyReport.isPending}
            className="w-fit"
          >
            <Send className="h-3.5 w-3.5" />
            {t("admin.sendNow")}
          </Button>
        </div>

        {/* Idle alert */}
        <div className="border-border bg-card flex flex-col gap-3 rounded-xl border p-4">
          <div className="flex items-center justify-between">
            <span className="text-foreground font-mono text-sm font-semibold">{t("admin.idleAlert")}</span>
            <Switch
              checked={form.idleAlert.enabled}
              onCheckedChange={(checked) =>
                setForm({ ...form, idleAlert: { ...form.idleAlert, enabled: checked } })
              }
            />
          </div>
          <p className="text-muted-foreground font-mono text-xs">{t("admin.idleAlertDesc")}</p>

          <div>
            <span className="text-muted-foreground mb-1.5 block font-mono text-xs tracking-wider uppercase">
              {t("admin.watchedClusters")}
            </span>
            <div className="flex flex-col gap-1.5">
              {clustersData.clusters.map((c) => (
                <label key={c.id} className="flex items-center gap-2 font-mono text-xs">
                  <Checkbox
                    checked={form.idleAlert.watchedClusters.includes(c.id)}
                    onCheckedChange={(checked) => toggleWatchedCluster(c.id, !!checked)}
                  />
                  {c.name || c.id}
                </label>
              ))}
            </div>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
              {t("admin.idleThresholdMinutes")}
            </span>
            <Input
              type="number"
              min={1}
              value={form.idleAlert.idleThresholdMinutes}
              onChange={(e) =>
                setForm({
                  ...form,
                  idleAlert: { ...form.idleAlert, idleThresholdMinutes: Number(e.target.value) },
                })
              }
              className="w-24"
            />
          </div>

          <div className="flex items-center gap-3">
            <Button
              variant={form.idleAlert.armed ? "destructive" : "secondary"}
              size="sm"
              onClick={handleToggleArmed}
              disabled={armIdleAlert.isPending || disarmIdleAlert.isPending}
              className="w-fit"
            >
              {form.idleAlert.armed ? (
                <ShieldOff className="h-3.5 w-3.5" />
              ) : (
                <ShieldAlert className="h-3.5 w-3.5" />
              )}
              {form.idleAlert.armed ? t("admin.disarm") : t("admin.arm")}
            </Button>
            <span className="text-muted-foreground font-mono text-xs">
              {form.idleAlert.armed
                ? t("admin.armedSince", {
                    time: form.idleAlert.armedAt ? new Date(form.idleAlert.armedAt).toLocaleString() : "—",
                  })
                : t("admin.disarmed")}
            </span>
          </div>
        </div>
      </div>

      {/* History */}
      <div>
        <h3 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
          {t("admin.notificationHistory")}
        </h3>
        {!history || history.length === 0 ? (
          <span className="text-muted-foreground font-mono text-xs">{t("admin.notificationHistoryEmpty")}</span>
        ) : (
          <div className="border-border overflow-hidden rounded-xl border">
            <table className="w-full">
              <tbody>
                {history
                  .slice()
                  .reverse()
                  .slice(0, 20)
                  .map((entry, i) => (
                    <tr
                      key={`${entry.time}-${i}`}
                      className="border-border hover:bg-secondary/50 border-b last:border-0"
                    >
                      <td className="text-muted-foreground px-4 py-2 font-mono text-xs whitespace-nowrap">
                        {new Date(entry.time).toLocaleString()}
                      </td>
                      <td className="text-foreground px-4 py-2 font-mono text-xs">{entry.type}</td>
                      <td className="px-4 py-2">
                        <span
                          className={`font-mono text-xs font-bold uppercase ${
                            entry.result === "success"
                              ? "text-green-600 dark:text-green-400"
                              : "text-red-600 dark:text-red-400"
                          }`}
                        >
                          {entry.result}
                        </span>
                      </td>
                      <td className="text-muted-foreground px-4 py-2 font-mono text-xs">{entry.detail ?? ""}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
