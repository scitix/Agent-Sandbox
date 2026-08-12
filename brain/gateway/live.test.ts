// S3 acceptance: the whole gateway, end to end, against a real Claude Code
// backend and a stubbed sandbox daemon.
//
// What this proves that the fake-backend tests cannot:
//   * preflight actually validates the endpoint dialect (and fails loudly on a
//     wrong one, which is the failure mode that cost a day on Codex);
//   * a run produces a well-formed event stream with the tool lifecycle in it;
//   * the thread the backend minted is listable and exportable afterwards, i.e.
//     the gateway's id really is bound to a harness session.
//
// Credentials come from dedicated env vars so it never spends a developer's own
// subscription: AGENTBOX_CC_TEST_BASE_URL / AGENTBOX_CC_TEST_TOKEN [/ _MODEL].
//
// The per-user workspace root is a fixed path (USER_DIR_ROOT), not a test knob, so
// running this outside the image needs it to exist and be writable:
// `sudo install -d -o $USER /home/agents/u`.
import { mkdtempSync } from 'node:fs'
import { createServer } from 'node:http'
import type { Server } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import type {
  InteractionRequest,
  ThreadInfo,
  TranscriptEntry,
} from './backend.ts'
import type { AgentBackend } from './backend.ts'
import {
  ClaudeCodeBackend,
  type ClaudeCodeConfig,
} from './backends/claude-code.ts'
import { InteractionRegistry } from './interactions.ts'
import { createGateway } from './server.ts'
import { ThreadStore } from './threads.ts'

/** One backend, in the shape the gateway now takes (it serves a map so the UI
 *  can switch harness). Tests exercise one at a time. */
function asBackends(b: AgentBackend) {
  return {
    backends: new Map([[b.id, b]]),
    defaultBackendId: b.id,
    statuses: [{ id: b.id, available: true }],
  }
}

const BASE_URL = process.env.AGENTBOX_CC_TEST_BASE_URL
const TOKEN = process.env.AGENTBOX_CC_TEST_TOKEN
const MODEL = process.env.AGENTBOX_CC_TEST_MODEL || 'claude-haiku-4-5-20251001'
const LIVE = !!BASE_URL && !!TOKEN

const SANDBOX_FILE = 'gateway-probe-7f31.txt'

let daemon: Server
let gateway: Server
let base: string
let threads: ThreadStore
let backend: ClaudeCodeBackend

beforeAll(async () => {
  if (!LIVE) return
  // Stub sandbox daemon: /healthz for preflight, tool endpoints for the run.
  daemon = createServer((req, res) => {
    const chunks: Buffer[] = []
    req.on('data', c => chunks.push(c as Buffer))
    req.on('end', () => {
      const path = req.url ?? ''
      if (path.startsWith('/healthz')) {
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end('{"ok":true}')
        return
      }
      const endpoint = path.split('/').filter(Boolean).pop() ?? ''
      const reply =
        endpoint === 'bash'
          ? {
              exit_code: 0,
              stdout: `${SANDBOX_FILE}\n`,
              stderr: '',
              cwd: '/home/u/probe',
            }
          : endpoint === 'glob'
            ? { exit_code: 0, paths: [SANDBOX_FILE] }
            : { exit_code: 0, matches: [] }
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(reply))
    })
  })
  await new Promise<void>(r => daemon.listen(0, '127.0.0.1', r))
  const dAddr = daemon.address()
  process.env.SBX_PROXY_URL = `http://127.0.0.1:${typeof dAddr === 'object' && dAddr ? dAddr.port : 0}`

  const root = mkdtempSync(join(tmpdir(), 'agentbox-gw-live-'))
  process.env.CLAUDE_CONFIG_DIR = join(root, 'cc')
  const config: ClaudeCodeConfig = {
    baseURL: BASE_URL,
    authToken: TOKEN,
    models: [{ id: MODEL, name: 'probe model' }],
    defaultModel: MODEL,
    smallModel: MODEL,
    effort: 'low',
  }
  threads = new ThreadStore(join(root, 'threads.json'))
  const interactions = new InteractionRegistry(5_000)
  backend = new ClaudeCodeBackend(config, threads, interactions)

  gateway = createGateway({
    ...asBackends(backend),
    threads,
    interactions,
  })
  await new Promise<void>(r => gateway.listen(0, '127.0.0.1', r))
  const gAddr = gateway.address()
  base = `http://127.0.0.1:${typeof gAddr === 'object' && gAddr ? gAddr.port : 0}`
})

