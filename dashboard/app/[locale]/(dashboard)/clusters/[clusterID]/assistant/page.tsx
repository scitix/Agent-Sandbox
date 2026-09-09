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
 * The assistant, scoped to a cluster.
 *
 * Cluster-scoped rather than standalone because the route is what tells the
 * agent WHICH cluster the question is about: it becomes the `<page/>` marker,
 * so "what sandboxes are running?" resolves against the cluster on screen
 * rather than making the user name it.
 *
 * Note what that marker is and is not. It says where the user IS, not where to
 * look — the prompt is explicit about it — so a cluster the assistant's own
 * endpoint cannot serve produces an honest refusal from `abx` rather than the
 * wrong cluster's rows.
 */

import { useCallback, useEffect, useRef } from "react"
import { useAtomValue, useSetAtom } from "jotai"
import { useAuiState } from "@assistant-ui/react"
import {
  useAssistantHydrating,
  useAssistantLoadState,
} from "@/components/assistant-ui/backend-port"
import { actingIdentityAtom } from "@/lib/atoms"
import {
  atomAssistantOpen,
  atomAssistantPendingAutoSend,
  atomAssistantUserKey,
} from "@/lib/assistant/store"
import { AssistantHost } from "@/components/assistant-ui/assistant-host"
import { AssistantSurface } from "@/components/assistant-ui/assistant-surface"

/**
 * Carry the conversation off this page — but only when there is one.
 *
 * Leaving the assistant for some other page opens the side panel if the current
 * conversation has anything in it, and leaves it shut otherwise. The asymmetry
 * is the point: someone mid-conversation who clicks through to a list wants to
 * keep talking about what they are now looking at, while someone who merely
 * passed through an empty assistant would get a panel they never asked for on
 * every page after it.
 *
 * Runs on unmount, so it fires on any way out — a nav click, the back button, a
 * redirect — rather than only the ones a link could be taught about.
 *
 * Split in two because reading the thread needs the assistant runtime, and the
 * runtime is NOT mounted for the whole life of this page: the host renders its
 * children without a provider until the conversation has loaded. The half that
 * reads is mounted only once that has happened; the half that REMEMBERS, and
 * the cleanup that acts on it, stay here — an unmounting probe must not be
 * mistaken for the user leaving.
 */
function PanelOnLeaveBridge() {
  const setAssistantOpen = useSetAtom(atomAssistantOpen)
  // A queued auto-send is a question asked ON THE WAY OUT: something navigates
  // to a page and expects the answer to arrive in the panel beside it. Without
  // this the empty-thread rule would shut the panel that click had just opened,
  // and the reply would stream into nothing.
  const autoSend = useAtomValue(atomAssistantPendingAutoSend)
  const loadState = useAssistantLoadState()

  // Both answers are mirrored into refs because the cleanup below runs once,
  // closing over the render that created it — it has to read them as of the
  // moment we leave, not as of mount.
  const hasMessagesRef = useRef(false)
  const autoSendRef = useRef(false)
  useEffect(() => {
    autoSendRef.current = !!autoSend
  }, [autoSend])
  const remember = useCallback((hasMessages: boolean) => {
    hasMessagesRef.current = hasMessages
  }, [])

  useEffect(
    () => () => setAssistantOpen(hasMessagesRef.current || autoSendRef.current),
    [setAssistantOpen]
  )

  // The same gate the surface uses before it renders the thread: while the
  // conversation is still loading there is no runtime to read, and a hook that
  // needs one throws rather than returning nothing.
  if (loadState === "loading") return null
  return <ThreadMessageProbe onChange={remember} />
}

/** Reports whether the open conversation has anything in it. Only ever mounted
 *  where the assistant runtime is. */
function ThreadMessageProbe({
  onChange,
}: {
  onChange: (hasMessages: boolean) => void
}) {
  const hasMessages = useAuiState((s) => s.thread.messages.length > 0)
  // While the transcript is still being replayed the runtime is legitimately
  // empty, and an empty runtime is indistinguishable from a new conversation.
  // Reporting during that window would tell the page a conversation with
  // history has none — so someone who opens an old thread and immediately
  // clicks away loses the panel that should have followed them.
  const hydrating = useAssistantHydrating()
  useEffect(() => {
    if (hydrating) return
    onChange(hasMessages)
  }, [hasMessages, hydrating, onChange])
  return null
}

export default function ClusterAssistantPage() {
  const acting = useAtomValue(actingIdentityAtom)
  const setUserKey = useSetAtom(atomAssistantUserKey)

  // Mirrored for the UI only. The gateway's own idea of who is asking comes
  // from the BFF, which overwrites it from a verified session — a value set
  // here could not authorise anything even if it were wrong.
  //
  // The ACTING identity, so an admin who selected a user sees that user's
  // workspace rather than their own empty one; the sandbox behind it is created
  // with that user's key, so this is what the server is operating on.
  useEffect(() => {
    const key = [acting.team, acting.user].filter(Boolean).join(".")
    setUserKey(key || undefined)
  }, [acting.team, acting.user, setUserKey])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AssistantHost active>
        <PanelOnLeaveBridge />
        <AssistantSurface mode="page" />
      </AssistantHost>
    </div>
  )
}
