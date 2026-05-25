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

import { useMemo } from "react"
import { Plus, Settings2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RelativeTime } from "@/components/custom/relative-time"
import { QueryTable } from "@/components/custom/query-table/table-with-query"
import { createPoolColumns } from "@/components/pools/columns"
import { envPoolsQueryOptions } from "@/lib/queries"
import type {
  AgentEnvObservedMember,
  AgentSandboxEnv,
  AgentSandboxPool,
} from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"

// ─── Spec ────────────────────────────────────────────────────────────────────

export function SpecSection({ env }: { env: AgentSandboxEnv }) {
  const { t } = useTranslation()
  const local = env.spec.clusters?.find((c) => c.members && c.members.length > 0)
  return (
    <section>
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("envs.detail.section.spec")}
      </h3>
      <dl className="border-border bg-muted/20 divide-border grid grid-cols-2 divide-y divide-x rounded border text-xs">
        <Row label={t("envs.detail.field.template")} value={env.spec.templateRef.name} />
        <Row label={t("envs.detail.field.mode")} value={env.spec.mode} />
        <Row
          label={t("envs.detail.field.defaults")}
          value={
            env.spec.defaults
              ? `${env.spec.defaults.instanceType ?? "—"} × ${env.spec.defaults.multiplier ?? 1}`
              : "—"
          }
        />
        <Row label={t("envs.detail.field.localCluster")} value={local?.clusterID ?? "—"} />
      </dl>
    </section>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <dt className="text-muted-foreground w-32 shrink-0 font-mono text-xs uppercase">{label}</dt>
      <dd className="truncate font-mono text-xs">{value}</dd>
    </div>
  )
}

// ─── Pools (full table — replaces the standalone /pools page for Env-owned pools) ──

export function EnvPoolsSection({
  env,
  onCreatePool,
  onEditPool,
  onEditAutoscaling,
  onDeletePool,
  onViewMetrics,
  onViewDocs,
}: {
  env: AgentSandboxEnv
  onCreatePool: () => void
  onEditPool: (pool: AgentSandboxPool) => void
  onEditAutoscaling: (pool: AgentSandboxPool) => void
  onDeletePool: (pool: AgentSandboxPool) => void
  onViewMetrics: (pool: AgentSandboxPool) => void
  onViewDocs: (pool: AgentSandboxPool) => void
}) {
  const { t } = useTranslation()

  // Build scalingGroup + observed lookups from the Env spec/status so the
  // pool table can surface autoscaler-derived info that the bare /sandboxpools
  // response doesn't carry.
  const { observedByPool, scalingGroupByPool, memberCount } = useMemo(() => {
    const observed = new Map<string, AgentEnvObservedMember>()
    const groups = new Map<string, string>()
    for (const cluster of env.status?.clusters ?? []) {
      for (const om of cluster.observedMembers ?? []) {
        observed.set(om.name, om)
      }
    }
    let count = 0
    for (const cluster of env.spec.clusters ?? []) {
      for (const m of cluster.members ?? []) {
        if (m.scalingGroup) groups.set(m.name, m.scalingGroup)
        count++
      }
    }
    return { observedByPool: observed, scalingGroupByPool: groups, memberCount: count }
  }, [env])

  const columns = useMemo(
    () =>
      createPoolColumns(t, onViewMetrics, onViewDocs, {
        hideOwningEnv: true,
        envObservedByPool: observedByPool,
        scalingGroupByPool: scalingGroupByPool,
        onEditPool,
        onEditAutoscaling,
        onDeletePool,
      }),
    [
      t,
      onViewMetrics,
      onViewDocs,
      observedByPool,
      scalingGroupByPool,
      onEditPool,
      onEditAutoscaling,
      onDeletePool,
    ],
  )

  const queryOptions = useMemo(() => envPoolsQueryOptions(env.name), [env.name])

  return (
    <section>
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.detail.section.pools")}
          </h3>
          <span className="text-muted-foreground font-mono text-[10px]">
            {t("envs.detail.pools.memberCount", { count: memberCount })}
          </span>
        </div>
      </div>
      <QueryTable
        columns={columns}
        idFn={(row: AgentSandboxPool) => row.name}
        queryOptions={queryOptions}
        toolbarConfig={{ globalSearch: { placeholder: t("pools.searchAll") } }}
      >
        <Button onClick={onCreatePool} size="sm" className="h-9 gap-1 px-2 text-xs" variant="secondary">
          <Plus className="h-3 w-3" /> {t("envs.poolForm.createAction")}
        </Button>
      </QueryTable>
    </section >
  )
}

