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
import { Table } from "@tanstack/react-table"
import { Settings2Icon } from "lucide-react"
import { useEffect, useRef } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTranslation } from "@/lib/i18n"

interface DataTableViewOptionsProps<TData> {
  table: Table<TData>
  getHeader: (key: string) => string
  hiddenColumns?: string[]
}

export function DataTableViewOptions<TData>({
  table,
  getHeader,
  hiddenColumns,
}: DataTableViewOptionsProps<TData>) {
  const { t } = useTranslation()
  // Apply default hidden columns only on initial mount
  const initializedRef = useRef(false)
  useEffect(() => {
    if (initializedRef.current) return
    if (hiddenColumns && hiddenColumns.length > 0) {
      hiddenColumns.forEach((columnId) => {
        const column = table.getColumn(columnId)
        if (column && column.getIsVisible()) {
          column.toggleVisibility(false)
        }
      })
      initializedRef.current = true
    }
  }, [table, hiddenColumns])

  return (
    <DropdownMenu>
      <TooltipProvider delay={100}>
        <Tooltip>
          <TooltipTrigger
            render={
              <DropdownMenuTrigger
                render={
                  <Button variant="outline" size="sm" className="ml-auto flex h-9 font-normal">
                    <Settings2Icon className="size-4" />
                    <span className="ml-1 hidden text-xs sm:inline">{t("table.visibility")}</span>
                    <span className="sr-only">{t("table.visibility")}</span>
                  </Button>
                }
              />
            }
          />
          <TooltipContent>
            <p>{t("table.selectColumnsToDisplay")}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <DropdownMenuContent align="end" className="w-62.5">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t("table.visibility")}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {table
            .getAllColumns()
            .filter((column) => typeof column.accessorFn !== "undefined" && column.getCanHide())
            .map((column) => {
              return (
                <DropdownMenuCheckboxItem
                  key={column.id}
                  className="capitalize"
                  checked={column.getIsVisible()}
                  onCheckedChange={(value) => column.toggleVisibility(!!value)}
                >
                  {getHeader(column.id)}
                </DropdownMenuCheckboxItem>
              )
            })}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
