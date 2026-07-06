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

// i18n-processed-v1.1.0 (no translatable strings)
import { createContext, useContext, useMemo } from "react"

import { DataTableFacetedFilterProps } from "./faceted-filter"

const EMPTY: readonly DataTableFacetedFilterProps[] = []

const TableFilterOptionsContext = createContext<readonly DataTableFacetedFilterProps[]>(EMPTY)

/**
 * Exposes the table's faceted filter options to column headers rendered via
 * flexRender, so a header can offer an in-place funnel for the dimension that
 * matches its column. Headers without a Provider (or without a matching
 * option) simply render no funnel.
 */
export function TableFilterOptionsProvider({
  value,
  children,
}: {
  value: readonly DataTableFacetedFilterProps[]
  children: React.ReactNode
}) {
  return (
    <TableFilterOptionsContext.Provider value={value}>
      {children}
    </TableFilterOptionsContext.Provider>
  )
}

/** Resolve the filter option configured for a column id, if any. */
export function useColumnFilterOption(columnId: string): DataTableFacetedFilterProps | undefined {
  const options = useContext(TableFilterOptionsContext)
  return useMemo(() => options.find((option) => option.columnKey === columnId), [options, columnId])
}
