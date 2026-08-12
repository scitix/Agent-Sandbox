// The only file that knows what the browser reads.
//
// The wire is AG-UI (https://github.com/ag-ui-protocol) over Server-Sent Events,
// and the browser now runs the protocol's own runtime (`@assistant-ui/react-ag-ui`
// over an `HttpAgent`). So everything here has to be not merely parseable but
// PROTOCOL-VALID: `@ag-ui/client` pipes every frame through a verifier and turns a
// violation into `RUN_ERROR`, i.e. a red bubble where an answer should be. The
// rules that constrain this file:
//
//   * `RUN_FINISHED` is rejected while any text message or tool call is still
//     open — which matters because an interrupt ends the run MID-turn. Hence
//     `interrupt()` closes everything first.
//   * `TOOL_CALL_ARGS` / `TOOL_CALL_END` are rejected for a call that did not
//     start in THIS run. After an interrupt the backend still emits the tail of
//     the call we force-closed, so the translator tracks which calls it opened and
//     drops events for any other. `TOOL_CALL_RESULT` is deliberately exempt — the
//     runtime routes a result for a call it does not own back to the message that
//     holds it, which is how the pre-interrupt tool card gets filled in.
//   * Exactly one `RUN_STARTED`…`RUN_FINISHED` pair per HTTP response.
//
// Backends still emit `AgentEvent` — the internal contract in agent-events.ts —
// and this file is the ONLY translation point, exactly
// as the backend contract promises ("BACKENDS DO NOT SPEAK THE WIRE PROTOCOL").
//
// Two of our concepts have no event of their own in AG-UI and travel as agent
// STATE (`STATE_SNAPSHOT`), not as `CUSTOM`: the runtime silently drops `CUSTOM`
// (its aggregator has no branch for it), whereas state is reduced, exposed through
// `useAgUiState`, echoed back on the next run, and restorable on a thread switch.
//   * per-turn statistics (model / tokens / cost), keyed by assistant message id;
//   * the gateway-assigned thread id, which the browser needs to keep talking to
//     the same conversation.
// A third, `notice`, has no consumer in the UI at all and is translated to
// nothing — provably invisible rather than accidentally so.
//
// `compaction` is the opposite case: it is POSITIONAL, so state is the wrong
// vehicle. It goes out as a synthetic tool call, which is the only in-order part
// the runtime's aggregator builds (it makes text, reasoning and tool calls, and
// nothing else). See COMPACTION_TOOL_NAME.
import { EventType } from '@ag-ui/core'
import type { BaseEvent, Interrupt } from '@ag-ui/core'
import { EventEncoder } from '@ag-ui/encoder'
import { COMPACTION_TOOL_NAME } from './agent-events.ts'
import { randomUUID } from 'node:crypto'

import type { AgentEvent, Usage } from './backend.ts'

/** Heartbeat cadence. Long enough to be cheap, short enough that an idle stream
 *  does not look dead to an intermediary with a read timeout. */
export const HEARTBEAT_MS = 15_000

export const SSE_HEADERS: Record<string, string> = {
  'content-type': 'text/event-stream; charset=utf-8',
  'cache-control': 'no-cache, no-transform',
  connection: 'keep-alive',
  // Belt-and-braces for a proxy that ignores cache-control.
  'x-accel-buffering': 'no',
}

const encoder = new EventEncoder()

export function encodeEvent(event: BaseEvent): string {
  return encoder.encodeSSE(event)
}

/** A comment frame: keeps the connection warm without being an event. The AG-UI
 *  SSE reader collects only `data:` lines, so this is invisible to the protocol. */
export function encodeHeartbeat(): string {
  return `: ping\n\n`
}

export interface TurnStats {
  model?: string
  usage?: Usage
  costUsd?: number
  stopReason?: string
}

/**
 * The agent state the browser reads with `useAgUiState`.
 *
 * Round-tripped: the runtime sends the last snapshot back on every run, so the
 * gateway can be stateless about it — merge this run's entry into what the client
 * sent and echo the whole thing.
 */
export interface AgentState {
  /** The gateway's own thread id. Authoritative: for a brand-new conversation the
   *  id the client generated is a placeholder. */
  threadId?: string
  /** Per assistant-message statistics. Bounded, oldest evicted. */
  stats?: Record<string, TurnStats>
  /**
   * Events the live-turn log had to discard before this reader attached.
   *
   * Present only on a re-attach that cannot show the whole turn. The UI says so;
   * without it a truncated replay looks like the turn's real beginning.
   */
  dropped?: number
}

/** How many turns' stats ride along in the round-tripped state. The browser only
 *  renders them for messages on screen, and the state is echoed on every run, so
 *  an unbounded map would grow the request body for no gain. */
const MAX_STATS_ENTRIES = 50

