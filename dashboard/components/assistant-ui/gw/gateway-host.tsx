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

// Mounts the assistant against the agent gateway.
//
// Structurally the mirror of the OpenCode host, and deliberately much smaller:
// there is no long-lived event stream to keep alive, heal, or reconnect, because
// a run and its stream are the same HTTP call. What remains is the lazy-mount
// gate (do not talk to the backend while the panel is closed) and filling the
// neutral port so every shared component works unchanged.
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import {
  AssistantPortProvider,
  type AssistantSessionStore,
} from '@/components/assistant-ui/backend-port'
import { useGatewayRuntime } from '@/components/assistant-ui/gw/use-gateway-runtime'
import {
  ATTACHMENT_ADAPTER,
  AssistantBridges,
  type AssistantHostProps,
  AssistantReconnectProvider,
  AssistantSessionProvider,
  currentPageContext,
  storageKey,
} from '@/components/assistant-ui/runtime-provider'
import {
  atomAssistantBackend,
  atomAssistantCurrentSessionId,
  atomAssistantModel,
  atomAssistantRunActive,
  atomAssistantUserKey,
} from '@/lib/assistant/store'
import { useQuery } from '@tanstack/react-query'
import { useAtomValue, useSetAtom } from 'jotai'
import { useEffect, useMemo, useState } from 'react'

import { type BackendCapabilities, gateway } from './client'

// A short window after the panel closes during which the runtime stays mounted,
// so a quick open/close does not throw away a loaded conversation.
const CONNECT_GRACE_MS = 30_000

/** What the UI may do before /capabilities has answered. Conservative: the
 *  thread list and export stay hidden rather than rendering an empty history the
 *  user reads as "my conversations are gone". */
const UNKNOWN_CAPABILITIES: BackendCapabilities = {
  interaction: false,
  threadList: false,
  fork: false,
  rename: false,
  compaction: false,
  transcriptExport: false,
  reasoningStream: false,
}

