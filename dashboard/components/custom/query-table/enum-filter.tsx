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
// Modified code - migrated to Base UI
import { Column } from "@tanstack/react-table"
import { CheckIcon, XIcon } from "lucide-react"
import { ReactNode, useEffect, useMemo, useState } from "react"

import { Badge } from "@/components/ui/badge"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"

import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"

export interface DataTableFacetedFilterOption {
  label: ReactNode
  value: string
  isDestructive?: boolean
  count?: number
}

export interface DataTableDefaultFilterProps<TData> {
  column?: Column<TData>
  title?: string
  options?: DataTableFacetedFilterOption[]
  emptyOption?: {
    label: ReactNode
  }
  renderer?: (value: string) => ReactNode
  defaultValues?: string[]
}

export function DataTableDefaultFilter<TData>({
  column,
  title,
  options: rawOptions,
  emptyOption,
  renderer,
  defaultValues,
}: DataTableDefaultFilterProps<TData>) {
  const facets = column?.getFacetedUniqueValues()
  const rawFilterValue = column?.getFilterValue()
  const selectedValues = new Set(
    Array.isArray(rawFilterValue)
      ? (rawFilterValue as string[])
      : typeof rawFilterValue === "string"
        ? [rawFilterValue]
        : [],
  )
  const [initialized, setInitialized] = useState(false)

  // load default values from column filter state
  useEffect(() => {
    if (defaultValues && selectedValues.size === 0 && !initialized) {
      column?.setFilterValue(defaultValues)
      setInitialized(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [defaultValues, selectedValues, initialized])

  const options: DataTableFacetedFilterOption[] = useMemo(() => {
    if (rawOptions) {
      return rawOptions.filter((option) => 0 < (facets?.get(option.value) || 0))
    }
    // return options from facets if no rawOptions provided
    // sort by count descending
    return Array.from(facets || [])
      .map(([value, count]) => {
        let label: ReactNode = value
        if (renderer) {
          label = renderer(value)
        }
        return {
          label: label || emptyOption?.label || "Unknown",
          value: value,
          isDestructive: false,
          count,
        }
      })
      .sort((a, b) => (b.count || 0) - (a.count || 0))
  }, [rawOptions, facets, renderer, emptyOption?.label])

  const triggerContent = (
    <TooltipButton
      variant="outline"
      size="sm"
      className="h-9 border-dashed px-2"
      tooltip={
        options.length > 0
          ? `Filter by ${title?.toLowerCase()}`
          : `No filterable ${title?.toLowerCase()} values`
      }
      disabled={facets?.size === 0}
    >
      <Badge className="bg-secondary text-secondary-foreground rounded-sm px-1 text-xs">
        {options.length}
      </Badge>
      <span className="hidden text-xs font-normal sm:inline">{title}</span>
      <span className="sr-only">{title}</span>
      {selectedValues.size > 0 && (
        <>
          <Separator orientation="vertical" className="hidden h-4 sm:block" />
          <Badge className="rounded-sm px-1 font-normal lg:hidden">{selectedValues.size}</Badge>
          <div className="hidden space-x-1 lg:flex">
            {selectedValues.size > 2 ? (
              <Badge className="rounded-sm px-1 font-normal">
                <span className="font-mono">{selectedValues.size}</span>
                {" selected"}
              </Badge>
            ) : (
              Array.from(selectedValues).map((value) => {
                const option =
                  rawOptions?.find((opt) => opt.value === value) ||
                  options.find((opt) => opt.value === value)
                return (
                  <Badge
                    key={value}
                    className="rounded-sm px-1 font-normal"
                    variant={
                      renderer !== undefined
                        ? "outline"
                        : option?.isDestructive
                          ? "destructive"
                          : "default"
                    }
                  >
                    {option?.label || value}
                  </Badge>
                )
              })
            )}
          </div>
        </>
      )}
    </TooltipButton>
  )

  return (
    <Popover>
      <PopoverTrigger render={triggerContent} />
      <PopoverContent className="w-75 p-0" align="start">
        <Command shouldFilter={true}>
          <CommandInput placeholder={`Search ${title?.toLowerCase()}...`} />
          <CommandList className="relative max-h-100 overflow-hidden">
            <CommandEmpty>No results</CommandEmpty>
            <div className="max-h-72 overflow-auto">
              <CommandGroup>
                {options
                  .filter((option) => 0 < (facets?.get(option.value) || 0))
                  .map((option) => {
                    const isSelected = selectedValues.has(option.value)
                    return (
                      <CommandItem
                        key={option.value}
                        value={option.value}
                        className="cursor-pointer"
                        onSelect={() => {
                          if (isSelected) {
                            selectedValues.delete(option.value)
                          } else {
                            selectedValues.add(option.value)
                          }
                          const filterValues = Array.from(selectedValues)
                          column?.setFilterValue(filterValues.length ? filterValues : undefined)
                        }}
                      >
                        <div
                          className={cn(
                            "border-primary flex size-4 items-center justify-center rounded-sm border",
                            isSelected
                              ? "bg-primary text-primary-foreground"
                              : "opacity-50 [&_svg]:invisible",
                          )}
                        >
                          <CheckIcon className={cn("text-primary-foreground size-4")} />
                        </div>
                        <span>{option.label}</span>
                        {facets?.get(option.value) && (
                          <span className="flex-1 text-right font-mono text-xs">
                            {facets.get(option.value)}
                          </span>
                        )}
                      </CommandItem>
                    )
                  })}
              </CommandGroup>
            </div>
            {selectedValues.size > 0 && (
              <div className="w-full">
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem
                    onSelect={() => {
                      column?.setFilterValue(null)
                    }}
                    className="justify-center text-center"
                  >
                    <XIcon />
                    Clear filters
                  </CommandItem>
                </CommandGroup>
              </div>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
