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

// @vitest-environment jsdom

import { act, useEffect } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { useDraggableBall } from "@/hooks/use-draggable-ball"

const KEY = "test.ball.position"

let container: HTMLDivElement
let root: Root
// A box rather than a bare binding: the hooks lint forbids a component writing
// to an outer variable, and it is right to — this is a test probe, and the
// mutable cell says so.
const seen: { ball?: ReturnType<typeof useDraggableBall> } = {}
const latest = () => seen.ball!

function Ball() {
  const { ref, style, dragging, onPointerDown } = useDraggableBall(KEY)
  // Recorded from an effect, not during render. `act` flushes effects before it
  // returns, so every assertion below still sees the value from the render it
  // just triggered.
  useEffect(() => {
    seen.ball = { ref, style, dragging, onPointerDown }
  })
  return (
    <button
      type="button"
      ref={ref as React.RefObject<HTMLButtonElement>}
      style={style}
      onPointerDown={onPointerDown}
    >
      mcp
    </button>
  )
}

function button(): HTMLButtonElement {
  return container.querySelector("button")!
}

/** jsdom lays nothing out: the element reports 0×0 at the origin unless told
 *  otherwise, and the hook measures both to keep the grab point under the
 *  cursor and the element on screen. */
function place(left: number, top: number, w = 120, h = 32) {
  const el = button()
  Object.defineProperty(el, "offsetWidth", { value: w, configurable: true })
  Object.defineProperty(el, "offsetHeight", { value: h, configurable: true })
  el.getBoundingClientRect = () => ({ left, top, width: w, height: h }) as DOMRect
}

function down(x: number, y: number) {
  act(() => {
    button().dispatchEvent(
      new PointerEvent("pointerdown", { clientX: x, clientY: y, button: 0, bubbles: true })
    )
  })
}
function move(x: number, y: number) {
  act(() => {
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: x, clientY: y }))
  })
}
function up() {
  act(() => {
    window.dispatchEvent(new PointerEvent("pointerup", {}))
  })
}

beforeEach(() => {
  window.localStorage.clear()
  window.innerWidth = 1000
  window.innerHeight = 800
  container = document.createElement("div")
  document.body.appendChild(container)
  root = createRoot(container)
  act(() => root.render(<Ball />))
  place(500, 400)
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
})

describe("dragging the floating button", () => {
  it("leaves a small movement as a click", () => {
    // Someone pressing a button moves a pixel or two. Swallowing the click for
    // that would read as a button that does not work.
    down(500, 400)
    move(502, 401)
    expect(latest().dragging).toBe(false)
    expect(latest().style).toBeUndefined()
    up()
  })

  it("moves once the pointer has really travelled", () => {
    down(500, 400)
    move(560, 450)
    expect(latest().dragging).toBe(true)
    // Grabbed at the element's origin, so the position follows the pointer.
    expect(latest().style).toMatchObject({ left: 560, top: 450 })
    up()
  })

  it("keeps the grab point under the cursor instead of jumping", () => {
    // Pressed 40px into the button, 10px down: it must not leap so its corner
    // lands on the pointer.
    place(460, 390)
    down(500, 400)
    move(600, 500)
    expect(latest().style).toMatchObject({ left: 560, top: 490 })
    up()
  })

  it("stays on screen", () => {
    down(500, 400)
    move(5000, 5000)
    // 1000 - 120 - 8 and 800 - 32 - 8.
    expect(latest().style).toMatchObject({ left: 872, top: 760 })
    move(-5000, -5000)
    expect(latest().style).toMatchObject({ left: 8, top: 8 })
    up()
  })

  it("remembers where it was left, and restores it next time", () => {
    down(500, 400)
    move(600, 300)
    up()
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({
      x: 600,
      y: 300,
    })

    // A fresh mount: nothing is applied during render — a position read from
    // storage would not exist on the server and would mismatch on hydration —
    // so it arrives from an effect.
    act(() => root.render(<Ball />))
    expect(latest().style).toMatchObject({ left: 600, top: 300 })
  })

  it("does not report a drag that never started", () => {
    down(500, 400)
    up()
    expect(latest().dragging).toBe(false)
    expect(window.localStorage.getItem(KEY)).toBeNull()
  })
})
