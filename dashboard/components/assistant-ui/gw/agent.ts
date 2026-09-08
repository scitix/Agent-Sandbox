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

// The AG-UI client agent pointed at our gateway.
//
// `HttpAgent` already speaks the protocol; what it cannot know is the handful of
// fields AG-UI has no slot for — which user, which model, which harness, and which
// page the user was looking at. `requestInit` is the documented seam for that
// ("Override this to customize the request"), and it is called per request, so the
// values can change between sends without rebuilding the agent.
//
// One kind of run is not a send at all: an ATTACH, which carries no prompt and
// asks the gateway to keep streaming a turn that never stopped running (a reload, a
// tab the user came back to, a socket an intermediary dropped). It rides the same
// seam — `attachFrom` becomes `forwardedProps.attach` — precisely so the browser
// reconstructs it with the ordinary `RunAggregator` rather than a second code path
// that has to re-derive messages from events.
//
// It also trims the outgoing message list, which matters more than it looks.
// The runtime posts the ENTIRE transcript on every run — including each tool
// result as its own `role:"tool"` message — while the gateway deliberately ignores
// client history (the harness session is the source of truth for what came
// before). So the untrimmed body is pure waste that grows with the conversation
// until it trips the gateway's 8 MB limit and starts failing sends with a 500 that
// reads like a UI bug.
//
// `state` is emphatically NOT trimmed: it is how per-turn statistics and the
// gateway's thread id round-trip, so dropping it would silently erase the model
// name and token counts from every earlier message.
//
// Finally, a send that loses the race to a turn already in flight is recovered
// HERE, inside the transport, rather than upstream in the runtime's `onError`.
// That is not a preference. `AgUiThreadRuntimeCore` dispatches `RUN_ERROR` into
// its aggregator BEFORE it calls `onError` and then rethrows, so by the time a
// handler learns about the 409 the assistant message is already marked
// `incomplete/error` and `MessagePrimitive.Error` is already rendering
// `HTTP 409: {...}` at the user — with no API to take it back. Below the
// protocol there is no error at all: the 409 response never reaches the parser,
// because the message is handed to the running turn and the same request is
// re-issued as an attach.
import { HttpAgent } from '@ag-ui/client'
import type { HttpAgentConfig, RunAgentInput } from '@ag-ui/client'
import { authHeaders } from '@/components/assistant-ui/gw/client'

/** The per-send context the browser knows and the protocol has no field for. */
export interface GatewayAgentContext {
  userKey: string
  model?: string
  /** Harness for a NEW conversation. Ignored once the thread exists — its backend
   *  is fixed server-side at creation. */
  backend?: string
  /** Evaluated at send time, so the marker names the page the user was actually
   *  on when they pressed send. */
  pageContext?: () => Record<string, string> | undefined
  /** A message that lost the race to a turn already in flight. Its bubble is on
   *  screen with nothing behind it, so the runtime re-runs it once that turn
   *  ends. */
  onDeferred?: (text: string) => void
}

/** The gateway's 409 body for a send that lost the race to a running turn. */
interface TurnInFlightBody {
  code?: string
  threadId?: string
  seq?: number
}

export class GatewayAgent extends HttpAgent {
  /** Mutated in place rather than passed to the constructor: a new agent per
   *  render would reset the abort controller mid-run and drop the runtime's
   *  bookkeeping with it. */
  ctx: GatewayAgentContext = { userKey: 'default' }

  constructor(config: HttpAgentConfig) {
    super(config)
    // `HttpAgent.run` calls `this.fetch(url, this.requestInit(input))` and hands
    // the response straight to the SSE parser, which turns any non-2xx into a
    // thrown error. Wrapping it here is what lets a 409 be answered rather than
    // reported: the parser only ever sees the attach that follows.
    this.fetch = (url, init) => this.fetchRecoveringTurnInFlight(url, init)
  }

  protected requestInit(input: RunAgentInput): RequestInit {
    const base = super.requestInit(input)
    const resuming = Array.isArray(
      (input as RunAgentInput & { resume?: unknown[] }).resume
    )
    const attachFrom = attachIntentOf(input)
    const body = {
      ...input,
      // A resume and an attach carry no new prompt; anything else needs only the
      // newest user message, since the gateway reads exactly that and ignores the
      // rest.
      messages:
        resuming || attachFrom !== null ? [] : lastUserMessage(input.messages),
      forwardedProps: {
        ...(input.forwardedProps as Record<string, unknown> | undefined),
        userKey: this.ctx.userKey,
        ...(this.ctx.model ? { model: this.ctx.model } : {}),
        ...(this.ctx.backend ? { backend: this.ctx.backend } : {}),
        ...(attachFrom !== null ? { attach: true, from: attachFrom } : {}),
        ...(() => {
          const page = this.ctx.pageContext?.()
          return page && Object.keys(page).length ? { pageContext: page } : {}
        })(),
      },
    }
    // The run goes through the same authenticating proxy as the REST calls, so
    // it needs the same header. Read per request rather than captured at
    // construction: this agent lives for the host's lifetime and would
    // otherwise keep sending a token from a previous session.
    return {
      ...base,
      headers: { ...(base.headers ?? {}), ...authHeaders() },
      body: JSON.stringify(body),
    }
  }

