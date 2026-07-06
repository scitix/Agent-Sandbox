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
import { XIcon } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

import { useTranslation } from "@/lib/i18n"

import { DataTableFacetedFilterNumberRangeProps } from "./faceted-filter"

/**
 * Format a number-range filter value (`[min, max]`) for display. Values may
 * arrive as strings when restored from searchParams. Returns null when the
 * range is empty.
 */
export function formatNumberRange(value: unknown, unit?: string): string | null {
  if (!Array.isArray(value)) return null
  const [min, max] = value as [unknown, unknown]
  const has = (v: unknown) => v !== undefined && v !== null && v !== ""
  const u = unit ?? ""
  if (has(min) && has(max)) {
    return `${min}${u} - ${max}${u}`
  }
  if (has(min)) {
    return `≥ ${min}${u}`
  }
  if (has(max)) {
    return `≤ ${max}${u}`
  }
  return null
}

interface FilterNumberPanelProps<TData> {
  column: Column<TData>
  option: DataTableFacetedFilterNumberRangeProps
  onApplied: () => void
  // focus the min input on mount (skip in the hover-opened flyout, where
  // stealing focus would interrupt typing in the dimension search)
  autoFocusInput?: boolean
}

/**
 * Min/max editor for one number-range filter dimension. Used both as the
 * second-level pane of the Filters menu and inside an active-filter chip
 * popover.
 */
export function FilterNumberPanel<TData>({
  column,
  option,
  onApplied,
  autoFocusInput = false,
}: FilterNumberPanelProps<TData>) {
  const { t } = useTranslation()
  // values may be strings when restored from searchParams
  const currentValues = (column.getFilterValue() as [(number | string)?, (number | string)?]) || [
    undefined,
    undefined,
  ]
  const [minValue, setMinValue] = useState(currentValues[0]?.toString() ?? "")
  const [maxValue, setMaxValue] = useState(currentValues[1]?.toString() ?? "")

  const hasFilter = currentValues[0] !== undefined || currentValues[1] !== undefined

  const toNumber = (v: number | string | undefined) =>
    v === undefined || v === null || v === "" ? undefined : parseFloat(`${v}`)

  const handleApply = () => {
    const min = minValue ? parseFloat(minValue) : undefined
    const max = maxValue ? parseFloat(maxValue) : undefined

    if (min === toNumber(currentValues[0]) && max === toNumber(currentValues[1])) {
      toast.warning(t("table.filterUnchanged"))
      return // No change, do nothing
    }

    if (min === undefined && max === undefined) {
      column.setFilterValue(undefined)
    } else {
      column.setFilterValue([min, max])
    }
    onApplied()
  }

  const handleClear = () => {
    setMinValue("")
    setMaxValue("")
    column.setFilterValue(undefined)
    onApplied()
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      handleApply()
    }
  }

  const unitSuffix = option.unit ? ` (${option.unit.trim()})` : ""

  return (
    <div className="p-1">
      <div className="truncate px-2 pt-1.5 text-sm font-medium">
        {option.title ?? option.columnKey}
      </div>
      <div className="space-y-2 p-2">
        <div className="space-y-1">
          <Label htmlFor={`${option.columnKey}-min-value`} className="text-xs">
            {t("table.minValue")}
            {unitSuffix}
          </Label>
          <Input
            id={`${option.columnKey}-min-value`}
            type="number"
            autoFocus={autoFocusInput}
            placeholder={option.placeholder?.min || t("table.enterMin")}
            value={minValue}
            onChange={(e) => setMinValue(e.target.value)}
            onKeyDown={handleKeyDown}
            className="h-8 text-sm"
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor={`${option.columnKey}-max-value`} className="text-xs">
            {t("table.maxValue")}
            {unitSuffix}
          </Label>
          <Input
            id={`${option.columnKey}-max-value`}
            type="number"
            placeholder={option.placeholder?.max || t("table.enterMax")}
            value={maxValue}
            onChange={(e) => setMaxValue(e.target.value)}
            onKeyDown={handleKeyDown}
            className="h-8 text-sm"
          />
        </div>
        <div className="flex items-center justify-center gap-2 pt-1">
          <Button size="sm" className="flex-1" onClick={handleApply}>
            {t("table.apply")}
          </Button>
          {hasFilter && (
            <Button size="sm" variant="outline" className="flex-1" onClick={handleClear}>
              <XIcon className="size-4" />
              {t("table.clear")}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
