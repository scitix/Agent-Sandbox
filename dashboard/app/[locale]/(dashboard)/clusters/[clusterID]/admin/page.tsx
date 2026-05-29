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

import { useAtomValue } from "jotai"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useMemo } from "react"
import { BarChart3, RefreshCw, Users, Box } from "lucide-react"
import { isAdminAtom } from "@/lib/atoms"
import { sandboxStatsQueryOptions, replicasOverviewQueryOptions } from "@/lib/queries"
import { Button } from "@/components/ui/button"
import { PrometheusSection } from "@/components/prometheus/prometheus-section"
import { useRouter } from "next/navigation"
import { useEffect } from "react"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { useTranslation } from "@/lib/i18n"

// ─── StatCard ──────────────────────────────────────────────────────────────

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

// ─── Main page ─────────────────────────────────────────────────────────────

export default function AdminPage() {
  const isAdmin = useAtomValue(isAdminAtom)
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()
  const qc = useQueryClient()

  useEffect(() => {
    if (!isAdmin) {
      router.replace(clusterPath(clusterID, "sandboxes", locale))
    }
  }, [isAdmin, router, clusterID, locale])

  // Probe Prometheus config first — gates whether fallback stats are shown
  const prometheusFilters = useMemo(() => ({ cluster: clusterID }), [clusterID])
  const { data: promOverview } = useQuery(replicasOverviewQueryOptions(prometheusFilters))
  const promResolved = promOverview !== undefined
  const prometheusConfigured = promOverview?.configured === true
  const showFallback = promResolved && !prometheusConfigured

  const sandboxStatsOptions = sandboxStatsQueryOptions()

  const { data: sandboxStatsData, isFetching: sandboxFetching } = useQuery(sandboxStatsOptions)

  const sandboxStats = sandboxStatsData?.statistics ?? null
  const loading = !sandboxStatsData && sandboxFetching

  const handleRefresh = () => {
    void qc.refetchQueries({ queryKey: sandboxStatsOptions.queryKey })
  }

  if (!isAdmin) return null

  const runningCount = sandboxStats?.byStatus?.["Running"] ?? 0
  const failedCount = sandboxStats?.byStatus?.["Failed"] ?? 0
  const activatingCount = sandboxStats?.byStatus?.["Starting"] ?? 0

  const namespaceEntries = sandboxStats?.byNamespace ? Object.entries(sandboxStats.byNamespace) : []

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      {/* Sub-header with refresh: only shown in fallback mode */}
      {showFallback && (
        <div className="border-border flex items-center gap-4 border-b px-6">
          <button className="border-foreground text-foreground flex items-center gap-1.5 border-b-2 py-3 font-mono text-sm font-semibold tracking-wide uppercase">
            <BarChart3 className="h-4 w-4" />
            {t("admin.overview")}
          </button>
          <div className="ml-auto">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleRefresh}
              className="text-muted-foreground h-7 w-7"
              disabled={sandboxFetching}
            >
              <RefreshCw className={`h-3.5 w-3.5 ${sandboxFetching ? "animate-spin" : ""}`} />
            </Button>
          </div>
        </div>
      )}

      {!promResolved || loading ? (
        <div className="flex flex-1 items-center justify-center">
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
                  {t("admin.sandboxes")}
                </h2>
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                  <StatCard
                    label="Total"
                    value={sandboxStats?.total ?? 0}
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
                    value={failedCount}
                    icon={Box}
                    color="text-red-600 dark:text-red-400"
                  />
                </div>
              </div>

              {/* Namespace breakdown */}
              {namespaceEntries.length > 0 && (
                <div>
                  <h2 className="text-muted-foreground mb-3 flex items-center gap-2 font-mono text-xs font-bold tracking-[0.15em] uppercase">
                    <Users className="h-3.5 w-3.5" />
                    {t("admin.byNamespace")}
                  </h2>
                  <div className="border-border overflow-hidden rounded-xl border">
                    <table className="w-full">
                      <thead>
                        <tr className="border-border bg-secondary border-b">
                          <th className="text-muted-foreground px-4 py-2 text-left font-mono text-xs font-bold tracking-wider uppercase">
                            {t("pools.col.namespace")}
                          </th>
                          <th className="text-muted-foreground px-4 py-2 text-right font-mono text-xs font-bold tracking-wider uppercase">
                            {t("admin.sandboxes")}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {namespaceEntries.map(([ns, count]) => (
                          <tr
                            key={ns}
                            className="border-border hover:bg-secondary/50 border-b last:border-0"
                          >
                            <td className="text-foreground px-4 py-2.5 font-mono text-sm">{ns}</td>
                            <td className="px-4 py-2.5 text-right font-mono text-sm">{count}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </>
          )}

          {/* Prometheus Metrics (hidden if not configured) */}
          <PrometheusSection clusterID={clusterID} showAdminControls />
        </div>
      )}
    </div>
  )
}
