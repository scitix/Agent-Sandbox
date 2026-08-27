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

// The live-turn registry's lifetime policy.
//
// Every case here is one row of the table in turns.ts, and each of them fails
// SILENTLY in production if it regresses: a turn that dies on detach makes the
// question card look answerable while the agent has already moved on; a turn that
// never dies makes a closed tab bill tokens indefinitely. Neither throws.
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AgentEvent } from './backend.ts'
import { AsyncQueue } from './queue.ts'
import { LiveTurnRegistry } from './turns.ts'

/** The production lease is 15 minutes; every timing case here injects a short one
 *  so it asserts the policy rather than the constant. */
const LEASE_MS = 30_000

function makeRegistry() {
  const cancelled: string[] = []
  const registry = new LiveTurnRegistry({
    cancelInteractions: id => cancelled.push(id),
    detachedMaxMs: LEASE_MS,
  })
  return { registry, cancelled }
}

/** Collect `n` events from an attach, then let go. */
async function take(
  registry: LiveTurnRegistry,
  turn: ReturnType<LiveTurnRegistry['start']>,
  cursor: number,
  n: number,
  opts?: { forInterrupt?: boolean }
): Promise<AgentEvent[]> {
  const out: AgentEvent[] = []
  try {
    for await (const e of registry.attach(turn, cursor)) {
      out.push(e)
      if (out.length >= n) break
    }
  } finally {
    registry.releaseReader(turn, opts)
  }
  return out
}

/** Drain an attach to the turn's end. */
async function drain(
  registry: LiveTurnRegistry,
  turn: ReturnType<LiveTurnRegistry['start']>,
  cursor = 0
): Promise<AgentEvent[]> {
  const out: AgentEvent[] = []
  try {
    for await (const e of registry.attach(turn, cursor)) out.push(e)
  } finally {
    registry.releaseReader(turn)
  }
  return out
}

let registries: LiveTurnRegistry[] = []
function fresh() {
  const r = makeRegistry()
  registries.push(r.registry)
  return r
}
afterEach(() => {
  for (const r of registries) r.clear()
  registries = []
  vi.useRealTimers()
})