// ─── Autoscaling read-only summary ───────────────────────────────────────────

export function AutoscalingSummary({
  env,
  onEdit,
}: {
  env: AgentSandboxEnv
  onEdit: () => void
}) {
  const { t } = useTranslation()
  const auto = env.spec.autoscaling
  const groups = auto?.groups ?? []
  const enabled = auto?.enabled === true
  return (
    <section>
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.detail.section.autoscaling")}
        </h3>
        <Button variant="outline" size="sm" onClick={onEdit} className="h-7 gap-1 px-2 text-xs">
          <Settings2 className="h-3 w-3" /> {t("envs.detail.actions.editAutoscaling")}
        </Button>
      </div>
      {!auto || groups.length === 0 ? (
        <p className="text-muted-foreground text-xs">{t("envs.detail.autoscaling.empty")}</p>
      ) : (
        <div className="border-border bg-muted/20 divide-border space-y-3 rounded border p-3">
          <div className="flex items-center gap-2">
            <Badge variant={enabled ? "default" : "outline"} className="font-mono text-xs">
              {enabled
                ? t("envs.detail.autoscaling.enabled")
                : t("envs.detail.autoscaling.disabled")}
            </Badge>
          </div>
          {groups.map((g, i) => (
            <div key={i} className="space-y-1">
              <div className="text-muted-foreground font-mono text-[10px] uppercase">
                {t("envs.detail.autoscaling.group")}: {g.name}
              </div>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 font-mono text-xs">
                <InfoCell label={t("envs.detail.autoscaling.minReplicas")} value={g.minReplicas} />
                <InfoCell label={t("envs.detail.autoscaling.maxReplicas")} value={g.maxReplicas} />
                <InfoCell
                  label={t("envs.detail.autoscaling.mode")}
                  value={g.scaleUpPolicy?.mode}
                />
                <InfoCell
                  label={t("envs.detail.autoscaling.cooldown")}
                  value={g.scaleUpPolicy?.cooldownSeconds}
                />
                <InfoCell
                  label={t("envs.detail.autoscaling.idleThreshold")}
                  value={g.scaleUpPolicy?.idleThresholdSeconds}
                />
                <InfoCell
                  label={t("envs.detail.autoscaling.saturationCooldown")}
                  value={g.scaleUpPolicy?.saturationCooldownSeconds}
                />
                <InfoCell
                  label={t("envs.detail.autoscaling.idleTimeout")}
                  value={g.scaleDownPolicy?.idleTimeoutSeconds}
                />
                <InfoCell
                  label={t("envs.detail.autoscaling.stabilization")}
                  value={g.scaleDownPolicy?.stabilizationSeconds}
                />
              </dl>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function InfoCell({
  label,
  value,
}: {
  label: string
  value: string | number | undefined | null
}) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-dashed py-1 last:border-b-0">
      <span className="text-muted-foreground text-[10px] uppercase">{label}</span>
      <span>{value === undefined || value === null ? "—" : String(value)}</span>
    </div>
  )
}

// ─── Status conditions ───────────────────────────────────────────────────────

export function StatusSection({ env }: { env: AgentSandboxEnv }) {
  const { t } = useTranslation()
  const conditions = env.status?.conditions ?? []
  const localCluster = env.status?.clusters?.find((c) => c.isLocal === true)
  if (conditions.length === 0 && !localCluster) return null
  return (
    <section>
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("envs.detail.section.status")}
      </h3>
      <div className="border-border bg-muted/20 rounded border p-3">
        <div className="space-y-1 font-mono text-xs">
          {conditions.map((c, i) => (
            <div key={i} className="flex items-start gap-2">
              <Badge
                variant={c.status === "True" ? "default" : "outline"}
                className="font-mono text-[10px]"
              >
                {c.type}
              </Badge>
              <span className="text-muted-foreground">{c.message ?? c.reason ?? ""}</span>
            </div>
          ))}
          {localCluster?.lastScaleUpTime && (
            <div className="text-muted-foreground pt-1 text-[10px]">
              {t("envs.col.lastScaleUp")}: <RelativeTime date={localCluster.lastScaleUpTime} />
            </div>
          )}
        </div>
      </div>
    </section>
  )
}
