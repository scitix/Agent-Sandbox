// Parked human-in-the-loop requests.
//
// When the agent asks a question mid-turn, the harness blocks on a callback while
// the browser renders a card. That callback is a promise we hold here until the
// browser answers, so this table is the join between an outbound `interaction`
// event and an inbound POST.
//
// Two failure modes it must not have:
//   * a user who closes the tab must not wedge the turn forever — hence the
//     timeout, which resolves as "unanswered" rather than rejecting, because the
//     agent handles "no answer" fine and a rejection would abort the run;
//   * a late or duplicate answer must be a no-op, not a crash.
//
// The parked REQUEST is kept alongside the promise, not just its id. The SSE
// stream that delivered the question dies with the page, so a browser that
// reloads mid-question would otherwise have a blocked turn it cannot see or
// answer — the listing is how it recovers the card.
import type { InteractionRequest } from './agent-events.ts'

export interface ParkedInteraction {
  requestId: string
  threadId: string
  createdAt: number
  /** The question itself, so a reloaded client can re-render it. */
  request: InteractionRequest
}

/** How long a question may stay unanswered before the turn moves on. */
const DEFAULT_TIMEOUT_MS = 10 * 60 * 1000

type Waiter = {
  resolve: (answers: Record<string, string> | null) => void
  timer: ReturnType<typeof setTimeout>
  threadId: string
  createdAt: number
  request: InteractionRequest
}

export class InteractionRegistry {
  private readonly waiters = new Map<string, Waiter>()

  constructor(private readonly timeoutMs: number = DEFAULT_TIMEOUT_MS) {}

  /**
   * Park a request and return a promise that settles with the user's answers, or
   * `null` if nobody answered in time / the run was cancelled.
   */
  park(
    requestId: string,
    threadId: string,
    request: InteractionRequest,
    signal?: AbortSignal
  ): Promise<Record<string, string> | null> {
    return new Promise(resolve => {
      const settle = (answers: Record<string, string> | null) => {
        const w = this.waiters.get(requestId)
        if (!w) return
        clearTimeout(w.timer)
        this.waiters.delete(requestId)
        resolve(answers)
      }
      const timer = setTimeout(() => settle(null), this.timeoutMs)
      // Node keeps the process alive for a pending timer; a 10-minute question
      // must not hold shutdown.
      timer.unref?.()
      this.waiters.set(requestId, {
        resolve: settle,
        timer,
        threadId,
        createdAt: Date.now(),
        request,
      })
      signal?.addEventListener('abort', () => settle(null), { once: true })
    })
  }

  /** Deliver an answer. Returns false when the request is unknown (already
   *  answered, timed out, or from a previous process). */
  answer(requestId: string, answers: Record<string, string>): boolean {
    const w = this.waiters.get(requestId)
    if (!w) return false
    w.resolve(answers)
    return true
  }

  /** Cancel everything parked for a thread (interrupt, or the client left). */
  cancelThread(threadId: string): number {
    let n = 0
    for (const w of this.waiters.values()) {
      if (w.threadId !== threadId) continue
      w.resolve(null)
      n++
    }
    return n
  }

  pending(): ParkedInteraction[] {
    return [...this.waiters.entries()].map(([requestId, w]) => ({
      requestId,
      threadId: w.threadId,
      createdAt: w.createdAt,
      request: w.request,
    }))
  }

  /**
   * Everything parked for one thread.
   *
   * An AG-UI interrupt outcome must list EVERY open interrupt, because the
   * browser's `submitInterruptResponses` refuses to send unless the caller answers
   * all of them. Reporting only the question that triggered the detach would wedge
   * the composer as soon as a harness parks two at once (OpenCode can).
   */
  byThread(threadId: string): ParkedInteraction[] {
    return this.pending().filter(p => p.threadId === threadId)
  }

  /** Is this request still awaiting an answer? Lets a resume tell "stale" from
   *  "unknown" without trying to settle it. */
  has(requestId: string): boolean {
    return this.waiters.has(requestId)
  }

  get size(): number {
    return this.waiters.size
  }
}
