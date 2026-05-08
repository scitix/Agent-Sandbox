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

import {
  ColumnFiltersState,
  OnChangeFn,
  PaginationState,
  SortingState,
} from "@tanstack/react-table"
import { useMemo, useState } from "react"

export interface TableExternalState {
  columnFilters: {
    state: ColumnFiltersState
    onChange: OnChangeFn<ColumnFiltersState>
  }
  globalFilter: {
    state: string
    onChange: OnChangeFn<string>
  }
  sorting: {
    state: SortingState
    onChange: OnChangeFn<SortingState>
  }
  pagination: {
    state: PaginationState
    onChange: OnChangeFn<PaginationState>
  }
  clearAll: () => void
}

/**
 * Simple in-memory table state management hook.
 * Unlike the navix-dashboard version, this doesn't depend on URL routing state.
 */
export function useTableState(options?: { defaultPageSize?: number }): TableExternalState {
  const defaultPageSize = options?.defaultPageSize ?? 20

  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([])
  const [globalFilter, setGlobalFilter] = useState<string>("")
  const [sorting, setSorting] = useState<SortingState>([])
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: defaultPageSize,
  })

  const onColumnFiltersChange: OnChangeFn<ColumnFiltersState> = (updaterOrValue) => {
    const newFilters =
      typeof updaterOrValue === "function" ? updaterOrValue(columnFilters) : updaterOrValue
    setColumnFilters(newFilters)
    // Reset to first page when filters change
    setPagination((prev) => ({ ...prev, pageIndex: 0 }))
  }

  const onGlobalFilterChange: OnChangeFn<string> = (updaterOrValue) => {
    const newFilter =
      typeof updaterOrValue === "function" ? updaterOrValue(globalFilter) : updaterOrValue
    setGlobalFilter(newFilter)
    // Reset to first page when global filter changes
    setPagination((prev) => ({ ...prev, pageIndex: 0 }))
  }

  const onSortingChange: OnChangeFn<SortingState> = (updaterOrValue) => {
    const newSorting =
      typeof updaterOrValue === "function" ? updaterOrValue(sorting) : updaterOrValue
    setSorting(newSorting)
  }

  const onPaginationChange: OnChangeFn<PaginationState> = (updaterOrValue) => {
    const newPagination =
      typeof updaterOrValue === "function" ? updaterOrValue(pagination) : updaterOrValue
    setPagination(newPagination)
  }

  const clearAll = () => {
    setColumnFilters([])
    setGlobalFilter("")
    setSorting([])
    setPagination((prev) => ({ ...prev, pageIndex: 0 }))
  }

  return useMemo(
    () => ({
      columnFilters: {
        state: columnFilters,
        onChange: onColumnFiltersChange,
      },
      globalFilter: {
        state: globalFilter,
        onChange: onGlobalFilterChange,
      },
      sorting: {
        state: sorting,
        onChange: onSortingChange,
      },
      pagination: {
        state: pagination,
        onChange: onPaginationChange,
      },
      clearAll,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [columnFilters, globalFilter, sorting, pagination],
  )
}
