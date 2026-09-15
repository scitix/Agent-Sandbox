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

import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useMemo, useState } from "react"

import type { LogEntry, LogQueryResult } from "@/components/logs/types"

export interface LogSearchState {
  entries: LogEntry[]
  isLoading: boolean
  error?: string
  truncated: boolean
  hasSearched: boolean
}

/**
 * Drives a log view. Imperative on purpose — a Search button triggers a query,
 * not a reactive dependency — but backed by TanStack Query so the RESULT is
 * cached under the exact query parameters.
 *
 * That cache is not an optimization, it is what removes visible jitter: the app
 * layout re-parents the routed page when the assistant panel opens (see
 * `app-layout.tsx`), which remounts this hook. With the result in the query cache
 * a remount re-renders the same lines synchronously instead of clearing the view
 * and going back to the network.
 *
 * The fetcher is generic so one state machine backs both the component-logs page
 * (queryComponentLogs) and the pod detail Logs tab (queryPodLogs); pass a
 * module-level fetcher so its identity is stable across renders.
 */
export function useLogSearch<TParams>(
  fetcher: (params: TParams) => Promise<LogQueryResult>,
  /** Namespaces this view's cache entries (e.g. 'component-logs' / 'pod-logs'). */
  cacheKey: string,
) {
  const client = useQueryClient()
  const [params, setParams] = useState<TParams | null>(null)
  const queryKey = useMemo(() => [cacheKey, params], [cacheKey, params])

  const query = useQuery({
    queryKey,
    queryFn: () => fetcher(params as TParams),
    enabled: params !== null,
    // A closed time window is immutable history, and even a relative one is only
    // expected to move when the user asks. So nothing refetches on its own —
    // remount, refocus and reconnect all serve the cache; `refresh()` is the one
    // path back to the network.
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
    // Keep the previous lines on screen while the next query runs, so a re-query
    // dims the view instead of blanking it.
    placeholderData: keepPreviousData,
  })

  /** Query these parameters, serving a cached result when there is one. */
  const search = useCallback((next: TParams) => {
    setParams((prev) => (sameParams(prev, next) ? prev : next))
  }, [])

  /** Query these parameters and go to the network even if they are unchanged. */
  const refresh = useCallback(
    (next: TParams) => {
      setParams((prev) => (sameParams(prev, next) ? prev : next))
      void client.invalidateQueries({ queryKey: [cacheKey, next], exact: true })
    },
    [client, cacheKey],
  )

  const data = query.data
  return {
    entries: data?.entries ?? [],
    // Reflects a refetch too, so the overlay tracks any in-flight query.
    isLoading: query.isFetching,
    error: query.error
      ? query.error instanceof Error
        ? query.error.message
        : String(query.error)
      : undefined,
    truncated: data?.truncated ?? false,
    hasSearched: params !== null,
    search,
    refresh,
  }
}

// Params are flat, JSON-safe objects (the same shape the query key hashes), so a
// stringify compare is enough to avoid a pointless state update.
function sameParams<TParams>(a: TParams | null, b: TParams): boolean {
  return a !== null && JSON.stringify(a) === JSON.stringify(b)
}
