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

// i18n-processed-v1.1.0
import { Column } from "@tanstack/react-table"
import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon } from "lucide-react"

import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"
import { useTranslation } from "@/lib/i18n"

interface ColumnHeaderSortProps<TData, TValue> {
  column: Column<TData, TValue>
  isVisible?: boolean
}

export function ColumnHeaderSort<TData, TValue>({
  column,
  isVisible = true,
}: ColumnHeaderSortProps<TData, TValue>) {
  const { t } = useTranslation()
  if (!column.getCanSort()) {
    return null
  }

  return (
    <TooltipButton
      variant="ghost"
      size="icon"
      tooltip={
        column.getIsSorted()
          ? `${column.getIsSorted() === "asc" ? t("table.sortAsc") : t("table.sortDesc")}`
          : t("table.sort")
      }
      className={cn(
        "data-[state=open]:bg-accent size-6 transition-opacity duration-200",
        isVisible ? "opacity-100" : "pointer-events-none opacity-0",
      )}
      onClick={column.getToggleSortingHandler()}
    >
      {column.getIsSorted() === "desc" ? (
        <ArrowDownIcon className="size-3.5" />
      ) : column.getIsSorted() === "asc" ? (
        <ArrowUpIcon className="size-3.5" />
      ) : (
        <ArrowUpDownIcon className="size-3.5" />
      )}
    </TooltipButton>
  )
}