/** Merge one turn's stats into the client's snapshot, newest-wins, bounded. */
export function mergeStats(
  previous: AgentState | undefined,
  messageId: string,
  stats: TurnStats
): AgentState {
  const merged: Record<string, TurnStats> = { ...(previous?.stats ?? {}) }
  // Re-insert so this id is newest in insertion order, which is what the eviction
  // below relies on.
  delete merged[messageId]
  merged[messageId] = stats
  const keys = Object.keys(merged)
  for (const key of keys.slice(0, Math.max(0, keys.length - MAX_STATS_ENTRIES)))
    delete merged[key]
  return { ...previous, stats: merged }
}

export interface TranslatorOptions {
  /** The state the client echoed back, so `STATE_SNAPSHOT` stays cumulative. */
  state?: AgentState
}

/** What ended a run that did not end its turn. `null` for a normal completion. */
export type RunOutcomeKind = 'interrupt' | null

/**
 * Translate one HTTP run's worth of AgentEvents into AG-UI.
 *
 * Stateful across the run because AG-UI is message-oriented while our events are
 * delta-oriented: text needs START/CONTENT/END around a message id, and every
 * delta of one run of text must share that id.
 *
 * `messageId` is pre-allocated by the caller and used both for the FIRST text
 * message and as the initial tool `parentMessageId`, so that whichever comes first
 * makes the browser adopt our id (its aggregator takes the first server id it
 * sees, from either `TEXT_MESSAGE_START.messageId` or
 * `TOOL_CALL_START.parentMessageId`). That is what lets `STATE_SNAPSHOT` key stats
 * by a message id the browser agrees with.
 *
 * A later run of text after a tool call gets a FRESH id, deliberately: reusing the
 * first id would make the browser append the new paragraph to the pre-tool text
 * part, teleporting it above the tool cards.
 *
 * The id must also be fresh per RUN, never reused across the two runs of an
 * interrupted turn: the browser deletes its placeholder when a reported id already
 * exists and then writes to a message that is gone, so everything after the
 * interrupt would silently fail to render.
 */
export class AgUiTranslator {
  private textMessageId: string | null = null
  private firstTextUsed = false
  private reasoningId: string | null = null
  /** AG-UI attaches a tool call to the assistant message that made it. Seeded with
   *  the pre-allocated id so a run that opens with a tool call still reports it. */
  private toolParentId: string | null
  /** Tool calls this run opened. Doubles as the verifier's own bookkeeping: an
   *  `ARGS`/`END` for a call absent from this set is dropped rather than sent,
   *  which is exactly what arrives after an interrupt (the call started in the
   *  PREVIOUS run and was force-closed) and would otherwise be rejected. */
  private readonly openToolIds = new Set<string>()
  private state: AgentState | undefined

  constructor(
    private readonly threadId: string,
    private readonly runId: string,
    private readonly messageId: string,
    opts: TranslatorOptions = {}
  ) {
    this.toolParentId = messageId
    this.state = opts.state
  }

  start(): BaseEvent[] {
    return [
      {
        type: EventType.RUN_STARTED,
        threadId: this.threadId,
        runId: this.runId,
      } as BaseEvent,
    ]
  }

  /** Everything still open, closed in order, then a successful RUN_FINISHED. */
  finish(): BaseEvent[] {
    return [
      ...this.closeAll(),
      {
        type: EventType.RUN_FINISHED,
        threadId: this.threadId,
        runId: this.runId,
        outcome: { type: 'success' },
      } as BaseEvent,
    ]
  }

  /**
   * End this run because the agent is waiting on the user.
   *
   * Any tool call still open is force-closed first (the verifier rejects
   * `RUN_FINISHED` otherwise). Its trailing `tool-args`/`tool-end` are then
   * dropped by the next run, which never saw it start — so an argument delta that
   * arrives after the question is lost from that card. Cosmetic, and unavoidable:
   * the protocol has no way to reopen a completed call.
   */
  interrupt(interrupts: Interrupt[]): BaseEvent[] {
    return [
      ...this.closeAll(),
      {
        type: EventType.RUN_FINISHED,
        threadId: this.threadId,
        runId: this.runId,
        outcome: { type: 'interrupt', interrupts },
      } as BaseEvent,
    ]
  }

  error(message: string): BaseEvent[] {
    return [
      ...this.closeAll(),
      { type: EventType.RUN_ERROR, message } as BaseEvent,
    ]
  }

