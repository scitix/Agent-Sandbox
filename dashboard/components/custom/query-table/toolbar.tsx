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
import { Table } from "@tanstack/react-table"
import { SearchIcon, XIcon } from "lucide-react"
import { useState } from "react"
import { useDebouncedCallback } from "use-debounce"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

import { DataTableFacetedFilterProps } from "./faceted-filter"
import { DataTableFilters } from "./filters"
import { DataTableViewOptions } from "./view-options"
import { useTranslation } from "@/lib/i18n"

export type DataTableToolbarConfig = {
  filterOptions?: readonly DataTableFacetedFilterProps[]
  getHeader?: (key: string) => string
  filterInput?: undefined
  globalSearch: { placeholder?: string }
  hiddenColumns?: string[]
}

interface DataTableToolbarProps<TData> extends React.HTMLAttributes<HTMLDivElement> {
  table: Table<TData>
  config: DataTableToolbarConfig
  isLoading: boolean
  clearAll?: () => void
}

export function DataTableToolbar<TData>({
  table,
  config,
  isLoading,
  children,
  clearAll,
}: DataTableToolbarProps<TData>) {
  const { t } = useTranslation()
  const { filterOptions, getHeader, globalSearch, hiddenColumns } = config
  const [searchInput, setSearchInput] = useState(table.getState().globalFilter || "")

  // Debounce function with 500ms delay
  const debouncedGlobalFilter = useDebouncedCallback((value: string) => {
    table.setGlobalFilter(value)
  }, 500)

  // Sync search input when external state changes (e.g., browser back/forward, clearAll)
  const currentGlobalFilter = (table.getState().globalFilter as string) || ""
  const [prevGlobalFilter, setPrevGlobalFilter] = useState(currentGlobalFilter)
  if (currentGlobalFilter !== prevGlobalFilter) {
    setPrevGlobalFilter(currentGlobalFilter)
    setSearchInput(currentGlobalFilter)
    debouncedGlobalFilter.cancel()
  }

  const handleSearchChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setSearchInput(value)
    debouncedGlobalFilter(value)
  }

  const isFiltered = table.getState().columnFilters?.length > 0 || !!table.getState().globalFilter

  return (
    <div className="flex h-9 items-center justify-between gap-2">
      <div className="flex h-9 min-w-0 flex-1 flex-row items-center justify-start space-x-2">
        {children}
        <div className="relative h-9 max-w-25 sm:max-w-50 md:grow-0 lg:max-w-62.5">
          <SearchIcon className="text-muted-foreground absolute top-2.5 left-2.5 size-4" />
          <Input
            placeholder={globalSearch.placeholder ?? t("table.globalSearch")}
            value={searchInput}
            onChange={handleSearchChange}
            className="bg-background h-9 w-full px-8 text-sm"
          />
          <Button
            variant="ghost"
            size="icon"
            className="absolute top-1/2 right-2 size-5 -translate-y-1/2 rounded-full"
            onClick={() => {
              setSearchInput("")
              table.resetGlobalFilter()
            }}
            hidden={!searchInput}
          >
            <XIcon className="size-3" />
          </Button>
        </div>
        <div className="flex h-9 items-center space-x-1 overflow-x-auto overflow-y-hidden sm:space-x-2">
          {!isLoading && filterOptions && filterOptions.length > 0 && (
            <DataTableFilters table={table} filterOptions={filterOptions} />
          )}
          {isFiltered && !isLoading && (
            <TooltipProvider delay={100}>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={() => {
                        setSearchInput("")
                        table.resetColumnFilters()
                        table.resetGlobalFilter()
                        clearAll?.()
                      }}
                      className="size-9 shrink-0 border-dashed"
                    >
                      <XIcon className="size-4" />
                      <span className="sr-only">{t("table.clearFilters")}</span>
                    </Button>
                  }
                />
                <TooltipContent>
                  <p>{t("table.clearFilters")}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>
      </div>
      <div className="shrink-0">
        <DataTableViewOptions
          table={table}
          getHeader={getHeader || ((key: string) => key)}
          hiddenColumns={hiddenColumns}
        />
      </div>
    </div>
  )
}
