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

// Telemetry shape and the guarantee that it cannot affect a turn.
//
// The shape matters because both official Langfuse integrations produce
// turn -> generation -> nested tool spans, and the existing dashboards are built
// on it: emitting the same shape is what keeps an A/B comparison between backends
// legible. The isolation matters because a telemetry backend being slow or down
// must never be visible to a user.
import { createServer } from 'node:http'
import type { Server } from 'node:http'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import type { AgentEvent } from './backend.ts'
import { TurnTrace, traceRun } from './telemetry.ts'

let server: Server
let received: { batch: Record<string, unknown>[] }[] = []
let cfg: Parameters<typeof traceRun>[0]

beforeAll(async () => {
  server = createServer((req, res) => {
    const chunks: Buffer[] = []
    req.on('data', c => chunks.push(c as Buffer))
    req.on('end', () => {
      try {
        received.push(
          JSON.parse(Buffer.concat(chunks).toString('utf-8')) as {
            batch: Record<string, unknown>[]
          }
        )
      } catch {
        /* ignore */
      }
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end('{}')
    })
  })
  await new Promise<void>(r => server.listen(0, '127.0.0.1', r))
  const addr = server.address()
  const port = typeof addr === 'object' && addr ? addr.port : 0
  cfg = {
    baseUrl: `http://127.0.0.1:${port}`,
    publicKey: 'pk-lf-test',
    secretKey: 'sk-lf-test',
    environment: 'test',
  }
})

afterAll(async () => {
  await new Promise<void>(r => server.close(() => r()))
})

const EVENTS: AgentEvent[] = [
  { t: 'turn-start' },
  { t: 'thinking', delta: 'let me look' },
  { t: 'tool-start', id: 'tu_1', name: 'mcp__sandbox__bash' },
  {
    t: 'tool-end',
    id: 'tu_1',
    args: { command: 'ls -la /workspace' },
  },
  { t: 'tool-result', id: 'tu_1', content: 'node-1\nnode-2' },
  { t: 'text', delta: 'two nodes' },
  {
    t: 'turn-end',
    usage: { inputTokens: 100, outputTokens: 20, cacheReadTokens: 4000 },
    costUsd: 0.012,
    model: 'claude-sonnet-5',
  },
]

describe('turn trace', () => {
  it('emits trace + generation + one span per tool, correctly nested', async () => {
    received = []
    const trace = new TurnTrace(
      cfg,
      {
        threadId: 'th_abc',
        userKey: 'alice',
        backendId: 'claude-code',
      },
      'how many nodes?'
    )
    for (const e of EVENTS) trace.observe(e)
    await trace.flush()

    expect(received).toHaveLength(1)
    const batch = received[0].batch
    const types = batch.map(e => e.type)
    expect(types).toEqual(['trace-create', 'generation-create', 'span-create'])

    const traceBody = batch[0].body as Record<string, unknown>
    const genBody = batch[1].body as Record<string, unknown>
    const spanBody = batch[2].body as Record<string, unknown>

    // The two identities the dashboards filter on. userId is a real user here,
    // not a directory basename.
    expect(traceBody.sessionId).toBe('th_abc')
    expect(traceBody.userId).toBe('alice')
    expect(traceBody.environment).toBe('test')
    expect(traceBody.output).toBe('two nodes')

    // Model, usage and cost are stated rather than inferred from attributes.
    expect(genBody.model).toBe('claude-sonnet-5')
    expect(genBody.usageDetails).toMatchObject({
      input: 100,
      output: 20,
      cache_read_input_tokens: 4000,
    })
    expect(genBody.costDetails).toEqual({ total: 0.012 })
    // Reasoning is kept alongside the answer rather than dropped.
    expect(String(genBody.output)).toContain('let me look')

    // The tool span hangs off the generation, which is the shape the dashboards
    // expect from both official integrations.
    expect(spanBody.parentObservationId).toBe(genBody.id)
    expect(spanBody.name).toBe('Tool: mcp__sandbox__bash')
    expect(spanBody.output).toBe('node-1\nnode-2')
  })

  it('records an error turn as ERROR without losing the trace', async () => {
    received = []
    const trace = new TurnTrace(
      cfg,
      { threadId: 'th_err', userKey: 'bob', backendId: 'claude-code' },
      'boom?'
    )
    trace.observe({ t: 'error', message: 'model refused', retryable: false })
    await trace.flush()
    const gen = received[0].batch[1].body as Record<string, unknown>
    expect(gen.level).toBe('ERROR')
    expect(gen.statusMessage).toBe('model refused')
  })
})

describe('isolation from the turn', () => {
  it('forwards every event unchanged', async () => {
    received = []
    const seen: AgentEvent[] = []
    for await (const e of traceRun(
      cfg,
      { threadId: 'th_x', userKey: 'alice', backendId: 'claude-code' },
      'hi',
      (async function* () {
        for (const e of EVENTS) yield e
      })()
    )) {
      seen.push(e)
    }
    expect(seen).toEqual(EVENTS)
  })

  it('is a no-op when Langfuse is not configured', async () => {
    const seen: AgentEvent[] = []
    for await (const e of traceRun(
      null,
      { threadId: 'th_y', userKey: 'alice', backendId: 'claude-code' },
      'hi',
      (async function* () {
        yield { t: 'text', delta: 'x' } as AgentEvent
      })()
    )) {
      seen.push(e)
    }
    expect(seen).toHaveLength(1)
  })

  it('survives an unreachable telemetry backend', async () => {
    const dead = {
      baseUrl: 'http://127.0.0.1:1',
      publicKey: 'pk',
      secretKey: 'sk',
    }
    const trace = new TurnTrace(
      dead,
      { threadId: 'th_z', userKey: 'alice', backendId: 'claude-code' },
      'hi'
    )
    trace.observe({ t: 'text', delta: 'x' })
    await expect(trace.flush()).resolves.toBeUndefined()
  })
})
