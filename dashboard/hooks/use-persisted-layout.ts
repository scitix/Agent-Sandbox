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

import { useCallback, useEffect, useRef } from "react"
import type { GroupImperativeHandle, Layout } from "react-resizable-panels"

/**
 * Remember how someone sized a split, and put it back next time.
 *
 * Applied AFTER mount through the group's imperative handle rather than passed
 * as `defaultLayout`. These pages are server-rendered, and a default read from
 * localStorage exists only on the client — so the server would render one set
 * of widths and the client would hydrate with another, which is a hydration
 * mismatch on every panel in the group. Restoring afterwards costs one frame at
 * the default width and is correct on both sides of the boundary.
 *
 * Failures are swallowed throughout: a browser with storage disabled, or a
 * stored value from an older layout, should cost someone their remembered
 * width and nothing else.
 */
export function usePersistedLayout(storageKey: string): {
  groupRef: React.RefObject<GroupImperativeHandle | null>
  onLayoutChanged: (layout: Layout) => void
} {
  const groupRef = useRef<GroupImperativeHandle | null>(null)

  useEffect(() => {
    const saved = readLayout(safeGet(storageKey))
    if (!saved || !groupRef.current) return
    try {
      groupRef.current.setLayout(saved)
    } catch {
      // A saved layout naming panels this group no longer has. The defaults
      // are already on screen, which is the right answer.
    }
  }, [storageKey])

  const onLayoutChanged = useCallback(
    (layout: Layout) => {
      try {
        window.localStorage.setItem(storageKey, JSON.stringify(layout))
      } catch {
        // Private mode, or a full quota. Not worth telling anyone about.
      }
    },
    [storageKey]
  )

  return { groupRef, onLayoutChanged }
}

/** The stored string a browser gave back, or null if it would not give one. */
function safeGet(storageKey: string): string | null {
  try {
    return window.localStorage.getItem(storageKey)
  } catch {
    return null
  }
}

/**
 * Parse a stored layout, rejecting anything the layout engine cannot use.
 *
 * Exported for its own test. Strict on purpose: handed a malformed layout the
 * group does not fall back to its defaults, it misbehaves — so a value that is
 * merely shaped wrong has to be caught here.
 */
export function readLayout(raw: string | null): Layout | null {
  try {
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null
    // Every value must be a finite number, or the library is handed a layout it
    // cannot apply and the whole group misbehaves rather than falling back.
    for (const v of Object.values(parsed as Record<string, unknown>)) {
      if (typeof v !== "number" || !Number.isFinite(v)) return null
    }
    return parsed as Layout
  } catch {
    return null
  }
}
