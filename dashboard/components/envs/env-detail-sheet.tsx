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

import { useQuery } from "@tanstack/react-query"
import Link from "next/link"
import { Boxes, ExternalLink, Settings2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { RelativeTime } from "@/components/custom/relative-time"
import { envQueryOptions, poolQueryOptions } from "@/lib/queries"
import type {
  AgentEnvObservedMember,
  AgentSandboxEnv,
  AgentSandboxPool,
} from "@/lib/api/client"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useTranslation } from "@/lib/i18n"
import { POOL_DOCS_PARAM } from "@/components/pools/pool-docs-sheet"

interface Props {
  env: AgentSandboxEnv | null
  onOpenChange: (open: boolean) => void
  onEditAutoscaling: (env: AgentSandboxEnv) => void
}

/**
 * EnvDetailSheet shows the read-only Env spec, an inline table of member
 * Pools (each row driven by its own live poolQueryOptions), a status section
 * (conditions + saturation flags), and an "Edit" entry to the autoscaling
 * editor.
 *
 * Shell-vs-inner split mirrors the project's standard SOP: the outer Sheet
 * controls open state, the inner component fetches fresh data only while
 * the sheet is mounted.
 */
export function EnvDetailSheet({ env, onOpenChange, onEditAutoscaling }: Props) {
  const isOpen = !!env
  return (
    <Sheet open={isOpen} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-4xl"
      >
        {env && <EnvDetailInner envName={env.name} onEditAutoscaling={onEditAutoscaling} />}
      </SheetContent>
    </Sheet>
  )
}

function EnvDetailInner({
  envName,
  onEditAutoscaling,
}: {
  envName: string
  onEditAutoscaling: (env: AgentSandboxEnv) => void
}) {
  const { data, isLoading } = useQuery(envQueryOptions(envName))
  const env = data?.env

  return (
    <>
      <SheetHeader className="border-border border-b px-5 py-4">
        <div className="flex items-center gap-3">
          <div className="bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg">
            <Boxes className="h-4 w-4" />
          </div>
          <SheetTitle className="font-mono text-base font-semibold">{envName}</SheetTitle>
        </div>
      </SheetHeader>

      <div className="flex-1 overflow-y-auto px-5 py-5">
        {isLoading || !env ? (
          <div className="text-muted-foreground text-sm">…</div>
        ) : (
          <div className="space-y-6">
            <SpecSection env={env} />
            <MembersSection env={env} />
            <AutoscalingSummary env={env} onEdit={() => onEditAutoscaling(env)} />
            <StatusSection env={env} />
          </div>
        )}
      </div>
    </>
  )
}

// ─── Spec ─────────────────────────────────────────────────────────────────────