describe('LiveTurnRegistry', () => {
  it('pumps a whole turn even with nobody attached, then replays it', async () => {
    // The property the whole module exists for: production does not depend on a
    // reader being present.
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'a' })
    q.push({ t: 'text', delta: 'b' })
    q.close()
    // Let the pump run before anyone reads.
    await new Promise(r => setTimeout(r, 0))

    expect(await drain(registry, turn)).toEqual([
      { t: 'text', delta: 'a' },
      { t: 'text', delta: 'b' },
    ])
  })

  it('lets a second reader replay from a cursor while the turn is still live', async () => {
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'one' })
    q.push({ t: 'text', delta: 'two' })

    const first = await take(registry, turn, 0, 2)
    expect(first).toHaveLength(2)
    // The resume reader picks up AFTER what the first one saw.
    const resumeCursor = turn.seq
    q.push({ t: 'text', delta: 'three' })
    q.close()
    expect(await drain(registry, turn, resumeCursor)).toEqual([
      { t: 'text', delta: 'three' },
    ])
  })

  it('keeps the turn alive when a reader detaches for an interrupt', async () => {
    // The zero-symptom bug this guards: ending the response to publish the
    // question must not cancel the question.
    vi.useFakeTimers()
    const { registry, cancelled } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const controller = new AbortController()
    const turn = registry.start('t1', 'alice', controller, q)
    q.push({ t: 'text', delta: 'thinking about it' })
    await vi.advanceTimersByTimeAsync(0)

    await take(registry, turn, 0, 1, { forInterrupt: true })
    registry.markResume(turn, 1)

    // Well past the detached lease: a parked turn is bounded by the park's own
    // timeout, not by this one.
    await vi.advanceTimersByTimeAsync(LEASE_MS * 3)
    expect(controller.signal.aborted).toBe(false)
    expect(cancelled).toEqual([])
    expect(registry.isBusy('t1')).toBe(true)
  })

  it('keeps an unwatched turn running, then ends it with a reason when the lease expires', async () => {
    // BOTH halves matter. Cancelling early is what made "the agent keeps working
    // while you are on another page" false; cancelling silently is what makes an
    // abandoned turn indistinguishable from a finished one on the next attach.
    vi.useFakeTimers()
    const { registry, cancelled } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const controller = new AbortController()
    const turn = registry.start('t1', 'alice', controller, q)
    q.push({ t: 'text', delta: 'hello' })
    await vi.advanceTimersByTimeAsync(0)

    // No `forInterrupt`: a closed tab.
    await take(registry, turn, 0, 1)
    // Well past the OLD 20s grace: the turn is still going.
    await vi.advanceTimersByTimeAsync(LEASE_MS - 1_000)
    expect(controller.signal.aborted).toBe(false)
    expect(registry.isBusy('t1')).toBe(true)
    q.push({ t: 'text', delta: 'still working' })
    await vi.advanceTimersByTimeAsync(0)

    await vi.advanceTimersByTimeAsync(2_000)
    expect(controller.signal.aborted).toBe(true)
    // Anything parked is released too, so a blocked harness unwinds.
    expect(cancelled).toEqual(['t1'])
    // And the log ends with an explanation rather than just stopping.
    expect(turn.log.at(-1)).toEqual({
      t: 'error',
      message: expect.stringContaining('no client attached'),
      retryable: true,
    })
  })

  it('renews the lease when a reader comes back', async () => {
    vi.useFakeTimers()
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const controller = new AbortController()
    const turn = registry.start('t1', 'alice', controller, q)
    q.push({ t: 'text', delta: 'hello' })
    await vi.advanceTimersByTimeAsync(0)
    await take(registry, turn, 0, 1)
    expect(registry.describe('t1')?.detachedSince).not.toBeNull()

    await vi.advanceTimersByTimeAsync(LEASE_MS / 2)
    // Re-attach: the pending cancellation must be called off, not merely delayed.
    const reader = registry.attach(turn, 0)
    await reader.next()
    expect(registry.describe('t1')?.detachedSince).toBeNull()
    await vi.advanceTimersByTimeAsync(LEASE_MS * 3)
    expect(controller.signal.aborted).toBe(false)
    await reader.return(undefined)
    registry.releaseReader(turn)
  })

  it('describes an in-flight turn for a reconnecting client', async () => {
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'a' })
    await new Promise(r => setTimeout(r, 0))

    expect(registry.describe('t1')).toMatchObject({
      inFlight: true,
      seq: 1,
      dropped: 0,
    })
    expect(registry.describe('nope')).toBeNull()
    q.close()
    await new Promise(r => setTimeout(r, 0))
    expect(registry.describe('t1')?.inFlight).toBe(false)
  })

  it('reports busy only while the turn is producing', async () => {
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    registry.start('t1', 'alice', new AbortController(), q)
    expect(registry.isBusy('t1')).toBe(true)
    expect(registry.isBusy('other')).toBe(false)
    q.close()
    await new Promise(r => setTimeout(r, 0))
    expect(registry.isBusy('t1')).toBe(false)
  })

  it('still serves the tail to an attach that arrives after the turn ended', async () => {
    // A resume can race the agent answering its own question. Losing the tail
    // would leave the last words of a turn permanently unrendered.
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'done already' })
    q.close()
    await new Promise(r => setTimeout(r, 0))
    expect(registry.get('t1')).toBeDefined()
    expect(await drain(registry, turn)).toEqual([
      { t: 'text', delta: 'done already' },
    ])
  })

  it('turns a backend that throws mid-turn into a terminal error event', async () => {
    // Without this the browser sees a truncated stream and spins forever.
    const { registry } = fresh()
    async function* boom(): AsyncGenerator<AgentEvent> {
      yield { t: 'text', delta: 'partial' }
      throw new Error('backend died')
    }
    const turn = registry.start('t1', 'alice', new AbortController(), boom())
    expect(await drain(registry, turn)).toEqual([
      { t: 'text', delta: 'partial' },
      { t: 'error', message: 'backend died', retryable: true },
    ])
  })

  it('drops the oldest events on overflow and says how many', async () => {
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    for (let i = 0; i < 5_050; i++) q.push({ t: 'text', delta: String(i) })
    q.close()
    await new Promise(r => setTimeout(r, 0))

    expect(turn.seq).toBe(5_050)
    expect(turn.dropped).toBeGreaterThan(0)
    expect(turn.log.length).toBeLessThanOrEqual(5_000)
    // A cursor pointing into the dropped range must land on the oldest survivor
    // rather than read past the array.
    const replayed = await drain(registry, turn, 0)
    expect(replayed.length).toBe(turn.log.length)
    expect(replayed[0]).toEqual({ t: 'text', delta: String(turn.dropped) })
  })

  it('cancel() releases parked interactions and aborts, even if the backend ignores the signal', async () => {
    const { registry, cancelled } = fresh()
    const controller = new AbortController()
    // A backend that never notices the abort: readers must still be released.
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', controller, q)
    const reader = registry.attach(turn, 0)
    const pending = reader.next()
    registry.cancel('t1')
    expect(controller.signal.aborted).toBe(true)
    expect(cancelled).toEqual(['t1'])
    expect((await pending).done).toBe(true)
    registry.releaseReader(turn)
  })

  it('serves two readers from independent cursors', async () => {
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'x' })
    q.push({ t: 'text', delta: 'y' })
    q.close()
    await new Promise(r => setTimeout(r, 0))
    // Both see everything: the log is shared, the position is not. (The old
    // single-consumer AsyncQueue could not do this — one reader stole the other's
    // events.)
    const [a, b] = await Promise.all([
      drain(registry, turn),
      drain(registry, turn),
    ])
    expect(a).toEqual(b)
    expect(a).toHaveLength(2)
  })

  it('replaces a cached finished turn when a new one starts on the same thread', async () => {
    const { registry } = fresh()
    const first = new AsyncQueue<AgentEvent>()
    registry.start('t1', 'alice', new AbortController(), first)
    first.push({ t: 'text', delta: 'old' })
    first.close()
    await new Promise(r => setTimeout(r, 0))

    const second = new AsyncQueue<AgentEvent>()
    const turn2 = registry.start('t1', 'alice', new AbortController(), second)
    second.push({ t: 'text', delta: 'new' })
    second.close()
    await new Promise(r => setTimeout(r, 0))
    expect(registry.get('t1')).toBe(turn2)
    expect(await drain(registry, turn2)).toEqual([{ t: 'text', delta: 'new' }])
  })
})

