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
import { ChevronDown, ChevronRight } from "lucide-react"
import React, { Fragment, useCallback, useEffect, useMemo, useState } from "react"

import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
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
import { RefreshIntervalPreset } from "@/lib/queries/refresh-interval-atom"
import { cn } from "@/lib/utils"

import TooltipButton from "../button/tooltip-button"
import { TableSkeleton } from "../layout/table-skeleton"
import { DataTablePagination, MultipleHandler } from "./pagination"
import { DataTableToolbar, DataTableToolbarConfig } from "./toolbar"
import { useTranslation } from "@/lib/i18n"

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
      {toolbarConfig && (
        <div
          className={cn(
            "flex h-13 w-full flex-row items-center justify-between gap-2",
            isFixedLayout ? "shrink-0 px-6 py-1" : "",
          )}
        >
          <Skeleton className="h-9 w-1/4" />
          <Skeleton className="h-9 w-20" />
        </div>
      )}
      <div className={isFixedLayout ? "min-h-0 flex-1" : ""}>
        <div
          className={
            isFixedLayout
              ? "h-full overflow-hidden border-y p-0 shadow-xs"
              : "overflow-hidden rounded-md p-0 shadow-xs"
          }
        >
          <div className={isFixedLayout ? "h-full p-0" : "p-0"}>
            <Table>
              <TableHeader className={cn(isFixedLayout ? "sticky top-0 z-10 [&_tr]:border-0" : "")}>
                <TableRow
                  className={cn(
                    "bg-sidebar hover:bg-sidebar",
                    isFixedLayout &&
                      "after:border-border relative after:absolute after:-bottom-px after:left-0 after:w-full after:border-b",
                  )}
                >
                  {[...Array(columnCount)].map((_, colIndex) => (
                    <TableHead key={colIndex} className="text-muted-foreground px-6" />
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody className="bg-card">
                <TableSkeleton rows={20} columns={columnCount} />
              </TableBody>
            </Table>
          </div>
        </div>
      </div>
      {isFixedLayout && (
        <div
          className={cn(
            "flex h-13 w-full flex-row items-center justify-between gap-2 border-0 px-6 py-2",
            "shrink-0",
          )}
        >
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
  isValidating = false,
  isFixedLayout = false,
}: DataTableBasicProps<TData> & {
  isFixedLayout?: boolean
}) => {
  const { t } = useTranslation()
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())

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
            tooltip={isExpanded ? "collapse" : "expand"}
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
      ...(multipleHandlers && multipleHandlers.length > 0 ? [selectColumn] : []),
      ...columns,
    ]
  }, [columns, expandedConfig, expandedRows, idFn, multipleHandlers])

  const table = useReactTable({
    data: data,
    columns: extendedColumns,
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

  const renderTableContent = useCallback(
    () => (
      <Table>
        <TableHeader
          className={cn("[&_tr]:border-b", {
            "bg-accent sticky top-0 z-10": isFixedLayout,
          })}
        >
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className={cn("bg-sidebar hover:bg-sidebar relative")}>
              {headerGroup.headers.map((header, i) => {
                return (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn("text-muted-foreground font-normal", {
                      "pl-6": i === 0,
                      "pr-6": i === headerGroup.headers.length - 1,
                    })}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                )
              })}
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
                  className="border-0"
                >
                  {row.getVisibleCells().map((cell, i) => (
                    <TableCell
                      key={cell.id}
                      className={cn({
                        "pl-6": i === 0,
                        "pr-6": i === row.getVisibleCells().length - 1,
                      })}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
                {expandedRows.has(idFn(row.original)) && (
                  <TableRow key={`${row.id}-pods`}>
                    {expandedConfig?.renderRow(extendedColumns, row)}
                  </TableRow>
                )}
              </Fragment>
            ))
          ) : (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={columns.length}
                className={cn(
                  "text-muted-foreground/85 text-center hover:bg-transparent",
                  isFixedLayout ? "h-0 p-0" : "h-full",
                )}
              ></TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    ),
    [columns.length, expandedConfig, expandedRows, extendedColumns, idFn, isFixedLayout, table],
  )

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

        <div className={cn("min-h-0 flex-1", toolbarConfig ? "" : "mt-0")}>
          <div className="relative h-full overflow-hidden border-y p-0">
            <CardContent className="h-full p-0">
              <ScrollArea className="h-full">
                {renderTableContent()}
                <ScrollBar orientation="horizontal" />
              </ScrollArea>
              {table.getRowModel().rows?.length === 0 && (
                <div className="bg-dot-pattern text-muted-foreground/85 absolute inset-0 flex flex-col items-center justify-center">
                  <div className="flex flex-1 items-center justify-center overflow-auto">
                    <div className="border-border bg-background/80 flex w-full max-w-md flex-col items-center rounded-lg border p-8 shadow-sm backdrop-blur-sm">
                      <h2 className="text-lg tracking-tight uppercase">{t("common.noData")}</h2>
                    </div>
                  </div>
                </div>
              )}
            </CardContent>
          </div>
        </div>

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
      <Card className="overflow-hidden rounded-md p-0 shadow-xs">
        <CardContent className="p-0">{renderTableContent()}</CardContent>
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
