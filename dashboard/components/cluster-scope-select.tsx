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
import { useAtomValue } from "jotai"
import { authAtom, clusterIDAtom, clustersAtom } from "@/lib/atoms"
import type { ClusterEntry } from "@/lib/api/client"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import { CloudyIcon, LayersIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import type { ClusterScope } from "@/hooks/use-cluster-scope-search-params"

const ALL_CLUSTERS: ClusterEntry = { id: "all", name: "", url: "" }

interface ClusterScopeSelectProps {
  value: ClusterScope
  onValueChange: (scope: ClusterScope) => void
  inputClassName?: string
}

/**
 * Cluster-scope picker for the cluster-agnostic `/overview` and `/admin`
 * pages: writes a searchParam instead of navigating/re-authenticating, unlike
 * `<ClusterSwitcher>`. API-key sessions are single-cluster credentials that
 * cannot aggregate across clusters, so for them this renders locked to the
 * cluster bound at login with no "all clusters" option.
 */
export function ClusterScopeSelect({ value, onValueChange, inputClassName }: ClusterScopeSelectProps) {
  const { t } = useTranslation()
  const auth = useAtomValue(authAtom)
  const boundClusterID = useAtomValue(clusterIDAtom)
  const clustersData = useAtomValue(clustersAtom)
  const clusters = clustersData.clusters

  const isApiKey = auth?.authMethod === "apikey"

  const items = useMemo<ClusterEntry[]>(() => {
    if (isApiKey) {
      return clusters.filter((c) => c.id === boundClusterID)
    }
    return [{ ...ALL_CLUSTERS, name: t("cluster.allClusters") }, ...clusters]
  }, [isApiKey, clusters, boundClusterID, t])

  const selected = items.find((c) => c.id === value) ?? items[0] ?? null

  if (isApiKey) {
    return (
      <div className="text-muted-foreground flex h-8 items-center gap-2 px-2 font-mono text-xs">
        <CloudyIcon className="size-4" />
        {clusters.find((c) => c.id === boundClusterID)?.name ?? boundClusterID}
      </div>
    )
  }

  return (
    <Combobox
      value={selected}
      onValueChange={(cluster) => onValueChange((cluster?.id as ClusterScope) ?? "all")}
      items={items}
      itemToStringLabel={(c) => (c.id === "all" ? t("cluster.allClusters") : (c.name ?? c.id))}
    >
      <ComboboxInput
        placeholder={t("cluster.searchCluster")}
        className={inputClassName ?? "h-8 w-[200px] font-mono text-xs"}
      />
      <ComboboxContent>
        <ComboboxEmpty>{t("cluster.noClustersFound")}</ComboboxEmpty>
        <ComboboxList>
          {(cluster) => (
            <ComboboxItem key={cluster.id} value={cluster}>
              {cluster.id === "all" ? (
                <LayersIcon className="size-4" />
              ) : (
                <CloudyIcon className="size-4" />
              )}
              <span className="font-mono">{cluster.id === "all" ? t("cluster.allClusters") : cluster.name}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
