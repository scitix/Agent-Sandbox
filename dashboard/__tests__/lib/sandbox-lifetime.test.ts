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

import { describe, it, expect } from "vitest"
import { sandboxLifetimeBounds } from "@/lib/prometheus/sandbox-lifetime"

// All ISO timestamps below are in UTC. `Math.floor(Date.parse(iso) / 1000)`
// yields the expected unix seconds regardless of the host timezone.
const CLAIMED_AT = "2026-04-20T17:07:24Z"
const STARTED_AT = "2026-04-20T17:07:25Z"
const TERMINATED_AT = "2026-04-20T21:32:51Z"
const RECYCLED_AT = "2026-04-20T21:38:06Z"

const sec = (iso: string) => Math.floor(Date.parse(iso) / 1000)

describe("sandboxLifetimeBounds", () => {
  it("prefers startedAt over claimedAt and recycledAt over terminatedAt (terminal sandbox)", () => {
    // Mirrors the eval-gold failed sandbox from the bug report: the old chart
    // used claimedAt → terminatedAt which undercounted the recycle tail and
    // overcounted the startup delay. The fix must use startedAt → recycledAt.
    const bounds = sandboxLifetimeBounds({
      claimedAt: CLAIMED_AT,
      startedAt: STARTED_AT,
      terminatedAt: TERMINATED_AT,
      recycledAt: RECYCLED_AT,
    })
    expect(bounds.start).toBe(sec(STARTED_AT))
    expect(bounds.end).toBe(sec(RECYCLED_AT))
  })

  it("falls back to claimedAt when startedAt is missing", () => {
    const bounds = sandboxLifetimeBounds({
      claimedAt: CLAIMED_AT,
      terminatedAt: TERMINATED_AT,
    })
    expect(bounds.start).toBe(sec(CLAIMED_AT))
    expect(bounds.end).toBe(sec(TERMINATED_AT))
  })

  it("falls back to terminatedAt when recycledAt is missing", () => {
    const bounds = sandboxLifetimeBounds({
      startedAt: STARTED_AT,
      terminatedAt: TERMINATED_AT,
    })
    expect(bounds.start).toBe(sec(STARTED_AT))
    expect(bounds.end).toBe(sec(TERMINATED_AT))
  })

  it("returns undefined bounds for a fresh sandbox with only claimedAt", () => {
    // Running sandbox that has not yet started — the sheet uses this to fall
    // back to a preset time window instead of a lifetime range.
    const bounds = sandboxLifetimeBounds({ claimedAt: CLAIMED_AT })
    expect(bounds.start).toBe(sec(CLAIMED_AT))
    expect(bounds.end).toBeUndefined()
  })

  it("returns undefined bounds when all timestamps are missing", () => {
    expect(sandboxLifetimeBounds({})).toEqual({
      start: undefined,
      end: undefined,
    })
  })
})
