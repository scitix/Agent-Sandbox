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
import { ChevronsLeftRightEllipsisIcon, XIcon } from "lucide-react"
import { ReactNode, useEffect, useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"

import TooltipButton from "../button/tooltip-button"

export interface DataTableNumberRangeFilterProps<TData> {
  column?: Column<TData>
  title?: string
  defaultValues?: [number?, number?]
  placeholder?: {
    min?: string
    max?: string
  }
}

export function DataTableNumberRangeFilter<TData>({
  column,
  title,
  defaultValues,
  placeholder,
}: DataTableNumberRangeFilterProps<TData>) {
  const currentValues = (column?.getFilterValue() as [number?, number?]) || [undefined, undefined]
  const [minValue, setMinValue] = useState<string>(currentValues[0]?.toString() || "")
  const [maxValue, setMaxValue] = useState<string>(currentValues[1]?.toString() || "")

  // load default values
  useEffect(() => {
    if (defaultValues) {
      column?.setFilterValue(defaultValues)
      setMinValue(defaultValues[0]?.toString() || "")
      setMaxValue(defaultValues[1]?.toString() || "")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultValues])

  const hasFilter = currentValues[0] !== undefined || currentValues[1] !== undefined

  const handleApply = () => {
    const min = minValue ? parseFloat(minValue) : undefined
    const max = maxValue ? parseFloat(maxValue) : undefined

    if (min === currentValues[0] && max === currentValues[1]) {
      toast.warning("Filter unchanged")
      return // No change, do nothing
    }

    if (min === undefined && max === undefined) {
      column?.setFilterValue(undefined)
    } else {
      column?.setFilterValue([min, max])
    }
  }

  const handleClear = () => {
    setMinValue("")
    setMaxValue("")
    column?.setFilterValue(undefined)
  }

  const getDisplayText = (): ReactNode => {
    if (currentValues[0] !== undefined && currentValues[1] !== undefined) {
      return `${currentValues[0]} - ${currentValues[1]}`
    }
    if (currentValues[0] !== undefined) {
      return `≥ ${currentValues[0]}`
    }
    if (currentValues[1] !== undefined) {
      return `≤ ${currentValues[1]}`
    }
    return null
  }

  const trigger = (
    <TooltipButton
      variant="outline"
      size="sm"
      className="h-9 border-dashed"
      tooltip={`Filter by ${title}`}
      side="bottom"
    >
      <ChevronsLeftRightEllipsisIcon className="size-4" />
      <span className="ml-1 hidden text-xs sm:inline">{title}</span>
      <span className="sr-only">{title}</span>
      {hasFilter && (
        <>
          <Separator orientation="vertical" className="hidden h-4 sm:block" />
          <Badge variant="secondary" className="rounded-sm px-1 font-normal">
            {getDisplayText()}
          </Badge>
        </>
      )}
    </TooltipButton>
  )

  return (
    <Popover>
      <PopoverTrigger render={trigger} />
      <PopoverContent className="w-[300px] p-4" align="start">
        <div className="space-y-4">
          <div className="text-sm font-medium">{title}</div>
          <div className="space-y-2">
            <div className="space-y-1">
              <Label htmlFor="min-value" className="text-xs">
                Min
              </Label>
              <Input
                id="min-value"
                type="number"
                placeholder={placeholder?.min || "Enter min value"}
                value={minValue}
                onChange={(e) => setMinValue(e.target.value)}
                className="h-8 text-sm"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="max-value" className="text-xs">
                Max
              </Label>
              <Input
                id="max-value"
                type="number"
                placeholder={placeholder?.max || "Enter max value"}
                value={maxValue}
                onChange={(e) => setMaxValue(e.target.value)}
                className="h-8 text-sm"
              />
            </div>
          </div>
          <div className="flex items-center justify-center gap-2">
            <Button size="sm" className="flex-grow" onClick={handleApply}>
              Apply
            </Button>
            {hasFilter && (
              <Button size="sm" variant="outline" className="flex-grow" onClick={handleClear}>
                <XIcon className="size-4" />
                Clear
              </Button>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