function SpecSection({ env }: { env: AgentSandboxEnv }) {
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

// ─── Members (inline per-pool status) ─────────────────────────────────────────

function MembersSection({ env }: { env: AgentSandboxEnv }) {
  const { t } = useTranslation()
  const allMembers = env.spec.clusters?.flatMap((c) => c.members ?? []) ?? []
  const observedByName = new Map<string, AgentEnvObservedMember>()
  for (const cluster of env.status?.clusters ?? []) {
    for (const om of cluster.observedMembers ?? []) {
      observedByName.set(om.name, om)
    }
  }
  if (allMembers.length === 0) {
    return (
      <section>
        <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.detail.section.members")}
        </h3>
        <p className="text-muted-foreground text-xs">{t("envs.empty")}</p>
      </section>
    )
  }
  return (
    <section>
      <h3 className="text-muted-foreground mb-2 font-mono text-xs font-bold tracking-[0.12em] uppercase">
        {t("envs.detail.section.members")}
      </h3>
      <div className="border-border divide-border divide-y overflow-hidden rounded border">
        <div className="bg-muted/30 text-muted-foreground grid grid-cols-[1.4fr_1fr_0.7fr_0.7fr_0.7fr_0.7fr_1fr_0.5fr] gap-2 px-3 py-2 font-mono text-[10px] font-bold tracking-wider uppercase">
          <span>{t("envs.detail.members.col.name")}</span>
          <span>{t("envs.detail.members.col.scalingGroup")}</span>
          <span className="text-right">{t("envs.detail.members.col.replicas")}</span>
          <span className="text-right">{t("envs.detail.members.col.idle")}</span>
          <span className="text-right">{t("envs.detail.members.col.running")}</span>
          <span className="text-right">{t("envs.detail.members.col.pending")}</span>
          <span>{t("envs.detail.members.col.state")}</span>
          <span></span>
        </div>
        {allMembers.map((m) => (
          <MemberRow
            key={m.name}
            memberName={m.name}
            scalingGroup={m.scalingGroup ?? ""}
            observed={observedByName.get(m.name)}
            envNamespace={env.namespace}
          />
        ))}
      </div>
    </section>
  )
}

function MemberRow({
  memberName,
  scalingGroup,
  observed,
  envNamespace,
}: {
  memberName: string
  scalingGroup: string
  observed?: AgentEnvObservedMember
  envNamespace: string
}) {
  const { t } = useTranslation()
  const { data: pool } = useQuery(poolQueryOptions(memberName))
  const p = pool?.template as AgentSandboxPool | undefined
  const clusterID = useClusterID()
  const replicas = p?.spec?.replicas ?? observed?.currentReplicas ?? "—"
  const idle = p?.status?.idleReplicas ?? observed?.idleCount ?? 0
  const running = p?.status?.runningReplicas ?? observed?.runningCount ?? 0
  const pending = p?.status?.pendingRequests ?? observed?.pendingRequests ?? 0
  const state = observed?.state ?? ""
  const saturatedUntil = observed?.saturatedUntil
  const lastResult = observed?.lastScaleUpAttemptResult

  // Mirror sandboxes → pool jump pattern; opens PoolDocsSheet on the Pools page.
  const poolHref = `${clusterPath(clusterID, "pools")}?${POOL_DOCS_PARAM}=${encodeURIComponent(memberName)}`
  // envNamespace is unused at the moment but reserved for future "open in
  // another namespace" scenarios — keep the param so we don't break callers.
  void envNamespace

  return (
    <div className="grid grid-cols-[1.4fr_1fr_0.7fr_0.7fr_0.7fr_0.7fr_1fr_0.5fr] items-center gap-2 px-3 py-2 font-mono text-xs">
      <span className="truncate font-semibold">{memberName}</span>
      <span className="text-muted-foreground truncate">{scalingGroup || "—"}</span>
      <span className="text-right">{replicas}</span>
      <span className="text-right">{idle}</span>
      <span className="text-right">{running}</span>
      <span className={`text-right ${pending > 0 ? "text-amber-600" : ""}`}>{pending}</span>
      <span className="flex flex-col gap-0.5">
        {state ? (
          <Badge variant="outline" className="w-fit font-mono text-[10px]">
            {state}
          </Badge>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
        {saturatedUntil && (
          <span className="text-amber-600 text-[10px]">
            {t("envs.detail.members.col.saturatedUntil")}: <RelativeTime date={saturatedUntil} />
          </span>
        )}
        {lastResult && lastResult !== "Success" && (
          <span className="text-muted-foreground text-[10px]">{lastResult}</span>
        )}
      </span>
      <Button
        variant="ghost"
        size="icon"
        className="h-6 w-6"
        title={t("envs.detail.members.openPool")}
        render={<Link href={poolHref} />}
      >
        <ExternalLink className="h-3 w-3" />
      </Button>
    </div>
  )
}

// ─── Autoscaling read-only summary ────────────────────────────────────────────

function AutoscalingSummary({ env, onEdit }: { env: AgentSandboxEnv; onEdit: () => void }) {
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

function StatusSection({ env }: { env: AgentSandboxEnv }) {
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
