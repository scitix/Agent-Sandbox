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

import { use, useMemo } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import { useQuery } from "@tanstack/react-query"
import { Loader2, TrendingUp } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { DetailHeader } from "@/components/custom/detail-header"
import { DataTable } from "@/components/custom/query-table/table-without-query"
import { ScaleUpCell, ScaleDownCell } from "@/components/envs/env-detail-sections"
import { createPoolColumns, poolNumberFilterOptions } from "@/components/pools/columns"
import { envQueryOptions, envPoolsQueryOptions } from "@/lib/queries"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"
import type { AgentEnvAutoscalingGroup, AgentSandboxPool } from "@/lib/api/client"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; group: string; locale: string }>
}

/**
 * Standalone detail page for one autoscaling scaling-group: its policy summary
 * plus every member Pool that belongs to the group (with the autoscaler's
 * observed per-member state). The Env layout yields its chrome to this route,
 * so the page renders its own header.
 */
export default function AutoscalingGroupPage({ params }: PageProps) {
  const { name, group } = use(params)
  const { t } = useTranslation()
  const router = useRouter()
  const clusterID = useClusterID()
  const locale = useLocale()

  const { data: envData, isLoading } = useQuery(envQueryOptions(name))
  const env = envData?.env ?? null
  const { data: pools } = useQuery(envPoolsQueryOptions(name))

  const groupConfig = env?.spec.autoscaling?.groups?.find((g) => g.name === group) ?? null

  // The member Pools that belong to this scaling group. The Autoscaling column
  // is intentionally omitted here (the whole page is already scoped to one
  // group), so we only need the membership filter — not the observed-state maps.
  const members = useMemo(() => {
    const inGroup = new Set<string>()
    for (const cluster of env?.spec.clusters ?? []) {
      for (const m of cluster.members ?? []) {
        if (m.config?.scalingGroup === group) inGroup.add(m.name)
      }
    }
    return (pools ?? []).filter((p: AgentSandboxPool) => inGroup.has(p.name))
  }, [env, pools, group])

  const envPath = `${clusterPath(clusterID, "envs", locale)}/${encodeURIComponent(name)}`

  const columns = useMemo(
    () =>
      createPoolColumns(
        t,
        (pool) => router.push(`${envPath}/pools/${encodeURIComponent(pool.name)}/metrics`),
        { hideOwningEnv: true },
      ),
    [t, router, envPath],
  )

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <DetailHeader
        icon={TrendingUp}
        title={group}
        copyValue={group}
        kind={t("envs.autoscaling.detail.kind")}
        meta={[
          {
            label: t("envs.tab.autoscaling"),
            value: (
              <Link
                href={`${envPath}/autoscaling`}
                className="text-foreground hover:text-brand underline-offset-2 hover:underline"
              >
                {name}
              </Link>
            ),
          },
        ]}
      />

      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-auto px-6 pb-6">
        {isLoading ? (
          <div className="flex flex-1 items-center justify-center">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        ) : !groupConfig ? (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-muted-foreground text-sm">{t("envs.autoscaling.detail.notFound")}</p>
          </div>
        ) : (
          <>
            <GroupPolicySection group={groupConfig} />
            <section className="flex min-h-0 flex-1 flex-col">
              <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("envs.autoscaling.detail.members")}
              </h3>
              <DataTable
                data={members}
                dataUpdatedAt={0}
                isLoading={false}
                columns={columns}
                idFn={(row: AgentSandboxPool) => row.name}
                toolbarConfig={{
                  filterOptions: poolNumberFilterOptions(t),
                  globalSearch: { placeholder: t("pools.searchAll") },
                }}
              />
            </section>
          </>
        )}
      </div>
    </div>
  )
}

function GroupPolicySection({ group }: { group: AgentEnvAutoscalingGroup }) {
  const { t } = useTranslation()
  return (
    <section>
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("envs.autoscaling.detail.policy")}
      </h3>
      <div className="border-border bg-muted/20 grid grid-cols-2 gap-px overflow-hidden rounded border md:grid-cols-4">
        <Cell label={t("envs.autoscaling.col.enabled")}>
          <Badge
            variant={group.enabled ? "default" : "outline"}
            className="w-fit font-mono text-[10px]"
          >
            {group.enabled
              ? t("envs.detail.autoscaling.enabled")
              : t("envs.detail.autoscaling.disabled")}
          </Badge>
        </Cell>
        <Cell label={t("envs.autoscaling.detail.minReplicas")}>
          <span className="font-mono text-xs">{group.minReplicas ?? "—"}</span>
        </Cell>
        <Cell label={t("envs.autoscaling.detail.maxReplicas")}>
          <span className="font-mono text-xs">{group.maxReplicas ?? "—"}</span>
        </Cell>
        <Cell label={t("envs.autoscaling.detail.scaleUp")}>
          <ScaleUpCell group={group} />
        </Cell>
        <Cell label={t("envs.autoscaling.detail.scaleDown")}>
          <ScaleDownCell group={group} />
        </Cell>
      </div>
    </section>
  )
}

function Cell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="bg-card flex flex-col gap-1 px-3 py-2.5">
      <span className="text-muted-foreground font-mono text-[10px] font-bold tracking-[0.12em] uppercase">
        {label}
      </span>
      {children}
    </div>
  )
}
