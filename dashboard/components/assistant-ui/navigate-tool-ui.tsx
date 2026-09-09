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

'use client'

import { makeAssistantToolUI, useAuiState } from '@assistant-ui/react'
import { useAtomValue } from 'jotai'
import { ExternalLink } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { type FC, useCallback, useEffect, useRef } from 'react'
import { Button } from '@/components/ui/button'
import { useClusterID } from '@/hooks/use-cluster-id'
import { useLocale } from '@/hooks/use-locale'
import {
  type Destination,
  OPEN_PAGE_TOOL_NAME,
  type OpenPageArgs,
  buildDestination,
} from '@/lib/assistant/navigation-catalog'
import { atomAssistantAutoOpenPage } from '@/lib/assistant/store'
import { useTranslation } from '@/lib/i18n'

/** The tool as it appears in the transcript. MCP tools are namespaced by their
 *  server, and matching the suffix keeps this working if the server is renamed
 *  or the same tool is offered by a second backend. */
function isOpenPageCall(toolName: string | undefined): boolean {
  return !!toolName && (toolName === OPEN_PAGE || toolName.endsWith(`__${OPEN_PAGE}`))
}
const OPEN_PAGE = 'open_page'

/**
 * Tool calls already acted on, keyed by toolCallId.
 *
 * Module-level so one call jumps exactly once no matter how often the thread
 * re-renders, and so reopening a conversation cannot replay its old jumps.
 */
const handled = new Set<string>()

/** Minimal view of the transcript parts this reads. */
interface ToolCallPart {
  type?: string
  toolName?: string
  toolCallId?: string
  args?: OpenPageArgs
  result?: unknown
  status?: { type?: string }
}

/**
 * Performs the navigation an `open_page` call asks for.
 *
 * Lives OUTSIDE the tool card on purpose. assistant-ui collapses tool calls
 * into a group whose children are not mounted until someone expands it, so a
 * side effect written into the card would fire on expand — which is to say,
 * usually never, and otherwise at the wrong moment. This subscribes to thread
 * state instead, which is always there.
 *
 * Navigation only fires for a LIVE run. Reopening an old conversation replays
 * its transcript, and every `open_page` in it would otherwise fire again and
 * drag the person somewhere they did not ask to go. So it arms on `isRunning`
 * — reopening never runs — and while disarmed it marks the calls it sees as
 * handled WITHOUT jumping, so they cannot fire later either.
 *
 * Mounted inside AssistantBridges, i.e. inside the runtime provider.
 */
export function NavigationBridge() {
  const router = useRouter()
  const locale = useLocale()
  const currentCluster = useClusterID()
  const autoOpenPage = useAtomValue(atomAssistantAutoOpenPage)

  const messages = useAuiState(s => s.thread.messages) as unknown as
    | ReadonlyArray<{ role?: string; content?: ToolCallPart[] }>
    | undefined
  const isRunning = useAuiState(s => s.thread.isRunning)
  const sessionId = useAuiState(
    s => s.threadListItem?.externalId ?? s.threadListItem?.remoteId
  )

  // Whether calls seen right now are instructions or history, and which
  // conversation that judgement was made for. Refs because they must survive
  // re-renders without causing one, and they are only ever touched inside the
  // effect below.
  const armedRef = useRef(false)
  const sessionRef = useRef<string | undefined>(undefined)

  useEffect(() => {
    // A new or reopened conversation starts disarmed: everything already in it
    // happened before now.
    if (sessionRef.current !== sessionId) {
      sessionRef.current = sessionId
      armedRef.current = false
    }
    // A run in progress means the person just said something, so anything the
    // agent does from here is live. Stays armed for the rest of the
    // conversation — later turns may navigate too.
    if (isRunning) armedRef.current = true

    for (const message of messages ?? []) {
      if (message.role !== 'assistant') continue
      for (const part of message.content ?? []) {
        if (part.type !== 'tool-call' || !isOpenPageCall(part.toolName)) continue
        const id = part.toolCallId
        if (!id || handled.has(id)) continue
        // Only once the call is finalised — args stream in, and half an
        // argument builds half a path.
        const done = part.status?.type === 'complete' || part.result !== undefined
        if (!done) continue

        // Marked handled even when we do not act, so a call that was history
        // when first seen cannot become an instruction later.
        handled.add(id)
        if (!armedRef.current) continue
        // Turned off from the composer's Tools menu: the card still appears
        // with its own button, so the offer is not lost — only the automatic
        // jump is.
        if (!autoOpenPage) continue

        const dest = buildDestination(part.args, currentCluster, locale)
        // A destination that cannot be built is left alone rather than
        // approximated: landing on the wrong page is worse than landing on
        // none, and the card still shows what was asked for.
        if (dest) router.push(dest.path)
      }
    }
    // Re-running on a cluster or setting change is harmless — every call it has
    // already seen is in `handled` — which is why these are read straight from
    // the closure instead of being mirrored into refs.
  }, [messages, isRunning, sessionId, autoOpenPage, currentCluster, locale, router])

  return null
}

/**
 * The card in the transcript for one `open_page` call.
 *
 * Rendered standalone rather than folded into the collapsed tool group,
 * because it is the only visible record that the page changed underneath the
 * person — and because its button is the way back if they navigated away, or
 * the way there at all if automatic opening is off.
 */
export const NavigateToolUI = makeAssistantToolUI<OpenPageArgs, string>({
  toolName: OPEN_PAGE_TOOL_NAME,
  // Outside the collapsed "N tool calls" group. The page moved under the
  // person's feet; the record of that should not be behind a disclosure
  // triangle. Pairs with the `standalone-tool-call` groupBy key in thread.tsx.
  display: 'standalone',
  render: function NavigateCard({ args }) {
    return <OpenPageCard args={args} />
  },
})

const OpenPageCard: FC<{ args: OpenPageArgs | undefined }> = ({ args }) => {
  const { t } = useTranslation()
  const router = useRouter()
  const locale = useLocale()
  const cluster = useClusterID()
  const dest: Destination | null = buildDestination(args, cluster, locale)
  const open = useCallback(() => {
    if (dest) router.push(dest.path)
  }, [dest, router])

  if (!dest) return null
  return (
    <div className="bg-muted/40 my-1 flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
      <ExternalLink className="text-muted-foreground size-4 shrink-0" />
      <span className="min-w-0 flex-1 truncate">
        <span className="text-muted-foreground">{t('assistant.opened')} </span>
        <span className="font-medium">{dest.label}</span>
      </span>
      <Button size="sm" variant="ghost" className="h-7 shrink-0" onClick={open}>
        {t('assistant.openAgain')}
      </Button>
    </div>
  )
}
