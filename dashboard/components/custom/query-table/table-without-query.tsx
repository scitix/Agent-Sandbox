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
import {
  ColumnDef,
  PaginationState,
  Row,
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from "@tanstack/react-table"
import { ChevronDown, ChevronRight, GridIcon } from "lucide-react"
import React, { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react"

import { gridPanel } from "@/components/custom-ui/surface"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"

import { TableExternalState } from "@/hooks/use-table-state"
import { useTranslation } from "@/lib/i18n"
import { RefreshIntervalPreset } from "@/lib/queries/refresh-interval-atom"
import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"
import { TableSkeleton } from "../layout/table-skeleton"
import { DataTableFacetedFilterProps } from "./faceted-filter"
import { TableFilterOptionsProvider } from "./filter-context"
import { DataTablePagination, MultipleHandler } from "./pagination"
import { TableScrollArea } from "./table-scroll-area"
import { DataTableToolbar, DataTableToolbarConfig } from "./toolbar"

const EMPTY_FILTER_OPTIONS: readonly DataTableFacetedFilterProps[] = []

export interface ExpandedConfig<TData> {
  expandable: (row: Row<TData>) => boolean
  renderRow: (columns: ColumnDef<TData>[], row: Row<TData>) => React.ReactNode
}

export interface DataTableCoreProps<TData> extends React.HTMLAttributes<HTMLDivElement> {
  idFn: (row: TData) => string
  columns: ColumnDef<TData>[]
  toolbarConfig?: DataTableToolbarConfig
  multipleHandlers?: MultipleHandler<TData>[]
  expandedConfig?: ExpandedConfig<TData>
  className?: string
  externalState?: TableExternalState
  /**
   * What to show when the caller has none of this resource at all.
   *
   * Only for a genuinely empty result — a table whose rows were filtered away
   * keeps the plain "no data", because the useful next step there is to widen
   * the filter, not to go and install an SDK. The two look identical from
   * `getRowModel()`, which is why the distinction is drawn against `data`.
   */
  emptyState?: React.ReactNode
}

export function TablePlaceHolder<TData, TValue>({
  columns,
  toolbarConfig,
  isFixedLayout = false,
  className,
}: {
  columns: ColumnDef<TData, TValue>[]
  toolbarConfig?: DataTableToolbarConfig
  isFixedLayout?: boolean
  className?: string
}) {
  const columnCount = columns.length - (toolbarConfig?.hiddenColumns?.length || 0)
  return (
    <div className={cn(isFixedLayout ? "flex h-full flex-col" : "flex flex-col gap-4", className)}>
      {/* The placeholder has to stand exactly where the loaded table will, or
          the swap shifts every edge on screen. In fixed layout that means the
          same `h-13 px-6 py-2` on the toolbar and pagination rows and the same
          panel around the grid — mirroring the live branch below, class for
          class. */}
      {toolbarConfig && (
        <div
          className={cn(
            "flex w-full flex-row items-center justify-between gap-2",
            isFixedLayout ? "h-13 shrink-0 px-6 py-2" : "h-11",
          )}
        >
          <Skeleton className="h-9 w-1/4" />
          <Skeleton className="h-9 w-20" />
        </div>
      )}
      <div className={isFixedLayout ? "min-h-0 flex-1 px-6 pb-3" : ""}>
        <Card className={cn(gridPanel, isFixedLayout && "h-full")}>
          <CardContent className={isFixedLayout ? "h-full p-0" : "p-0"}>
            <Table>
              <TableHeader className={isFixedLayout ? "sticky top-0 z-10" : ""}>
                <TableRow className="bg-accent hover:bg-accent">
                  {[...Array(columnCount)].map((_, colIndex) => (
                    <TableHead key={colIndex} className="text-muted-foreground" />
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody className="bg-card">
                <TableSkeleton rows={20} columns={columnCount} />
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
      {isFixedLayout && (
        <div className="flex h-13 w-full shrink-0 flex-row items-center justify-between gap-2 px-6 py-2">
          <Skeleton className="h-9 w-1/4" />
          <Skeleton className="h-9 w-1/8" />
        </div>
      )}
    </div>
  )
}

interface DataTableBasicProps<TData> extends DataTableCoreProps<TData> {
  data: TData[]
  dataUpdatedAt: number
  refetch?: () => Promise<unknown>
  refetchInterval?: number | false
  onRefetchIntervalChange?: (interval: RefreshIntervalPreset) => void
  isValidating?: boolean
}

export const TableContent = <TData,>({
  data,
  dataUpdatedAt,
  refetch,
  refetchInterval,
  onRefetchIntervalChange,
  idFn,
  columns,
  toolbarConfig,
  multipleHandlers,
  children,
  expandedConfig,
  className,
  externalState,
  emptyState,
  isValidating = false,
  isFixedLayout = false,
}: DataTableBasicProps<TData> & {
  isFixedLayout?: boolean
}) => {
  const { t } = useTranslation()
  // `data` is what the server returned; `getRowModel()` is what survived the
  // filters. The rich empty state belongs to the first being empty.
  const hasNothingAtAll = data.length === 0 && !!emptyState
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())
  const tableAreaRef = useRef<HTMLDivElement>(null)
  const tableRef = useRef<HTMLTableElement>(null)

  // Exposed to column headers so each can offer an in-place funnel for its
  // configured filter dimension (see column-header-filter.tsx).
  const filterOptions = toolbarConfig?.filterOptions ?? EMPTY_FILTER_OPTIONS

  const [internalPagination, setInternalPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })

  const pagination = externalState?.pagination?.state || internalPagination
  const setPagination = externalState?.pagination?.onChange || setInternalPagination

  useEffect(() => {
    if (data.length === 1 && expandedConfig) {
      setExpandedRows(new Set([idFn(data[0])]))
    }
  }, [data, expandedConfig, idFn])

  const hasSelect = !!(multipleHandlers && multipleHandlers.length > 0)

  const extendedColumns = useMemo(() => {
    if (!columns) {
      return columns
    }

    const expandColumn: ColumnDef<TData> = {
      id: "expand",
      header: "",
      cell: ({ row }) => {
        if (!expandedConfig?.expandable(row)) {
          return <></>
        }
        const isExpanded = expandedRows.has(idFn(row.original))
        return (
          <TooltipButton
            variant="ghost"
            size="icon"
            tooltip={isExpanded ? t("table.collapseDetails") : t("table.expandDetails")}
            onClick={() => {
              const newExpanded = new Set(expandedRows)
              if (isExpanded) {
                newExpanded.delete(idFn(row.original))
              } else {
                newExpanded.add(idFn(row.original))
              }
              setExpandedRows(newExpanded)
            }}
          >
            {isExpanded ? (
              <ChevronDown className="h-4 w-4" />
            ) : (
              <ChevronRight className="h-4 w-4" />
            )}
          </TooltipButton>
        )
      },
    }

    const selectColumn: ColumnDef<TData> = {
      id: "select",
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          hidden={table.getRowModel().rows.length === 0}
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
        />
      ),
      enableSorting: false,
      enableHiding: false,
    }

    return [
      ...(expandedConfig ? [expandColumn] : []),
      ...(hasSelect ? [selectColumn] : []),
      ...columns,
    ]
  }, [columns, expandedConfig, expandedRows, hasSelect, idFn, t])

  // Columns a table starts with hidden, declared on the column itself rather
  // than listed here.
  //
  // The alternative is a list of ids in every consumer, which drifts from the
  // columns it names — rename a column and the list silently stops hiding
  // anything. `initialState` rather than controlled state on purpose: this is a
  // starting point the reader is free to change from the view menu, and it must
  // not be re-imposed on every render.
  const initialColumnVisibility = useMemo(() => {
    const hidden: Record<string, boolean> = {}
    for (const c of extendedColumns) {
      const meta = c.meta as { hiddenByDefault?: boolean } | undefined
      const id = c.id ?? ("accessorKey" in c ? String(c.accessorKey) : undefined)
      if (meta?.hiddenByDefault && id) hidden[id] = false
    }
    return hidden
  }, [extendedColumns])

  const table = useReactTable({
    data: data,
    columns: extendedColumns,
    initialState: { columnVisibility: initialColumnVisibility },
    state: {
      ...(externalState?.columnFilters?.state
        ? { columnFilters: externalState.columnFilters.state }
        : {}),
      ...(externalState?.globalFilter ? { globalFilter: externalState.globalFilter.state } : {}),
      ...(externalState?.sorting?.state ? { sorting: externalState.sorting.state } : {}),
      pagination: pagination,
    },
    enableRowSelection: true,
    ...(externalState?.columnFilters?.onChange
      ? { onColumnFiltersChange: externalState.columnFilters.onChange }
      : {}),
    ...(externalState?.globalFilter?.onChange
      ? { onGlobalFilterChange: externalState.globalFilter.onChange }
      : {}),
    ...(externalState?.sorting?.onChange
      ? { onSortingChange: externalState.sorting.onChange }
      : {}),
    onPaginationChange: setPagination,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    autoResetPageIndex: externalState ? false : true,
  })

  // Fixed layout freezes the leading column(s): the first data column plus any
  // expand / multi-select utility column to its left (so the checkbox column
  // and the first data column both stay anchored).
  const pinnedCount = isFixedLayout ? (expandedConfig ? 1 : 0) + (hasSelect ? 1 : 0) + 1 : 0

  // Freeze the trailing actions column against the right edge (fixed layout).
  // The actions column is always appended last and is non-hideable, so the
  // last rendered cell is the one to pin.
  const lastColumn = extendedColumns?.[extendedColumns.length - 1]
  const pinRight = isFixedLayout && lastColumn?.id === "actions"

  // base-ui's ScrollArea only re-measures overflow when the *viewport* resizes,
  // not when the table content does — so a table that grows wider than the
  // viewport after async data arrives (or after the web font swaps in) never
  // flips `hiddenState.x` and the horizontal scrollbar stays hidden. Observe
  // the table element instead and nudge base-ui (synthetic scroll) whenever its
  // size changes, so the bar appears/disappears as content overflow demands.
  //
  // In fixed layout the same pass measures the rendered widths of the frozen
  // cells and exposes them as CSS vars: the sticky cells read the `left`/`right`
  // offsets and the horizontal scrollbar insets past the frozen blocks instead
  // of running underneath them (see globals.css).
  useEffect(() => {
    const tableEl = tableRef.current
    if (!tableEl) {
      return
    }
    const viewport = tableEl.closest<HTMLElement>('[data-slot="scroll-area-viewport"]')
    const wrap = tableAreaRef.current
    const update = () => {
      if (wrap && isFixedLayout) {
        const headerRow = tableEl.querySelector<HTMLElement>('[data-slot="table-header"] tr')
        const cells = headerRow ? (Array.from(headerRow.children) as HTMLElement[]) : []
        let acc = 0
        const offsets: number[] = []
        for (let i = 0; i < pinnedCount && i < cells.length; i++) {
          offsets.push(acc)
          acc += cells[i].offsetWidth
        }
        wrap.style.setProperty("--pinned-left-1", `${offsets[1] ?? 0}px`)
        wrap.style.setProperty("--pinned-left-2", `${offsets[2] ?? 0}px`)
        wrap.style.setProperty("--pinned-col-width", `${acc}px`)
        wrap.style.setProperty(
          "--pinned-right-col-width",
          `${pinRight ? (cells[cells.length - 1]?.offsetWidth ?? 0) : 0}px`,
        )
        // The sticky header sits inside the same scroll viewport as the
        // scrollbars, so the vertical bar would otherwise run up underneath it.
        // Expose the header height; the CSS insets the vertical scrollbar's top
        // by this amount so it spans only the scrollable body (see globals.css).
        const header = tableEl.querySelector<HTMLElement>('[data-slot="table-header"]')
        wrap.style.setProperty("--table-header-height", `${header?.offsetHeight ?? 0}px`)
      }
      viewport?.dispatchEvent(new Event("scroll"))
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(tableEl)
    return () => observer.disconnect()
  }, [isFixedLayout, pinnedCount, pinRight, extendedColumns.length])

  const renderTableContent = useCallback(
    () => (
      // `overflow-x-visible` hands horizontal scrolling to the enclosing
      // ScrollArea viewport so the bottom scrollbar pins to the visible area.
      // In fixed layout `pinned-cols` additionally freezes the leading column(s)
      // (`sticky left`); auto-height tables scroll every column (see globals.css).
      <TableFilterOptionsProvider value={filterOptions}>
        {/* Plain <table> (not the ui <Table> wrapper, whose container forces
            `overflow-x-auto`): the enclosing TableScrollArea viewport owns
            horizontal scrolling so the bottom scrollbar pins to the visible
            area and the pinned cells stick relative to the viewport. */}
        <table
          ref={tableRef}
          data-slot="table"
          className={cn(
            "w-full caption-bottom text-sm",
            isFixedLayout && "pinned-cols",
            pinRight && "pinned-right-col",
          )}
          data-pinned-count={isFixedLayout ? pinnedCount : undefined}
        >
          <TableHeader
            className={cn("[&_tr]:border-0", {
              "sticky top-0 z-20": isFixedLayout,
            })}
          >
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow
                key={headerGroup.id}
                className={cn(
                  "bg-accent hover:bg-accent relative",
                  "after:border-border after:absolute after:-bottom-px after:left-0 after:w-full after:border-b",
                )}
              >
                {headerGroup.headers.map((header, i) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn("text-muted-foreground font-normal", {
                      // The grid's inset from its own panel edge. Smaller than
                      // the page gutter the panel sits in, because the two add
                      // up: at pl-6 the first column would start 48px into the
                      // page, visibly deeper than the toolbar above it.
                      "pl-4": i === 0,
                      "pr-4": i === headerGroup.headers.length - 1,
                    })}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody className="bg-card">
            {table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map((row) => (
                <Fragment key={row.id}>
                  <TableRow
                    key={row.id}
                    data-state={row.getIsSelected() && "selected"}
                    className="hover:bg-card border-0"
                  >
                    {row.getVisibleCells().map((cell, i) => (
                      <TableCell
                        key={cell.id}
                        className={cn({
                          "pl-4": i === 0,
                          "pr-4": i === row.getVisibleCells().length - 1,
                        })}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </TableCell>
                    ))}
                  </TableRow>
                  {expandedRows.has(idFn(row.original)) && (
                    <TableRow key={`${row.id}-pods`} className="hover:bg-card border-0">
                      {expandedConfig?.renderRow(extendedColumns, row)}
                    </TableRow>
                  )}
                </Fragment>
              ))
            ) : isFixedLayout ? null : (
              // Fixed layout renders its own centered overlay below; only the
              // auto-height table shows the empty state inline (avoids rendering
              // "no data" twice — see the overlay in the fixed layout branch).
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className="text-muted-foreground/85 h-full text-center hover:bg-transparent"
                >
                  {hasNothingAtAll ? (
                    <div className="py-10">{emptyState}</div>
                  ) : (
                    <div className="flex flex-col items-center justify-center py-16">
                      <div className="bg-muted mb-4 rounded-full p-3">
                        <GridIcon className="h-6 w-6" />
                      </div>
                      <p className="text-sm select-none">{t("table.noData")}</p>
                    </div>
                  )}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </table>
      </TableFilterOptionsProvider>
    ),
    [
      columns.length,
      emptyState,
      expandedConfig,
      expandedRows,
      extendedColumns,
      filterOptions,
      hasNothingAtAll,
      idFn,
      isFixedLayout,
      pinnedCount,
      pinRight,
      table,
      t,
    ],
  )

  // Fixed layout mode
  if (isFixedLayout) {
    return (
      <div className={cn("flex h-full flex-col", className)}>
        {toolbarConfig && (
          <div className="h-13 shrink-0 overflow-hidden px-6 py-2">
            <DataTableToolbar
              table={table}
              config={toolbarConfig}
              isLoading={false}
              clearAll={externalState?.clearAll}
            >
              {children}
            </DataTableToolbar>
          </div>
        )}

        {/* Scrollable table area, inset to the page gutter so the grid reads as
            a panel on the canvas rather than as the page's full width.

            The gutter goes on THIS wrapper, outside the scroll area. Pinned
            columns are `position: sticky; left: 0` against the scroll viewport
            (app/globals.css), so padding the scroll container itself would move
            the origin they freeze at and leave them floating mid-cell. */}
        <div
          ref={tableAreaRef}
          className={cn("min-h-0 flex-1 px-6 pb-3", toolbarConfig ? "" : "mt-0")}
        >
          <div className={cn(gridPanel, "relative h-full")}>
            <TableScrollArea orientation="both" className="table-scroll-area h-full">
              {renderTableContent()}
            </TableScrollArea>
            {table.getRowModel().rows?.length === 0 &&
              (hasNothingAtAll ? (
                <div className="bg-card absolute inset-0 overflow-y-auto">{emptyState}</div>
              ) : (
                <div className="bg-card text-muted-foreground/85 absolute inset-0 flex flex-col items-center justify-center">
                  <div className="bg-muted mb-4 rounded-full p-3">
                    <GridIcon className="h-6 w-6" />
                  </div>
                  <p className="text-sm select-none">{t("table.noData")}</p>
                </div>
              ))}
          </div>
        </div>

        {/* Pagination pinned to the bottom */}
        <div className="h-13 shrink-0 overflow-hidden px-6 py-2">
          <DataTablePagination
            table={table}
            refetch={refetch}
            dataUpdatedAt={dataUpdatedAt}
            refetchInterval={refetchInterval}
            onRefetchIntervalChange={onRefetchIntervalChange}
            multipleHandlers={multipleHandlers}
            columns={columns as ColumnDef<TData, unknown>[]}
            isValidating={isValidating}
          />
        </div>
      </div>
    )
  }

  // Default auto-expand mode
  return (
    <div className={cn("flex flex-col gap-4", className)}>
      {toolbarConfig && (
        <DataTableToolbar
          table={table}
          config={toolbarConfig}
          isLoading={false}
          clearAll={externalState?.clearAll}
        >
          {children}
        </DataTableToolbar>
      )}
      {/* No gutter here: the pages that use this mode (templates, images,
          datasets, scaling groups) already lay themselves out inside `px-6`,
          and adding another would nest one inset in the other. */}
      <Card className={gridPanel}>
        <CardContent className="p-0">
          <TableScrollArea orientation="both" className="table-scroll-area">
            {renderTableContent()}
          </TableScrollArea>
        </CardContent>
      </Card>
      <DataTablePagination
        table={table}
        refetch={refetch}
        dataUpdatedAt={dataUpdatedAt}
        refetchInterval={refetchInterval}
        onRefetchIntervalChange={onRefetchIntervalChange}
        multipleHandlers={multipleHandlers}
        columns={columns as ColumnDef<TData, unknown>[]}
        isValidating={isValidating}
      />
    </div>
  )
}

export interface DataTableProps<TData> extends DataTableBasicProps<TData> {
  isLoading: boolean
  isValidating?: boolean
}

export function DataTable<TData>({ isLoading, isValidating, ...props }: DataTableProps<TData>) {
  const isFixedLayout = useMemo(
    () => props.className?.includes("table-layout-fixed"),
    [props.className],
  )

  if (isLoading) {
    return <TablePlaceHolder isFixedLayout={isFixedLayout} {...props} />
  }
  return <TableContent isFixedLayout={isFixedLayout} isValidating={isValidating} {...props} />
}
