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

import type { ClusterEntry } from "@/lib/api/client"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import { useMemo } from "react"
import { CloudyIcon, LayersIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"

/** Sentinel id for the "all clusters" entry, when the caller opts into it. */
export const ALL_CLUSTERS_ID = "all"

interface ClusterComboboxProps {
  clusters: ClusterEntry[]
  value: string | null
  onValueChange: (clusterId: string | null) => void
  placeholder?: string
  inputClassName?: string
  /**
   * Prepend an "all clusters" entry. The caller decides what selecting it
   * means — this component only reports the id back.
   */
  allowAll?: boolean
  /** aria-invalid for form integration */
  "aria-invalid"?: boolean
}

export function ClusterCombobox({
  clusters,
  value,
  onValueChange,
  placeholder,
  inputClassName = "h-8 font-mono text-xs",
  allowAll = false,
  "aria-invalid": ariaInvalid,
}: ClusterComboboxProps) {
  const { t } = useTranslation()

  const items = useMemo<ClusterEntry[]>(
    () =>
      allowAll
        ? [{ id: ALL_CLUSTERS_ID, name: t("cluster.allClusters"), url: "" }, ...clusters]
        : clusters,
    [allowAll, clusters, t],
  )
  const selected = items.find((c) => c.id === value) ?? null

  return (
    <Combobox
      value={selected}
      onValueChange={(cluster) => onValueChange(cluster?.id ?? null)}
      items={items}
      itemToStringLabel={(c) => c.name ?? c.id}
    >
      <ComboboxInput
        placeholder={placeholder ?? t("cluster.searchCluster")}
        className={inputClassName}
        aria-invalid={ariaInvalid}
      />
      <ComboboxContent>
        <ComboboxEmpty>{t("cluster.noClustersFound")}</ComboboxEmpty>
        <ComboboxList>
          {(cluster) => (
            <ComboboxItem key={cluster.id} value={cluster}>
              {cluster.id === ALL_CLUSTERS_ID ? (
                <LayersIcon className="size-4" />
              ) : (
                <CloudyIcon className="size-4" />
              )}
              <span className="font-mono">{cluster.name}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
