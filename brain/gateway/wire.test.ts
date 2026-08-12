// Wire framing — AG-UI, validated against the protocol's own verifier.
//
// The browser reconstructs messages with `@assistant-ui/react-ag-ui` over
// `@ag-ui/client`, which pipes every frame through `verifyEvents()` and turns a
// violation into RUN_ERROR — a red bubble where the answer should be. So the
// assertion that matters most here is not about our bytes, it is `expectValid`:
// every fixture stream is replayed through the REAL verifier. Two of its rules are
// easy to break and impossible to notice from the server side:
//
//   * RUN_FINISHED is rejected while a text message or tool call is still open,
//     and an interrupt ends a run MID-turn;
//   * TOOL_CALL_ARGS/END are rejected for a call that did not start in this run,
//     which is exactly what the backend emits after an interrupt force-closed one.
import { verifyEvents } from '@ag-ui/client'
import { EventType } from '@ag-ui/core'
import type { BaseEvent } from '@ag-ui/core'
import { COMPACTION_TOOL_NAME } from './agent-events.ts'
import { lastValueFrom, of, toArray } from 'rxjs'
import { describe, expect, it } from 'vitest'

import type { AgentEvent } from './backend.ts'
import { type AgentState, mergeStats, streamRun } from './wire.ts'

async function* from(events: AgentEvent[]): AsyncGenerator<AgentEvent> {
  for (const e of events) yield e
}

interface Parsed {
  events: Record<string, unknown>[]
  heartbeats: number
}

function parse(frames: string[]): Parsed {
  const events: Record<string, unknown>[] = []
  let heartbeats = 0
  for (const frame of frames) {
    if (frame.startsWith(': ping')) {
      heartbeats++
      continue
    }
    // The AG-UI encoder emits `data: {...}` frames.
    for (const line of frame.split('\n')) {
      if (!line.startsWith('data: ')) continue
      events.push(JSON.parse(line.slice('data: '.length)))
    }
  }
  return { events, heartbeats }
}

const RUN = { threadId: 't1', runId: 'r1', messageId: 'm1' }

async function run(
  events: AgentEvent[],
  opts: Partial<Parameters<typeof streamRun>[1]> = {}
): Promise<Parsed> {
  const frames: string[] = []
  for await (const f of streamRun(from(events), { ...RUN, ...opts }))
    frames.push(f)
  return parse(frames)
}

const types = (p: Parsed) => p.events.map(e => e.type)

/**
 * Replay a stream through the client's verifier.
 *
 * This is the guard the whole file exists for: it fails on exactly the protocol
 * violations that would reach a user as an error bubble instead of an answer.
 */
async function expectValid(p: Parsed): Promise<void> {
  const source = of(...(p.events as unknown as BaseEvent[]))
  await expect(
    lastValueFrom(source.pipe(verifyEvents(false), toArray()))
  ).resolves.toHaveLength(p.events.length)
}

