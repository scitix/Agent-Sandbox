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
import { CloudyIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"

interface ClusterComboboxProps {
  clusters: ClusterEntry[]
  value: string | null
  onValueChange: (clusterId: string | null) => void
  placeholder?: string
  inputClassName?: string
  /** aria-invalid for form integration */
  "aria-invalid"?: boolean
}

export function ClusterCombobox({
  clusters,
  value,
  onValueChange,
  placeholder,
  inputClassName = "h-8 font-mono text-xs",
  "aria-invalid": ariaInvalid,
}: ClusterComboboxProps) {
  const { t } = useTranslation()
  const selected = clusters.find((c) => c.id === value) ?? null

  return (
    <Combobox
      value={selected}
      onValueChange={(cluster) => onValueChange(cluster?.id ?? null)}
      items={clusters}
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
              <CloudyIcon className="size-4" />
              <span className="font-mono">{cluster.name}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
