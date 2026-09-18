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

import type { ClusterEntry, PeerSite } from "@/lib/api/client"
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox"
import { useMemo } from "react"
import { CloudyIcon, LayersIcon, ArrowUpRightIcon } from "lucide-react"
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
  /**
   * Other Dashboard deployments, listed under the clusters as links out.
   *
   * They are pinned below the scrolling list rather than mixed into it because
   * they are not clusters and picking one is not a selection — it leaves this
   * deployment. Keeping them out of `items` is also what stops the search box
   * from filtering away the only route to the other site.
   */
  peerSites?: PeerSite[]
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
  peerSites,
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
        {peerSites && peerSites.length > 0 && (
          <>
            <div className="bg-border h-px" />
            <div className="p-1">
              {peerSites.map((site) => (
                // A real anchor, not an onClick handler: the whole point is to
                // leave for another origin, and this keeps middle-click and
                // "open in new tab" working on the way out. `target="_blank"`
                // makes that the default too: the other console is a separate
                // product with its own session, and sending the tab away would
                // throw away the work open in this one.
                <a
                  key={site.url}
                  href={site.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent focus-visible:text-accent-foreground relative flex w-full items-center gap-2 rounded-sm py-1.5 pr-2 pl-2 text-sm outline-hidden"
                >
                  <ArrowUpRightIcon className="text-muted-foreground size-4 shrink-0" />
                  <span className="truncate font-mono">{site.name}</span>
                </a>
              ))}
            </div>
          </>
        )}
      </ComboboxContent>
    </Combobox>
  )
}
