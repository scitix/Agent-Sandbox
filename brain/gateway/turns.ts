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

// In-flight backend turns, decoupled from the HTTP responses that read them.
//
// WHY THIS EXISTS
//
// AG-UI models human-in-the-loop as "the run ENDS with an interrupt outcome, and
// answering starts a NEW run carrying `resume`". Both our harnesses do the
// opposite: they PARK inside the turn (Claude Code blocks in `canUseTool`,
// OpenCode holds the session while `question.reply` is outstanding). So the
// gateway has to be able to end one HTTP run without ending the backend turn, and
// then let a later request pick the same turn back up.
//
// Before this module, a turn's lifetime WAS its response's lifetime: `/run` wired
// `res.on('close')` straight into the backend's AbortController, and
// `InteractionRegistry.park` resolves `null` on abort. Ending the response to
// publish an interrupt would therefore have cancelled the very question it was
// publishing — a zero-symptom failure, because the card renders, the answer
// "succeeds", and the agent simply proceeds as if nobody replied.
//
// THE SHAPE
//
// One pump per turn drains the backend iterator into a bounded append-only log,
// always, regardless of whether anyone is listening. An HTTP response is just a
// cursor over that log. Two properties follow, and they are the whole point:
//
//   * A turn cannot be killed by a reader going away. Cancellation is a decision
//     this module makes (see the policy table below), not a side effect of socket
//     bookkeeping.
//   * What is buffered is our own `AgentEvent`, not encoded AG-UI frames. Each
//     attach builds a FRESH translator over "log suffix, then live", so protocol
//     validity is structural rather than something the replay path has to
//     remember. `@ag-ui/client` runs a strict verifier and rejects, for example,
//     `RUN_FINISHED` while a tool call is still open — a replay that spliced
//     pre-encoded frames would have to reason about that; this one cannot get it
//     wrong.
//
// The registry holds at most ONE turn per thread. A second concurrent run on the
// same thread is a caller error (409) rather than a second harness turn racing the
// first over one session — but "no reader" is emphatically NOT that error: see the
// detached lease below.
import type { AgentEvent } from './backend.ts'

/** How much of a detached turn's output survives until someone re-attaches.
 *  Generous on purpose: overflowing costs the user visible text, and a turn's
 *  events are small. */
const MAX_LOG_EVENTS = 5_000
const MAX_LOG_BYTES = 4 * 1024 * 1024

/**
 * How long a turn with no reader and nothing parked may keep running.
 *
 * This used to be 20 seconds, which made "the agent keeps working while you are
 * on another page" false for anything but the shortest tool call: a tab switch,
 * a laptop lid, a proxy dropping an idle socket, all landed on a cancelled turn.
 * The window is now long enough to cover a real reconnect (which the browser
 * performs on visibility, on `online`, and on a stream that ended early) while
 * still bounding the one case this guard exists for — a closed tab must not
 * leave a harness billing tokens forever.
 *
 * Expiry is NOT silent: the lease appends a terminal error before cancelling,
 * because a truncated log and a finished turn are indistinguishable to a late
 * reader.
 */
const DETACHED_MAX_MS = Math.max(
  1_000,
  Number(process.env.ASSISTANT_TURN_DETACHED_MAX_MS) || 15 * 60_000
)

/** How long a FINISHED turn is kept so a late attach can still collect the tail
 *  (the resume POST for an interrupt the agent answered itself, say). */
const DONE_TTL_MS = 2 * 60 * 1000

export interface TurnHooks {
  /** Cancel everything parked for this thread. Called on any path that ends the
   *  turn without an answer, so a blocked harness is released rather than left
   *  waiting on a promise nobody will settle. */
  cancelInteractions(threadId: string): void
  /** Override the detached lease. Injectable so a test can assert the timeout
   *  actually fires without waiting out the production value. */
  detachedMaxMs?: number
}

/** A turn's observable state, for the REST surface and the thread listing.
 *  Deliberately not the entry itself: callers get facts, not handles. */
export interface TurnSnapshot {
  inFlight: boolean
  /** Total events produced. A reconnect that has nothing to replay asks for this
   *  rather than 0 (see `dropped`). */
  seq: number
  /** Events the log had to discard. Non-zero means an attach from 0 cannot show
   *  the whole turn, which the UI says out loud rather than quietly omitting. */
  dropped: number
  startedAt: number
  /** When the last reader went away, or null while someone is watching. */
  detachedSince: number | null
}

