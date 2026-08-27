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

// The OpenCode event subscription is released when the turn ends.
//
// Worth its own test because the failure mode is invisible for an hour and then
// takes the pod down. `event.subscribe()` opens an HTTP connection to a firehose
// that is server-wide: it never completes on its own, and the SDK's SSE client
// re-dials by itself if the socket merely drops. So a turn that does not abort
// its subscription leaks one live stream per turn, every subscriber receives a
// full copy of every event, and the per-event decode work grows with the number
// of turns the pod has ever served — until the gateway's event loop can no
// longer answer its own /healthz and Kubernetes restarts it mid-conversation.
//
// It was observed on manager-test as one extra ESTABLISHED connection to the
// OpenCode server per turn, monotonic, never returned.
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
  new ThreadStore(join(mkdtempSync(join(tmpdir(), 'oc-sub-')), 'threads.json'))

const backend = () =>
  new OpenCodeBackend(
    {
      baseUrl: 'http://127.0.0.1:1',
      readyTimeoutMs: 10,
    } as unknown as ConstructorParameters<typeof OpenCodeBackend>[0],
    store(),
    new InteractionRegistry(50)
  )

/**
 * A firehose that behaves like the real one: it yields whatever it is given and
 * then goes QUIET — parked on the next read forever — rather than ending. Only
 * an abort can free a reader sitting on it, which is the whole point of the
 * test. Reports whether it was ever closed.
 */
function quietFirehose(signal: AbortSignal, first: unknown[] = []) {
  const state = { closed: false }
  const stream = (async function* () {
    try {
      for (const e of first) yield e
      await new Promise<void>((_resolve, reject) => {
        if (signal.aborted) return reject(new Error('aborted'))
        signal.addEventListener('abort', () => reject(new Error('aborted')), {
          once: true,
        })
      })
    } finally {
      state.closed = true
    }
  })()
  return { result: { stream }, state }
}

// `pump` is private to the class; TypeScript's `private` is compile-time only,
// so the test reaches it directly rather than forcing a refactor purely for
// testability.
type LiveRunLike = {
  controller: AbortController
  events: AsyncQueue<AgentEvent>
  pending: { id: string; text: string }[]
  promoted: number
}

/** The turn state the pump reads. Nothing here delivers a mid-turn message, so
 *  the queue is always empty; what matters is the event sink. */
const liveRun = (): LiveRunLike => ({
  controller: new AbortController(),
  events: new AsyncQueue<AgentEvent>(),
  pending: [],
  promoted: 0,
})

type Pumpable = {
  pump: (
    stream: { stream: AsyncIterable<unknown> },
    sessionID: string,
    threadId: string,
    live: LiveRunLike,
    signal: AbortSignal,
    client: unknown,
    subscription: AbortController
  ) => { drain: () => Promise<void> }
}

describe('the OpenCode event subscription', () => {
  it('is aborted when the turn drains, even on a silent server', async () => {
    const subscription = new AbortController()
    const { result, state } = quietFirehose(subscription.signal)
    const pump = (backend() as unknown as Pumpable).pump(
      result,
      'ses_1',
      'th_1',
      liveRun(),
      new AbortController().signal,
      {},
      subscription
    )

    // The server emits nothing further — the reader is parked on the next event.
    // Draining must still finish: if it waited for the stream to go quiet, or
    // relied on a flag only read at the top of the loop, this would hang.
    await expect(
      Promise.race([
        pump.drain().then(() => 'drained'),
        new Promise(r => setTimeout(() => r('timeout'), 2000)),
      ])
    ).resolves.toBe('drained')

    expect(subscription.signal.aborted).toBe(true)
    expect(state.closed).toBe(true)
  })

  it('does not reject when the in-flight read is aborted', async () => {
    // Aborting rejects the read, which surfaces inside the pump task. It has to
    // be swallowed there: an unhandled rejection on every single turn is the
    // kind of thing that takes down a Bun process under load.
    const rejections: unknown[] = []
    const onRejection = (e: unknown) => rejections.push(e)
    process.on('unhandledRejection', onRejection)
    try {
      const subscription = new AbortController()
      const { result } = quietFirehose(subscription.signal)
      const pump = (backend() as unknown as Pumpable).pump(
        result,
        'ses_1',
        'th_1',
        liveRun(),
        new AbortController().signal,
        {},
        subscription
      )
      await pump.drain()
      // Let any unhandled rejection surface before asserting there was none.
      await new Promise(r => setTimeout(r, 50))
      expect(rejections).toEqual([])
    } finally {
      process.off('unhandledRejection', onRejection)
    }
  })

  it('leaves nothing to leak across repeated turns', async () => {
    // The regression, stated the way it was observed: N turns must close N
    // subscriptions.
    const closed: boolean[] = []
    for (let i = 0; i < 5; i++) {
      const subscription = new AbortController()
      const { result, state } = quietFirehose(subscription.signal)
      const pump = (backend() as unknown as Pumpable).pump(
        result,
        `ses_${i}`,
        `th_${i}`,
        liveRun(),
        new AbortController().signal,
        {},
        subscription
      )
      await pump.drain()
      closed.push(state.closed)
    }
    expect(closed).toEqual([true, true, true, true, true])
  })
})
