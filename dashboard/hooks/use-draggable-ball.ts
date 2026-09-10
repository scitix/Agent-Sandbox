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

import { useCallback, useEffect, useRef, useState } from "react"

/** How far the pointer must travel before this counts as a drag and not a
 *  click. Below it the button still opens what it opens — a floating control
 *  that swallowed clicks because a finger moved two pixels would read as
 *  broken. */
const DRAG_THRESHOLD_PX = 4

/** Kept fully on screen, with a little air around it. */
const EDGE_GAP_PX = 8

export interface DraggableBall {
  /** Put on the element that should move. Null style until a position is
   *  known, so the element keeps whatever CSS corner it was placed in. */
  style: React.CSSProperties | undefined
  /** Put on the grabbable element. */
  onPointerDown: (e: React.PointerEvent) => void
  /** True from the first movement past the threshold until release. Render the
   *  overlay while it is true, and suppress the click it would otherwise
   *  produce. */
  dragging: boolean
  /** Ref for the element being moved; its size is read to keep it on screen. */
  ref: React.RefObject<HTMLElement | null>
}

/**
 * Make a floating control draggable, and remember where it was left.
 *
 * Position is applied only after mount. It is read from localStorage, which
 * exists on the client alone — a position baked into the first render would
 * differ from the server's and mismatch on hydration. Until then the element
 * sits wherever its own CSS puts it, which is also the answer for anyone who
 * has never moved it.
 */
export function useDraggableBall(storageKey: string): DraggableBall {
  const ref = useRef<HTMLElement | null>(null)
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  // Where in the element the pointer went down, so it does not jump to have its
  // corner under the cursor.
  const grab = useRef({ dx: 0, dy: 0 })

  useEffect(() => {
    const saved = readPoint(storageKey)
    if (saved) setPos(clamp(saved, ref.current))
  }, [storageKey])

  // Re-clamp on resize: a window narrowed after the control was parked on the
  // right edge would otherwise leave it off screen with no way to get it back.
  useEffect(() => {
    const onResize = () => setPos(p => (p ? clamp(p, ref.current) : p))
    window.addEventListener("resize", onResize)
    return () => window.removeEventListener("resize", onResize)
  }, [])

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      // Left button / touch / pen only, and never a modifier-click.
      if (e.button !== 0) return
      const el = ref.current
      if (!el) return
      const box = el.getBoundingClientRect()
      grab.current = { dx: e.clientX - box.left, dy: e.clientY - box.top }
      const start = { x: e.clientX, y: e.clientY }
      let moved = false

      const onMove = (ev: PointerEvent) => {
        if (
          !moved &&
          Math.hypot(ev.clientX - start.x, ev.clientY - start.y) <
            DRAG_THRESHOLD_PX
        ) {
          return
        }
        moved = true
        setDragging(true)
        setPos(
          clamp(
            { x: ev.clientX - grab.current.dx, y: ev.clientY - grab.current.dy },
            el
          )
        )
      }

      const onUp = () => {
        window.removeEventListener("pointermove", onMove)
        window.removeEventListener("pointerup", onUp)
        window.removeEventListener("pointercancel", onUp)
        if (!moved) return
        // Cleared on the next frame, not now: the click event that follows
        // pointerup has not fired yet, and the flag is what tells the button to
        // ignore it.
        requestAnimationFrame(() => setDragging(false))
        setPos(p => {
          if (p) save(storageKey, p)
          return p
        })
      }

      window.addEventListener("pointermove", onMove)
      window.addEventListener("pointerup", onUp)
      window.addEventListener("pointercancel", onUp)
    },
    [storageKey]
  )

  return {
    ref,
    dragging,
    onPointerDown,
    style: pos
      ? { left: pos.x, top: pos.y, right: "auto", bottom: "auto" }
      : undefined,
  }
}

function clamp(
  p: { x: number; y: number },
  el: HTMLElement | null
): { x: number; y: number } {
  const w = el?.offsetWidth ?? 0
  const h = el?.offsetHeight ?? 0
  const maxX = Math.max(EDGE_GAP_PX, window.innerWidth - w - EDGE_GAP_PX)
  const maxY = Math.max(EDGE_GAP_PX, window.innerHeight - h - EDGE_GAP_PX)
  return {
    x: Math.min(Math.max(p.x, EDGE_GAP_PX), maxX),
    y: Math.min(Math.max(p.y, EDGE_GAP_PX), maxY),
  }
}

function readPoint(key: string): { x: number; y: number } | null {
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return null
    const p: unknown = JSON.parse(raw)
    if (!p || typeof p !== "object") return null
    const { x, y } = p as { x?: unknown; y?: unknown }
    if (typeof x !== "number" || typeof y !== "number") return null
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null
    return { x, y }
  } catch {
    return null
  }
}

function save(key: string, p: { x: number; y: number }): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(p))
  } catch {
    // Storage disabled. The control still moved; it just will not remember.
  }
}
