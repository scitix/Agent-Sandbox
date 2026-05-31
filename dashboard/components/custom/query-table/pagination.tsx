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

import { ColumnDef, Row, Table } from "@tanstack/react-table"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsUpDown,
  DownloadIcon,
  RefreshCcw,
} from "lucide-react"
import React from "react"
import { toast } from "sonner"

import {
  Pagination,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
} from "@/components/ui/pagination"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

import TooltipButton from "@/components/custom/button/tooltip-button"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"

import { useRefreshCountdown } from "@/hooks/use-refresh-countdown"
import { exportToCSV } from "@/lib/csv"
import { useTranslation } from "@/lib/i18n"
import {
  REFRESH_INTERVAL_PRESETS,
  RefreshIntervalPreset,
} from "@/lib/queries/refresh-interval-atom"
import { cn } from "@/lib/utils"

const DOTS = "..."

function usePagination({
  currentPage,
  totalPages,
  siblingCount = 1,
}: {
  currentPage: number
  totalPages: number
  siblingCount?: number
}) {
  return React.useMemo(() => {
    const totalPageNumbers = siblingCount + 5 // Start, end, current, and 2 siblings

    // Case 1: If the number of pages is less than the page numbers we want to show
    if (totalPageNumbers >= totalPages) {
      return Array.from({ length: totalPages }, (_, i) => i + 1)
    }

    const leftSiblingIndex = Math.max(currentPage - siblingCount, 1)
    const rightSiblingIndex = Math.min(currentPage + siblingCount, totalPages)

    const shouldShowLeftDots = leftSiblingIndex > 2
    const shouldShowRightDots = rightSiblingIndex < totalPages - 2

    const firstPageIndex = 1
    const lastPageIndex = totalPages

    // Case 2: No left dots to show, but rights dots to be shown
    if (!shouldShowLeftDots && shouldShowRightDots) {
      const leftItemCount = 3 + 2 * siblingCount
      const leftRange = Array.from({ length: leftItemCount }, (_, i) => i + 1)

      return [...leftRange, DOTS, totalPages]
    }

    // Case 3: No right dots to show, but left dots to be shown
    if (shouldShowLeftDots && !shouldShowRightDots) {
      const rightItemCount = 3 + 2 * siblingCount
      const rightRange = Array.from(
        { length: rightItemCount },
        (_, i) => totalPages - rightItemCount + i + 1,
      )

      return [firstPageIndex, DOTS, ...rightRange]
    }

    // Case 4: Both left and right dots to be shown
    if (shouldShowLeftDots && shouldShowRightDots) {
      const middleRange = Array.from(
        { length: rightSiblingIndex - leftSiblingIndex + 1 },
        (_, i) => leftSiblingIndex + i,
      )

      return [firstPageIndex, DOTS, ...middleRange, DOTS, lastPageIndex]
    }

    return []
  }, [currentPage, totalPages, siblingCount])
}

export interface MultipleHandler<TData> {
  title: (rows: Row<TData>[]) => string
  description: (rows: Row<TData>[]) => React.ReactNode
  handleSubmit: (rows: Row<TData>[]) => void
  icon: React.ReactNode
  isDanger?: boolean
}

interface DataTablePaginationProps<TData> {
  dataUpdatedAt: number
  refetchInterval?: number | false
  refetch?: () => Promise<unknown>
  onRefetchIntervalChange?: (interval: RefreshIntervalPreset) => void
  table: Table<TData>
  multipleHandlers?: MultipleHandler<TData>[]
  columns?: ColumnDef<TData, unknown>[]
  filename?: string
  isValidating?: boolean
}