  translate(event: AgentEvent): BaseEvent[] {
    switch (event.t) {
      case 'thread':
        // The gateway assigns the id and the browser needs it to keep talking to
        // the same thread. RUN_STARTED already carried a threadId, but for a
        // brand-new thread that was the client's placeholder.
        this.state = { ...this.state, threadId: event.threadId }
        return [this.snapshot()]

      case 'turn-start':
        return []

      case 'turn-end':
        this.state = mergeStats(this.state, this.messageId, {
          ...(event.model ? { model: event.model } : {}),
          ...(event.usage ? { usage: event.usage } : {}),
          ...(typeof event.costUsd === 'number'
            ? { costUsd: event.costUsd }
            : {}),
          ...(event.stopReason ? { stopReason: event.stopReason } : {}),
        })
        // Text and reasoning are closed first so the snapshot cannot land between
        // a START and its END.
        return [...this.flushText(), ...this.flushThinking(), this.snapshot()]

      case 'text': {
        const out: BaseEvent[] = [...this.flushThinking()]
        if (!this.textMessageId) {
          this.textMessageId = this.firstTextUsed
            ? randomUUID()
            : this.messageId
          this.firstTextUsed = true
          this.toolParentId = this.textMessageId
          out.push({
            type: EventType.TEXT_MESSAGE_START,
            messageId: this.textMessageId,
            role: 'assistant',
          } as BaseEvent)
        }
        out.push({
          type: EventType.TEXT_MESSAGE_CONTENT,
          messageId: this.textMessageId,
          delta: event.delta,
        } as BaseEvent)
        return out
      }

      case 'thinking': {
        // REASONING_MESSAGE_*, not THINKING_TEXT_MESSAGE_* — the latter is
        // deprecated in the protocol and slated for removal in 1.0.
        const out: BaseEvent[] = [...this.flushText()]
        if (!this.reasoningId) {
          this.reasoningId = randomUUID()
          out.push({
            type: EventType.REASONING_MESSAGE_START,
            messageId: this.reasoningId,
            role: 'reasoning',
          } as BaseEvent)
        }
        out.push({
          type: EventType.REASONING_MESSAGE_CONTENT,
          messageId: this.reasoningId,
          delta: event.delta,
        } as BaseEvent)
        return out
      }

      case 'tool-start': {
        // A tool call cannot interleave with an open text message.
        const out: BaseEvent[] = [...this.flushText(), ...this.flushThinking()]
        this.openToolIds.add(event.id)
        out.push({
          type: EventType.TOOL_CALL_START,
          toolCallId: event.id,
          toolCallName: event.name,
          ...(this.toolParentId ? { parentMessageId: this.toolParentId } : {}),
        } as BaseEvent)
        return out
      }

      case 'tool-args':
        if (!this.openToolIds.has(event.id)) return []
        return [
          {
            type: EventType.TOOL_CALL_ARGS,
            toolCallId: event.id,
            delta: event.delta,
          } as BaseEvent,
        ]

      case 'tool-end': {
        if (!this.openToolIds.has(event.id)) return []
        const out: BaseEvent[] = []
        // A backend that only learns the arguments at the end (OpenCode sends
        // the whole part rather than a delta stream) emits them here, once.
        if (event.args !== undefined) {
          out.push({
            type: EventType.TOOL_CALL_ARGS,
            toolCallId: event.id,
            delta: JSON.stringify(event.args),
          } as BaseEvent)
        }
        this.openToolIds.delete(event.id)
        out.push({
          type: EventType.TOOL_CALL_END,
          toolCallId: event.id,
        } as BaseEvent)
        return out
      }

      case 'tool-result':
        // Deliberately NOT gated on `openToolIds`: a result for a call this run
        // never started is exactly what arrives after an interrupt, and the
        // browser routes it back to the message that holds the call. Re-opening
        // the call instead would duplicate the card.
        return [
          {
            type: EventType.TOOL_CALL_RESULT,
            messageId: randomUUID(),
            toolCallId: event.id,
            content: event.content,
            role: 'tool',
          } as BaseEvent,
        ]

      case 'interaction':
        // Nothing on the wire: this is the signal that ends the run. The caller
        // reads the parked requests and calls `interrupt()`.
        return []

      case 'compaction': {
        // A synthetic tool call, because the runtime materialises no other kind
        // of in-order part (see COMPACTION_TOOL_NAME). It is opened, argued and
        // closed in one go — a call left open would be force-closed by
        // `finish()` anyway, and the result is what stops the card rendering as
        // a tool still running.
        const id = `compaction-${randomUUID()}`
        return [
          ...this.flushText(),
          ...this.flushThinking(),
          {
            type: EventType.TOOL_CALL_START,
            toolCallId: id,
            toolCallName: COMPACTION_TOOL_NAME,
            ...(this.toolParentId
              ? { parentMessageId: this.toolParentId }
              : {}),
          } as BaseEvent,
          {
            type: EventType.TOOL_CALL_ARGS,
            toolCallId: id,
            delta: JSON.stringify({ auto: event.auto }),
          } as BaseEvent,
          { type: EventType.TOOL_CALL_END, toolCallId: id } as BaseEvent,
          {
            type: EventType.TOOL_CALL_RESULT,
            messageId: randomUUID(),
            toolCallId: id,
            content: '',
            role: 'tool',
          } as BaseEvent,
        ]
      }

      case 'notice':
        // No consumer in the UI. Dropped here so that fact is stated in one place
        // rather than inferred from the absence of a renderer.
        return []

      case 'error':
        return this.error(event.message)
    }
  }

