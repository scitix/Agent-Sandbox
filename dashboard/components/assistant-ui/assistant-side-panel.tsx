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
import { useAtomValue, useSetAtom } from "jotai"
import { actingIdentityAtom } from "@/lib/atoms"
import {
  atomAssistantOpen,
  atomAssistantUserKey,
} from "@/lib/assistant/store"
import { AssistantHost } from "@/components/assistant-ui/assistant-host"
import { AssistantSurface } from "@/components/assistant-ui/assistant-surface"

/** How wide the column is. Narrow enough to keep the page readable beside it. */
const PANEL_WIDTH = "min(38rem, 42vw)"

export const AssistantSidePanel: FC = () => {
  const open = useAtomValue(atomAssistantOpen)
  const acting = useAtomValue(actingIdentityAtom)
  const setUserKey = useSetAtom(atomAssistantUserKey)
  // Once mounted it stays: unmounting takes the in-memory thread and any open
  // question with it, and a closed panel costs nothing but a hidden element.
  //
  // Latched during render rather than in an effect, so the first render after
  // the button is pressed already mounts the host — an effect would leave one
  // frame where the panel is open and empty.
  const [everOpened, setEverOpened] = useState(false)
  if (open && !everOpened) setEverOpened(true)

  // Mirrored for the UI only. The gateway's idea of who is asking comes from
  // the BFF, which overwrites it from a verified session. The ACTING identity,
  // so a selected user's workspace is what shows — matching the sandbox, which
  // is created with that user's key.
  useEffect(() => {
    const key = [acting.team, acting.user].filter(Boolean).join(".")
    setUserKey(key || undefined)
  }, [acting.team, acting.user, setUserKey])

  if (!everOpened) return null

  return (
    <aside
      // `hidden` rather than unmounted, so a turn in flight keeps streaming
      // while the panel is shut.
      hidden={!open}
      style={{ width: open ? PANEL_WIDTH : 0 }}
      className="bg-background flex min-h-0 shrink-0 flex-col border-l"
      aria-label="assistant"
    >
      <AssistantHost active={open}>
        <AssistantSurface mode="panel" />
      </AssistantHost>
    </aside>
  )
}
