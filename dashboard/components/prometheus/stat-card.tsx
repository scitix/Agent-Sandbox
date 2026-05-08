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

import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@/components/ui/tooltip"

export function StatCard({
  label,
  value,
  sub,
  icon: Icon,
  color,
  tooltip,
  isLoading,
}: {
  label: string
  value: number | string
  sub?: string
  icon: React.ComponentType<{ className?: string }>
  color: string
  tooltip?: string
  isLoading?: boolean
}) {
  return (
    <div className="border-border bg-card rounded-xl border p-4">
      <div className="mb-3 flex items-start justify-between">
        {tooltip ? (
          <TooltipProvider delay={300}>
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="text-muted-foreground cursor-help font-mono text-xs font-bold tracking-[0.15em] uppercase select-none" />
                }
              >
                {label}
              </TooltipTrigger>
              <TooltipContent side="top">{tooltip}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : (
          <span className="text-muted-foreground font-mono text-xs font-bold tracking-[0.15em] uppercase">
            {label}
          </span>
        )}
        <Icon className={`h-4 w-4 ${color}`} />
      </div>
      {isLoading ? (
        <div className="mt-1 flex flex-col gap-2">
          <div className="bg-muted h-8 w-16 animate-pulse rounded" />
          {sub && <div className="bg-muted h-3 w-24 animate-pulse rounded" />}
        </div>
      ) : (
        <>
          <div className={`font-mono text-3xl font-bold ${color}`}>{value}</div>
          {sub && <div className="text-muted-foreground mt-1 font-mono text-xs">{sub}</div>}
        </>
      )}
    </div>
  )
}
