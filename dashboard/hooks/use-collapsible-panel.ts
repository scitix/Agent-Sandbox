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

import { useEffect, useRef } from "react"
import type { PanelImperativeHandle } from "react-resizable-panels"

/**
 * Open and shut a resizable panel that is collapsed rather than unmounted.
 *
 * Only CHANGES are driven. The commit that first mounts the panel is also the
 * one that wants it open, and reaching for the imperative handle there throws —
 * "Panel constraints not found" — because the group has not measured the panel
 * it has only just been handed. There is nothing to do in that commit anyway:
 * the panel's own `defaultSize` already opens it, and the handle is for the
 * toggles that come after.
 *
 * `enabled` is false until the panel exists at all, so a caller that mounts it
 * lazily does not have to special-case the ref being null.
 */
export function useCollapsiblePanel(
  open: boolean,
  enabled: boolean
): React.RefObject<PanelImperativeHandle | null> {
  const panelRef = useRef<PanelImperativeHandle | null>(null)
  // `null` until the first run, which is how "mounted like this" is told apart
  // from "changed to this".
  const lastOpen = useRef<boolean | null>(null)

  useEffect(() => {
    if (!enabled) return
    if (lastOpen.current === null) {
      // First sight of the panel: whatever it mounted as is what was wanted.
      lastOpen.current = open
      return
    }
    if (lastOpen.current === open) return
    lastOpen.current = open

    const panel = panelRef.current
    if (!panel) return
    try {
      if (open) panel.expand()
      else panel.collapse()
    } catch (e) {
      // A backstop, not the mechanism — the ordering above is what makes this
      // work. But this library throws where most would no-op, and a column that
      // fails to open should not take the whole page down with it.
      console.error("[assistant] could not toggle the panel:", e)
    }
  }, [open, enabled])

  return panelRef
}