describe('thread identity', () => {
  it('re-keys itself when the backend announces the real thread id', async () => {
    // A new conversation starts under the id the BROWSER generated; the gateway's
    // own id arrives mid-turn. Without re-keying, a question parked under the
    // real id would never be found for the turn still filed under the
    // placeholder — and the card would simply never appear.
    const { registry, cancelled } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start(
      'client-placeholder',
      'alice',
      new AbortController(),
      q
    )
    q.push({ t: 'thread', threadId: 'th_real' })
    await new Promise(r => setTimeout(r, 0))

    expect(turn.threadId).toBe('th_real')
    expect(registry.get('th_real')).toBe(turn)
    expect(registry.get('client-placeholder')).toBeUndefined()
    expect(registry.isBusy('th_real')).toBe(true)

    // And cancellation releases parked questions under the REAL id.
    registry.cancel('th_real')
    expect(cancelled).toEqual(['th_real'])
    q.close()
  })
})

describe('resuming after an interrupt', () => {
  it('picks up from the READER position, not the write cursor', async () => {
    // The pump runs ahead of whoever is reading. Marking the interrupt at
    // `turn.seq` instead of the reader's own position drops everything the harness
    // produced between the question and the end of that response — silently, since
    // the resume stream still looks well formed.
    const { registry } = fresh()
    const q = new AsyncQueue<AgentEvent>()
    const turn = registry.start('t1', 'alice', new AbortController(), q)
    q.push({ t: 'text', delta: 'asking' })
    q.push({
      t: 'interaction',
      request: { requestId: 'q1', kind: 'question', questions: [] },
    })
    // Produced while the response was still being written out.
    q.push({ t: 'text', delta: 'meanwhile' })
    await new Promise(r => setTimeout(r, 0))
    expect(turn.seq).toBe(3)

    // A reader that stopped right after the interaction is at position 2.
    registry.markResume(turn, 2)
    q.push({ t: 'text', delta: 'after the answer' })
    q.close()
    expect(await drain(registry, turn, turn.resumeCursor ?? 0)).toEqual([
      { t: 'text', delta: 'meanwhile' },
      { t: 'text', delta: 'after the answer' },
    ])
  })
})
