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

import { useEffect, useState } from "react"

/**
 * Hook that returns a countdown (in seconds) until the next auto-refresh.
 *
 * @param dataUpdatedAt  - timestamp (ms) of the last successful data fetch
 * @param refetchInterval - interval (ms) between automatic refetches.
 *                          Pass `undefined` / `false` / `0` to disable the countdown.
 * @returns seconds remaining until the next refresh, clamped to [0, interval].
 *          Returns `null` when the countdown is disabled.
 */
export function useRefreshCountdown(
  dataUpdatedAt: number,
  refetchInterval: number | false | undefined,
): number | null {
  const intervalMs =
    typeof refetchInterval === "number" && refetchInterval > 0 ? refetchInterval : 0

  const [remaining, setRemaining] = useState<number | null>(() => {
    if (!intervalMs) return null
    const elapsed = Date.now() - dataUpdatedAt
    return Math.max(0, Math.ceil((intervalMs - elapsed) / 1000))
  })

  useEffect(() => {
    if (!intervalMs) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setRemaining(null)
      return
    }

    const tick = () => {
      const elapsed = Date.now() - dataUpdatedAt
      const secs = Math.max(0, Math.ceil((intervalMs - elapsed) / 1000))
      setRemaining(secs)
    }

    // Sync immediately on mount / when dataUpdatedAt changes
    tick()

    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [dataUpdatedAt, intervalMs])

  return remaining
}
