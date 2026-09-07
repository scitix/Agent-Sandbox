// The backend-neutral port every assistant component reads.
//
// The rule this file exists to enforce: `if (backend === 'claude-code')` appears
// exactly ONCE in the dashboard — in `AssistantHost`, which picks a host. Nothing
// downstream knows which harness is running; it reads capabilities and a handful
// of operations from here.
//
// Capabilities are OPTIONAL SUB-OBJECTS rather than booleans plus parallel
// method bags. A backend that cannot list conversations has no `sessions`, so a
// component that renders the history list is a type error away from being wrong
// — instead of calling a method that quietly rejects at runtime.
import { type ReactNode, createContext, useContext } from 'react'

/** Tri-state on purpose. `error` is a READY state for the composer gates: a
 *  conversation that failed to load still accepts a new message, and collapsing
 *  this into a boolean leaves the composer disabled forever. */
export type AssistantLoadState = 'loading' | 'ready' | 'error'

export interface AssistantSessionState {
  /** The conversation currently open; null before one is resolved. */
  sessionId: string | null
  error: Error | null
}

/** One conversation in the history list. */
export interface AssistantSessionSummary {
  id: string
  title?: string
  slug?: string
  /** The harness is still naming this conversation. The row shows a spinner where
   *  its actions would be, and the title arrives by push — the alternative, a
   *  harness placeholder like `New session - <ISO>`, is not a title anyone wants
   *  to read in a history list. */
  titlePending?: boolean
  updatedAt: number
  /** Which harness runs it. The gateway serves several at once and the list
   *  merges them, so this is what tells two otherwise identical rows apart. */
  backendId?: string
}

/** Present only when the backend can enumerate conversations. */
export interface AssistantSessionStore {
  /**
   * Every conversation, most recently updated first.
   *
   * A VALUE, not a `list()` call. The runtime already holds this as React state
   * and pushes every change into it (a title landing, a rename, a delete in
   * another tab), so a fetch here bought nothing and cost correctness: each
   * consumer kept its own copy, and a mutation that re-read through the store it
   * had captured at click time wrote the PRE-mutation list back over itself —
   * the renamed row showing the old title in the very view that renamed it,
   * while the other view updated correctly.
   */
  sessions: AssistantSessionSummary[]
  create(): Promise<string | null>
  remove(id: string): Promise<void>
  switchTo(id: string): void
  /** Absent when the harness cannot rename — the row menu then offers only
   *  delete, rather than an action that would fail. */
  rename?(id: string, title: string): Promise<void>
}

/**
 * End the turn in flight so a new message can be sent.
 *
 * A message cannot join a turn that is already running — neither harness offers a
 * way to do it that keeps the transcript honest, and AG-UI has no way to put a user
 * message inside a run. So the composer ASKS, and this is what a confirmed answer
 * calls: it stops the harness and waits until this tab is no longer streaming, so
 * the send that follows starts a turn of its own.
 */
export type AssistantInterruptForSend = () => Promise<void>

export interface AssistantPort {
  backendId: string
  /** Re-establish the event transport. A no-op where a run IS its own stream. */
  reconnect(): void
  loadState: AssistantLoadState
  /** A conversation the user has already chosen is still being replayed. */
  hydrating: boolean
  sessions?: AssistantSessionStore
  interruptForSend?: AssistantInterruptForSend
  /** The agent is working on this conversation — on the server, whether or not
   *  this tab is the one reading the stream. */
  turnLive?: boolean
}

const LoadStateContext = createContext<AssistantLoadState>('ready')
const HydratingContext = createContext<boolean>(false)
const SessionStoreContext = createContext<AssistantSessionStore | null>(null)
const BackendIdContext = createContext<string>('opencode')
const InterruptForSendContext = createContext<AssistantInterruptForSend | null>(
  null
)
const TurnLiveContext = createContext(false)

/**
 * Whether the open conversation has settled.
 *
 * Four readiness gates key off this (the Ask-AI fresh-session branch, both
 * composer prefill bridges, and the auto-send bridge) — they wait for
 * `ready || error` so a prompt lands in the conversation the user will actually
 * see, not one still being switched.
 */
export function useAssistantLoadState(): AssistantLoadState {
  return useContext(LoadStateContext)
}

/**
 * Is the open conversation still being replayed into the runtime?
 *
 * Distinct from `loadState`, and deliberately so: `loadState` is about the
 * SESSION (which conversation are we in, is the composer usable), and it stays
 * `ready` across a switch from one conversation to another. This is about the
 * MESSAGES, and it is true for exactly the window in which the runtime has been
 * cleared for a switch but the transcript has not arrived yet.
 *
 * An empty runtime is otherwise indistinguishable from a new conversation, so
 * without this the thread renders its landing — greeting, suggestions, cluster
 * board — for a few hundred milliseconds in the middle of every switch. Blank
 * with a spinner is a truthful "loading"; a landing screen is a lie about which
 * conversation you are in.
 */
export function useAssistantHydrating(): boolean {
  return useContext(HydratingContext)
}

/** The conversation store, or null when the backend cannot enumerate them. */
export function useAssistantSessionStore(): AssistantSessionStore | null {
  return useContext(SessionStoreContext)
}

/** Which harness is running the OPEN conversation. Read this for capability
 *  decisions, not to branch on behaviour — the port is what differs, not the
 *  component. */
export function useAssistantBackendId(): string {
  return useContext(BackendIdContext)
}

/** How to end the turn in flight, or null before a session exists. */
export function useAssistantInterruptForSend(): AssistantInterruptForSend | null {
  return useContext(InterruptForSendContext)
}

/**
 * Is the agent working on this conversation?
 *
 * Deliberately not `thread.isRunning`, which answers the narrower question "is
 * THIS TAB streaming a run". A turn outlives the response that started it, so a
 * reload, a background tab or a dropped socket leaves the two disagreeing — and
 * the composer keying off the tab's answer is what let a send collide with a turn
 * that was still working.
 */
export function useAssistantTurnLive(): boolean {
  return useContext(TurnLiveContext)
}

export function AssistantPortProvider({
  backendId,
  loadState,
  hydrating = false,
  sessions,
  interruptForSend,
  turnLive = false,
  children,
}: {
  backendId: string
  loadState: AssistantLoadState
  hydrating?: boolean
  sessions?: AssistantSessionStore | null
  interruptForSend?: AssistantInterruptForSend | null
  turnLive?: boolean
  children: ReactNode
}) {
  return (
    <BackendIdContext.Provider value={backendId}>
      <LoadStateContext.Provider value={loadState}>
        <HydratingContext.Provider value={hydrating}>
          <SessionStoreContext.Provider value={sessions ?? null}>
            <InterruptForSendContext.Provider value={interruptForSend ?? null}>
              <TurnLiveContext.Provider value={turnLive}>
                {children}
              </TurnLiveContext.Provider>
            </InterruptForSendContext.Provider>
          </SessionStoreContext.Provider>
        </HydratingContext.Provider>
      </LoadStateContext.Provider>
    </BackendIdContext.Provider>
  )
}
