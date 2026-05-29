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
import { useMemo } from "react"
import { useAtomValue } from "jotai"
import { LayoutDashboard, Box, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  userSandboxStatsQueryOptions,
  sandboxesQueryOptions,
  replicasOverviewQueryOptions,
} from "@/lib/queries"
import { PrometheusSection } from "@/components/prometheus/prometheus-section"
import { useClusterID } from "@/hooks/use-cluster-id"
import { isActualAdminAtom, impersonationAtom, authAtom } from "@/lib/atoms"
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
  const clusterID = useClusterID()
  const isActualAdmin = useAtomValue(isActualAdminAtom)
  const impersonation = useAtomValue(impersonationAtom)
  const auth = useAtomValue(authAtom)

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

  // Probe Prometheus config first — result gates whether fallback stats are shown
  const prometheusFilters = useMemo(() => ({ cluster: clusterID }), [clusterID])
  const { data: promOverview } = useQuery(replicasOverviewQueryOptions(prometheusFilters))
  // undefined = still loading; false = not configured; true = configured
  const promResolved = promOverview !== undefined
  const prometheusConfigured = promOverview?.configured === true

  const statsOptions = userSandboxStatsQueryOptions()
  const sandboxOptions = sandboxesQueryOptions({ limit: 10, offset: 0 })

  const { data: statsData, isFetching: statsFetching } = useQuery(statsOptions)
  const { data: sandboxes, isFetching: sandboxFetching } = useQuery(sandboxOptions)

  const stats = statsData?.statistics ?? null
  const loading = !statsData && statsFetching

  const handleRefresh = () => {
    void qc.refetchQueries({ queryKey: statsOptions.queryKey })
    void qc.refetchQueries({ queryKey: sandboxOptions.queryKey })
  }

  const runningCount = stats?.byStatus?.["Running"] ?? 0
  const activatingCount = stats?.byStatus?.["Starting"] ?? 0
  const faultyCount = stats?.byStatus?.["Failed"] ?? 0

  // Show fallback only after probe resolves AND Prometheus is not configured
  const showFallback = promResolved && !prometheusConfigured

  return (
    <div className="overflow-y-auto">
      {/* Sub-header: only shown in fallback mode (Prometheus not configured) */}
      {showFallback && (
        <div className="border-border flex items-center gap-4 border-b px-6">
          <button className="border-foreground text-foreground flex items-center gap-1.5 border-b-2 py-3 font-mono text-sm font-semibold tracking-wide uppercase">
            <LayoutDashboard className="h-4 w-4" />
            {t("overview.title")}
          </button>
          {scopeLabel && (
            <span className="text-muted-foreground font-mono text-xs">
              {t("overview.viewingAs", { user: scopeLabel })}
            </span>
          )}
          <div className="ml-auto">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleRefresh}
              className="text-muted-foreground h-7 w-7"
              disabled={statsFetching || sandboxFetching}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${statsFetching || sandboxFetching ? "animate-spin" : ""}`}
              />
            </Button>
          </div>
        </div>
      )}

      {/* Loading state: wait for both probe and stats */}
      {!promResolved || loading ? (
        <div className="flex h-[calc(100vh-51px)] items-center justify-center">
          <div className="flex flex-col items-center gap-2">
            <div className="bg-brand h-1 w-24 animate-pulse" />
            <span className="text-muted-foreground font-mono text-xs tracking-wider uppercase">
              {t("common.loading")}
            </span>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-8 p-6">
          {/* Fallback stats — only when Prometheus is not configured */}
          {showFallback && (
            <>
              {/* Sandbox Stats */}
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

              {/* Recent Sandboxes */}
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

          {/* Prometheus Metrics — scoped to current user (admin's own data, or impersonated user) */}
          <PrometheusSection clusterID={clusterID} scopeToCurrentUser />
        </div>
      )}
    </div>
  )
}