afterAll(async () => {
  if (!LIVE) return
  await new Promise<void>(r => gateway.close(() => r()))
  await new Promise<void>(r => daemon.close(() => r()))
})

/** Read an AG-UI SSE body to completion and return the decoded events. */
async function readStream(res: Response) {
  const text = await res.text()
  const events: Record<string, unknown>[] = []
  for (const line of text.split('\n')) {
    if (!line.startsWith('data: ')) continue
    events.push(JSON.parse(line.slice(6)) as Record<string, unknown>)
  }
  const types = events.map(e => String(e.type))
  return { events, types, raw: text }
}

describe.skipIf(!LIVE)('gateway end to end (live)', () => {
  it('passes preflight against the configured endpoint', async () => {
    await expect(backend.preflight()).resolves.toBeUndefined()
  }, 60_000)

  it('refuses to start when the endpoint speaks the wrong dialect', async () => {
    // The OpenAI-shaped path on the same host: right host, wrong dialect. This is
    // exactly the misconfiguration that must fail at boot rather than mid-turn.
    const wrong = new ClaudeCodeBackend(
      {
        baseURL: `${BASE_URL}/v1/chat`,
        authToken: TOKEN,
        models: [{ id: MODEL, name: 'x' }],
        defaultModel: MODEL,
      },
      threads,
      new InteractionRegistry()
    )
    await expect(wrong.preflight()).rejects.toThrow(/answered \d+/)
  }, 60_000)

  it('streams a run with the tool lifecycle, then lists and exports it', async () => {
    const res = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        threadId: 'th_client_generated',
        runId: 'run_probe',
        state: {},
        tools: [],
        context: [],
        messages: [
          {
            id: 'm0',
            role: 'user',
            content:
              'List the files in the current directory using your shell tool, ' +
              'then reply with just the filename you found.',
          },
        ],
        forwardedProps: {
          userKey: 'probe',
          pageContext: { key: 'node_detail', cluster: 'foo' },
        },
      }),
    })
    expect(res.status).toBe(200)
    expect(res.headers.get('content-type')).toContain('text/event-stream')

    const { events, types } = await readStream(res)

    // The stream is well formed: the run was framed and terminated rather than
    // being cut off, and the real thread id was announced.
    expect(types).toContain('RUN_STARTED')
    expect(types).toContain('RUN_FINISHED')
    expect(types).not.toContain('RUN_ERROR')

    // The tool lifecycle came through, and it went to the sandbox binding.
    const toolStarts = events.filter(e => e.type === 'TOOL_CALL_START')
    expect(toolStarts.length).toBeGreaterThan(0)
    for (const t of toolStarts) {
      expect(String(t.toolCallName)).toMatch(/^mcp__sandbox__/)
    }
    expect(types).toContain('TOOL_CALL_RESULT')

    // The model answered from the sandbox's view of the world.
    const said = events
      .filter(e => e.type === 'TEXT_MESSAGE_CONTENT')
      .map(e => String(e.delta))
      .join('')
    expect(said).toContain(SANDBOX_FILE)

    // The real thread id arrives as agent state, not as a CUSTOM event: the
    // AG-UI runtime drops CUSTOM, and state is what it actually reduces.
    const announced = events
      .filter(e => e.type === 'STATE_SNAPSHOT')
      .map(e => (e.snapshot as { threadId?: string })?.threadId)
      .filter(Boolean)
      .at(-1)
    const threadId = announced as string

    // The gateway id is bound to a harness session, so history works.
    const list = (await (
      await fetch(`${base}/threads?userKey=probe`)
    ).json()) as { threads: ThreadInfo[] }
    expect(list.threads.map(t => t.id)).toContain(threadId)

    const exported = (await (
      await fetch(`${base}/threads/${threadId}/export?userKey=probe`)
    ).json()) as { entries: TranscriptEntry[] }
    expect(exported.entries.length).toBeGreaterThan(0)
    // The page marker was folded into the prompt the agent actually received —
    // asserted on the raw part text, not a JSON dump, so quote escaping in the
    // serialisation cannot make a correct marker look wrong.
    const userText = (
      exported.entries.find(e => e.role === 'user')?.parts ?? []
    )
      .map(p => (p.type === 'text' ? p.text : ''))
      .join('')
    expect(userText).toContain('<page ')
    expect(userText).toContain('key="node_detail"')
    expect(userText).toContain('cluster="foo"')
  }, 300_000)
})

