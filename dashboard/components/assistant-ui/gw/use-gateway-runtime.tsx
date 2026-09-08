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

// The assistant runtime: AG-UI's own, over our gateway.
//
// This used to be a hand-rolled projection — decode AG-UI back into our
// `AgentEvent`, reduce that with a reducer in @scitix/navix-headless, hand the
// result to `useExternalStoreRuntime`. The wire has spoken AG-UI for a while, so
// that reduction was a second implementation of what the protocol already
// specifies. `useAgUiRuntime` owns it now: streaming, the optimistic user message,
// tool-call parts (including `argsText`, which every tool card renders from),
// reasoning, `isRunning`, and cancellation.
//
// What is left here is what the protocol has no opinion about:
//
//   * WHICH conversation. Thread identity is the gateway's, so `adapters.threadList`
//     is wired to our thread REST rather than to anything client-side. Supplying a
//     real `threadId` also repairs four call sites that silently read the literal
//     string "default" (assistant-ui's placeholder when no thread list exists):
//     the export filename, the navigation bridge's per-session re-arm, the
//     composer attachment gate, and the topic-switch interrupt.
//   * REPLAYING history. `onSwitchToThread` returns fully-formed messages, which
//     is the one seam where WE control `createdAt` and `metadata` — so a reloaded
//     conversation keeps its send times and per-turn stats. The library's own
//     snapshot importer carries neither.
//   * RECONCILING interrupts. The library persists them in message metadata, but
//     the gateway holds parked questions in memory: after a restart a persisted
//     interrupt is unanswerable, and rendering it wedges the composer behind a
//     card that can never be dismissed. Hydration cross-checks `GET /interactions`
//     and drops what is gone.
//   * REJOINING a turn. A run and its stream are one HTTP call, but a TURN is not:
//     the gateway keeps it running under a lease when nobody is reading. So losing
//     the stream — a reload, a tab in the background, a dropped socket, a remount —
//     is recoverable, and recovering is this file's job: ask whether a turn is in
//     flight, and if so start a run that carries `attach` instead of a prompt.
//     Without it the browser sat silent while the harness worked, and the next send
//     got a 409 for a conversation that was perfectly healthy.
//
//     Deliberately NOT `unstable_resume` / `adapters.history.resume`, which the
//     library also offers: that path wants an `AsyncGenerator<ChatModelRunResult>`,
//     i.e. AG-UI events projected into assistant-ui message parts by us — the hand
//     rolled reduction this runtime exists to have deleted. An attach run reuses the
//     protocol path in full.
//   * MID-TURN MESSAGES. Both harnesses accept another message while they are
//     working. AG-UI cannot insert a user message into a run in progress, so the
//     gateway ends the run at the moment the harness takes it and this file renders
//     the message and attaches to the rest of the same turn — "assistant, user,
//     assistant" for one backend turn, which is the ordering the CLIs show.
//
//     Whether the composer offers that is `turnLive` below — the GATEWAY has a
//     turn — and not `thread.isRunning`, which only says whether this tab is
//     reading one. The two disagree exactly when a tab has lost touch with a
//     conversation that is still working, which is the case the whole file is
//     about; a composer that keyed off the tab's answer looked idle and collided
//     with the running turn. Colliding is now survivable too: `GatewayAgent`
//     answers the 409 in its own fetch, so this file is only told which way it
//     went (`onAdmitted` / `onDeferred`).
import { fromThreadMessageLike } from '@assistant-ui/react'
import type { AttachmentAdapter, ThreadMessage } from '@assistant-ui/react'
import {
  useAgUiInterrupts,
  useAgUiRuntime,
  useAgUiState,
} from '@assistant-ui/react-ag-ui'
import type { UseAgUiThreadListAdapter } from '@assistant-ui/react-ag-ui'
import {
  type FC,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'

import { GatewayAgent, attachRunConfig } from './agent'
import {
  type BackendCapabilities,
  type ModelInfo,
  type NavixAgentState,
  type ThreadInfo,
  type TranscriptEntry,
  type TurnStats,
  gateway,
  gatewayBaseUrl,
} from './client'
import { statsOf, transcriptToMessages } from './transcript'

/**
 * How long `interruptForSend` waits for this tab's run to finish draining.
 *
 * A ceiling, not a delay: the wait normally ends on the run's own falling edge
 * within a frame or two. It exists so a run whose stream never closes cannot leave
 * the composer unable to send at all — the worse of the two failures.
 */
const INTERRUPT_DRAIN_TIMEOUT_MS = 3_000

/** What the AG-UI thread-list adapter accepts as restored agent state. Derived
 *  from the library rather than importing its transitive JSON type. */
type HydratedState = Awaited<
  ReturnType<NonNullable<UseAgUiThreadListAdapter['onSwitchToThread']>>
>['state']

export interface GatewayRuntimeOptions {
  userKey: string
  /** Lazy-connection gate: nothing is fetched or created while false. */
  enabled: boolean
  /** Persisted last-viewed thread, so a refresh lands where the user left off. */
  persistKey: string
  capabilities: BackendCapabilities
  /** Harness for NEW conversations (the toolbar picker). */
  backend?: string
  /** Page context to attach to each send, evaluated at send time. */
  pageContext?: () => Record<string, string> | undefined
  model?: string
  attachments?: AttachmentAdapter
}

export interface GatewayRuntimeResult {
  runtime: ReturnType<typeof useAgUiRuntime>
  threadId: string | null
  /** Tri-state on purpose: 'error' is what the readiness gates key off, and
   *  collapsing it into a boolean leaves the composer disabled forever. */
  loadState: 'loading' | 'ready' | 'error'
  /** The chosen conversation's transcript is still on its way into the runtime.
   *  Not part of `loadState`: the SESSION is settled, its MESSAGES are not, and
   *  a consumer that shows an empty thread during this window shows a landing
   *  screen for a conversation that has history. */
  hydrating: boolean
  error: Error | null
  threads: ThreadInfo[]
  switchThread(threadId: string): void
  deleteThread(threadId: string): Promise<void>
  renameThread(threadId: string, title: string): Promise<void>
  createThread(): Promise<string | null>
  /** True while the agent is waiting on the user. Read by the panel's unmount
   *  grace window, which would otherwise discard an in-memory interrupt. */
  interruptsPending: boolean
  /**
   * The GATEWAY has a turn in this conversation — which this tab may or may not
   * be streaming. Read by the composer, so that "the agent is working" is decided
   * by the conversation rather than by whether this particular tab still has the
   * socket.
   */
  turnLive: boolean
  /**
   * End the turn in flight so a new message can be sent.
   *
   * Resolves once the gateway has no turn and this tab is no longer streaming
   * one — the composer awaits it and then sends normally, because a send issued
   * while the old run is still draining would collide with it.
   */
  interruptForSend(): Promise<void>
  /** Rejoin the turn in flight, if there is one. Idempotent and safe to call when
   *  there is nothing to rejoin. Exposed so the shared reconnect affordance and
   *  the host's visibility handling drive one implementation. */
  reconnect(): void
  /**
   * Mount INSIDE `AssistantRuntimeProvider`.
   *
   * Agent state and interrupts live in the runtime's own context, which does not
   * exist at the point this hook is called. Everything the host needs from them
   * therefore comes back through a bridge component rather than a return value.
   */
  StateBridge: FC
}

export function useGatewayRuntime(
  opts: GatewayRuntimeOptions
): GatewayRuntimeResult {
  const {
    userKey,
    enabled,
    persistKey,
    capabilities,
    model,
    pageContext,
    attachments,
    backend,
  } = opts

  // WHICH conversation we mean to be in.
  const [threadId, setThreadId] = useState<string | null>(null)
  // Which conversation the RUNTIME has actually loaded. It lags `threadId` by one
  // switch, and that lag is the whole point: `ExternalStoreThreadListRuntimeCore`
  // starts `switchToThread` with `if (this._mainThreadId === threadId) return`,
  // and `_mainThreadId` is whatever the adapter last reported as current. So
  // naming a thread current BEFORE the library has loaded it wedges that thread
  // permanently empty — every later switch is the early return. That is what a
  // reload used to do: blank conversation, its history row highlighted as current,
  // clicking the row a no-op, and "New" refusing because the thread had no
  // messages. Only the reconcile effect below advances this.
  const [shownThreadId, setShownThreadId] = useState<string | null>(null)
  // A thread id the backend minted DURING a run, waiting for that run to end.
  // See the effect below: publishing it as current mid-stream rebuilds the thread
  // the stream is writing to.
  const [adoptPending, setAdoptPending] = useState<string | null>(null)
  const [threads, setThreads] = useState<ThreadInfo[]>([])
  const [error, setError] = useState<Error | null>(null)
  const [interruptsPending, setInterruptsPending] = useState(false)
  // Does the GATEWAY have a turn in this conversation — which is not the same
  // question as "is this tab streaming one". A turn outlives the response that
  // started it, so the two disagree for exactly as long as a tab is out of touch
  // with a conversation that is still working: the window in which the composer
  // used to look idle and a send collided with the running turn.
  const [turnLive, setTurnLive] = useState(false)
  // Read by callbacks that must stay identity-stable, and by the run-ended handler
  // which fires from a subscription rather than from a render.
  const interruptsRef = useRef(false)
  interruptsRef.current = interruptsPending
  /** A message whose bubble is in the transcript with nothing behind it: it lost
   *  the race to a running turn (another tab's, or one this tab had not noticed).
   *  Re-run once that turn is over — from the bubble that is already there, so it
   *  is not shown twice. */
  const deferredRef = useRef<string | null>(null)
  const [loadState, setLoadState] = useState<'loading' | 'ready' | 'error'>(
    'loading'
  )
  const listRequestRef = useRef(0)

  // One agent for the lifetime of the host. Rebuilding it per render would reset
  // its abort controller mid-run, so the per-send context is MUTATED instead.
  const agentRef = useRef<GatewayAgent | null>(null)
  if (!agentRef.current) {
    agentRef.current = new GatewayAgent({ url: `${gatewayBaseUrl()}/run` })
  }
  const agent = agentRef.current
  agent.ctx = {
    userKey,
    ...(model ? { model } : {}),
    ...(backend ? { backend } : {}),
    ...(pageContext ? { pageContext } : {}),
    // A send that lost the race to a turn already in flight. The agent rejoined
    // that turn; the message itself is run once it ends.
    onDeferred: text => {
      deferredRef.current = text
    },
  }
  // The runtime reads `agent.threadId` when it builds a run, so this is how the
  // gateway's id — rather than the placeholder AG-UI would invent — reaches the
  // server. Also assigned EAGERLY by every switch below, because a send that
  // lands before the next render would otherwise address the previous thread.
  if (threadId) agent.threadId = threadId
  // Latest-value mirror, read by callbacks that must stay identity-stable and by
  // the bridge's effect — which runs BEFORE any effect of this hook, so syncing it
  // in an effect would hand that callback a stale id.
  const threadIdRef = useRef<string | null>(threadId)
  threadIdRef.current = threadId

  const listThreads = useCallback(
    () =>
      capabilities.threadList
        ? gateway.listThreads(userKey)
        : Promise.resolve([]),
    [capabilities.threadList, userKey]
  )

  /** Refresh history metadata without disturbing the open conversation. A run
   *  completing changes titles/order, but that is not a session initialization
   *  and must never unmount the runtime or flash the full-page loading state. */
  const refreshThreads = useCallback(() => {
    const request = ++listRequestRef.current
    void listThreads()
      .then(list => {
        if (request === listRequestRef.current) setThreads(list)
      })
      .catch(e => {
        // History is secondary to the live conversation. Keep the current thread
        // usable when a background metadata refresh fails.
        console.warn('[gateway] failed to refresh threads:', e)
      })
  }, [listThreads])

  // Thread metadata is PUSHED, never polled: a title the harness generates after
  // the turn, the first run adopting a harness session, a rename from another tab
  // and a delete all arrive here. Patching the row in place (rather than
  // re-listing) is what keeps a "naming…" spinner from flickering the whole list.
  useEffect(() => {
    if (!enabled || !capabilities.threadList) return
    return gateway.watchThreads(userKey, event => {
      // The same channel is how a tab that is NOT reading a conversation still
      // learns that one is running in it — including one this tab started and
      // then lost the stream to.
      if (event.type !== 'deleted' && event.thread.id === threadIdRef.current) {
        setTurnLive(!!event.thread.live)
      }
      setThreads(prev => {
        if (event.type === 'deleted') return prev.filter(t => t.id !== event.id)
        const next = prev.filter(t => t.id !== event.thread.id)
        next.push(event.thread)
        return next.sort((a, b) => b.updatedAt - a.updatedAt)
      })
    })
  }, [enabled, capabilities.threadList, userKey])

  // Resolve which thread to open: the last-viewed one if it still exists, else
  // the most recently updated, else none (a fresh thread is minted lazily by the
  // first send, so opening the panel costs nothing).
  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    setLoadState('loading')
    void (async () => {
      try {
        const request = ++listRequestRef.current
        const list = await listThreads()
        if (cancelled) return
        // The request counter arbitrates ONE thing: which listing gets to write
        // `threads`. It must not gate the rest, because a background refresh that
        // starts while this one is in flight (a run adopting its thread id, a New
        // or a Delete) bumps the counter — and returning here left `loadState` on
        // 'loading' with nothing that would ever move it again. The assistant then
        // sat on "initializing session…" until the next reload, which is the same
        // race, so it could stick for good. `cancelled` is this effect's own
        // currency and is the only guard it needs.
        if (request === listRequestRef.current) setThreads(list)
        const stored = localStorage.getItem(persistKey)
        const pick =
          (stored && list.find(t => t.id === stored)?.id) || list[0]?.id || null
        setThreadId(pick)
        if (pick) localStorage.setItem(persistKey, pick)
        setLoadState('ready')
        setError(null)
      } catch (e) {
        if (cancelled) return
        setError(e as Error)
        setLoadState('error')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [enabled, persistKey, listThreads])

  /** An existing conversation's messages and agent state, ready to hydrate. */
  const hydrate = useCallback(
    async (id: string) => {
      const [entries, parked] = await Promise.all([
        capabilities.transcriptExport
          ? gateway.exportThread(userKey, id)
          : Promise.resolve([] as TranscriptEntry[]),
        // Read only to reconcile — see the file header.
        capabilities.interaction
          ? gateway.pendingInteractions().catch(() => [])
          : Promise.resolve([]),
      ])
      // Sub-agent turns belong to the tool card that spawned them, not to the
      // top-level transcript.
      const visible = entries.filter(e => !e.parentAgentId)
      const stillParked = parked
        .filter(p => p.threadId === id)
        .map(p => p.request)
      const messages = transcriptToMessages(visible, stillParked).map(m =>
        // The only public ThreadMessageLike → ThreadMessage converter. Marked
        // `@deprecated` in the sense of "experimental", not "replaced".
        fromThreadMessageLike(m, m.id ?? crypto.randomUUID(), {
          type: 'complete',
          reason: 'unknown',
        })
      )
      const stats: Record<string, TurnStats> = {}
      for (const entry of visible) {
        const s = statsOf(entry)
        if (s && entry.uuid) stats[entry.uuid] = s
      }
      const state: NavixAgentState = { threadId: id, stats }
      return {
        messages,
        // The runtime treats agent state as opaque JSON it stores and echoes back,
        // so this cast is only about optional properties: its JSON type has no
        // room for `undefined`, which every optional field admits.
        state: state as unknown as HydratedState,
      }
    },
    [userKey, capabilities.transcriptExport, capabilities.interaction]
  )

  const threadListAdapter = useMemo(
    () =>
      capabilities.threadList
        ? {
            // A REAL id, so `threadListItem.remoteId` is the gateway's thread
            // rather than assistant-ui's "default" placeholder. Deliberately the
            // SHOWN id, never the intended one — see `shownThreadId`.
            ...(shownThreadId ? { threadId: shownThreadId } : {}),
            threads: threads.map(t => ({
              status: 'regular' as const,
              id: t.id,
              remoteId: t.id,
              ...(t.title ? { title: t.title } : {}),
            })),
            onSwitchToThread: hydrate,
            onSwitchToNewThread: () => {
              // Nothing to fetch: the runtime clears its own messages, and the
              // gateway thread record is created by `createThread` below.
            },
          }
        : undefined,
    [capabilities.threadList, shownThreadId, threads, hydrate]
  )

  const runtime = useAgUiRuntime({
    agent,
    showThinking: capabilities.reasoningStream,
    isDisabled: !enabled || loadState === 'loading',
    // `isRunning` is FALSE while an interrupt is open, so without this the
    // composer looks usable and `append` throws "cannot start a new run while
    // interrupts are pending" in the user's face.
    isSendDisabled: interruptsPending,
    // Deliberately NOT `unstable_enableMessageQueue`, which 0.0.53 added and
    // which looks like exactly this feature. It holds a message sent during a run
    // and sends it as a NEW run once that run settles — a different policy from
    // handing it to the turn that is already working, and one that would fight
    // this file in two places. `ExternalStoreThreadRuntimeCore.append` routes
    // into the queue on `parentId === head` alone, so the promotion append below
    // (`startRun: false`, a transcript entry and not a request) would be swallowed
    // into it and surface as a pending chip; and `onReload`/`onEdit`/`onCancel`
    // each `queue.clear()`, so every rejoin would silently drop whatever the user
    // had waiting. What the library queue was actually needed for — a composer
    // that accepts Enter while the agent works — the composer does directly.
    //
    // A 409 never arrives here any more: `GatewayAgent` answers it below the
    // protocol, because the runtime paints RUN_ERROR into the transcript before
    // this is called and nothing can take that back. What is left is genuinely
    // the conversation failing.
    onError: setError,
    // The runtime's own cancel only stops its bookkeeping: `HttpAgent.runAgent`
    // derives its own controller and ignores the signal the runtime passes it, so
    // without `abortRun` the fetch stays open and the server keeps streaming. The
    // server-side turn needs stopping too, or the agent burns tokens into a log
    // nobody is reading.
    onCancel: () => {
      agent.abortRun()
      if (threadId) void gateway.interrupt(userKey, threadId).catch(() => {})
    },
    adapters: {
      // Staging talks to the sandbox daemon, not the agent, so it is shared.
      ...(attachments ? { attachments } : {}),
      ...(threadListAdapter ? { threadList: threadListAdapter } : {}),
    },
  })

  /**
   * Rejoin the turn in flight for the open conversation.
   *
   * Asks the gateway first, and does nothing when there is nothing to rejoin —
   * which is the common case and must stay silent, since this is called from a
   * visibility change and from every run ending.
   *
   * There is only one thing to ask for: the turn from its beginning. A client that
   * has lost its stream has rendered none of the turn (the transcript export stops
   * before a running turn), so a replay cannot duplicate anything.
   */
  const attachingRef = useRef(false)
  const attachRun = useCallback(async (): Promise<boolean> => {
    const id = threadIdRef.current
    if (!id || attachingRef.current) return false
    // A run in flight in the BROWSER means we are already reading this turn.
    // Starting a second reader renders the same output into two messages.
    if (runtime.thread.getState().isRunning) return false
    // An open question is the user's move, not ours: attaching would replay the
    // question and publish it a second time. Checked against the RUNTIME's own
    // messages as well as our mirror, because this is called from a store
    // subscription that can fire before the bridge's effect has updated the
    // mirror — and the transition into "waiting on the user" is exactly when.
    if (interruptsRef.current) return false
    attachingRef.current = true
    try {
      const live = await gateway.live(id)
      setTurnLive(live.inFlight)
      if (!live.inFlight) return false
      const state = runtime.thread.getState()
      if (state.isRunning || awaitingUser(state.messages)) return false
      // `resumeRun`, not `startRun`: they reach the same place, but `startRun`
      // is the RELOAD path and the library treats a reload as the user throwing
      // the pending work away — it aborts client tool invocations and clears
      // anything the composer is holding. Rejoining a stream is neither.
      runtime.thread.resumeRun({
        parentId: state.messages.at(-1)?.id ?? null,
        runConfig: attachRunConfig(),
      })
      return true
    } catch (e) {
      // A failed reconnect is not a failed conversation: the next trigger tries
      // again, and the transcript on screen is still valid.
      console.warn('[gateway] failed to rejoin the live turn', e)
      return false
    } finally {
      attachingRef.current = false
    }
  }, [runtime])

  const reconnect = useCallback(() => void attachRun(), [attachRun])

  /**
   * Run a message that lost the race to a turn which could not take it.
   *
   * Its bubble is already in the transcript — the runtime appends optimistically,
   * and the send failed after that — so this must not put a second one there.
   * Running FROM that bubble is what gets it answered without showing it twice;
   * appending is only the fallback for a transcript that has moved on since.
   */
  const runDeferred = useCallback(
    (attached: boolean) => {
      const text = deferredRef.current
      if (!text || attached) return
      const state = runtime.thread.getState()
      if (state.isRunning || awaitingUser(state.messages)) return
      deferredRef.current = null
      const last = state.messages.at(-1)
      if (last && last.role === 'user' && textOf(last.content) === text) {
        runtime.thread.startRun({ parentId: last.id })
      } else {
        runtime.thread.append({
          role: 'user',
          content: [{ type: 'text', text }],
        })
      }
    },
    [runtime]
  )

  // A run ending is the moment to ask whether the TURN ended with it.
  //
  // Two ways it did not: the stream died early, or this browser was never the one
  // reading (another tab). Both look the same from here — the run stopped and the
  // gateway still has a turn — and both are fixed the same way.
  useEffect(() => {
    if (!enabled) return
    let wasRunning = runtime.thread.getState().isRunning
    return runtime.thread.subscribe(() => {
      const running = runtime.thread.getState().isRunning
      const ended = wasRunning && !running
      // A run in this tab is a turn on the gateway, no round trip needed. The
      // falling edge says nothing on its own — that is what `attachRun` settles.
      if (running && !wasRunning) setTurnLive(true)
      wasRunning = running
      if (!ended || interruptsRef.current) return
      void attachRun().then(runDeferred)
    })
  }, [enabled, runtime, attachRun, runDeferred])

  // Everything that means "this tab may have missed some of a turn".
  //
  // Visibility covers the case the library's own resume does not (it only fires on
  // mount, so a backgrounded tab stays silent — see vercel/ai#11865 for the same
  // gap); `online` covers a network that came back. Both are cheap: `attachRun`
  // asks the gateway and returns immediately when nothing is running.
  useEffect(() => {
    if (!enabled || !threadId) return
    const check = () => {
      if (document.visibilityState !== 'visible') return
      void attachRun()
    }
    document.addEventListener('visibilitychange', check)
    window.addEventListener('online', check)
    return () => {
      document.removeEventListener('visibilitychange', check)
      window.removeEventListener('online', check)
    }
  }, [enabled, threadId, attachRun])

  /**
   * End the turn in flight so the user's next message can be its own turn.
   *
   * The composer awaits this and then sends normally. Both halves are needed:
   * `/interrupt` stops the harness and drops the gateway's turn, and the WAIT is
   * for this tab's own run to finish draining — a send issued while the runtime
   * still reports `isRunning` overlaps two runs on one thread, which the gateway
   * answers with a 409 and the library renders as an error on a conversation that
   * is fine.
   *
   * Bounded, because "the composer never sends again" is a worse failure than
   * "the send raced the tail of a run that was ending anyway".
   */
  const interruptForSend = useCallback(async () => {
    const id = threadIdRef.current
    if (!id) return
    try {
      await gateway.interrupt(userKey, id)
    } catch (e) {
      // Already over, or unreachable. Either way the wait below is what decides
      // whether it is safe to send.
      console.warn('[gateway] could not interrupt the running turn', e)
    }
    setTurnLive(false)
    if (!runtime.thread.getState().isRunning) return
    await new Promise<void>(resolve => {
      const timer = setTimeout(() => {
        unsubscribe()
        resolve()
      }, INTERRUPT_DRAIN_TIMEOUT_MS)
      const unsubscribe = runtime.thread.subscribe(() => {
        if (runtime.thread.getState().isRunning) return
        clearTimeout(timer)
        unsubscribe()
        resolve()
      })
    })
  }, [userKey, runtime])

  // The ONE place a conversation is loaded into the runtime: bring the runtime to
  // whatever `threadId` currently says. Every other path just sets `threadId`.
  //
  // It lives after `useAgUiRuntime` because it needs the runtime, and it is an
  // effect rather than part of the resolve above so it also covers a switch from
  // history — one code path, so a thread can never end up named-but-not-loaded.
  const switchingRef = useRef<string | null>(null)
  useEffect(() => {
    // Without a thread list there is no adapter to switch through, and asking
    // anyway throws rather than no-ops.
    if (!enabled || !capabilities.threadList || !threadId) return
    // A pending adoption means the runtime is ALREADY streaming this thread under
    // its placeholder id. Hydrating would replay a transcript over the live turn.
    if (adoptPending === threadId) return
    if (threadId === shownThreadId || switchingRef.current === threadId) return
    // Guard the in-flight switch by id: this effect re-runs whenever the runtime
    // object identity changes, which would otherwise fire a second hydrate for the
    // same thread while the first is still fetching.
    switchingRef.current = threadId
    void runtime.threads
      .switchToThread(threadId)
      .then(() => {
        setShownThreadId(threadId)
        // The transcript stops before a turn that is still running (the gateway
        // trims the export), so this is where a reload picks that turn back up.
        void attachRun()
      })
      .catch((e: unknown) => {
        // Surface it: a failed replay is indistinguishable from an empty
        // conversation, and staying silent is how this was invisible before.
        console.warn('[gateway] failed to load thread', threadId, e)
        setError(e as Error)
      })
      .finally(() => {
        if (switchingRef.current === threadId) switchingRef.current = null
      })
  }, [
    enabled,
    capabilities.threadList,
    threadId,
    shownThreadId,
    adoptPending,
    runtime,
    attachRun,
  ])

  // The adopted id reaches the thread-list adapter only when the turn is over.
  //
  // `adapter.threadId` is not a label. `ExternalStoreThreadListRuntimeCore`
  // rebuilds the main thread whenever it changes (`if (previousThreadId !==
  // newThreadId) { this._mainThread = this.threadFactory() }`), and a thread built
  // mid-stream starts from the store snapshot, sees a run in flight with no
  // assistant message yet, and inserts its OWN empty optimistic placeholder. The
  // deltas keep flowing to the message the old thread owned, so the FIRST turn of
  // a new conversation rendered an empty bubble that a reload "fixed" — a reload
  // replays the transcript from the gateway.
  useEffect(() => {
    if (!adoptPending) return
    const flush = () => {
      if (runtime.thread.getState().isRunning) return
      setShownThreadId(adoptPending)
      setAdoptPending(null)
    }
    flush()
    return runtime.thread.subscribe(flush)
  }, [adoptPending, runtime])

  /** Adopt the gateway's thread id once the backend mints one. */
  const adoptThreadId = useCallback(
    (id: string) => {
      // Guarded on the ref, not on a state updater: the bridge reports the id from
      // an effect that can fire more than once per value, and the work here (two
      // state writes, localStorage, a list refresh) does not belong inside a
      // reducer where React is free to call it twice.
      if (threadIdRef.current === id) return
      threadIdRef.current = id
      localStorage.setItem(persistKey, id)
      agent.threadId = id
      setThreadId(id)
      // NOT `setShownThreadId`: the runtime is already streaming this conversation,
      // and renaming it now would rebuild the thread underneath the stream. The
      // effect above publishes the id the moment the turn ends.
      setAdoptPending(id)
      refreshThreads()
    },
    [persistKey, refreshThreads, agent]
  )

  const StateBridge = useMemo(
    () => makeStateBridge(adoptThreadId, setInterruptsPending),
    [adoptThreadId]
  )

  const createThread = useCallback(async () => {
    if (!capabilities.threadList) return null
    const id = await gateway.createThread(userKey, undefined, backend)
    localStorage.setItem(persistKey, id)
    await runtime.threads.switchToNewThread()
    agent.threadId = id
    setThreadId(id)
    // Drop any adoption still waiting on the previous turn: its flush would name
    // that thread current again, behind this one's back.
    setAdoptPending(null)
    // A brand-new thread has nothing to replay, and the runtime was just cleared
    // by `switchToNewThread` — so it IS showing this conversation.
    setShownThreadId(id)
    refreshThreads()
    return id
  }, [
    capabilities.threadList,
    userKey,
    persistKey,
    backend,
    refreshThreads,
    runtime,
    agent,
  ])

  const switchThread = useCallback(
    (id: string) => {
      if (id === threadId) return
      localStorage.setItem(persistKey, id)
      agent.threadId = id
      // Naming the thread is the whole job: the reconcile effect above loads it
      // and then advances `shownThreadId`.
      setThreadId(id)
      setAdoptPending(null)
      // Describes the conversation being left, not the one being opened. The
      // switch's own `attachRun` re-establishes it for the new one.
      setTurnLive(false)
    },
    [persistKey, threadId, agent]
  )

  /** Rename, then let the push channel correct the list. The optimistic patch is
   *  for the dialog's own responsiveness — a rename the user typed should not wait
   *  on a round trip to appear. */
  const renameThread = useCallback(
    async (id: string, title: string) => {
      setThreads(prev => prev.map(t => (t.id === id ? { ...t, title } : t)))
      await gateway.renameThread(userKey, id, title)
    },
    [userKey]
  )

  const deleteThread = useCallback(
    async (id: string) => {
      await gateway.deleteThread(userKey, id)
      const request = ++listRequestRef.current
      const list = await listThreads()
      if (request !== listRequestRef.current) return
      setThreads(list)
      if (id !== threadId) return

      const next = list[0]?.id ?? null
      if (next) {
        localStorage.setItem(persistKey, next)
        agent.threadId = next
        await runtime.threads.switchToThread(next)
      } else {
        localStorage.removeItem(persistKey)
        await runtime.threads.switchToNewThread()
      }
      setThreadId(next)
      // Switched here rather than through the reconcile effect, because the
      // deleted thread must not stay on screen while an effect catches up.
      setShownThreadId(next)
    },
    [userKey, threadId, persistKey, listThreads, runtime, agent]
  )

  // The runtime has been pointed at a conversation it has not replayed yet.
  //
  // `shownThreadId` advances only when `switchToThread` resolves, so the gap
  // between the two ids IS the hydration window — the one in which the runtime
  // holds no messages for a conversation that certainly has some. An adoption is
  // excluded: there the ids differ because a live turn is streaming under the
  // placeholder id, which is the opposite of "nothing to show".
  const hydrating =
    !!threadId && threadId !== shownThreadId && adoptPending !== threadId

  return {
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
  }
}

/**
 * Publish what only exists inside the runtime's context.
 *
 * A component rather than a hook because `useAgUiState` / `useAgUiInterrupts` read
 * the runtime's extras, which are not reachable from the code that CREATES the
 * runtime.
 */
function makeStateBridge(
  onThreadId: (id: string) => void,
  onInterrupts: (pending: boolean) => void
): FC {
  return function GatewayStateBridge() {
    const state = useAgUiState<NavixAgentState>()
    const interrupts = useAgUiInterrupts()
    const reported = state?.threadId
    useEffect(() => {
      if (reported) onThreadId(reported)
    }, [reported])
    useEffect(() => {
      onInterrupts(interrupts.length > 0)
    }, [interrupts.length])
    return null
  }
}

/**
 * Is the conversation waiting on the user?
 *
 * The library marks the assistant message that carries an open interrupt
 * `requires-action`, and that flag flips with the store rather than with a React
 * effect — which is what makes it usable from a subscription callback.
 */
function awaitingUser(messages: readonly ThreadMessage[]): boolean {
  const last = messages.at(-1)
  return last?.role === 'assistant' && last.status?.type === 'requires-action'
}

/** A message's text parts, joined — how a rendered bubble is compared with the
 *  plain string the gateway and this file pass around. */
function textOf(content: ThreadMessage['content']): string {
  return content
    .map(part => (part.type === 'text' ? part.text : ''))
    .join('')
    .trim()
}

// --- transcript replay -------------------------------------------------------

// The conversion itself lives in `./transcript`, which has no React and no live
// stack — the read-only analysis viewer imports it without dragging this module
// (and `useAgUiRuntime`, and `GatewayAgent`) in behind it. Re-exported here so
// hydration's own call site, and anything already importing it from the runtime,
// stay where they are.
export { transcriptToMessages }

export type { ModelInfo, ThreadInfo, BackendCapabilities, ThreadMessage }