export function GatewayAssistantHost({
  children,
  scope = 'default',
  active = false,
}: AssistantHostProps) {
  const userKey = useAtomValue(atomAssistantUserKey)
  const effectiveScope = userKey ? `${userKey}.${scope}` : scope
  const persistKey = storageKey(effectiveScope)

  // Same lazy-mount gate as the OpenCode host: `active` (panel open), plus a run
  // in flight (so closing the panel mid-reply does not cut it off), plus a grace
  // window for open/close churn.
  const runActive = useAtomValue(atomAssistantRunActive)
  const [graceUp, setGraceUp] = useState(false)
  useEffect(() => {
    if (active) {
      setGraceUp(true)
      return
    }
    const t = setTimeout(() => setGraceUp(false), CONNECT_GRACE_MS)
    return () => clearTimeout(t)
  }, [active])
  const wantActive = active || graceUp || runActive

  const model = useAtomValue(atomAssistantModel)
  // The toolbar picker. Only consulted when a NEW conversation is created: the
  // gateway pins a thread to the harness that made it, so re-reading this for an
  // existing thread would be a lie the server ignores anyway.
  const picked = useAtomValue(atomAssistantBackend)

  // Which harness the OPEN conversation runs on, which is not necessarily the
  // picked one — the list merges every backend's threads, so opening an older
  // conversation can land on the other harness. Resolved from the thread list
  // one render after a switch (see the effect below); until then the pick is the
  // best available guess.
  const [openBackend, setOpenBackend] = useState<string | undefined>(undefined)
  const effectiveBackend = openBackend ?? picked

  // Capabilities are static per backend, so this is fetched once per harness and
  // cached for the session. Keyed by the harness: an affordance the OPEN thread's
  // backend cannot do must not be offered just because the picked one can.
  const { data: info } = useQuery({
    queryKey: ['assistant-gateway-capabilities', effectiveBackend],
    staleTime: Infinity,
    gcTime: Infinity,
    retry: 1,
    enabled: wantActive,
    queryFn: () => gateway.info(effectiveBackend),
  })
  const capabilities = info?.capabilities ?? UNKNOWN_CAPABILITIES

  const {
    runtime,
    threadId,
    loadState,
    hydrating,
    error,
    threads,
    switchThread,
    deleteThread,
    renameThread,
    createThread,
    interruptsPending,
    turnLive,
    interruptForSend,
    reconnect,
    StateBridge,
  } = useGatewayRuntime({
    userKey: userKey || 'default',
    enabled: wantActive,
    persistKey,
    capabilities,
    pageContext: currentPageContext as () => Record<string, string> | undefined,
    ...(model?.modelID ? { model: model.modelID } : {}),
    // For NEW conversations only — an existing thread keeps the harness it was
    // created under, which the gateway enforces server-side.
    backend: picked,
    // Staging talks to the sandbox daemon, not the agent, so it is shared.
    attachments: ATTACHMENT_ADAPTER,
  })

  const sessions = useMemo<AssistantSessionStore | null>(
    () =>
      capabilities.threadList
        ? {
            // UNSTARTED THREADS ARE NOT HISTORY. A thread exists from the moment
            // the user clicks New, but nothing has been said in it until a run
            // creates the harness session — showing it would put an empty
            // "New session" row above every real conversation, and clicking New
            // twice would add two.
            sessions: threads
              .filter(t => t.started)
              .map(t => ({
                id: t.id,
                title: t.title,
                titlePending: t.titlePending,
                updatedAt: t.updatedAt,
                backendId: t.backendId,
              }))
              // Sorted HERE, once, so every consumer shares one order: the push
              // channel keeps `threads` ordered but the initial listing is the
              // server's order, and a list that re-sorted per component is a
              // second place for two views to disagree.
              .sort((a, b) => b.updatedAt - a.updatedAt),
            create: createThread,
            async remove(id: string) {
              await deleteThread(id)
            },
            switchTo(id: string) {
              switchThread(id)
            },
            // Absent, not disabled, when the harness cannot rename: the row menu
            // is built from what the port offers.
            ...(capabilities.rename ? { rename: renameThread } : {}),
          }
        : null,
    // userKey / persistKey are deliberately NOT listed: the store only closes
    // over the three callbacks, and each of those already depends on them
    // (see use-gateway-runtime), so a user / thread-scope change re-creates the
    // store through them. Re-adding them here changes nothing but the lint.
    [
      capabilities.threadList,
      capabilities.rename,
      threads,
      createThread,
      switchThread,
      deleteThread,
      renameThread,
    ]
  )

  // `wantActive` alone is not enough to decide whether to keep the runtime
  // MOUNTED: while the agent waits on a question the runtime reports not running,
  // so closing the panel would unmount it after the grace window and take the
  // in-memory interrupt with it. (It stays out of `wantActive` itself because that
  // feeds the hook that produces `interruptsPending`.)
  const keepMounted = wantActive || interruptsPending

  const threadBackend = threads.find(t => t.id === threadId)?.backendId
  useEffect(() => {
    setOpenBackend(threadBackend)
  }, [threadBackend])

  const session = useMemo(
    () => ({ sessionId: threadId, error }),
    [threadId, error]
  )

  return (
    <AssistantSessionProvider value={session}>
      {/* A run carries its own stream, but a TURN outlives it: reconnecting means
          rejoining the turn still running server-side. */}
      <AssistantReconnectProvider value={reconnect}>
        <AssistantPortProvider
          backendId={effectiveBackend}
          loadState={loadState}
          hydrating={hydrating}
          sessions={sessions}
          interruptForSend={interruptForSend}
          turnLive={turnLive}
        >
          {keepMounted ? (
            <AssistantRuntimeProvider runtime={runtime}>
              {/* Agent state and interrupts only exist inside the provider. */}
              <StateBridge />
              <CurrentSessionPublisher sessionId={threadId} />
              <AssistantBridges />
              {children}
            </AssistantRuntimeProvider>
          ) : (
            children
          )}
        </AssistantPortProvider>
      </AssistantReconnectProvider>
    </AssistantSessionProvider>
  )
}

/** Expose the open conversation so the attachment adapter stages files into the
 *  matching sandbox. Cleared on unmount, so a staged file can never target a
 *  stale conversation. */
function CurrentSessionPublisher({ sessionId }: { sessionId: string | null }) {
  const setCurrent = useSetAtom(atomAssistantCurrentSessionId)
  useEffect(() => {
    setCurrent(sessionId)
  }, [sessionId, setCurrent])
  useEffect(() => () => setCurrent(null), [setCurrent])
  return null
}
