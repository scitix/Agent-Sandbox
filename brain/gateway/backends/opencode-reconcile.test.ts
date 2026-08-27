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

// The answer must survive losing a race it is guaranteed to sometimes lose.
//
// A turn's parts arrive on OpenCode's server-wide event stream — a DIFFERENT
// connection from the one `session.prompt()` answers on — and nothing orders the
// two. When the prompt resolves, the pump aborts that subscription, and anything
// still sitting in the socket is discarded with it. Models that emit their whole
// reply in one burst at the end of the turn hit this window often.
//
// Observed as: reasoning renders, then the run finishes with no text at all,
// while reloading the page shows the complete reply (the transcript is fetched
// over HTTP and never had the race). On the wire it is a stream that goes
// REASONING_MESSAGE_END → STATE_SNAPSHOT → RUN_FINISHED with no
// TEXT_MESSAGE_START anywhere in it.
//
// The fix is not a longer grace period — waiting longer does not decide a race.
// `prompt()`'s own response carries the finished message's parts, so it is
// replayed through the same emitters and the same per-part bookkeeping. These
// tests pin both halves of that: nothing is lost, and nothing is doubled.
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

import type { AgentEvent } from '../backend.ts'
import { InteractionRegistry } from '../interactions.ts'
import { AsyncQueue } from '../queue.ts'
import { ThreadStore } from '../threads.ts'
import { OpenCodeBackend } from './opencode.ts'

const store = () =>
  new ThreadStore(join(mkdtempSync(join(tmpdir(), 'oc-rec-')), 'threads.json'))

const backend = () =>
  new OpenCodeBackend(
    {
      baseUrl: 'http://127.0.0.1:1',
      readyTimeoutMs: 10,
    } as unknown as ConstructorParameters<typeof OpenCodeBackend>[0],
    store(),
    new InteractionRegistry(50)
  )

/** A firehose that yields what it is given, then parks until aborted — the real
 *  one is server-wide and never ends on its own. */
function firehose(signal: AbortSignal, events: unknown[] = []) {
  return {
    stream: (async function* () {
      for (const e of events) yield e
      await new Promise<void>((_resolve, reject) => {
        if (signal.aborted) return reject(new Error('aborted'))
        signal.addEventListener('abort', () => reject(new Error('aborted')), {
          once: true,
        })
      })
    })(),
  }
}

// `pump` is private to the class; TypeScript's `private` is compile-time only,
// so the test reaches it directly rather than forcing a refactor purely for
// testability.
type Pumpable = {
  pump: (
    stream: { stream: AsyncIterable<unknown> },
    sessionID: string,
    threadId: string,
    live: {
      controller: AbortController
      events: AsyncQueue<AgentEvent>
      pending: { id: string; text: string }[]
      promoted: number
    },
    signal: AbortSignal,
    client: unknown,
    subscription: AbortController
  ) => { drain: () => Promise<void>; reconcile: (parts: unknown[]) => void }
}

/** The `message.part.updated` envelope, narrowed to what the pump reads. */
const update = (sessionID: string, part: unknown) => ({
  type: 'message.part.updated',
  properties: { sessionID, part },
})

/** Run one turn: stream `delivered` through the pump, then reconcile against
 *  `final` (what `prompt()` reported), and return every AgentEvent produced. */
async function turn(
  delivered: unknown[],
  final: unknown[]
): Promise<AgentEvent[]> {
  const subscription = new AbortController()
  const events = new AsyncQueue<AgentEvent>()
  const pump = (backend() as unknown as Pumpable).pump(
    firehose(
      subscription.signal,
      delivered.map(p => update('ses_1', p))
    ),
    'ses_1',
    'th_1',
    { controller: new AbortController(), events, pending: [], promoted: 0 },
    new AbortController().signal,
    {},
    subscription
  )
  await pump.drain()
  pump.reconcile(final)
  events.close()
  const out: AgentEvent[] = []
  for await (const e of events) out.push(e)
  return out
}

const text = (id: string, body: string) => ({ type: 'text', id, text: body })
const reasoning = (id: string, body: string) => ({
  type: 'reasoning',
  id,
  text: body,
})

describe('reconciling a turn against the prompt response', () => {
  it('emits text the event stream never delivered', async () => {
    // The exact production failure: reasoning made it through, the answer did
    // not. Before the reconcile pass this produced a `thinking` event and
    // nothing else, and the user watched a run finish with no reply in it.
    const out = await turn(
      [reasoning('prt_r', 'Let me check the clusters.')],
      [reasoning('prt_r', 'Let me check the clusters.'), text('prt_t', 'Four.')]
    )
    expect(out).toEqual([
      { t: 'thinking', delta: 'Let me check the clusters.' },
      { t: 'text', delta: 'Four.' },
    ])
  })

  it('emits only the tail of text that arrived half-streamed', async () => {
    // The partial case, which a naive "re-send the final part" would duplicate.
    const out = await turn(
      [text('prt_t', 'Four cl')],
      [text('prt_t', 'Four clusters.')]
    )
    expect(out).toEqual([
      { t: 'text', delta: 'Four cl' },
      { t: 'text', delta: 'usters.' },
    ])
  })

  it('adds nothing when the stream already delivered everything', async () => {
    // The common path, and the one that must stay silent: reconciling is not
    // allowed to make a healthy turn say anything twice.
    const out = await turn(
      [text('prt_t', 'Four'), text('prt_t', 'Four clusters.')],
      [text('prt_t', 'Four clusters.')]
    )
    expect(out).toEqual([
      { t: 'text', delta: 'Four' },
      { t: 'text', delta: ' clusters.' },
    ])
  })

  it('recovers a tool call the stream missed, once', async () => {
    const tool = {
      type: 'tool',
      id: 'prt_x',
      callID: 'call_1',
      tool: 'platform_list',
      state: { status: 'completed', input: { kind: 'node' }, output: 'ok' },
    }
    expect(await turn([], [tool])).toEqual([
      { t: 'tool-start', id: 'call_1', name: 'platform_list' },
      { t: 'tool-end', id: 'call_1', args: { kind: 'node' } },
      { t: 'tool-result', id: 'call_1', content: 'ok' },
    ])
  })

  it('does not repeat a tool call the stream already reported', async () => {
    const tool = {
      type: 'tool',
      id: 'prt_x',
      callID: 'call_1',
      tool: 'platform_list',
      state: { status: 'completed', input: { kind: 'node' }, output: 'ok' },
    }
    expect(await turn([tool], [tool])).toEqual([
      { t: 'tool-start', id: 'call_1', name: 'platform_list' },
      { t: 'tool-end', id: 'call_1', args: { kind: 'node' } },
      { t: 'tool-result', id: 'call_1', content: 'ok' },
    ])
  })

  it('puts exactly one divider where a compaction happened', async () => {
    // Compaction carries no content to diff, so "already sent" has to be
    // remembered explicitly — otherwise the reconcile pass (or simply a second
    // update of the same part) draws a second divider in the conversation.
    const part = { type: 'compaction', id: 'prt_c', auto: true }
    expect(await turn([part, part], [part])).toEqual([
      { t: 'compaction', auto: true },
    ])
  })
})
