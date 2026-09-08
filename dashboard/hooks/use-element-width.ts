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

import { useCallback, useEffect, useState } from 'react'

/**
 * Measure an element's width: `const [ref, width] = useElementWidth()`.
 *
 * For layout decisions a CSS container query cannot make — swapping one
 * COMPONENT for another (a split panel for a sheet), rather than restyling the
 * same one. Anything expressible as a style belongs in an `@container` variant
 * instead; reach for this only when the branch is structural.
 *
 * It measures the element, not the window: a column inside a resizable split is
 * narrow whenever the split says so, whatever the screen is doing. `width` is
 * `null` until the first measure, so callers state their own default for the
 * frame before the element exists.
 *
 * The ref is a CALLBACK ref, not an object one: the measured node is often
 * behind a loading branch and appears on a later render, which an effect keyed
 * on a (permanently identical) ref object would never notice.
 */
export function useElementWidth(): [
  (el: HTMLElement | null) => void,
  number | null,
] {
  const [node, setNode] = useState<HTMLElement | null>(null)
  const [width, setWidth] = useState<number | null>(null)

  const ref = useCallback((el: HTMLElement | null) => setNode(el), [])

  useEffect(() => {
    if (!node) return
    // A ticking clock and an element observer are external systems; the
    // state they push is not derivable at render time.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setWidth(node.getBoundingClientRect().width)
    const observer = new ResizeObserver(entries => {
      const next = entries[0]?.contentRect.width
      if (typeof next === 'number') setWidth(next)
    })
    observer.observe(node)
    return () => observer.disconnect()
  }, [node])

  return [ref, width]
}
