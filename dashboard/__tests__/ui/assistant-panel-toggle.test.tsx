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

// Opening the assistant column drives a resizable panel through its imperative
// handle, and the handle throws rather than no-ops when it is not ready:
//
//   Uncaught Error: Panel constraints not found for Panel assistant
//
// It threw on the very first open, because that is the commit in which the
// panel mounts — the effect that expands it ran before the group had measured
// it. Nothing about that is visible in a type check or a Node test, which is
// why this file exists and runs in a DOM.

import { act, useLayoutEffect } from "react"
import { createRoot, type Root } from "react-dom/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable"
import { useCollapsiblePanel } from "@/hooks/use-collapsible-panel"

let container: HTMLDivElement
let root: Root
const errors: unknown[] = []

// jsdom has no ResizeObserver, and the panel group will not mount without one —
// which hides the very failure this file is about behind a different one. The
// stub reports a width once, which is all the group needs to lay panels out.
class StubResizeObserver {
  constructor(private readonly cb: ResizeObserverCallback) {}
  observe(target: Element) {
    // `borderBoxSize` is the one the library reads; a bare contentRect leaves
    // it reading index 0 of undefined.
    this.cb(
      [
        {
          target,
          borderBoxSize: [{ inlineSize: 1000, blockSize: 800 }],
          contentRect: { width: 1000, height: 800 } as DOMRectReadOnly,
        } as unknown as ResizeObserverEntry,
      ],
      this as unknown as ResizeObserver
    )
  }
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver =
  StubResizeObserver as unknown as typeof ResizeObserver

beforeEach(() => {
  errors.length = 0
  container = document.createElement("div")
  document.body.appendChild(container)
  root = createRoot(container, {
    // A throw inside an effect is reported here rather than propagating out of
    // `act`, so without this the test would pass through the very crash it is
    // meant to catch.
    onUncaughtError: (e) => errors.push(e),
  })
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
})

/** The shell's shape: the page is always the first panel, and the assistant
 *  joins as a collapsible second one the first time it is opened. */
function Shell({ open, everOpened }: { open: boolean; everOpened: boolean }) {
  const panelRef = useCollapsiblePanel(open, everOpened)
  return (
    <ResizablePanelGroup orientation="horizontal">
      <ResizablePanel id="page" minSize="40%">
        page
      </ResizablePanel>
      {everOpened ? (
        <>
          <ResizableHandle />
          <ResizablePanel
            id="assistant"
            panelRef={panelRef}
            collapsible
            collapsedSize="0%"
            defaultSize="32%"
            minSize="22%"
            maxSize="60%"
          >
            assistant
          </ResizablePanel>
        </>
      ) : null}
    </ResizablePanelGroup>
  )
}

function render(props: { open: boolean; everOpened: boolean }) {
  act(() => root.render(<Shell {...props} />))
}

describe("opening the assistant column", () => {
  it("does not throw on the first open, when the panel is still being measured", () => {
    // Closed: the column has never been opened, so it is not in the group.
    render({ open: false, everOpened: false })
    expect(errors).toEqual([])

    // The first open mounts the panel AND asks for it to be shown, in one
    // commit. This is the crash.
    render({ open: true, everOpened: true })
    expect(errors, `threw on first open: ${String(errors[0])}`).toEqual([])
  })

  it("survives being closed and opened again", () => {
    render({ open: false, everOpened: false })
    render({ open: true, everOpened: true })
    render({ open: false, everOpened: true })
    render({ open: true, everOpened: true })
    expect(errors, `threw while toggling: ${String(errors[0])}`).toEqual([])
  })

  it("keeps the panel mounted while it is shut, so the conversation survives", () => {
    render({ open: true, everOpened: true })
    render({ open: false, everOpened: true })
    // Collapsed, not removed: unmounting would take the in-memory conversation
    // and any question waiting on an answer with it.
    expect(container.textContent).toContain("assistant")
  })

  it("actually drives the panel rather than silently doing nothing", () => {
    // The bug it replaced was a throw, and the obvious over-correction is a
    // blanket try/catch that turns a broken toggle into a dead one.
    const collapse = vi.fn()
    const expand = vi.fn()
    function Probe({ open }: { open: boolean }) {
      const ref = useCollapsiblePanel(open, true)
      // A layout effect, so the stand-in handle is in place before the hook's
      // own passive effect looks for it — the order a real panel is attached in.
      useLayoutEffect(() => {
        ref.current = { collapse, expand } as never
      })
      return null
    }
    act(() => root.render(<Probe open={true} />))
    act(() => root.render(<Probe open={false} />))
    expect(collapse).toHaveBeenCalled()
    act(() => root.render(<Probe open={true} />))
    expect(expand).toHaveBeenCalled()
  })
})