describe('streamRun', () => {
  it('brackets a run and streams text as one START/CONTENT/END message', async () => {
    const p = await run([
      { t: 'turn-start' },
      { t: 'text', delta: 'Hel' },
      { t: 'text', delta: 'lo' },
      { t: 'turn-end', model: 'claude-sonnet-5' },
    ])
    await expectValid(p)
    expect(types(p)).toEqual([
      EventType.RUN_STARTED,
      EventType.TEXT_MESSAGE_START,
      EventType.TEXT_MESSAGE_CONTENT,
      EventType.TEXT_MESSAGE_CONTENT,
      EventType.TEXT_MESSAGE_END,
      EventType.STATE_SNAPSHOT,
      EventType.RUN_FINISHED,
    ])
    // Every delta of one run of text shares the id, and the FIRST text message
    // uses the pre-allocated id — that is what makes the browser adopt it and so
    // what lets the state below key stats by a message id both sides agree on.
    const ids = p.events
      .filter(e => String(e.type).startsWith('TEXT_MESSAGE'))
      .map(e => e.messageId)
    expect(new Set(ids)).toEqual(new Set(['m1']))
  })

  it('reports the assistant message id even when the run opens with a tool call', async () => {
    // The browser adopts the first id it sees from EITHER TEXT_MESSAGE_START or
    // TOOL_CALL_START.parentMessageId. Without seeding the parent id, a
    // tool-call-first run would report nothing and its stats would be orphaned.
    const p = await run([
      { t: 'tool-start', id: 'c1', name: 'read' },
      { t: 'tool-args', id: 'c1', delta: '{"p":1}' },
      { t: 'tool-end', id: 'c1' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    const start = p.events.find(e => e.type === EventType.TOOL_CALL_START)
    expect(start?.parentMessageId).toBe('m1')
  })

  it('gives a post-tool run of text a FRESH id, so it cannot jump above the tool card', async () => {
    // Reusing the first id makes the browser append the new paragraph to the
    // pre-tool text part, which renders it before the tool it came after.
    const p = await run([
      { t: 'text', delta: 'before' },
      { t: 'tool-start', id: 'c1', name: 'read' },
      { t: 'tool-end', id: 'c1' },
      { t: 'text', delta: 'after' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    const starts = p.events
      .filter(e => e.type === EventType.TEXT_MESSAGE_START)
      .map(e => e.messageId)
    expect(starts).toHaveLength(2)
    expect(starts[0]).toBe('m1')
    expect(starts[1]).not.toBe('m1')
  })

  it('closes an open text message before a tool call', async () => {
    const p = await run([
      { t: 'text', delta: 'let me look' },
      { t: 'tool-start', id: 'c1', name: 'read' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    expect(types(p).indexOf(EventType.TEXT_MESSAGE_END)).toBeLessThan(
      types(p).indexOf(EventType.TOOL_CALL_START)
    )
  })

  it('uses the non-deprecated REASONING_* events for thinking', async () => {
    const p = await run([
      { t: 'thinking', delta: 'hmm' },
      { t: 'text', delta: 'ok' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    expect(types(p)).toContain(EventType.REASONING_MESSAGE_START)
    expect(types(p)).toContain(EventType.REASONING_MESSAGE_END)
    expect(types(p)).not.toContain(EventType.THINKING_TEXT_MESSAGE_START)
  })

  it('closes open text AND tool calls before an interrupt outcome', async () => {
    // The rule that bites: OpenCode asks for permission BEFORE the tool runs, so
    // at interrupt time there is an open tool call. RUN_FINISHED with one still
    // open is rejected outright.
    const p = await run(
      [
        { t: 'text', delta: 'I need to ask' },
        { t: 'tool-start', id: 'c1', name: 'write' },
        {
          t: 'interaction',
          request: { requestId: 'q1', kind: 'question', questions: [] },
        },
        { t: 'text', delta: 'never reached' },
      ],
      {
        onInteraction: () => [{ id: 'q1', reason: 'input_required' }],
      }
    )
    await expectValid(p)
    expect(types(p)).toEqual([
      EventType.RUN_STARTED,
      EventType.TEXT_MESSAGE_START,
      EventType.TEXT_MESSAGE_CONTENT,
      // A tool call cannot interleave with an open text message.
      EventType.TEXT_MESSAGE_END,
      EventType.TOOL_CALL_START,
      // …and the call itself is force-closed before the outcome, which is the
      // rule an interrupt is most likely to break.
      EventType.TOOL_CALL_END,
      EventType.RUN_FINISHED,
    ])
    const finished = p.events.at(-1)
    expect(finished?.runId).toBe('r1') // an empty runId makes the client DROP the event
    expect(finished?.outcome).toEqual({
      type: 'interrupt',
      interrupts: [{ id: 'q1', reason: 'input_required' }],
    })
  })

  it('keeps streaming when an interaction has nothing parked to publish', async () => {
    const p = await run(
      [
        {
          t: 'interaction',
          request: { requestId: 'q1', kind: 'question', questions: [] },
        },
        { t: 'text', delta: 'carry on' },
      ],
      {
        onInteraction: () => [],
      }
    )
    await expectValid(p)
    expect(types(p)).toContain(EventType.TEXT_MESSAGE_CONTENT)
    expect(p.events.at(-1)?.outcome).toEqual({ type: 'success' })
  })

  it('drops the tail of a tool call this run never started', async () => {
    // The resume run replays from after the question, so the force-closed call's
    // trailing args/end arrive with no matching START. Passing them through is a
    // verifier violation; dropping them is not.
    const p = await run([
      { t: 'tool-args', id: 'closed-earlier', delta: '{"x":1}' },
      { t: 'tool-end', id: 'closed-earlier' },
      { t: 'text', delta: 'continuing' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    expect(types(p)).not.toContain(EventType.TOOL_CALL_ARGS)
    expect(types(p)).not.toContain(EventType.TOOL_CALL_END)
  })

  it('still forwards a RESULT for a tool call this run never started', async () => {
    // The counterpart of the rule above, and deliberate: the runtime routes a
    // result for a call it does not own back to the message holding it, which is
    // how the pre-interrupt tool card gets its output. Re-opening the call would
    // duplicate the card instead.
    const p = await run([
      { t: 'tool-result', id: 'closed-earlier', content: 'done' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    const result = p.events.find(e => e.type === EventType.TOOL_CALL_RESULT)
    expect(result?.toolCallId).toBe('closed-earlier')
  })

  it('carries turn statistics as agent state keyed by the message id', async () => {
    const p = await run([
      { t: 'text', delta: 'hi' },
      {
        t: 'turn-end',
        model: 'claude-sonnet-5',
        costUsd: 0.0012,
        usage: { inputTokens: 10, outputTokens: 4 },
      },
    ])
    await expectValid(p)
    const snap = p.events.find(e => e.type === EventType.STATE_SNAPSHOT)
      ?.snapshot as AgentState
    expect(snap.stats?.['m1']).toEqual({
      model: 'claude-sonnet-5',
      costUsd: 0.0012,
      usage: { inputTokens: 10, outputTokens: 4 },
    })
  })

  it('keeps the stats the client echoed back, so earlier turns do not lose theirs', async () => {
    const p = await run([{ t: 'turn-end', model: 'new' }], {
      state: { stats: { older: { model: 'old' } } },
    })
    const snap = p.events.find(e => e.type === EventType.STATE_SNAPSHOT)
      ?.snapshot as AgentState
    expect(snap.stats?.older).toEqual({ model: 'old' })
    expect(snap.stats?.['m1']).toEqual({ model: 'new' })
  })

  it('reports the gateway thread id through agent state', async () => {
    // The browser needs it: for a new conversation the id it generated was a
    // placeholder, and it must talk to the gateway's from then on.
    const p = await run([
      { t: 'thread', threadId: 'real-id' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    const snap = p.events.find(e => e.type === EventType.STATE_SNAPSHOT)
      ?.snapshot as AgentState
    expect(snap.threadId).toBe('real-id')
  })

  it('translates a notice to nothing at all', async () => {
    // No consumer in the UI. Stated here so "it does not show up" is a tested
    // fact rather than something inferred from a missing renderer.
    const p = await run([
      { t: 'notice', level: 'info', text: 'context compacted' },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    expect(types(p)).toEqual([
      EventType.RUN_STARTED,
      EventType.STATE_SNAPSHOT,
      EventType.RUN_FINISHED,
    ])
  })

  it('puts a compaction in the transcript as a complete synthetic tool call', async () => {
    // AG-UI has no event for "a boundary happened here", and the runtime's
    // aggregator materialises only text, reasoning and tool calls — so this is
    // how the divider gets to sit in the right PLACE. It must arrive complete:
    // an unclosed call fails the verifier at RUN_FINISHED, and one without a
    // result renders as a tool still running.
    const p = await run([
      { t: 'text', delta: 'before' },
      { t: 'compaction', auto: true },
      { t: 'turn-end' },
    ])
    await expectValid(p)
    expect(types(p)).toEqual([
      EventType.RUN_STARTED,
      EventType.TEXT_MESSAGE_START,
      EventType.TEXT_MESSAGE_CONTENT,
      // The open text message is closed first: a tool call may not interleave.
      EventType.TEXT_MESSAGE_END,
      EventType.TOOL_CALL_START,
      EventType.TOOL_CALL_ARGS,
      EventType.TOOL_CALL_END,
      EventType.TOOL_CALL_RESULT,
      EventType.STATE_SNAPSHOT,
      EventType.RUN_FINISHED,
    ])
    const start = p.events.find(e => e.type === EventType.TOOL_CALL_START)
    expect(start?.toolCallName).toBe(COMPACTION_TOOL_NAME)
    const args = p.events.find(e => e.type === EventType.TOOL_CALL_ARGS)
    expect(JSON.parse(args?.delta as string)).toEqual({ auto: true })
  })

  it('reports a manual compaction as not automatic', async () => {
    const p = await run([{ t: 'compaction', auto: false }, { t: 'turn-end' }])
    await expectValid(p)
    const args = p.events.find(e => e.type === EventType.TOOL_CALL_ARGS)
    expect(JSON.parse(args?.delta as string)).toEqual({ auto: false })
  })

  it('turns a mid-stream error into a renderable RUN_ERROR and stops', async () => {
    // A silent truncation shows as a spinner forever. And nothing may follow
    // RUN_ERROR — the verifier rejects it, RUN_FINISHED included.
    const p = await run([
      { t: 'text', delta: 'partial' },
      { t: 'error', message: 'backend died', retryable: true },
      { t: 'text', delta: 'unreachable' },
    ])
    await expectValid(p)
    expect(types(p)).toEqual([
      EventType.RUN_STARTED,
      EventType.TEXT_MESSAGE_START,
      EventType.TEXT_MESSAGE_CONTENT,
      EventType.TEXT_MESSAGE_END,
      EventType.RUN_ERROR,
    ])
  })

  it('emits a heartbeat while the backend is quiet, invisibly to the protocol', async () => {
    async function* slow(): AsyncGenerator<AgentEvent> {
      await new Promise(r => setTimeout(r, 30))
      yield { t: 'text', delta: 'eventually' }
    }
    const frames: string[] = []
    for await (const f of streamRun(slow(), { ...RUN, heartbeatMs: 5 }))
      frames.push(f)
    const p = parse(frames)
    expect(p.heartbeats).toBeGreaterThan(0)
    // The SSE reader collects only `data:` lines, so a comment frame cannot
    // appear as an event.
    await expectValid(p)
  })

  it('produces a complete empty run for a resume with nothing to rejoin', async () => {
    // After a restart the browser has already dropped its interrupts and has a
    // run in flight; a well-formed empty run is the only thing that leaves the
    // thread able to send again.
    const p = await run([])
    await expectValid(p)
    expect(types(p)).toEqual([EventType.RUN_STARTED, EventType.RUN_FINISHED])
  })
})

describe('mergeStats', () => {
  it('bounds the map so the round-tripped state cannot grow forever', () => {
    // The client echoes this back on EVERY run, so an unbounded map is a request
    // body that grows without limit for output nobody looks at.
    let state: AgentState | undefined
    for (let i = 0; i < 60; i++)
      state = mergeStats(state, `m${i}`, { model: `model-${i}` })
    expect(Object.keys(state?.stats ?? {})).toHaveLength(50)
    expect(state?.stats?.['m59']).toEqual({ model: 'model-59' })
    expect(state?.stats?.['m0']).toBeUndefined()
  })

  it('keeps a re-reported message newest rather than evicting it', () => {
    let state = mergeStats(undefined, 'keep', { model: 'first' })
    for (let i = 0; i < 49; i++)
      state = mergeStats(state, `m${i}`, { model: `m${i}` })
    state = mergeStats(state, 'keep', { model: 'second' })
    state = mergeStats(state, 'one-more', { model: 'x' })
    expect(state.stats?.keep).toEqual({ model: 'second' })
  })
})