describe.skipIf(!LIVE)('human-in-the-loop round trip (live)', () => {
  // The most intricate seam in the design, and the one the whole live-turn
  // registry exists for: the harness blocks a turn on a permission callback, the
  // gateway ENDS that run with an AG-UI interrupt outcome while the harness stays
  // parked, and a SECOND run carrying `resume` settles the park and rejoins the
  // same turn. Getting it wrong is silent — the tool reports "the user did not
  // answer" and the agent carries on — so this asserts the answer actually
  // reached the model.
  it('ends a run with an interrupt, then resumes it with the answer', async () => {
    const ask = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        threadId: 'th_client_generated',
        runId: 'run_hitl_1',
        state: {},
        tools: [],
        context: [],
        messages: [
          {
            id: 'm0',
            role: 'user',
            content:
              'Use the AskUserQuestion tool to ask me whether I mean the ' +
              'CURRENT cluster or ALL clusters. Do not guess. After I answer, ' +
              'reply with exactly the scope I chose and nothing else.',
          },
        ],
        forwardedProps: { userKey: 'probe-hitl' },
      }),
    })
    expect(ask.status).toBe(200)
    const first = await readStream(ask)

    // The run ended with an interrupt rather than a success, and the card's whole
    // payload rode along — without `metadata.agentbox.questions` the browser would
    // render a question with no options to choose from.
    const finished = first.events.find(e => e.type === 'RUN_FINISHED')
    const outcome = finished?.outcome as {
      type: string
      interrupts?: {
        id: string
        metadata?: { agentbox?: { questions?: InteractionRequest['questions'] } }
      }[]
    }
    expect(outcome?.type).toBe('interrupt')
    const interrupt = outcome.interrupts?.[0]
    expect(interrupt?.id).toBeTruthy()
    const questions = interrupt?.metadata?.agentbox?.questions ?? []
    expect(questions.length).toBeGreaterThan(0)

    // Choose the SECOND option so the reply is distinguishable from a default or
    // a guess. The answer map is keyed by the question text.
    const answers: Record<string, string> = {}
    for (const q of questions)
      answers[q.key] = q.options[1]?.label ?? q.options[0]?.label ?? ''

    const threadId = first.events
      .filter(e => e.type === 'STATE_SNAPSHOT')
      .map(e => (e.snapshot as { threadId?: string })?.threadId)
      .filter(Boolean)
      .at(-1) as string
    expect(threadId).toBeTruthy()

    const resumed = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        threadId,
        runId: 'run_hitl_2',
        state: {},
        tools: [],
        context: [],
        messages: [],
        resume: [
          {
            interruptId: interrupt?.id,
            status: 'resolved',
            payload: { answers },
          },
        ],
        forwardedProps: { userKey: 'probe-hitl' },
      }),
    })
    expect(resumed.status).toBe(200)
    const second = await readStream(resumed)
    const said = second.events
      .filter(e => e.type === 'TEXT_MESSAGE_CONTENT')
      .map(e => String(e.delta))
      .join('')

    // The model saw the selection rather than "not answered", and the second run
    // is the continuation of the SAME turn.
    expect(said.toLowerCase()).toContain('all clusters')
    expect(said).not.toContain('did not answer the questions')
    expect(second.types).not.toContain('RUN_ERROR')
  }, 300_000)
})
