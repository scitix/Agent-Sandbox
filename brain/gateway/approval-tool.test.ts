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

import { describe, expect, it } from 'vitest'
import type { AgentEvent } from './agent-events.ts'
import {
  DECISION_APPROVE_ONCE,
  DECISION_DENY,
  DECISION_KEY,
  parseApprovalTrailer,
  requestApproval,
} from './approval-tool.ts'
import { InteractionRegistry } from './interactions.ts'
import { AsyncQueue } from './queue.ts'

// Verbatim from the CLI's own renderer. Pinned as a fixture because this is the
// contract between two languages: nothing in TypeScript breaks when the Python
// trailer changes wording, and the only symptom would be an agent that stops
// being able to ask.
const REFUSAL = [
  'error: approval required: Create an environment',
  'approval:',
  '  Create an environment (t1)',
  '  https://console.example.com/agentbox/clusters/foo/approvals?id=apr_931d62ab185ac98d',
  '  (once, or for this whole session)',
  'hint:',
  '  # Show the link above and END YOUR TURN. Do NOT run the',
  '  abx approvals wait apr_931d62ab185ac98d',
].join('\n')

const DESTRUCTIVE = REFUSAL.replace(
  '  (once, or for this whole session)',
  '  (once)'
)

describe('parseApprovalTrailer', () => {
  it('reads every field the card needs out of the printed trailer', () => {
    const ask = parseApprovalTrailer(REFUSAL)
    expect(ask).toEqual({
      approvalId: 'apr_931d62ab185ac98d',
      cluster: 'foo',
      operation: 'Create an environment',
      summary: 'Create an environment (t1)',
      onceOnly: false,
    })
  })

  it('marks a one-call-only operation, so the card hides the wider scope', () => {
    expect(parseApprovalTrailer(DESTRUCTIVE)?.onceOnly).toBe(true)
  })

  it('falls back to the configured cluster when there is no console link', () => {
    const noLink = REFUSAL.replace(/ *https:\/\/\S+\n/, '')
    expect(parseApprovalTrailer(noLink, 'bar')?.cluster).toBe('bar')
  })

  it('is null for a failure that is not an approval', () => {
    expect(parseApprovalTrailer('error: env "t1" already exists')).toBeNull()
  })
})

/** A tool run with a scripted sandbox. */
function harness(exits: number[], output = REFUSAL) {
  const calls: string[] = []
  const events = new AsyncQueue<AgentEvent>()
  const interactions = new InteractionRegistry(5_000)
  const deps = {
    threadId: 'th_1',
    events,
    interactions,
    signal: new AbortController().signal,
    exec: async (command: string) => {
      calls.push(command)
      const code = exits[calls.length - 1] ?? 0
      return {
        exit_code: code,
        stdout: code === 0 ? 'created env t1' : output,
        stderr: '',
        cwd: '/home/user',
      }
    },
  }
  return { deps, calls, events, interactions }
}

/** The requestId the tool just announced on the event stream. */
async function announced(events: AsyncQueue<AgentEvent>): Promise<string> {
  for await (const e of events) {
    if (e.t === 'interaction') return e.request.requestId
  }
  throw new Error('no interaction was announced')
}

describe('request_approval', () => {
  it('runs the command again once the person approves', async () => {
    const { deps, calls, events, interactions } = harness([3, 0])
    const running = requestApproval(deps, 'abx envs create --name t1')
    const id = await announced(events)
    expect(
      interactions.answer(id, { [DECISION_KEY]: DECISION_APPROVE_ONCE })
    ).toBe(true)

    const text = await running
    // Twice: once to find out it is gated, once to do it. The first attempt is
    // what produces the approval id, so it cannot be skipped.
    expect(calls).toEqual([
      'abx envs create --name t1',
      'abx envs create --name t1',
    ])
    expect(text).toContain('Approved. The command has run.')
    expect(text).toContain('created env t1')
  })

  it('does not run the command when the person says no', async () => {
    const { deps, calls, events, interactions } = harness([3, 0])
    const running = requestApproval(deps, 'abx envs delete t1')
    interactions.answer(await announced(events), {
      [DECISION_KEY]: DECISION_DENY,
    })

    const text = await running
    expect(calls).toHaveLength(1)
    expect(text).toContain('NOT approved')
  })

  it('treats an unanswered card as a refusal', async () => {
    const { deps, calls, events, interactions } = harness([3, 0])
    const running = requestApproval(deps, 'abx envs delete t1')
    await announced(events)
    // What a closed tab or a cancelled run does.
    interactions.cancelThread(deps.threadId)

    expect(await running).toContain('NOT approved')
    expect(calls).toHaveLength(1)
  })

  it('carries the approval to the card, which is the only party that can decide', async () => {
    const { deps, events, interactions } = harness([3, 0])
    const running = requestApproval(deps, 'abx envs create --name t1')
    let seen: unknown
    for await (const e of events) {
      if (e.t === 'interaction') {
        seen = e.request.approval
        interactions.answer(e.request.requestId, {
          [DECISION_KEY]: DECISION_APPROVE_ONCE,
        })
        break
      }
    }
    await running
    expect(seen).toMatchObject({
      approvalId: 'apr_931d62ab185ac98d',
      cluster: 'foo',
      command: 'abx envs create --name t1',
    })
  })

  it('offers no session scope for an operation the platform only grants once', async () => {
    const { deps, events, interactions } = harness([3, 0], DESTRUCTIVE)
    const running = requestApproval(deps, 'abx envs delete t1')
    let labels: string[] = []
    for await (const e of events) {
      if (e.t === 'interaction') {
        labels = (e.request.questions[0]?.options ?? []).map(o => o.label)
        interactions.answer(e.request.requestId, {
          [DECISION_KEY]: DECISION_APPROVE_ONCE,
        })
        break
      }
    }
    await running
    expect(labels).toEqual([DECISION_APPROVE_ONCE, DECISION_DENY])
  })

  it('asks nobody when the command was not gated at all', async () => {
    const { deps, calls } = harness([0])
    const text = await requestApproval(deps, 'abx envs')
    expect(calls).toHaveLength(1)
    expect(text).toContain('needed no approval')
  })

  it('does not park on a failure that has nothing to do with approval', async () => {
    const { deps, calls } = harness([1], 'error: env "t1" already exists')
    const text = await requestApproval(deps, 'abx envs create --name t1')
    expect(calls).toHaveLength(1)
    expect(text).toContain('did not fail for want of approval')
  })

  it('explains a second refusal rather than looking like a broken approval', async () => {
    // The retry produced a NEW approval id: what ran was not what was granted.
    const { deps, events, interactions } = harness([3, 3])
    const running = requestApproval(deps, 'abx envs create --name t1')
    interactions.answer(await announced(events), {
      [DECISION_KEY]: DECISION_APPROVE_ONCE,
    })
    expect(await running).toContain('DIFFERENT request')
  })
})
