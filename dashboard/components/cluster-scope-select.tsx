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

import { useMemo, useState } from "react"
import { useAtomValue } from "jotai"
import { CloudyIcon, LayersIcon, ChevronsUpDownIcon } from "lucide-react"
import { clustersAtom } from "@/lib/atoms"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover"
import {
  Command,
  CommandInput,
  CommandList,
  CommandGroup,
  CommandItem,
  CommandSeparator,
  CommandEmpty,
} from "@/components/ui/command"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { isAllClusters, type ClusterScope } from "@/hooks/use-cluster-scope-search-params"

interface ClusterScopeSelectProps {
  value: ClusterScope
  onValueChange: (scope: ClusterScope) => void
  className?: string
}

/**
 * Cluster-scope picker for the cluster-agnostic `/overview` and `/admin`
 * pages: writes a searchParam instead of navigating/re-authenticating, unlike
 * `<ClusterSwitcher>`.
 *
 * Multi-select, because comparing a couple of clusters side by side is a
 * normal thing to want and the metric queries express it as one regex matcher.
 * "All clusters" is mutually exclusive with an explicit subset — it tracks the
 * cluster list rather than freezing today's members.
 */
export function ClusterScopeSelect({ value, onValueChange, className }: ClusterScopeSelectProps) {
  const { t } = useTranslation()
  const clusters = useAtomValue(clustersAtom).clusters
  const [open, setOpen] = useState(false)

  const selectedIDs = useMemo(
    () => new Set(isAllClusters(value) ? [] : value),
    [value],
  )

  const label = useMemo(() => {
    if (isAllClusters(value)) return t("cluster.allClusters")
    if (value.length === 1) {
      const only = clusters.find((c) => c.id === value[0])
      return only?.name ?? value[0]
    }
    return t("cluster.nSelected", { count: value.length })
  }, [value, clusters, t])

  const toggle = (id: string) => {
    const next = new Set(selectedIDs)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    // Deselecting the last cluster falls back to "all" rather than leaving an
    // empty scope that would render every chart blank.
    onValueChange(next.size === 0 ? "all" : clusters.filter((c) => next.has(c.id)).map((c) => c.id))
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            className={cn("h-8 w-[200px] justify-between font-mono text-xs", className)}
          />
        }
      >
        <span className="flex min-w-0 items-center gap-2">
          {isAllClusters(value) ? (
            <LayersIcon className="size-4 shrink-0" />
          ) : (
            <CloudyIcon className="size-4 shrink-0" />
          )}
          <span className="truncate">{label}</span>
        </span>
        <ChevronsUpDownIcon className="text-muted-foreground size-4 shrink-0" />
      </PopoverTrigger>
      <PopoverContent className="w-[240px] p-0" align="end">
        <Command>
          <CommandInput placeholder={t("cluster.searchCluster")} />
          <CommandList>
            <CommandEmpty>{t("cluster.noClustersFound")}</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value={t("cluster.allClusters")}
                className="cursor-pointer [&>svg:last-child]:hidden"
                onSelect={() => {
                  onValueChange("all")
                  setOpen(false)
                }}
              >
                <LayersIcon className="size-4" />
                <span className="font-mono">{t("cluster.allClusters")}</span>
                {isAllClusters(value) && (
                  <span className="text-muted-foreground ml-auto font-mono text-xs">✓</span>
                )}
              </CommandItem>
            </CommandGroup>
            <CommandSeparator />
            <CommandGroup>
              {clusters.map((c) => (
                <CommandItem
                  key={c.id}
                  value={`${c.name ?? ""} ${c.id}`}
                  className="cursor-pointer [&>svg:last-child]:hidden"
                  onSelect={() => toggle(c.id)}
                >
                  {/* Presentational only: onSelect owns the toggle, so the
                      checkbox must not handle the click itself. */}
                  <Checkbox
                    checked={selectedIDs.has(c.id)}
                    tabIndex={-1}
                    className="pointer-events-none"
                  />
                  <span className="truncate font-mono">{c.name ?? c.id}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