export interface LiveTurn {
  /**
   * The thread this turn belongs to.
   *
   * NOT readonly: a brand-new conversation starts under the id the browser
   * generated, because the gateway's own id does not exist until the backend
   * mints it and announces it with a `thread` event. The registry re-keys itself
   * at that moment. Anything that looks a turn up by thread id — a resume, the
   * busy check, cancelling parked questions — depends on that, and the failure
   * without it is silent: the question parks under the real id while the turn is
   * still filed under the placeholder, so nothing is ever found to publish.
   */
  threadId: string
  readonly userKey: string
  /** The ONLY thing that cancels this turn. Deliberately not derived from any
   *  request: see the file header. */
  readonly controller: AbortController
  /** Events produced so far, oldest first. Truncated from the front on overflow;
   *  `dropped` says how many are missing. */
  readonly log: AgentEvent[]
  /** Total events ever produced. Exceeds `log.length` once anything is dropped. */
  seq: number
  dropped: number
  /** The iterator is exhausted; no further events will arrive. */
  done: boolean
  /** Log position at the moment a run ended while the turn continued, so the next
   *  one attaches after the question (or after the admitted message) rather than
   *  replaying the whole turn. */
  resumeCursor: number | null
  readonly startedAt: number
}

interface TurnEntry extends LiveTurn {
  readers: number
  bytes: number
  waiters: (() => void)[]
  timer: ReturnType<typeof setTimeout> | null
  detachedSince: number | null
}

function sizeOf(e: AgentEvent): number {
  // Rough and cheap: the log bound exists to stop unbounded growth, not to be an
  // accountant. Only the delta-carrying events are ever large.
  const text =
    ('delta' in e && typeof e.delta === 'string' && e.delta) ||
    ('content' in e && typeof e.content === 'string' && e.content) ||
    ''
  return text.length + 64
}

export class LiveTurnRegistry {
  private readonly turns = new Map<string, TurnEntry>()

  constructor(private readonly hooks: TurnHooks) {}

  /** The turn in flight (or recently finished) for a thread, if any. */
  get(threadId: string): LiveTurn | undefined {
    return this.turns.get(threadId)
  }

  /** Is a NEW run allowed for this thread? False while one is still producing. */
  isBusy(threadId: string): boolean {
    const t = this.turns.get(threadId)
    return !!t && !t.done
  }

  /** What a reconnecting client (or a thread row) needs to know. */
  describe(threadId: string): TurnSnapshot | null {
    const t = this.turns.get(threadId)
    if (!t) return null
    return {
      inFlight: !t.done,
      seq: t.seq,
      dropped: t.dropped,
      startedAt: t.startedAt,
      detachedSince: t.detachedSince,
    }
  }

  /**
   * Begin pumping a backend turn.
   *
   * `events` is consumed here and nowhere else — a reader never touches the
   * iterator, which is what makes "the response ended" and "the turn ended"
   * independent facts.
   */
  start(
    threadId: string,
    userKey: string,
    controller: AbortController,
    events: AsyncIterable<AgentEvent>
  ): LiveTurn {
    // A finished-but-cached entry for the same thread is replaced outright: its
    // only purpose was to let a late reader collect the tail.
    const stale = this.turns.get(threadId)
    if (stale?.timer) clearTimeout(stale.timer)

    const entry: TurnEntry = {
      threadId,
      userKey,
      controller,
      log: [],
      seq: 0,
      dropped: 0,
      done: false,
      resumeCursor: null,
      startedAt: Date.now(),
      readers: 0,
      bytes: 0,
      waiters: [],
      timer: null,
      // A turn begins unwatched: `start` is called before the response attaches,
      // and an auto-triage run has no browser at all. So the lease arms from the
      // outset rather than only after someone has watched and left.
      detachedSince: Date.now(),
    }
    this.turns.set(threadId, entry)
    // Armed before anyone attaches, on purpose: the response that started this
    // turn attaches within the same request, so this never fires spuriously — but
    // a caller that starts a turn and then throws before reading cannot leave an
    // unbounded harness behind either.
    this.armLease(entry)
    void this.pump(entry, events)
    return entry
  }

  /**
   * Read a turn from `cursor` onward: the buffered suffix first, then live events
   * as they arrive, ending when the turn does.
   *
   * `signal` ends the read early — a client that hung up. It has to be a parameter
   * rather than something the caller can achieve with `generator.return()`, because
   * a generator suspended in an `await` does not process a queued return until that
   * await settles, and this one is waiting for an event that may be minutes away
   * (or never come). Without the signal an abandoned reader is never released, the
   * no-reader policy never arms, and the harness keeps generating into a log nobody
   * reads.
   *
   * The caller MUST balance this with `releaseReader` (a `finally` block), or the
   * turn will be treated as still-watched and never time out.
   */
  async *attach(
    turn: LiveTurn,
    cursor: number,
    signal?: AbortSignal
  ): AsyncGenerator<AgentEvent, void, void> {
    const entry = turn as TurnEntry
    entry.readers++
    // Someone came back before the lease expired.
    this.clearLease(entry)
    // Cursors are in `seq` space, so a cursor pointing at dropped events lands on
    // the oldest surviving one rather than reading past the end of the array.
    let i = Math.max(0, cursor - entry.dropped)
    for (;;) {
      if (i < entry.log.length) {
        yield entry.log[i++]
        continue
      }
      if (entry.done || signal?.aborted) return
      await new Promise<void>(resolve => {
        entry.waiters.push(resolve)
        signal?.addEventListener('abort', () => resolve(), { once: true })
      })
      if (signal?.aborted) return
    }
  }

