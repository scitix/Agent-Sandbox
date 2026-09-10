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

/**
 * The assistant as a column beside whatever page you are on.
 *
 * Mounted once in the shell rather than per page, for two reasons. A run
 * outlives the page that started it — navigating away must not cut off a reply
 * — and the panel is opened from the header, which is also in the shell, so a
 * page-level mount would mean the button could only work where someone
 * remembered to add it.
 *
 * It renders nothing at all until first opened. The host behind it connects
 * lazily, so a user who never opens the panel never causes a request to the
 * gateway; but once mounted it stays, because unmounting mid-turn would drop
 * the conversation to save nothing.
 */

import { useEffect, useState, type FC } from "react"
import { usePathname } from "next/navigation"
import { useAtomValue, useSetAtom } from "jotai"
import { actingIdentityAtom } from "@/lib/atoms"
import {
  atomAssistantOpen,
  atomAssistantUserKey,
} from "@/lib/assistant/store"
import { AssistantHost } from "@/components/assistant-ui/assistant-host"
import { AssistantSurface } from "@/components/assistant-ui/assistant-surface"

/**
 * Whether the assistant column should be showing, and whether it has ever been.
 *
 * Split out of the panel so the LAYOUT can own the sizing. The column is one
 * half of a resizable split with the page, and a split's two sides have to be
 * siblings in one panel group — so the layout renders both, and this supplies
 * the two facts it needs to know.
 */
export function useAssistantPanelState(): {
  open: boolean
  everOpened: boolean
} {
  const rawOpen = useAtomValue(atomAssistantOpen)
  const setOpen = useSetAtom(atomAssistantOpen)
  const pathname = usePathname()
  // On the assistant's own page the panel is the same assistant, twice — two
  // greetings, two composers, two conversations that do not know about each
  // other. Suppressed here rather than by hiding the button that opens it,
  // because the flag also gets set by things that are not that button: a status
  // card queues a prompt and opens the panel on its way to a list page, and
  // arriving BACK on the assistant page must not bring it with you.
  const onAssistantPage = /(^|\/)assistant\/?$/.test(pathname)
  const open = rawOpen && !onAssistantPage

  // Reset the flag rather than only ignoring it: leaving it set means the panel
  // springs open the moment the user navigates to any other page, having been
  // told to open by something they did on this one.
  useEffect(() => {
    if (onAssistantPage && rawOpen) setOpen(false)
  }, [onAssistantPage, rawOpen, setOpen])

  // Once mounted it stays: unmounting takes the in-memory thread and any open
  // question with it, and a shut column costs nothing but a collapsed panel.
  //
  // Latched during render rather than in an effect, so the first render after
  // the button is pressed already mounts the host — an effect would leave one
  // frame where the panel is open and empty.
  const [everOpened, setEverOpened] = useState(false)
  if (open && !everOpened) setEverOpened(true)

  return { open, everOpened }
}

export const AssistantSidePanel: FC = () => {
  const { open } = useAssistantPanelState()
  const acting = useAtomValue(actingIdentityAtom)
  const setUserKey = useSetAtom(atomAssistantUserKey)

  // Mirrored for the UI only. The gateway's idea of who is asking comes from
  // the BFF, which overwrites it from a verified session. The ACTING identity,
  // so a selected user's workspace is what shows — matching the sandbox, which
  // is created with that user's key.
  useEffect(() => {
    const key = [acting.team, acting.user].filter(Boolean).join(".")
    setUserKey(key || undefined)
  }, [acting.team, acting.user, setUserKey])

  return (
    <aside
      // `hidden` rather than unmounted, so a turn in flight keeps streaming
      // while the column is shut. The width is the panel's, not this element's:
      // it is one side of a split the person can drag.
      hidden={!open}
      // No border of its own. The resize handle beside it already draws the
      // seam, and a border here put a second 1px line hard against the first —
      // two rules where the eye expects one. It was correct back when this was
      // a plain sibling of the page with nothing between them; the handle took
      // that job over.
      className="bg-background flex h-full min-h-0 min-w-0 flex-col"
      aria-label="assistant"
    >
      <AssistantHost active={open}>
        <AssistantSurface mode="panel" />
      </AssistantHost>
    </aside>
  )
}
