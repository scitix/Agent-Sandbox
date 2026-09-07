import {
  type AskAttachment,
  atomAssistantOpen,
  atomAssistantPendingAttachment,
  atomAssistantPendingPrompt,
} from '@/lib/assistant/store'
import { useSetAtom } from 'jotai'
import { useCallback } from 'react'

export interface AskInput {
  /** Question text dropped into the composer (user still hits Send). */
  prompt?: string
  /** Content-heavy payload added as a composer attachment. */
  attachment?: AskAttachment
}

/**
 * The single source of truth for Ask AI button *behavior*. Every button in this
 * folder shares it, so they behave identically regardless of design:
 *
 *  - Opens the assistant panel (which is what mounts the runtime — the panel may
 *    be closed when the button is clicked).
 *  - Queues the prompt/attachment for the CURRENT session — asking about a table
 *    or a report is usually a continuation of what the user is already
 *    discussing, so it appends context instead of branching away from it. The
 *    prefill lands runtime-side once mounted (see the composer prefill bridges
 *    in runtime-provider.tsx).
 *  - Prefills only — never auto-sends.
 *
 * Intentionally uses NO runtime-bound hooks (no `useAui`/`useOpenCode*`): with
 * the lazy-connection model the runtime isn't mounted while the panel is closed,
 * and an Ask AI button must still work (and stay visible) on any page to open it.
 */
export function useAskAI() {
  const setOpen = useSetAtom(atomAssistantOpen)
  const setPendingPrompt = useSetAtom(atomAssistantPendingPrompt)
  const setPendingAttachment = useSetAtom(atomAssistantPendingAttachment)

  const ask = useCallback(
    ({ prompt, attachment }: AskInput) => {
      setOpen(true)
      // Attachment first so it's queued before the prompt's send-readiness. The
      // atom takes a list (the topic-switch move carries several); a click has one.
      if (attachment) setPendingAttachment([attachment])
      if (prompt) setPendingPrompt(prompt)
    },
    [setOpen, setPendingAttachment, setPendingPrompt]
  )

  return { ask, busy: false }
}