  /**
   * Send, and if the conversation turns out to already have a turn in flight,
   * join that turn instead of failing.
   *
   * The composer asks before sending into a running turn, so reaching here means
   * this tab did not know there was one — another tab's turn, or one that started
   * between the check and the send. Two things happen, and the order is the point:
   * the very same request is re-issued as an attach, so what comes back is an
   * ordinary AG-UI run and the runtime reconstructs it the ordinary way; and the
   * message is reported as deferred, because its bubble is already in the
   * transcript with nothing behind it.
   *
   * A 409 that is not this one, and a body that cannot be read, travel on
   * untouched: recovering something we did not understand would be worse.
   *
   * The cursor is 0 — the whole turn, not just the tail. A tab that never saw
   * this turn (a reload, a second tab, a socket that died early) would otherwise
   * be shown an answer starting from the middle with nothing saying so, and a
   * turn shown twice is a smaller lie than a turn shown headless.
   */
  private async fetchRecoveringTurnInFlight(
    url: string,
    init: RequestInit
  ): Promise<Response> {
    const res = await fetch(url, init)
    if (res.status !== 409) return res
    const body = (await res
      .clone()
      .json()
      .catch(() => null)) as TurnInFlightBody | null
    if (body?.code !== 'turn_in_flight') return res
    const sent = parseBody(init.body)
    // An attach that lost to another attach has nothing to hand over and nothing
    // to re-send; retrying it as an attach is still right.
    if (!sent) return res

    // The bubble is already on screen — the runtime appends optimistically and
    // this send failed after that — so the text is reported as DEFERRED and run
    // once the turn it collided with is over. Attachments come along, because
    // what gets re-run is the message itself.
    const prompt = lastUserPrompt(sent.messages)
    if (prompt) this.ctx.onDeferred?.(prompt.text)

    return fetch(url, {
      ...init,
      body: JSON.stringify({
        ...sent,
        messages: [],
        forwardedProps: {
          ...(sent.forwardedProps as Record<string, unknown> | undefined),
          attach: true,
          cursor: 0,
        },
      }),
    })
  }
}

/** The request body we ourselves serialized in `requestInit`, parsed back. */
function parseBody(body: BodyInit | null | undefined): RunAgentInput | null {
  if (typeof body !== 'string') return null
  try {
    return JSON.parse(body) as RunAgentInput
  } catch {
    return null
  }
}

/**
 * The newest user message in an outgoing body, as text plus whether that text is
 * the whole of it.
 *
 * A message carrying attachments arrives as an ARRAY of parts rather than a
 * string — which is why `textOnly` is reported rather than assumed: text-only is
 * the one shape `/deliver` can carry faithfully.
 */
function lastUserPrompt(
  messages: RunAgentInput['messages']
): { text: string; textOnly: boolean } | null {
  const [message] = lastUserMessage(messages)
  const content = message?.content as
    | string
    | { type?: string; text?: string }[]
    | undefined
  if (typeof content === 'string') {
    return content.trim() ? { text: content, textOnly: true } : null
  }
  if (!Array.isArray(content)) return null
  const text = content
    .filter(part => part?.type === 'text')
    .map(part => part.text ?? '')
    .join('\n')
  if (!text.trim()) return null
  return { text, textOnly: content.every(part => part?.type === 'text') }
}

/**
 * What an attach is relative to what this client has already rendered.
 *
 * `'start'` replays the turn from its beginning: a reload, a tab coming back, a
 * socket that died — anything holding none of it. `'resume'` continues after the
 * cut that ended the previous run, which only the run following a mid-turn message
 * can truthfully claim.
 *
 * The POSITION is deliberately not part of this: it is the reader's offset inside
 * the run the gateway cut, which the browser never saw. Sending the intent instead
 * of a number is what keeps the reload path correct by construction.
 */
export type AttachFrom = 'start' | 'resume'

/**
 * The run config that marks a run as an attach, for `thread.resumeRun`.
 *
 * `runConfig.custom` is the library's per-run channel: the core copies it into
 * `forwardedProps.runConfig`, which is the only thing about a run that this agent
 * can read at request time. A mutable flag on the agent would be simpler and wrong
 * — a run that never reaches `fetch` would leave it set, and the next real message
 * would go out as an attach: a send that replays and answers nothing.
 */
export function attachRunConfig(from: AttachFrom = 'start') {
  return { custom: { navixAttach: { from } } }
}

/** How this run should attach, or null for an ordinary send. */
function attachIntentOf(input: RunAgentInput): AttachFrom | null {
  const forwarded = input.forwardedProps as
    | { runConfig?: { navixAttach?: { from?: string } } }
    | undefined
  const from = forwarded?.runConfig?.navixAttach?.from
  return from === 'resume' || from === 'start' ? from : null
}

/** The newest user message, as a one-element list (or none). */
function lastUserMessage(
  messages: RunAgentInput['messages']
): RunAgentInput['messages'] {
  for (let i = (messages?.length ?? 0) - 1; i >= 0; i--) {
    const m = messages[i]
    if (m?.role === 'user') return [m]
  }
  return []
}
