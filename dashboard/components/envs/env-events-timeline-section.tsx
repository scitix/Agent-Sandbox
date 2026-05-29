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
 * EnvEventsTimelineSection — vertical activity timeline that surfaces the
 * K8s Events emitted against a SandboxEnv and its member SandboxPools.
 * Backed by the `/v1/envs/{name}/events` endpoint, which merges
 * events.k8s.io/v1 (modern recorder output) with core/v1 (kubelet /
 * built-in signals) and sorts newest-first. Reads land within seconds of
 * the controller emitting them — no Prometheus round-trip — so this is
 * the timeline of record for scale, health, and refresh activity.
 */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  AlertTriangle,
  Circle,
  RefreshCw,
  TrendingDown,
  TrendingUp,
  type LucideIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useTranslation } from "@/lib/i18n"
import { C } from "@/components/prometheus/colors"
import { envEventsQueryOptions } from "@/lib/queries"
import type { components } from "@/lib/api/schema"

type EnvEvent = components["schemas"]["EnvEvent"]

type EventCategory = "scale" | "health" | "refresh" | "other"

const SCALE_REASONS = new Set([
  "ScaleUp",
  "ScaleDown",
  "AutoscalerScaleUp",
  "AutoscalerScaleDown",
  "SandboxPoolPhaseScalingUp",
  "SandboxPoolPhaseScalingDown",
])
const HEALTH_REASONS = new Set([
  "PoolReady",
  "PoolRecovered",
  "Degraded",
  "SandboxPoolPhaseDegraded",
])
const REFRESH_REASONS = new Set(["RefreshMember", "SyncTemplate"])

function categorize(reason: string): EventCategory {
  if (SCALE_REASONS.has(reason)) return "scale"
  if (HEALTH_REASONS.has(reason)) return "health"
  if (REFRESH_REASONS.has(reason)) return "refresh"
  return "other"
}

function iconFor(reason: string): { Icon: LucideIcon; color: string } {
  if (reason === "ScaleUp" || reason === "AutoscalerScaleUp" || reason === "SandboxPoolPhaseScalingUp") {
    return { Icon: TrendingUp, color: C.warning }
  }
  if (reason === "ScaleDown" || reason === "AutoscalerScaleDown" || reason === "SandboxPoolPhaseScalingDown") {
    return { Icon: TrendingDown, color: C.idle }
  }
  if (reason === "PoolReady" || reason === "PoolRecovered") {
    return { Icon: Circle, color: C.success }
  }
  if (reason === "Degraded" || reason === "SandboxPoolPhaseDegraded") {
    return { Icon: AlertTriangle, color: C.error }
  }
  if (reason === "RefreshMember" || reason === "SyncTemplate") {
    return { Icon: RefreshCw, color: C.desired }
  }
  return { Icon: Circle, color: C.idle }
}

function formatRelative(t: string | undefined): string {
  if (!t) return "—"
  const date = new Date(t)
  const diff = Date.now() - date.getTime()
  const m = Math.floor(diff / 60_000)
  if (m < 1) return "just now"
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

export interface EnvEventsTimelineSectionProps {
  envName: string
}

const CATEGORIES: EventCategory[] = ["scale", "health", "refresh"]

export function EnvEventsTimelineSection({ envName }: EnvEventsTimelineSectionProps) {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<"all" | EventCategory>("all")

  const { data: events = [] } = useQuery({
    ...envEventsQueryOptions(envName, 100),
    refetchInterval: 30_000,
  })

  const filtered = useMemo(() => {
    if (filter === "all") return events
    return events.filter((e: EnvEvent) => categorize(e.reason) === filter)
  }, [events, filter])

  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.detail.section.events")}
        </h3>
        <div className="flex items-center gap-1">
          <FilterChip
            label={t("envs.detail.events.filter.all")}
            active={filter === "all"}
            onClick={() => setFilter("all")}
          />
          {CATEGORIES.map((c) => (
            <FilterChip
              key={c}
              label={t(
                c === "scale"
                  ? "envs.detail.events.filter.scale"
                  : c === "health"
                    ? "envs.detail.events.filter.health"
                    : "envs.detail.events.filter.refresh",
              )}
              active={filter === c}
              onClick={() => setFilter(c)}
            />
          ))}
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className="border-border text-muted-foreground rounded border border-dashed p-6 text-center font-mono text-xs">
          {t("envs.detail.events.empty")}
        </div>
      ) : (
        <ol className="border-border bg-background relative space-y-2 rounded border p-3">
          {filtered.map((e: EnvEvent, i: number) => (
            <TimelineRow key={`${e.involvedKind}-${e.involvedName}-${e.reason}-${i}`} event={e} envName={envName} />
          ))}
        </ol>
      )}
    </section>
  )
}

function FilterChip({
  label,
  active,
  onClick,
}: {
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <Button
      size="sm"
      variant={active ? "default" : "outline"}
      className="h-6 px-2 font-mono text-[10px] tracking-wide uppercase"
      onClick={onClick}
    >
      {label}
    </Button>
  )
}

function TimelineRow({ event, envName }: { event: EnvEvent; envName: string }) {
  const { t } = useTranslation()
  const { Icon, color } = iconFor(event.reason)
  const isEnv = event.involvedKind === "SandboxEnv" && event.involvedName === envName
  const isWarning = event.type === "Warning"

  return (
    <li className="flex items-start gap-3 py-1">
      <div
        className="bg-background border-border mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full border"
        style={{ color }}
      >
        <Icon className="h-3.5 w-3.5" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          <span className="font-mono font-semibold">{event.reason}</span>
          {event.count > 1 ? (
            <Badge variant="outline" className="font-mono text-[10px]">
              {t("envs.detail.events.countSuffix", { count: String(event.count) })}
            </Badge>
          ) : null}
          {isWarning ? (
            <Badge
              variant="outline"
              className="font-mono text-[10px]"
              style={{ borderColor: C.error, color: C.error }}
            >
              Warning
            </Badge>
          ) : null}
          {isEnv ? (
            <Badge variant="outline" className="font-mono text-[10px]">
              {t("envs.detail.events.envBadge")}
            </Badge>
          ) : (
            <Badge variant="outline" className="font-mono text-[10px]">
              {t("envs.detail.events.poolBadge", { name: event.involvedName })}
            </Badge>
          )}
        </div>
        <p className="text-muted-foreground font-mono text-[11px] break-words">
          {event.message}
        </p>
        <span className="text-muted-foreground font-mono text-[10px]">
          {formatRelative(event.lastTimestamp ?? event.firstTimestamp ?? undefined)}
        </span>
      </div>
    </li>
  )
}
