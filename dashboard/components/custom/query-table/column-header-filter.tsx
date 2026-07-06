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
import { FilterIcon, SearchIcon } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

import { DataTableFacetedFilterProps } from "./faceted-filter"
import { FilterDimensionPanel } from "./filters"

interface ColumnHeaderFilterProps<TData, TValue> {
  column: Column<TData, TValue>
  option: DataTableFacetedFilterProps
  title: string
  isVisible?: boolean
  onOpenChange?: (open: boolean) => void
}

/**
 * In-place funnel for a column whose dimension is configured in the table's
 * filter options. Opens the same editor pane as the toolbar Filters menu,
 * anchored under the header icon; selections flow through the column filter
 * state, so the matching toolbar chip appears automatically.
 */
export function ColumnHeaderFilter<TData, TValue>({
  column,
  option,
  title,
  isVisible = true,
  onOpenChange,
}: ColumnHeaderFilterProps<TData, TValue>) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const hasActiveFilter = column.getFilterValue() !== undefined

  const handleOpenChange = (next: boolean) => {
    setOpen(next)
    onOpenChange?.(next)
  }

  // Text dimensions present a magnifier (fuzzy search) rather than a funnel.
  const isText = option.variant === "text"
  const label = isText
    ? t("table.searchField", { field: title })
    : t("table.filterField", { field: title })
  const TriggerIcon = isText ? SearchIcon : FilterIcon

  return (
    <Popover open={open} onOpenChange={handleOpenChange} modal={true}>
      {/* The funnel button is both the tooltip and popover trigger: base-ui
          composes them by nesting `render` props onto a single Button. */}
      <TooltipProvider delay={100}>
        <Tooltip>
          <TooltipTrigger
            render={
              <PopoverTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={label}
                    className={cn(
                      "data-[popup-open]:bg-accent size-6 transition-opacity duration-200",
                      isVisible ? "opacity-100" : "pointer-events-none opacity-0",
                      hasActiveFilter && "text-primary",
                    )}
                  />
                }
              />
            }
          >
            <TriggerIcon className="size-3.5" />
          </TooltipTrigger>
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent className="w-65 p-0" align="start">
        <FilterDimensionPanel
          entry={{ option, column: column as Column<TData> }}
          onApplied={() => handleOpenChange(false)}
          autoFocusInput
        />
      </PopoverContent>
    </Popover>
  )
}
