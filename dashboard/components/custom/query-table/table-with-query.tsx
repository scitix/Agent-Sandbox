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
// Modified code
import { useQuery, useQueryClient } from "@tanstack/react-query"
import type { UseQueryOptions } from "@tanstack/react-query"
import { useAtom } from "jotai"
import { useCallback } from "react"

import {
  globalRefreshIntervalAtom,
  RefreshIntervalPreset,
} from "@/lib/queries/refresh-interval-atom"

import { DataTableCoreProps, DataTable } from "./table-without-query"

export interface QueryTableProps<TData> extends DataTableCoreProps<TData> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  queryOptions: UseQueryOptions<any, any, TData[], any>
}

export function QueryTable<TData>({ queryOptions, className, ...props }: QueryTableProps<TData>) {
  const [globalInterval, setGlobalInterval] = useAtom(globalRefreshIntervalAtom)
  const qc = useQueryClient()

  // Resolve effective refetchInterval: prefer query-level override, else use global atom (default 20s)
  const effectiveInterval =
    queryOptions.refetchInterval !== undefined
      ? (queryOptions.refetchInterval as number | false | undefined)
      : globalInterval === false
        ? false
        : (globalInterval ?? 20000)

  const {
    data = [],
    isLoading,
    isFetching,
    dataUpdatedAt,
  } = useQuery({
    ...queryOptions,
    refetchInterval: effectiveInterval,
  })

  // Stable callback — avoids creating a new function on every render
  const refetch = useCallback(
    () => qc.refetchQueries({ queryKey: queryOptions.queryKey }),
    [qc, queryOptions.queryKey],
  )

  const handleRefetchIntervalChange = useCallback(
    (interval: RefreshIntervalPreset) => {
      setGlobalInterval(interval)
    },
    [setGlobalInterval],
  )

  // isLoading is true only on the very first fetch (no data in cache yet).
  // keepPreviousData ensures subsequent revalidations keep the table visible.
  return (
    <DataTable
      {...props}
      className={className}
      data={data}
      dataUpdatedAt={dataUpdatedAt}
      refetch={refetch}
      refetchInterval={effectiveInterval}
      onRefetchIntervalChange={handleRefetchIntervalChange}
      isLoading={isLoading}
      isValidating={isFetching}
    />
  )
}
