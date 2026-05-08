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
import { ListFilterIcon, XIcon } from "lucide-react"
import { ReactNode, useState } from "react"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"

interface ColumnHeaderNumberFilterProps<TData, TValue> {
  column: Column<TData, TValue>
  title: string
  options: {
    title?: string
    unit?: string
    placeholder?: {
      min?: string
      max?: string
    }
  }
  onOpenChange?: (open: boolean) => void
  isVisible?: boolean
}

export function ColumnHeaderNumberFilter<TData, TValue>({
  column,
  title,
  options,
  onOpenChange,
  isVisible = true,
}: ColumnHeaderNumberFilterProps<TData, TValue>) {
  const [minValue, setMinValue] = useState<string>("")
  const [maxValue, setMaxValue] = useState<string>("")

  // Get current filter values for number range — extract as primitives so
  // React can do a cheap equality check in the useEffect dependency array.
  const filterValue = column?.getFilterValue() as [number?, number?] | undefined
  const currentMin = filterValue?.[0]
  const currentMax = filterValue?.[1]
  const hasNumberFilter = currentMin !== undefined || currentMax !== undefined

  // Sync external filter changes (e.g. toolbar "Clear all") to local inputs
  // during render, avoiding useEffect + setState cascading render issues.
  // See: https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes
  const [prevMin, setPrevMin] = useState(currentMin)
  const [prevMax, setPrevMax] = useState(currentMax)
  if (currentMin !== prevMin || currentMax !== prevMax) {
    setPrevMin(currentMin)
    setPrevMax(currentMax)
    setMinValue(currentMin?.toString() || "")
    setMaxValue(currentMax?.toString() || "")
  }

  const handleApplyNumberFilter = () => {
    const min = minValue ? parseFloat(minValue) : undefined
    const max = maxValue ? parseFloat(maxValue) : undefined

    if (min === currentMin && max === currentMax) {
      toast.warning("Filter unchanged")
      return
    }

    if (min === undefined && max === undefined) {
      column?.setFilterValue(undefined)
    } else {
      column?.setFilterValue([min, max])
    }
  }

  const handleClearNumberFilter = () => {
    setMinValue("")
    setMaxValue("")
    column?.setFilterValue(undefined)
  }

  const getNumberFilterDisplayText = (): ReactNode => {
    const unit = options.unit || ""

    if (currentMin !== undefined && currentMax !== undefined) {
      return `${currentMin}${unit}-${currentMax}${unit}`
    }
    if (currentMin !== undefined) {
      return `>=${currentMin}${unit}`
    }
    if (currentMax !== undefined) {
      return `<=${currentMax}${unit}`
    }
    return null
  }

  const triggerEl = hasNumberFilter ? (
    <button>
      <Badge
        className={cn(
          "ml-1 cursor-pointer px-1 py-0 font-mono text-xs transition-opacity duration-200",
          !isVisible && "pointer-events-none opacity-0",
        )}
      >
        {getNumberFilterDisplayText()}
      </Badge>
    </button>
  ) : (
    <TooltipButton
      variant="ghost"
      size="icon"
      className={cn(
        "size-6 transition-opacity duration-200",
        !isVisible && "pointer-events-none opacity-0",
      )}
      tooltip={`Filter by ${options.title || title}`}
    >
      <ListFilterIcon className="size-3.5" />
    </TooltipButton>
  )

  return (
    <Popover onOpenChange={onOpenChange}>
      <PopoverTrigger render={triggerEl} />
      <PopoverContent className="w-75 p-4" align="start">
        <div className="space-y-4">
          <div className="text-sm font-medium">Filter by {options.title || title}</div>
          <div className="space-y-2">
            <div className="space-y-1">
              <Label htmlFor="min-value" className="text-xs">
                Min
                {options.unit && ` (${options.unit.trim()})`}
              </Label>
              <Input
                id="min-value"
                type="number"
                placeholder={options?.placeholder?.min || "Enter min value"}
                value={minValue}
                onChange={(e) => setMinValue(e.target.value)}
                className="h-8 text-sm"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="max-value" className="text-xs">
                Max
                {options.unit && ` (${options.unit.trim()})`}
              </Label>
              <Input
                id="max-value"
                type="number"
                placeholder={options?.placeholder?.max || "Enter max value"}
                value={maxValue}
                onChange={(e) => setMaxValue(e.target.value)}
                className="h-8 text-sm"
              />
            </div>
          </div>
          <div className="flex items-center justify-center gap-2">
            {hasNumberFilter && (
              <Button
                size="sm"
                variant="outline"
                className="grow"
                onClick={handleClearNumberFilter}
              >
                <XIcon className="size-4" />
                Clear
              </Button>
            )}
            <Button
              size="sm"
              className="grow"
              variant="secondary"
              onClick={handleApplyNumberFilter}
            >
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
