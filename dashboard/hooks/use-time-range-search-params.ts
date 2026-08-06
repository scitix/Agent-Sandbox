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

import { useCallback, useMemo } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { isTimeRangePreset, type TimeRangePreset, type TimeRangeValue } from "@/lib/types/prometheus"

/**
 * Persists a chart TimeRangeValue to the `range` (preset) or `from`/`to`
 * (absolute) URL search params, so a shared link reproduces the same window.
 */
export function useTimeRangeSearchParams(
  defaultPreset: TimeRangePreset = "1h",
): readonly [TimeRangeValue, (next: TimeRangeValue) => void] {
  const router = useRouter()
  const searchParams = useSearchParams()

  const value = useMemo<TimeRangeValue>(() => {
    const range = searchParams.get("range")
    const from = searchParams.get("from")
    const to = searchParams.get("to")
    if (from && to) return { type: "absolute", start: Number(from), end: Number(to) }
    if (isTimeRangePreset(range)) return { type: "preset", preset: range }
    return { type: "preset", preset: defaultPreset }
  }, [searchParams, defaultPreset])

  const setValue = useCallback(
    (next: TimeRangeValue) => {
      const params = new URLSearchParams(searchParams)
      if (next.type === "preset") {
        params.set("range", next.preset)
        params.delete("from")
        params.delete("to")
      } else {
        params.set("from", String(next.start))
        params.set("to", String(next.end))
        params.delete("range")
      }
      router.replace(`?${params}`, { scroll: false })
    },
    [router, searchParams],
  )

  return [value, setValue] as const
}
