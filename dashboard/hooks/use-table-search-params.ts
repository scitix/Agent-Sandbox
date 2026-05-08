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

import { useQueryStates, parseAsArrayOf, parseAsString, parseAsInteger, parseAsBoolean } from "nuqs"
import {
  type ColumnFiltersState,
  type OnChangeFn,
  type PaginationState,
  type SortingState,
} from "@tanstack/react-table"
import { useMemo, useCallback, useRef } from "react"
import { type TableExternalState } from "./use-table-state"

export type FilterColumnType = "string" | "number-range" | "faceted"

export interface FilterColumnDef {
  id: string
  type: FilterColumnType
}

/**
 * Hook that persists table state (filters, sorting, pagination) to URL search params via nuqs.
 *
 * All filter values are serialized as string arrays in the URL for uniformity:
 * - String filter: [string] → ?name=abc
 * - Number range: [min, max] → ?cpu=1,4
 * - Faceted filter: [val1, val2, ...] → ?status=running,starting
 *
 * @param filterColumns - Stable array of column filter definitions (should NOT change between renders)
 * @param options.defaultPageSize - Default page size (default: 20)
 */
export function useTableSearchParams(
  filterColumns: FilterColumnDef[],
  options?: { defaultPageSize?: number },
): TableExternalState {
  const defaultPageSize = options?.defaultPageSize ?? 15

  // Capture initial filterColumns to keep parsers stable across renders
  const filterColumnsRef = useRef(filterColumns)

  // Build nuqs parser map — must be stable (same keys) across renders
  const parsers = useMemo(() => {
    const p: Record<
      string,
      | ReturnType<typeof parseAsString.withDefault>
      | ReturnType<typeof parseAsInteger.withDefault>
      | ReturnType<typeof parseAsBoolean.withDefault>
      | ReturnType<ReturnType<typeof parseAsArrayOf<string>>["withDefault"]>
    > = {
      q: parseAsString.withDefault(""),
      page: parseAsInteger.withDefault(0),
      size: parseAsInteger.withDefault(defaultPageSize),
      sort: parseAsString.withDefault(""),
      desc: parseAsBoolean.withDefault(false),
    }
    for (const col of filterColumnsRef.current) {
      // All filter columns use parseAsArrayOf(parseAsString) for uniform serialization
      p[col.id] = parseAsArrayOf(parseAsString).withDefault([])
    }
    return p
  }, [defaultPageSize])

  const [state, setState] = useQueryStates(parsers)

  // Extract primitive values for stable dependency tracking
  const globalFilter: string = (state as Record<string, unknown>).q as string
  const sortCol = (state as Record<string, unknown>).sort as string
  const sortDesc = (state as Record<string, unknown>).desc as boolean
  const pageIndex = (state as Record<string, unknown>).page as number
  const pageSize = (state as Record<string, unknown>).size as number

  // ── Column Filters: URL → tanstack-table ──────────────────────────────

  const columnFilters: ColumnFiltersState = useMemo(() => {
    const filters: ColumnFiltersState = []
    for (const col of filterColumnsRef.current) {
      const values = (state as Record<string, unknown>)[col.id] as string[] | undefined
      if (!values || values.length === 0) continue

      switch (col.type) {
        case "string": {
          // URL [string] → tanstack-table string
          if (values[0]) {
            filters.push({ id: col.id, value: values[0] })
          }
          break
        }
        case "number-range": {
          // URL ["min", "max"] → tanstack-table [number?, number?]
          const rawMin = values[0] ? parseFloat(values[0]) : undefined
          const rawMax = values[1] ? parseFloat(values[1]) : undefined
          const min = rawMin !== undefined && !isNaN(rawMin) ? rawMin : undefined
          const max = rawMax !== undefined && !isNaN(rawMax) ? rawMax : undefined
          if (min !== undefined || max !== undefined) {
            filters.push({ id: col.id, value: [min, max] })
          }
          break
        }
        case "faceted": {
          // URL string[] → tanstack-table string[]
          filters.push({ id: col.id, value: values })
          break
        }
      }
    }
    return filters
  }, [state])

  // ── Column Filters: tanstack-table → URL ──────────────────────────────

  const onColumnFiltersChange: OnChangeFn<ColumnFiltersState> = useCallback(
    (updaterOrValue) => {
      const newFilters =
        typeof updaterOrValue === "function" ? updaterOrValue(columnFilters) : updaterOrValue

      const update: Record<string, unknown> = {}

      // Clear all filter columns first
      for (const col of filterColumnsRef.current) {
        update[col.id] = null
      }

      // Set new values
      for (const filter of newFilters) {
        const col = filterColumnsRef.current.find((c) => c.id === filter.id)
        if (!col) continue

        switch (col.type) {
          case "string": {
            // tanstack-table string → URL [string]
            const val = filter.value as string
            update[col.id] = val ? [val] : null
            break
          }
          case "number-range": {
            // tanstack-table [number?, number?] → URL ["min", "max"]
            const [min, max] = filter.value as [number?, number?]
            if (min !== undefined && max !== undefined) {
              update[col.id] = [String(min), String(max)]
            } else if (min !== undefined) {
              update[col.id] = [String(min)]
            } else if (max !== undefined) {
              update[col.id] = ["", String(max)]
            }
            break
          }
          case "faceted": {
            // tanstack-table string[] → URL string[]
            const val = filter.value as string[] | null
            update[col.id] = val && val.length > 0 ? val : null
            break
          }
        }
      }

      // Reset page when filters change
      update.page = null
      setState(update as Parameters<typeof setState>[0])
    },
    [columnFilters, setState],
  )

  // ── Global Filter ─────────────────────────────────────────────────────

  const onGlobalFilterChange: OnChangeFn<string> = useCallback(
    (updaterOrValue) => {
      const newValue =
        typeof updaterOrValue === "function" ? updaterOrValue(globalFilter) : updaterOrValue
      setState({ q: newValue || null, page: null } as Parameters<typeof setState>[0])
    },
    [globalFilter, setState],
  )

  // ── Sorting (single column) ───────────────────────────────────────────

  const sorting: SortingState = useMemo(() => {
    if (!sortCol) return []
    return [{ id: sortCol, desc: sortDesc ?? false }]
  }, [sortCol, sortDesc])

  const onSortingChange: OnChangeFn<SortingState> = useCallback(
    (updaterOrValue) => {
      const newSorting =
        typeof updaterOrValue === "function" ? updaterOrValue(sorting) : updaterOrValue
      if (newSorting.length === 0) {
        setState({ sort: null, desc: null } as Parameters<typeof setState>[0])
      } else {
        setState({
          sort: newSorting[0].id || null,
          desc: newSorting[0].desc || null,
        } as Parameters<typeof setState>[0])
      }
    },
    [sorting, setState],
  )

  // ── Pagination ────────────────────────────────────────────────────────

  const pagination: PaginationState = useMemo(
    () => ({
      pageIndex: pageIndex ?? 0,
      pageSize: pageSize ?? defaultPageSize,
    }),
    [pageIndex, pageSize, defaultPageSize],
  )

  const onPaginationChange: OnChangeFn<PaginationState> = useCallback(
    (updaterOrValue) => {
      const newPagination =
        typeof updaterOrValue === "function" ? updaterOrValue(pagination) : updaterOrValue
      setState({
        page: newPagination.pageIndex || null,
        size: newPagination.pageSize !== defaultPageSize ? newPagination.pageSize : null,
      } as Parameters<typeof setState>[0])
    },
    [pagination, defaultPageSize, setState],
  )

  // ── Clear All ─────────────────────────────────────────────────────────

  const clearAll = useCallback(() => {
    const update: Record<string, null> = {
      q: null,
      page: null,
      sort: null,
      desc: null,
      size: null,
    }
    for (const col of filterColumnsRef.current) {
      update[col.id] = null
    }
    setState(update as Parameters<typeof setState>[0])
  }, [setState])

  // ── Return TableExternalState ─────────────────────────────────────────

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
