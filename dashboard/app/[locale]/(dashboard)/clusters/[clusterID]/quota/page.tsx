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
import { quotasQueryOptions } from "@/lib/queries"
import { PageHeader } from "@/components/page-header"
import { DotPattern } from "@/components/patterns"
import { cn } from "@/lib/utils"
import type { QuotaItem } from "@/lib/api/client"
import { useTranslation } from "@/lib/i18n"

interface ResourceCardProps {
  resourceKey: string
  used: number
  reserved: number
  free: number
  total: number
  usedPct: number
  reservedPct: number
}

function ResourceCard({
  resourceKey,
  used,
  reserved,
  free,
  total,
  usedPct,
  reservedPct,
}: ResourceCardProps) {
  const { t } = useTranslation()
  return (
    <div className="border-border bg-card relative overflow-hidden border">
      <DotPattern className="opacity-15" />
      <div className="relative p-5">
        <div className="mb-3">
          <span className="text-muted-foreground font-mono text-[11px] font-bold tracking-widest uppercase">
            {resourceKey}
          </span>
        </div>

        <div className="text-foreground mb-1 font-mono text-3xl font-bold">
          {used}
          <span className="text-muted-foreground text-lg font-normal"> / {total}</span>
        </div>
        <div className="text-muted-foreground mb-3 font-mono text-xs">{t("quota.usedTotal")}</div>

        {/* Stacked progress bar: used (brand) + reserved (amber) */}
        <div className="bg-secondary relative h-1.5 w-full overflow-hidden">
          <div
            className="bg-brand absolute top-0 left-0 h-full transition-all"
            style={{ width: `${Math.min(usedPct, 100)}%` }}
          />
          <div
            className="absolute top-0 h-full bg-amber-400 transition-all dark:bg-amber-500"
            style={{
              left: `${Math.min(usedPct, 100)}%`,
              width: `${Math.min(reservedPct, 100 - usedPct)}%`,
            }}
          />
        </div>

        {/* Stat pills */}
        <div className="mt-3 flex flex-wrap gap-3">
          <div className="flex items-center gap-1.5">
            <span className="bg-brand h-2 w-2 rounded-full" />
            <span className="text-muted-foreground font-mono text-[12px]">
              {t("quota.used")}{" "}
              <span className={cn("font-semibold", "text-foreground")}>{used}</span>
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-amber-400 dark:bg-amber-500" />
            <span className="text-muted-foreground font-mono text-[12px]">
              {t("quota.reserved")}{" "}
              <span className="text-foreground font-semibold">{reserved}</span>
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="bg-secondary h-2 w-2 rounded-full border" />
            <span className="text-muted-foreground font-mono text-[12px]">
              {t("quota.free")} <span className="text-foreground font-semibold">{free}</span>
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

function QuotaMeta({ quota }: { quota: QuotaItem }) {
  return (
    <div className="mb-6 flex flex-wrap items-center gap-3">
      {quota.team && (
        <span className="border-border bg-secondary border px-2 py-0.5 font-mono text-xs tracking-wide uppercase">
          team: {quota.team}
        </span>
      )}
      {quota.user && (
        <span className="border-border bg-secondary border px-2 py-0.5 font-mono text-xs tracking-wide uppercase">
          user: {quota.user}
        </span>
      )}
      <span className="text-muted-foreground font-mono text-xs break-all">{quota.quotaUrl}</span>
    </div>
  )
}

export default function QuotaPage() {
  const { data: quotas, isPending } = useQuery(quotasQueryOptions())
  const { t } = useTranslation()

  return (
    <div className="flex flex-1 flex-col overflow-auto">
      <PageHeader title={t("quota.title")} />

      <div className="p-6">
        {isPending ? (
          <div className="text-muted-foreground flex flex-col items-center justify-center py-20 text-center">
            <p className="font-mono text-sm">{t("common.loading")}</p>
          </div>
        ) : !quotas || quotas?.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center justify-center py-20 text-center">
            <p className="font-mono text-sm">{t("quota.noQuotasAvailable")}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-8">
            {quotas?.map((quota) => (
              <div key={quota.quotaUrl}>
                <QuotaMeta quota={quota} />
                {!quota.resources || Object.keys(quota.resources).length === 0 ? (
                  <div className="text-muted-foreground py-6 font-mono text-sm">
                    {t("quota.noResourceData")}
                  </div>
                ) : (
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
                    {Object.entries(quota.resources).map(([key, total]) => {
                      const used = quota.used?.[key] ?? "0"
                      const reserved = quota.reserved?.[key] ?? "0"
                      const usedNum = Number(used)
                      const reservedNum = Number(reserved)
                      const totalNum = Number(total)
                      const freeNum = Math.max(0, totalNum - usedNum - reservedNum)
                      const usedPct = totalNum > 0 ? (usedNum / totalNum) * 100 : 0
                      const reservedPct = totalNum > 0 ? (reservedNum / totalNum) * 100 : 0

                      return (
                        <ResourceCard
                          key={key}
                          resourceKey={key}
                          used={usedNum}
                          reserved={reservedNum}
                          free={freeNum}
                          total={totalNum}
                          usedPct={usedPct}
                          reservedPct={reservedPct}
                        />
                      )
                    })}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
