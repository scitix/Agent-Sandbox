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
import { useState } from "react"

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

import { cn } from "@/lib/utils"

import { ColumnHeaderFilter } from "./column-header-filter"
import { ColumnHeaderSort } from "./column-header-sort"
import { useColumnFilterOption } from "./filter-context"

type DataTableColumnHeaderProps<TData, TValue> = React.HTMLAttributes<HTMLDivElement> & {
  column: Column<TData, TValue>
  title: string
  tooltip?: string
}

export function DataTableColumnHeader<TData, TValue>({
  column,
  title,
  tooltip,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  const [isHovered, setIsHovered] = useState(false)
  const [isFilterOpen, setIsFilterOpen] = useState(false)

  // Enum / number-range / text filter configured for this column (if any).
  // Text dimensions render a magnifier; the others a funnel (see
  // column-header-filter.tsx).
  const filterOption = useColumnFilterOption(column.id)
  const showFunnelFilter = !!filterOption

  // Check if filter or sort has non-default values
  const hasActiveSort = column.getIsSorted() !== false
  const hasActiveFilter = column.getFilterValue() !== undefined

  // Fine-grained per-element visibility:
  // elements are shown when hovered/interacting, OR when they have a non-default value
  const isInteracting = isHovered || isFilterOpen
  const sortVisible = isInteracting || hasActiveSort
  const filterVisible = isInteracting || hasActiveFilter

  // If column has no interactive features, just show title
  if (!column.getCanSort() && !filterOption) {
    return <div className={cn("text-xs", className)}>{title}</div>
  }

  return (
    <div
      className={cn("flex items-center", className)}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      {/* Show title only if not editing string filter */}
      {tooltip ? (
        <TooltipProvider delay={100}>
          <Tooltip>
            <TooltipTrigger
              render={<span className="mr-0.5 cursor-help text-xs select-none">{title}</span>}
            />
            <TooltipContent>{tooltip}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : (
        <span className="mr-0.5 text-xs select-none">{title}</span>
      )}

      <div className="flex min-w-0 flex-1 flex-row items-center">
        {/* Enum / number-range / text filter, left of the sort button */}
        {showFunnelFilter && (
          <ColumnHeaderFilter
            column={column}
            option={filterOption}
            title={title}
            isVisible={filterVisible}
            onOpenChange={setIsFilterOpen}
          />
        )}

        {/* Sort */}
        <ColumnHeaderSort column={column} isVisible={sortVisible} />
      </div>
    </div>
  )
}