export function DataTablePagination<TData>({
  dataUpdatedAt,
  refetchInterval,
  refetch,
  onRefetchIntervalChange,
  table,
  multipleHandlers,
  columns,
  filename = "export",
  isValidating = false,
}: DataTablePaginationProps<TData>) {
  const { t } = useTranslation()
  const countdown = useRefreshCountdown(dataUpdatedAt, refetchInterval)
  const currentPage = table.getState().pagination.pageIndex + 1
  const totalPages = table.getPageCount()

  const paginationRange = usePagination({
    currentPage,
    totalPages,
    siblingCount: 1,
  })

  const onPageChange = (page: number) => {
    table.setPageIndex(page - 1)
  }

  const handleExportCSV = () => {
    if (!columns) {
      console.warn("No columns provided for CSV export")
      return
    }

    const filteredRows = table.getFilteredRowModel().rows
    exportToCSV(filteredRows, columns, filename)
  }

  // Convert refetchInterval to a Select value string
  const intervalSelectValue =
    refetchInterval === false
      ? "false"
      : refetchInterval != null
        ? String(refetchInterval)
        : "false"

  const handleIntervalChange = (value: string | null) => {
    if (!onRefetchIntervalChange || value === null) return
    if (value === "false") {
      onRefetchIntervalChange(false)
    } else {
      onRefetchIntervalChange(Number(value) as RefreshIntervalPreset)
    }
  }

  return (
    <div className="flex w-full items-center justify-between">
      <div className="flex flex-row items-center space-x-1.5 text-xs">
        {table.getFilteredSelectedRowModel().rows.length > 0 &&
          multipleHandlers &&
          multipleHandlers?.length > 0 &&
          multipleHandlers.map((multipleHandler, index) => (
            <AlertDialog key={index}>
              <AlertDialogTrigger
                render={
                  <TooltipButton
                    variant="outline"
                    size="icon"
                    className="size-9"
                    tooltip={multipleHandler.title(table.getFilteredSelectedRowModel().rows)}
                  >
                    {multipleHandler.icon}
                  </TooltipButton>
                }
              />
              <AlertDialogContent className="flex max-h-[80vh] flex-col overflow-hidden">
                <AlertDialogHeader className="shrink-0">
                  <AlertDialogTitle>
                    {multipleHandler.title(table.getFilteredSelectedRowModel().rows)}
                  </AlertDialogTitle>
                  {/* Render as a div: handler descriptions contain block-level
                      content (lists of selected rows), which is invalid nested
                      inside the default <p> element. */}
                  <AlertDialogDescription className="overflow-auto" render={<div />}>
                    {multipleHandler.description(table.getFilteredSelectedRowModel().rows)}
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter className="shrink-0">
                  <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                  <AlertDialogAction
                    variant={multipleHandler.isDanger ? "destructive" : "default"}
                    onClick={() => {
                      multipleHandler.handleSubmit(table.getFilteredSelectedRowModel().rows)
                      table.resetRowSelection()
                    }}
                  >
                    {t("table.confirm")}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          ))}
        {!!refetch && (
          <TooltipButton
            variant="outline"
            size="icon"
            className="size-9"
            tooltip={t("table.refresh")}
            onClick={() => refetch().then(() => toast.success(t("table.refreshed")))}
          >
            <RefreshCcw className={cn("h-3.5 w-3.5", isValidating && "animate-spin")} />
          </TooltipButton>
        )}
        <TooltipButton
          variant="outline"
          size="icon"
          className="size-9"
          tooltip={t("table.exportCsv")}
          onClick={handleExportCSV}
          hidden
        >
          <DownloadIcon className="h-3.5 w-3.5" />
        </TooltipButton>
        <Select
          value={`${table.getState().pagination.pageSize}`}
          onValueChange={(value) => {
            table.setPageSize(Number(value))
          }}
        >
          <SelectTrigger className="bg-background h-9 w-30 pr-2 pl-3 text-xs">
            <SelectValue placeholder={table.getState().pagination.pageSize} />
          </SelectTrigger>
          <SelectContent side="top">
            {[10, 15, 20, 50, 100, 500].map((pageSize) => (
              <SelectItem key={pageSize} value={`${pageSize}`}>
                {t("table.perPage", { count: String(pageSize) })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="text-muted-foreground hidden items-center truncate pl-1.5 font-normal md:flex">
          <span>
            {table.getFilteredSelectedRowModel().rows.length === 0 ? (
              <>
                {t("table.total", {
                  count: String(table.getFilteredRowModel().rows.length),
                })}
              </>
            ) : (
              <>
                {t("table.selectedCount", {
                  selected: String(table.getFilteredSelectedRowModel().rows.length),
                  total: String(table.getFilteredRowModel().rows.length),
                })}
              </>
            )}
            {", "}
            {countdown !== null ? (
              (() => {
                // Split "Auto-refresh {{countdown}}s" into prefix/suffix around the number
                const parts = t("table.autoRefresh", { countdown: "\x00" }).split("\x00")
                return (
                  <>
                    {parts[0]}
                    <span className="font-mono tabular-nums">{countdown}</span>
                    {parts[1]}
                  </>
                )
              })()
            ) : (
              <>
                {refetchInterval === false
                  ? t("table.autoRefreshOff")
                  : (() => {
                      const timeStr = new Date(dataUpdatedAt).toLocaleString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                        second: "2-digit",
                      })
                      const parts = t("table.updatedAt", { time: "\x00" }).split("\x00")
                      return (
                        <>
                          {parts[0]}
                          <span className="font-mono tabular-nums">{timeStr}</span>
                          {parts[1]}
                        </>
                      )
                    })()}
              </>
            )}
          </span>
          {!!onRefetchIntervalChange && (
            <Select value={intervalSelectValue} onValueChange={handleIntervalChange}>
              <SelectTrigger className="ml-1 h-auto w-auto border-0 p-0 shadow-none focus:ring-0 [&>svg:last-child]:hidden">
                <ChevronsUpDown className="size-3 cursor-pointer opacity-40 hover:opacity-100" />
              </SelectTrigger>
              <SelectContent side="top" align="start">
                {REFRESH_INTERVAL_PRESETS.map((preset) => (
                  <SelectItem key={String(preset)} value={String(preset)}>
                    {preset === false
                      ? t("table.autoRefreshOff")
                      : t("table.autoRefreshInterval", {
                          interval: `${preset / 1000}s`,
                        })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
      </div>
      <div className="flex items-center space-x-6">
        <div className="flex items-center space-x-2">
          <Pagination>
            <PaginationContent>
              {/* Previous button */}
              <PaginationItem>
                <PaginationLink
                  aria-label={t("table.goToPreviousPage")}
                  size="icon"
                  className={
                    currentPage <= 1
                      ? "pointer-events-none cursor-not-allowed opacity-50"
                      : "cursor-pointer"
                  }
                  onClick={() => currentPage > 1 && onPageChange(currentPage - 1)}
                >
                  <ChevronLeftIcon className="size-4" />
                </PaginationLink>
              </PaginationItem>

              {/* Page numbers */}
              {paginationRange.map((pageNumber, index) => {
                if (pageNumber === DOTS) {
                  return (
                    <PaginationItem
                      key={`dots-${index}`}
                      className="text-muted-foreground hidden sm:flex"
                    >
                      <PaginationEllipsis />
                    </PaginationItem>
                  )
                }

                return (
                  <PaginationItem
                    key={pageNumber}
                    className={cn("hidden sm:flex", {
                      flex: pageNumber === currentPage,
                    })}
                  >
                    <PaginationLink
                      onClick={() => onPageChange(pageNumber as number)}
                      isActive={pageNumber === currentPage}
                      className="cursor-pointer select-none"
                    >
                      {pageNumber}
                    </PaginationLink>
                  </PaginationItem>
                )
              })}

              {/* Next button */}
              <PaginationItem>
                <PaginationLink
                  aria-label={t("table.goToNextPage")}
                  size="icon"
                  className={
                    currentPage >= totalPages
                      ? "pointer-events-none cursor-not-allowed opacity-50"
                      : "cursor-pointer"
                  }
                  onClick={() => currentPage < totalPages && onPageChange(currentPage + 1)}
                >
                  <ChevronRightIcon className="size-4" />
                </PaginationLink>
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      </div>
    </div>
  )
}