  /** The state the browser should hold, as of now. */
  snapshot(): BaseEvent {
    return {
      type: EventType.STATE_SNAPSHOT,
      snapshot: this.state ?? {},
    } as BaseEvent
  }

  /** Everything the verifier considers "still active", closed in a valid order. */
  private closeAll(): BaseEvent[] {
    const tools = [...this.openToolIds].map(
      id => ({ type: EventType.TOOL_CALL_END, toolCallId: id }) as BaseEvent
    )
    this.openToolIds.clear()
    return [...this.flushText(), ...this.flushThinking(), ...tools]
  }

  private flushText(): BaseEvent[] {
    if (!this.textMessageId) return []
    const id = this.textMessageId
    this.textMessageId = null
    return [{ type: EventType.TEXT_MESSAGE_END, messageId: id } as BaseEvent]
  }

  private flushThinking(): BaseEvent[] {
    if (!this.reasoningId) return []
    const id = this.reasoningId
    this.reasoningId = null
    return [
      { type: EventType.REASONING_MESSAGE_END, messageId: id } as BaseEvent,
    ]
  }
}

export interface StreamRunOptions {
  threadId: string
  runId: string
  /** Pre-allocated assistant message id for THIS run. Never shared with another
   *  run of the same turn. */
  messageId: string
  state?: AgentState
  heartbeatMs?: number
  signal?: AbortSignal
  /**
   * The agent parked a question. Return the interrupts to publish, and the run
   * ends with them; return an empty array to keep streaming (nothing to ask).
   */
  onInteraction?: () => Interrupt[]
}

/**
 * Serialise one run to AG-UI SSE text, emitting heartbeats while the backend is
 * quiet.
 *
 * Also the single place that turns a thrown error into a wire `RUN_ERROR`: a
 * backend that dies mid-stream must still produce a frame the UI can render,
 * otherwise the browser sees a silent truncation and shows a spinner forever.
 *
 * Unlike its predecessor this does NOT call `iterator.return()` on the way out —
 * the events it consumes belong to a turn in the live-turn registry, which may
 * outlive this response (that is the whole mechanism behind interrupts).
 */
export async function* streamRun(
  events: AsyncIterable<AgentEvent>,
  opts: StreamRunOptions
): AsyncGenerator<string> {
  const heartbeatMs = opts.heartbeatMs ?? HEARTBEAT_MS
  const translator = new AgUiTranslator(
    opts.threadId,
    opts.runId,
    opts.messageId,
    opts.state ? { state: opts.state } : {}
  )
  const iterator = events[Symbol.asyncIterator]()

  for (const e of translator.start()) yield encodeEvent(e)

  let ended = false
  try {
    for (;;) {
      let next: IteratorResult<AgentEvent>
      // Race the backend against the heartbeat so a long tool call still keeps
      // the connection alive.
      const pending = iterator.next()
      const timer = new Promise<'heartbeat'>(resolve =>
        setTimeout(() => resolve('heartbeat'), heartbeatMs)
      )
      const raced = await Promise.race([pending, timer])
      if (raced === 'heartbeat') {
        yield encodeHeartbeat()
        // The backend promise is still in flight; await it next round rather
        // than dropping it (dropping would lose an event).
        next = await pending
      } else {
        next = raced
      }
      if (next.done) break

      if (next.value.t === 'interaction') {
        const interrupts = opts.onInteraction?.() ?? []
        if (interrupts.length) {
          for (const e of translator.interrupt(interrupts)) yield encodeEvent(e)
          ended = true
          return
        }
        continue
      }

      // `RUN_ERROR` is terminal to the verifier — anything after it, including the
      // `RUN_FINISHED` below, is rejected. So an error event ends the run here
      // rather than merely being translated.
      if (next.value.t === 'error') {
        for (const e of translator.error(next.value.message))
          yield encodeEvent(e)
        ended = true
        return
      }

      for (const e of translator.translate(next.value)) yield encodeEvent(e)
      if (opts.signal?.aborted) break
    }
  } catch (e) {
    ended = true
    const message = e instanceof Error ? e.message : String(e)
    for (const ev of translator.error(message)) yield encodeEvent(ev)
  }
  if (!ended) {
    for (const e of translator.finish()) yield encodeEvent(e)
  }
}