  /** Balance one `attach`. Applies the no-reader policy when the last one goes. */
  releaseReader(turn: LiveTurn, opts: { forInterrupt?: boolean } = {}): void {
    const entry = turn as TurnEntry
    entry.readers = Math.max(0, entry.readers - 1)
    if (entry.readers > 0 || entry.done) return
    if (opts.forInterrupt) {
      // A deliberate detach to publish an interrupt. The harness is blocked on a
      // callback (spending nothing), and `InteractionRegistry` already bounds the
      // wait, so let the park's own timeout be the deadline instead of racing it
      // with a lease.
      entry.detachedSince = Date.now()
      return
    }
    // Nobody is watching, and nothing is parked. The turn KEEPS RUNNING — that is
    // the point — under a lease that a reconnect renews.
    this.armLease(entry)
  }

  /**
   * Mark where the next run should pick up.
   *
   * `cursor` must be the READER's position — one past the event it just handled —
   * not `turn.seq`. The pump runs ahead of the reader, so using `seq` would skip
   * every event the harness produced between that point and the moment the
   * response ended, and the user would simply never see them.
   */
  markResume(turn: LiveTurn, cursor: number): void {
    ;(turn as TurnEntry).resumeCursor = cursor
  }

  /**
   * Stop a turn: the user pressed Stop, the detached lease expired, or the thread
   * is being torn down. Releases anything parked so a blocked harness unwinds.
   */
  cancel(threadId: string): void {
    const entry = this.turns.get(threadId)
    if (!entry) return
    this.hooks.cancelInteractions(threadId)
    entry.controller.abort()
    // The pump's own completion will finalize; finalize here too so a backend
    // that ignores the signal cannot leave readers hanging.
    this.finalize(entry)
  }

  /** Drop everything. For tests and shutdown. */
  clear(): void {
    for (const entry of this.turns.values()) {
      if (entry.timer) clearTimeout(entry.timer)
      entry.controller.abort()
      this.finalize(entry)
    }
    this.turns.clear()
  }

  /** Start (or restart) the unwatched-turn deadline. Idempotent. */
  private armLease(entry: TurnEntry): void {
    if (entry.done) return
    if (entry.timer) clearTimeout(entry.timer)
    entry.detachedSince ??= Date.now()
    entry.timer = setTimeout(() => {
      entry.timer = null
      if (entry.readers > 0 || entry.done) return
      // Say why. A log that simply stops is read as "the answer ended there",
      // and the user has no way to tell a completed turn from an abandoned one.
      this.append(entry, {
        t: 'error',
        message:
          `this turn was stopped after ${Math.round(this.leaseMs() / 1_000)}s ` +
          `with no client attached`,
        retryable: true,
      })
      this.cancel(entry.threadId)
    }, this.leaseMs())
    entry.timer.unref?.()
  }

  private clearLease(entry: TurnEntry): void {
    if (entry.timer) clearTimeout(entry.timer)
    entry.timer = null
    entry.detachedSince = null
  }

  private leaseMs(): number {
    return this.hooks.detachedMaxMs ?? DETACHED_MAX_MS
  }

  private async pump(
    entry: TurnEntry,
    events: AsyncIterable<AgentEvent>
  ): Promise<void> {
    try {
      for await (const event of events) {
        this.append(entry, event)
      }
    } catch (e) {
      // A backend that dies mid-turn still owes its readers a terminal event, or
      // the browser sees a truncated stream and spins forever.
      this.append(entry, {
        t: 'error',
        message: e instanceof Error ? e.message : String(e),
        retryable: true,
      })
    } finally {
      this.finalize(entry)
    }
  }

  private append(entry: TurnEntry, event: AgentEvent): void {
    if (event.t === 'thread' && event.threadId !== entry.threadId) {
      if (this.turns.get(entry.threadId) === entry)
        this.turns.delete(entry.threadId)
      entry.threadId = event.threadId
      this.turns.set(entry.threadId, entry)
    }
    entry.log.push(event)
    entry.seq++
    entry.bytes += sizeOf(event)
    while (
      entry.log.length > MAX_LOG_EVENTS ||
      (entry.bytes > MAX_LOG_BYTES && entry.log.length > 1)
    ) {
      const gone = entry.log.shift()
      if (!gone) break
      entry.bytes -= sizeOf(gone)
      entry.dropped++
    }
    this.wake(entry)
  }

  private finalize(entry: TurnEntry): void {
    if (entry.done) return
    entry.done = true
    this.wake(entry)
    // Keep the finished turn briefly so a resume that raced the ending can still
    // read the tail, then forget it.
    const evict = setTimeout(() => {
      if (this.turns.get(entry.threadId) === entry)
        this.turns.delete(entry.threadId)
    }, DONE_TTL_MS)
    evict.unref?.()
    if (entry.timer) clearTimeout(entry.timer)
    entry.timer = null
  }

  private wake(entry: TurnEntry): void {
    const waiters = entry.waiters.splice(0)
    for (const w of waiters) w()
  }
}
