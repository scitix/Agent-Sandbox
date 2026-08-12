// Gateway HTTP contract.
//
// Driven by a fake backend, so this runs with no credentials and no network and
// still covers the things a frontend depends on: capability negotiation, thread
// CRUD, the run stream's framing, the interaction round trip, and — the one with
// security weight — that one user cannot address another user's thread.
import { mkdtempSync } from 'node:fs'
import type { Server } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import type {
  AgentBackend,
  AgentEvent,
  BackendCapabilities,
  BackendId,
  ModelInfo,
  RunRequest,
  ThreadInfo,
  TranscriptEntry,
} from './backend.ts'
import { InteractionRegistry } from './interactions.ts'
import { AsyncQueue } from './queue.ts'
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

const CAPS: BackendCapabilities = {
  interaction: true,
  threadList: true,
  fork: true,
  rename: true,
  compaction: false,
  transcriptExport: true,
  reasoningStream: true,
}

/** Records what it was asked to do and replays a scripted event stream. */
class FakeBackend implements AgentBackend {
  readonly capabilities = CAPS
  readonly sandboxing = 'mcp' as const
  script: AgentEvent[] = [{ t: 'turn-start' }, { t: 'turn-end' }]
  lastRun?: RunRequest
  interrupted: string[] = []
  answered: { requestId: string; answers: Record<string, string> }[] = []
  answerFails = false
  /** The last thread this backend minted, so a resume can address it. */
  lastCreatedThreadId?: string
  /** Keep the turn open after the script runs out, so a concurrent run sees a
   *  turn in flight. `release()` lets it finish. */
  hold = false
  private releaseHold: (() => void) | null = null
  release(): void {
    this.releaseHold?.()
    this.releaseHold = null
  }

  /** Set when the script parks a question: the promise the harness is blocked on,
   *  so a test can assert it actually unblocks. */
  parked?: Promise<Record<string, string> | null>
  /** The queue of the turn still in flight, so `answer` can resume it. */
  /** Not private: a test pushes the rest of a turn into it, which is what the
   *  harness does after the gateway cut the browser's run at a promotion. */
  open?: AsyncQueue<AgentEvent>

  constructor(
    private readonly threads: ThreadStore,
    private readonly interactions: InteractionRegistry,
    /** Which harness this double impersonates — the gateway serves several. */
    readonly id: BackendId = 'claude-code'
  ) {}

  async preflight(): Promise<void> {}
  async models(): Promise<ModelInfo[]> {
    return [{ id: 'claude-sonnet-5', name: 'Claude Sonnet 5' }]
  }
  async listThreads(userKey: string): Promise<ThreadInfo[]> {
    // Own threads only, like the real backends: the gateway stamps each answer
    // with the answering backend's id, so a backend that returns another
    // harness's threads puts every conversation in the list twice.
    return this.threads.list(userKey, this.id).map(r => this.threads.toInfo(r))
  }
  async createThread(userKey: string, title?: string): Promise<string> {
    const id = this.threads.create(userKey, this.id, title).id
    this.lastCreatedThreadId = id
    return id
  }
  async forkThread(userKey: string, threadId: string): Promise<string> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) throw new Error('unknown thread')
    return this.threads.create(userKey, this.id, ref.title).id
  }
  async renameThread(
    userKey: string,
    threadId: string,
    title: string
  ): Promise<void> {
    if (!this.threads.getForUser(threadId, userKey))
      throw new Error('unknown thread')
    this.threads.update(threadId, { title })
  }
  async deleteThread(userKey: string, threadId: string): Promise<void> {
    if (this.threads.getForUser(threadId, userKey))
      this.threads.remove(threadId)
  }
  readonly exported: string[] = []
  /** Which key each export was performed AS. The read-only viewer's correctness
   *  turns on this being the thread's owner rather than the caller. */
  readonly exportedFor: { userKey: string; threadId: string }[] = []
  /** The `until` each export was asked for, so the "do not replay an in-flight
   *  turn as history" wiring is observable. */
  readonly exportedUntil: (number | undefined)[] = []
  async exportThread(
    userKey: string,
    threadId: string,
    opts: { until?: number } = {}
  ): Promise<TranscriptEntry[]> {
    this.exported.push(threadId)
    this.exportedFor.push({ userKey, threadId })
    this.exportedUntil.push(opts.until)
    return [{ role: 'user', parts: [{ type: 'text', text: 'hi' }] }]
  }
  run(req: RunRequest): AsyncIterable<AgentEvent> {
    this.lastRun = req
    const q = new AsyncQueue<AgentEvent>()
    // Creating the thread is the BACKEND's job (it needs its own session first),
    // and the id reaches the client as a `thread` event rather than in a
    // response body — the run is already streaming by then.
    if (!req.threadId) {
      const created = this.threads.create(req.userKey, this.id)
      this.lastCreatedThreadId = created.id
      q.push({ t: 'thread', threadId: created.id })
    }
    // The thread the harness is actually working in, which is what it parks
    // questions under — for a new conversation this is the id it just minted, NOT
    // the placeholder the browser sent.
    const workingThread = req.threadId ?? (this.lastCreatedThreadId as string)
    // The harness session belongs to the FIRST RUN, never to `createThread` —
    // both real backends work this way, and it is what makes `started` (and so
    // "does this conversation belong in the history list") mean anything.
    this.threads.update(workingThread, {
      backendThreadId: `sess_${workingThread}`,
    })
    let parks = false
    for (const e of this.script) {
      if (e.t === 'interaction') {
        // Park exactly as a harness does: block inside the turn, resolved out of
        // band when the answer arrives.
        parks = true
        this.parked = this.interactions.park(
          e.request.requestId,
          workingThread,
          e.request
        )
      }
      q.push(e)
    }
    // Held open for anything that pushes into a turn after `run` returned: an
    // answer to a parked question, or a message delivered mid-turn.
    this.open = q
    if (this.hold) {
      this.releaseHold = () => q.close()
    } else if (!parks) {
      q.close()
    }
    return q
  }
  async interrupt(threadId: string): Promise<void> {
    this.interrupted.push(threadId)
  }
  async answer(
    requestId: string,
    answers: Record<string, string>
  ): Promise<void> {
    if (this.answerFails) throw new Error('not awaiting an answer')
    this.answered.push({ requestId, answers })
    // A real harness settles the park (that is what unblocks `canUseTool` /
    // `question.reply`) and then keeps producing. Both halves matter: without the
    // first the turn stays parked forever, without the second the resume stream
    // never ends.
    this.interactions.answer(requestId, answers)
    this.open?.push({ t: 'text', delta: 'continuing' })
    this.open?.push({ t: 'turn-end' })
    this.open?.close()
  }
}

let server: Server
let base: string
let backend: FakeBackend
let threads: ThreadStore
// Long enough that a test can park, list and answer without racing the timeout;
// nothing here exercises the expiry path.
let interactions: InteractionRegistry

beforeAll(async () => {
  const dir = mkdtempSync(join(tmpdir(), 'agentbox-gw-'))
  threads = new ThreadStore(join(dir, 'threads.json'))
  interactions = new InteractionRegistry(5_000)
  backend = new FakeBackend(threads, interactions)
  server = createGateway({
    ...asBackends(backend),
    threads,
    interactions,
    // Short enough that the abandoned-run test can observe the lease expiring.
    detachedMaxMs: 50,
  })
  await new Promise<void>(r => server.listen(0, '127.0.0.1', r))
  const addr = server.address()
  const port = typeof addr === 'object' && addr ? addr.port : 0
  base = `http://127.0.0.1:${port}`
})

afterAll(async () => {
  await new Promise<void>(r => server.close(() => r()))
})

const get = (p: string) => fetch(`${base}${p}`)
const post = (p: string, body?: unknown) =>
  fetch(`${base}${p}`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })

describe('capability negotiation', () => {
  it('reports the backend id and its capabilities', async () => {
    const r = await get('/capabilities')
    expect(r.status).toBe(200)
    expect(await r.json()).toEqual({
      backendId: 'claude-code',
      capabilities: CAPS,
      defaultBackendId: 'claude-code',
    })
  })

  // The harness picker. Reporting a backend the pod cannot serve — WITH the
  // reason — is the point: a silently absent entry looks like the feature was
  // never built, while a greyed-out one with "no credential" is actionable.
  it('lists every backend the deployment could run, available or not', async () => {
    const r = await get('/backends')
    expect(r.status).toBe(200)
    expect(await r.json()).toEqual({
      backends: [{ id: 'claude-code', available: true }],
      defaultBackendId: 'claude-code',
    })
  })

  // The safety property behind the picker: a conversation is pinned to the
  // harness that created it, so switching cannot replay one agent's history
  // through another.
  it('keeps a thread on its own backend even when another is requested', async () => {
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    const r = await get(
      `/threads/${created.threadId}/export?userKey=alice&backend=opencode`
    )
    expect(r.status).toBe(200)
    // Served by the thread's own (only) backend rather than erroring on the
    // unknown request, because the picker is a convenience, not a gate.
    expect(backend.exported).toContain(created.threadId)
  })

  // Everything a multi-harness deployment shows per row (which logo, which
  // model list) hangs off this stamp, and only the gateway can supply it: a
  // backend listing its own threads has no reason to name itself.
  it('stamps each listed thread with the backend that owns it', async () => {
    const created = (await (
      await post('/threads', { userKey: 'stamped' })
    ).json()) as { threadId: string }
    const body = (await (await get('/threads?userKey=stamped')).json()) as {
      threads: (ThreadInfo & { backendId?: string })[]
    }
    const row = body.threads.find(t => t.id === created.threadId)
    expect(row?.backendId).toBe('claude-code')
  })

  it('serves the configured model list', async () => {
    const body = (await (await get('/models')).json()) as {
      models: ModelInfo[]
    }
    expect(body.models[0].id).toBe('claude-sonnet-5')
  })
})

describe('threads', () => {
  it('creates, lists, renames, forks, exports and deletes', async () => {
    const created = (await (
      await post('/threads', { userKey: 'alice', title: 'first' })
    ).json()) as { threadId: string }
    expect(created.threadId).toMatch(/^th_[0-9a-f]{16}$/)

    const list = (await (await get('/threads?userKey=alice')).json()) as {
      threads: ThreadInfo[]
    }
    expect(list.threads.map(t => t.id)).toContain(created.threadId)

    const renamed = await fetch(`${base}/threads/${created.threadId}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ userKey: 'alice', title: 'renamed' }),
    })
    expect(renamed.status).toBe(200)
    expect(threads.get(created.threadId)?.title).toBe('renamed')

    const forked = (await (
      await post(`/threads/${created.threadId}/fork`, { userKey: 'alice' })
    ).json()) as { threadId: string }
    expect(forked.threadId).not.toBe(created.threadId)

    const md = await get(
      `/threads/${created.threadId}/export?format=md&userKey=alice`
    )
    expect(md.headers.get('content-type')).toContain('text/markdown')
    expect(await md.text()).toContain('## user')

    const del = await fetch(`${base}/threads/${created.threadId}`, {
      method: 'DELETE',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ userKey: 'alice' }),
    })
    expect(del.status).toBe(200)
    expect(threads.get(created.threadId)).toBeUndefined()
  })

  // The history list is "conversations", not "rows we happened to allocate". A
  // thread exists from the moment the user clicks New, but the harness session —
  // and therefore anything anyone said — only exists after the first run.
  it('reports a thread as unstarted until its first run', async () => {
    const created = (await (
      await post('/threads', { userKey: 'starter' })
    ).json()) as { threadId: string }
    const before = (await (await get('/threads?userKey=starter')).json()) as {
      threads: ThreadInfo[]
    }
    const idle = before.threads.find(t => t.id === created.threadId)
    expect(idle?.started).toBe(false)
    // Nothing has started, so nothing is waiting to be named either.
    expect(idle?.titlePending).toBe(false)

    backend.script = [{ t: 'text', delta: 'hi' }, { t: 'turn-end' }]
    await post(
      '/run',
      agUiRun({ userKey: 'starter', threadId: created.threadId, text: 'hello' })
    )

    const after = (await (await get('/threads?userKey=starter')).json()) as {
      threads: ThreadInfo[]
    }
    const row = after.threads.find(t => t.id === created.threadId)
    expect(row?.started).toBe(true)
    // Started and still nameless: the UI shows a spinner and waits for the push.
    expect(row?.titlePending).toBe(true)
  })

  // Titles arrive after the fact (a separate model call in every harness), so the
  // browser is told rather than asked. Renaming is the same channel, which is why
  // it is what this asserts.
  it('pushes thread changes to subscribers', async () => {
    const created = (await (
      await post('/threads', { userKey: 'watcher' })
    ).json()) as { threadId: string }
    const abort = new AbortController()
    const stream = await fetch(`${base}/threads/events?userKey=watcher`, {
      signal: abort.signal,
    })
    expect(stream.headers.get('content-type')).toContain('text/event-stream')
    const reader = (stream.body as ReadableStream<Uint8Array>).getReader()

    await fetch(`${base}/threads/${created.threadId}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ userKey: 'watcher', title: 'pushed' }),
    })

    // Heartbeats and partial frames are both possible; read until the update
    // lands or the stream goes quiet.
    const decoder = new TextDecoder()
    let seen = ''
    while (!seen.includes('"pushed"')) {
      const { value, done } = await reader.read()
      if (done) break
      seen += decoder.decode(value, { stream: true })
    }
    abort.abort()
    const frame = seen
      .split('\n')
      .find(l => l.startsWith('data:') && l.includes('"pushed"'))
    expect(frame).toBeDefined()
    const payload = JSON.parse(frame!.slice('data:'.length)) as {
      type: string
      thread: ThreadInfo
    }
    expect(payload.type).toBe('updated')
    expect(payload.thread.id).toBe(created.threadId)
    expect(payload.thread.title).toBe('pushed')
  })

  it('does not leak another user in the list', async () => {
    await post('/threads', { userKey: 'bob', title: 'bob thread' })
    const list = (await (await get('/threads?userKey=alice')).json()) as {
      threads: ThreadInfo[]
    }
    for (const t of list.threads) {
      expect(threads.get(t.id)?.userKey).toBe('alice')
    }
  })
})

/** What AG-UI's HttpAgent posts. The gateway reads only the last user message
 *  and `forwardedProps`; everything else is the client's copy of history. */
function agUiRun(opts: {
  userKey?: string
  text?: string | string[]
  threadId?: string
  pageContext?: Record<string, string>
  model?: string
  /** Rejoin the turn in flight instead of starting one. */
  attach?: boolean
  cursor?: number
}) {
  const texts =
    opts.text === undefined
      ? ['hi']
      : Array.isArray(opts.text)
        ? opts.text
        : [opts.text]
  return {
    threadId: opts.threadId ?? 'th_client_generated',
    runId: 'run_1',
    state: {},
    tools: [],
    context: [],
    messages: texts.map((content, i) => ({
      id: `m${i}`,
      role: 'user' as const,
      content,
    })),
    forwardedProps: {
      ...(opts.userKey ? { userKey: opts.userKey } : {}),
      ...(opts.pageContext ? { pageContext: opts.pageContext } : {}),
      ...(opts.model ? { model: opts.model } : {}),
      ...(opts.attach ? { attach: true } : {}),
      ...(opts.cursor !== undefined ? { cursor: opts.cursor } : {}),
    },
  }
}

/** The AG-UI event types of one SSE response, in order. */
function sseTypes(body: string): string[] {
  const out: string[] = []
  for (const line of body.split('\n')) {
    if (!line.startsWith('data: ')) continue
    const event = JSON.parse(line.slice('data: '.length)) as { type?: string }
    if (event.type) out.push(event.type)
  }
  return out
}

describe('run stream', () => {
  it('streams sse and reports a new thread id', async () => {
    backend.script = [
      { t: 'turn-start' },
      { t: 'text', delta: 'hello' },
      { t: 'turn-end', costUsd: 0.01 },
    ]
    const res = await post(
      '/run',
      agUiRun({ userKey: 'alice', text: 'hi there' })
    )
    expect(res.status).toBe(200)
    expect(res.headers.get('content-type')).toContain('text/event-stream')
    const text = await res.text()
    expect(text).toContain('"RUN_STARTED"')
    expect(text).toContain('"TEXT_MESSAGE_CONTENT"')
    expect(text).toContain('"RUN_FINISHED"')
    // The client's thread id is one we have never seen, so this is a new
    // conversation: the backend minted the real id and announced it through
    // AGENT STATE. Not CUSTOM — the AG-UI runtime silently drops CUSTOM events,
    // whereas state is reduced, exposed via useAgUiState, and echoed back.
    expect(backend.lastRun?.threadId).toBeNull()
    expect(backend.lastRun?.userKey).toBe('alice')
    expect(text).toContain('"STATE_SNAPSHOT"')
    expect(text).toMatch(/"threadId":"th_[0-9a-f]{16}"/)
  })

  it('passes the page context through for the marker', async () => {
    await post(
      '/run',
      agUiRun({
        userKey: 'alice',
        text: 'why is this stuck?',
        pageContext: { key: 'node_detail', cluster: 'foo', id: 'gpu-7' },
      })
    )
    expect(backend.lastRun?.pageContext).toEqual({
      key: 'node_detail',
      cluster: 'foo',
      id: 'gpu-7',
    })
  })

  it('rejects an empty input rather than starting a turn', async () => {
    expect(
      (await post('/run', agUiRun({ userKey: 'alice', text: '' }))).status
    ).toBe(400)
    expect(
      (await post('/run', agUiRun({ userKey: 'alice', text: '   \n ' }))).status
    ).toBe(400)
    expect(
      (await post('/run', agUiRun({ userKey: 'alice', text: [] }))).status
    ).toBe(400)
  })

  // Security-relevant: another user's thread must be indistinguishable from one
  // that never existed, so a caller cannot probe for other users' ids. Under
  // AG-UI the client mints thread ids, so "unknown" is the normal case for a new
  // conversation — both therefore START A NEW THREAD rather than 404, which
  // keeps them indistinguishable AND never resumes someone else's history.
  it('treats another user thread exactly like a missing one: a new thread', async () => {
    const bobs = (await (
      await post('/threads', { userKey: 'bob' })
    ).json()) as { threadId: string }
    const stolen = await post(
      '/run',
      agUiRun({ userKey: 'alice', threadId: bobs.threadId })
    )
    const missing = await post(
      '/run',
      agUiRun({ userKey: 'alice', threadId: 'th_0000000000000000' })
    )
    expect(stolen.status).toBe(200)
    expect(missing.status).toBe(200)
    // Neither resumed anything: the backend was asked for a NEW thread, so
    // bob's history is never continued under alice.
    expect(backend.lastRun?.threadId).toBeNull()
    // RUN_STARTED echoes the id ALICE sent (the protocol requires a threadId on
    // every run frame, and she already knew it), but the thread she actually
    // gets is a freshly minted one announced through agent state. Read the id
    // off the snapshot, not off RUN_STARTED — the latter is the echo, the former
    // is what the tab actually binds to.
    const minted = /"snapshot":\{"threadId":"(th_[0-9a-f]{16})"/.exec(
      await stolen.text()
    )
    expect(minted?.[1]).toBeDefined()
    expect(minted?.[1]).not.toBe(bobs.threadId)
  })
})

describe('human-in-the-loop over AG-UI interrupts', () => {
  // The shape: the run ENDS with `outcome.interrupt`, and the answer arrives as
  // `resume` on the NEXT run, which rejoins the same still-parked backend turn.
  // The property that makes it work is that ending the response does not cancel
  // the turn — before the live-turn registry, publishing the question killed it.
  it('ends the run with an interrupt outcome, then settles it from a resume', async () => {
    const req = {
      requestId: 'int_flow',
      kind: 'question' as const,
      questions: [
        {
          key: 'Which scope?',
          question: 'Which scope?',
          options: [{ label: 'One cluster' }, { label: 'All clusters' }],
        },
      ],
    }
    // The backend parks mid-turn, exactly as a harness does, and keeps the turn
    // open afterwards.
    backend.script = [
      { t: 'text', delta: 'I need to ask something' },
      { t: 'interaction', request: req },
    ]

    const first = await post('/run', agUiRun({ userKey: 'alice', text: 'go' }))
    const firstText = await first.text()
    expect(first.status).toBe(200)
    // The outcome, not a CUSTOM event: this is what puts the assistant message
    // into `requires-action` and surfaces the card via `useAgUiInterrupts`.
    expect(firstText).toContain('"interrupt"')
    expect(firstText).toContain('int_flow')
    // The question array has to survive the trip or the card has no options to
    // render, and `key` has to survive or the answer lands under the wrong one.
    expect(firstText).toContain('Which scope?')
    expect(firstText).toContain('All clusters')

    const threadId = backend.lastCreatedThreadId as string
    const resumed = await post('/run', {
      ...agUiRun({ userKey: 'alice', threadId, text: 'ignored on a resume' }),
      resume: [
        {
          interruptId: 'int_flow',
          status: 'resolved',
          payload: { answers: { 'Which scope?': 'All clusters' } },
        },
      ],
    })
    expect(resumed.status).toBe(200)
    // The resume REJOINS the same turn rather than starting a new one, so the
    // harness's continuation arrives on this second response.
    const resumedText = await resumed.text()
    expect(resumedText).toContain('continuing')
    expect(resumedText).toContain('"RUN_FINISHED"')
    expect(backend.answered[0]).toEqual({
      requestId: 'int_flow',
      answers: { 'Which scope?': 'All clusters' },
    })
    // And the harness actually unblocks, which is the whole point.
    expect(await backend.parked).toEqual({ 'Which scope?': 'All clusters' })
  })

  it('treats a cancelled resume as an empty answer, which is how a decline reads', async () => {
    const req = {
      requestId: 'int_decline',
      kind: 'permission' as const,
      questions: [{ key: 'Allow?', question: 'Allow?', options: [] }],
    }
    const parked = interactions.park('int_decline', 'th_decline', req)
    await post('/run', {
      ...agUiRun({ userKey: 'alice', text: 'x' }),
      resume: [{ interruptId: 'int_decline', status: 'cancelled' }],
    })
    expect(backend.answered.at(-1)).toEqual({
      requestId: 'int_decline',
      answers: {},
    })
    expect(await parked).toEqual({})
  })

  it('answers a resume for an interrupt nobody is waiting on with a valid EMPTY run', async () => {
    // After a gateway restart the browser still holds the card, has already
    // cleared it locally, and has a run in flight. A 4xx here leaves the thread
    // unable to send anything ever again, so this must be a well-formed run.
    const res = await post('/run', {
      ...agUiRun({ userKey: 'alice', text: 'x' }),
      resume: [{ interruptId: 'never_existed', status: 'cancelled' }],
    })
    expect(res.status).toBe(200)
    const text = await res.text()
    expect(text).toContain('"RUN_STARTED"')
    expect(text).toContain('"RUN_FINISHED"')
    expect(text).not.toContain('"RUN_ERROR"')
  })

  it('lists a parked question WITH its payload, so a reload can re-render it', async () => {
    const req = {
      requestId: 'int_listing',
      kind: 'question' as const,
      questions: [
        {
          key: 'Which scope?',
          question: 'Which scope?',
          options: [{ label: 'One cluster' }, { label: 'All clusters' }],
        },
      ],
    }
    const parked = interactions.park('int_listing', 'th_x', req)

    const listed = (await (await fetch(`${base}/interactions`)).json()) as {
      pending: { request: { questions: { options: unknown[] }[] } }[]
    }
    // Answers no longer come through here, but this listing does: the browser
    // persists interrupts in its own message metadata, and after a restart those
    // are unanswerable. Hydration cross-checks against this and drops what is
    // gone, or the user gets a card that can never be dismissed.
    expect(listed.pending).toHaveLength(1)
    expect(listed.pending[0].request.questions[0].options).toHaveLength(2)

    interactions.answer('int_listing', { 'Which scope?': 'All clusters' })
    expect(await parked).toEqual({ 'Which scope?': 'All clusters' })
  })
})

describe('an abandoned run', () => {
  it('releases the reader when the client hangs up mid-turn', async () => {
    // The leaking case is a QUIET stream: writes to a dead response silently
    // succeed, so if nothing happens to be written after the socket dies, nothing
    // notices the hang-up. The reader is then never released, the registry's
    // no-reader policy never arms, and the harness keeps generating into a log
    // nobody reads — a token bill with no symptom. Hence the script is empty here:
    // the run opens and then says nothing, exactly like a long tool call.
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    backend.script = []
    backend.hold = true

    const abort = new AbortController()
    const inflight = fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(
        agUiRun({ userKey: 'alice', threadId: created.threadId, text: 'go' })
      ),
      signal: abort.signal,
    })
    const opened = await inflight
    // Drain what there is, so no write is left in flight to trip over the abort.
    const reader = opened.body?.getReader()
    await reader?.read()

    // While it is being read, the thread is busy.
    expect(
      (
        await post(
          '/run',
          agUiRun({ userKey: 'alice', threadId: created.threadId, text: 'x' })
        )
      ).status
    ).toBe(409)

    abort.abort()
    // Once the reader is gone the grace window (shortened for this suite) elapses
    // and the turn is cancelled. The observable proof is that the same thread
    // accepts a new run again.
    await new Promise(r => setTimeout(r, 200))
    backend.hold = false
    backend.script = [{ t: 'text', delta: 'ok' }, { t: 'turn-end' }]
    const again = await post(
      '/run',
      agUiRun({ userKey: 'alice', threadId: created.threadId, text: 'retry' })
    )
    expect(again.status).toBe(200)
    await again.text()
  })
})

describe('one turn per thread', () => {
  it('rejects a second concurrent run on the same conversation, recoverably', async () => {
    // Two tabs sending at once used to start two harness turns over one session,
    // where only the last cancellation handle survived. The `code` is what makes
    // this a signal rather than a dead end: without it the browser showed an error
    // for a conversation that was working perfectly, and had no way back to the
    // stream it had lost.
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    backend.hold = true
    const first = post(
      '/run',
      agUiRun({ userKey: 'alice', threadId: created.threadId, text: 'one' })
    )
    // Give the first run time to register its turn.
    await new Promise(r => setTimeout(r, 20))
    const second = await post(
      '/run',
      agUiRun({ userKey: 'alice', threadId: created.threadId, text: 'two' })
    )
    expect(second.status).toBe(409)
    expect(await second.json()).toMatchObject({
      code: 'turn_in_flight',
      threadId: created.threadId,
    })
    backend.release()
    await (await first).text()
    backend.hold = false
  })
})

describe('rejoining a turn the browser lost its stream to', () => {
  /** Start a held turn and read its first frames, exactly as a tab that is then
   *  going to disappear would. */
  async function startHeldTurn(threadId: string, script: AgentEvent[]) {
    backend.script = script
    backend.hold = true
    const abort = new AbortController()
    const res = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(agUiRun({ userKey: 'alice', threadId, text: 'go' })),
      signal: abort.signal,
    })
    const reader = res.body?.getReader()
    await reader?.read()
    return { abort, reader }
  }

  it('reports whether a turn is in flight, and where it is', async () => {
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    const before = (await (
      await get(`/threads/${created.threadId}/live`)
    ).json()) as { live: { inFlight: boolean } }
    expect(before.live.inFlight).toBe(false)

    const { abort } = await startHeldTurn(created.threadId, [
      { t: 'text', delta: 'thinking' },
    ])
    const during = (await (
      await get(`/threads/${created.threadId}/live`)
    ).json()) as { live: { inFlight: boolean; seq: number } }
    expect(during.live.inFlight).toBe(true)
    expect(during.live.seq).toBeGreaterThan(0)

    abort.abort()
    backend.release()
    backend.hold = false
    await new Promise(r => setTimeout(r, 150))
  })

  it('replays the turn so far and then follows it live', async () => {
    // The whole point: the harness never stopped, so what the tab missed is still
    // in the log and the rest is still coming.
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    const { abort } = await startHeldTurn(created.threadId, [
      { t: 'text', delta: 'first half' },
    ])
    // The tab goes away mid-turn. The turn does NOT.
    abort.abort()
    await new Promise(r => setTimeout(r, 20))
    expect(
      (
        (await (await get(`/threads/${created.threadId}/live`)).json()) as {
          live: { inFlight: boolean }
        }
      ).live.inFlight
    ).toBe(true)

    const rejoined = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(
        agUiRun({
          userKey: 'alice',
          threadId: created.threadId,
          attach: true,
          cursor: 0,
        })
      ),
    })
    expect(rejoined.status).toBe(200)
    // Produced AFTER the reattach, to prove this is a live tail and not a replay.
    setTimeout(() => {
      backend.release()
      backend.hold = false
    }, 30)
    const body = await rejoined.text()
    expect(body).toContain('first half')
    expect(sseTypes(body)).toContain('RUN_FINISHED')
    // No new turn was started: the attach carries no prompt.
    expect(backend.lastRun?.input.map(p => p.text)).toEqual(['go'])
  })

  it('answers an attach for a turn that already finished with a valid empty run', async () => {
    // The race is normal: the browser asks, the turn ends, the request lands. An
    // error here would leave a thread that cannot send, since the client already
    // has a run in flight.
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    backend.script = [{ t: 'turn-end' }]
    backend.hold = false
    const res = await fetch(`${base}/run`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(
        agUiRun({ userKey: 'alice', threadId: created.threadId, attach: true })
      ),
    })
    expect(res.status).toBe(200)
    expect(sseTypes(await res.text())).toEqual(['RUN_STARTED', 'RUN_FINISHED'])
  })

  it('exports only completed history while a turn is in flight', async () => {
    // Otherwise a reload renders the running turn twice: once as history, once as
    // the stream still producing it.
    const created = (await (
      await post('/threads', { userKey: 'alice' })
    ).json()) as { threadId: string }
    const { abort } = await startHeldTurn(created.threadId, [
      { t: 'text', delta: 'mid' },
    ])
    await get(`/threads/${created.threadId}/export?userKey=alice`)
    expect(backend.exportedUntil.at(-1)).toBeTypeOf('number')

    abort.abort()
    backend.release()
    backend.hold = false
    await new Promise(r => setTimeout(r, 150))
    await get(`/threads/${created.threadId}/export?userKey=alice`)
    // Nothing running: the whole transcript is history again.
    expect(backend.exportedUntil.at(-1)).toBeUndefined()
  })
})

describe('interrupt and health', () => {
  it('forwards an interrupt', async () => {
    const t = (await (await post('/threads', { userKey: 'alice' })).json()) as {
      threadId: string
    }
    const r = await post(`/threads/${t.threadId}/interrupt`, {
      userKey: 'alice',
    })
    expect(r.status).toBe(200)
    expect(backend.interrupted).toContain(t.threadId)
  })

  it('answers healthz', async () => {
    expect((await get('/healthz')).status).toBe(200)
  })

  it('404s an unknown route', async () => {
    expect((await get('/nope')).status).toBe(404)
  })
})

describe('a deployment where no harness can serve', () => {
  // The pod stays up and says why. It also serves the workspace-fs server, which
  // touches no model, and the entrypoint kills every child together when one
  // exits — so refusing to start would turn one bad model credential into a
  // CrashLoopBackOff that also takes down attachment upload and the file browser,
  // with the reason visible only in a log.
  const REASONS = [
    {
      id: 'claude-code' as BackendId,
      available: false,
      reason: '401 from the endpoint',
    },
    { id: 'opencode' as BackendId, available: false, reason: 'no models' },
  ]

  async function withDeadGateway<T>(fn: (b: string) => Promise<T>): Promise<T> {
    const dir = mkdtempSync(join(tmpdir(), 'agentbox-gw-dead-'))
    const deadServer = createGateway({
      backends: new Map(),
      defaultBackendId: 'opencode',
      statuses: REASONS,
      threads: new ThreadStore(join(dir, 'threads.json')),
      interactions: new InteractionRegistry(50),
    })
    await new Promise<void>(r => deadServer.listen(0, '127.0.0.1', r))
    const addr = deadServer.address()
    const b = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`
    try {
      return await fn(b)
    } finally {
      await new Promise<void>(r => deadServer.close(() => r()))
    }
  }

  it('stays healthy and reports every harness with its reason', async () => {
    await withDeadGateway(async b => {
      expect((await fetch(`${b}/healthz`)).status).toBe(200)
      const r = await fetch(`${b}/backends`)
      expect(r.status).toBe(200)
      expect(await r.json()).toEqual({
        backends: REASONS,
        defaultBackendId: 'opencode',
      })
    })
  })

  it('answers reads empty rather than erroring', async () => {
    await withDeadGateway(async b => {
      // An empty dropdown next to a visible reason beats a failed request.
      const models = await fetch(`${b}/models`)
      expect(models.status).toBe(200)
      expect(await models.json()).toEqual({ models: [] })
      const threads = await fetch(`${b}/threads?userKey=alice`)
      expect(threads.status).toBe(200)
      expect(await threads.json()).toEqual({ threads: [] })
    })
  })

  it('fails a write with the collected reasons, not a TypeError', async () => {
    await withDeadGateway(async b => {
      const r = await fetch(`${b}/threads`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ userKey: 'alice' }),
      })
      expect(r.ok).toBe(false)
      const body = (await r.json()) as { error: string }
      expect(body.error).toContain('no agent harness is available')
      // The actual diagnosis, in front of whoever asked — this is the whole
      // point of staying up.
      expect(body.error).toContain('claude-code: 401 from the endpoint')
      expect(body.error).toContain('opencode: no models')
    })
  })
})

describe('background bot API', () => {
  // The bots have no browser: the proxy creates a thread, stages evidence, fires
  // a prompt and never reads the stream. The only completion signal is the agent
  // calling back with a report, which is why identity is explicit here.
  it('creates a thread, stages before the turn, then dispatches fire-and-forget', async () => {
    const staged: unknown[] = []
    const botServer = createGateway({
      ...asBackends(backend),
      threads,
      interactions: new InteractionRegistry(50),
      attachments: {
        async stage(input) {
          staged.push(input)
          return {
            path: `/opt/agentbox/attachments/${input.sandboxName}`,
            sandboxName: input.sandboxName,
          }
        },
      },
    })
    await new Promise<void>(r => botServer.listen(0, '127.0.0.1', r))
    const addr = botServer.address()
    const botBase = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`
    const p = (path: string, body: unknown) =>
      fetch(`${botBase}${path}`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body),
      })

    try {
      const created = (await (
        await p('/bot/threads', {
          userKey: 'diag-bot',
          title: 'autotriage:triage:c1:Pod:ns/x',
        })
      ).json()) as { threadId: string }
      expect(created.threadId).toMatch(/^th_/)

      const att = await p('/bot/attachments', {
        userKey: 'diag-bot',
        threadId: created.threadId,
        sandboxName: 'evidence/pod.yaml',
        content: 'kind: Pod\n',
      })
      expect(att.status).toBe(201)
      expect(await att.json()).toMatchObject({
        path: '/opt/agentbox/attachments/evidence/pod.yaml',
      })
      // The staging key must be the thread id — that is what the backend binds on
      // the daemon, and a mismatch means the flush silently finds nothing.
      expect(staged).toEqual([
        {
          sessionKey: created.threadId,
          directory: '/home/agents/u/diag-bot',
          sandboxName: 'evidence/pod.yaml',
          content: 'kind: Pod\n',
        },
      ])

      const run = await p('/bot/run', {
        userKey: 'diag-bot',
        threadId: created.threadId,
        agent: 'autotriage',
        input: 'analyse this',
        jobKey: 'triage:c1:Pod:ns/x',
      })
      // 202: accepted and abandoned on purpose. The caller has its own reconcile
      // loop and must never block on the analysis.
      expect(run.status).toBe(202)
      expect(backend.lastRun?.jobKey).toBe('triage:c1:Pod:ns/x')
      expect(backend.lastRun?.agent).toBe('autotriage')
    } finally {
      await new Promise<void>(r => botServer.close(() => r()))
    }
  })

  // jobKey is BOTH the report tools' join key and the gate that registers them.
  // Accepting a bot run without one would start an analysis that cannot publish.
  it('refuses a bot run with no jobKey', async () => {
    const t = (await (
      await post('/bot/threads', { userKey: 'daily-bot' })
    ).json()) as { threadId: string }
    const res = await post('/bot/run', {
      userKey: 'daily-bot',
      threadId: t.threadId,
      input: 'summarise',
    })
    expect(res.status).toBe(400)
    expect(JSON.stringify(await res.json())).toContain('jobKey')
  })

  it('refuses to touch another user thread', async () => {
    const mine = (await (
      await post('/bot/threads', { userKey: 'diag-bot' })
    ).json()) as { threadId: string }
    const res = await post('/bot/run', {
      userKey: 'daily-bot',
      threadId: mine.threadId,
      input: 'x',
      jobKey: 'k',
    })
    expect(res.status).toBe(404)
  })

  // The unattended flows must NOT inherit the browser's default harness. A
  // deployment that switches its default for users would otherwise move
  // auto-triage / digest / Q&A onto the new agent at the same time — a change to
  // the one path with nobody watching, made as a side effect of an unrelated one.
  // So the proxy pins the harness and the gateway honours the pin.
  it('runs on the harness the caller pins, not the deployment default', async () => {
    const oc = new FakeBackend(threads, new InteractionRegistry(50), 'opencode')
    const twoServer = createGateway({
      backends: new Map<BackendId, AgentBackend>([
        ['claude-code', backend],
        ['opencode', oc],
      ]),
      defaultBackendId: 'claude-code',
      statuses: [
        { id: 'claude-code', available: true },
        { id: 'opencode', available: true },
      ],
      threads,
      interactions,
    })
    await new Promise<void>(r => twoServer.listen(0, '127.0.0.1', r))
    const addr = twoServer.address()
    const b = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`
    try {
      const created = (await (
        await fetch(`${b}/bot/threads`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ userKey: 'pinned-bot', backend: 'opencode' }),
        })
      ).json()) as { threadId: string }

      const listed = (await (
        await fetch(`${b}/threads?userKey=pinned-bot`)
      ).json()) as { threads: ThreadInfo[] }
      expect(
        listed.threads.find(t => t.id === created.threadId)?.backendId
      ).toBe('opencode')
    } finally {
      await new Promise<void>(r => twoServer.close(() => r()))
    }
  })
})

// Reading the unattended bots' work. This is the one surface where a caller sees
// a conversation that is not theirs, so the tests are about the boundary: which
// owners, how far back, and that "read" never becomes "act as".
describe('analysis (read-only bot transcripts)', () => {
  const SOURCES = ['diag-bot', 'daily-bot']
  let aServer: Server
  let aBase: string
  let aThreads: ThreadStore
  let aBackend: FakeBackend

  /** A thread that has been spoken in, so it is not filtered out as unstarted. */
  const started = (userKey: string, title: string) => {
    const ref = aThreads.create(userKey, 'claude-code', title)
    aThreads.update(ref.id, { backendThreadId: `sess-${ref.id}` })
    return ref.id
  }
  /** Backdate a row. `update` always stamps `updatedAt` with now, so the window
   *  cases have to reach the stored object — there is no API for time travel. */
  const backdate = (id: string, ms: number) => {
    const ref = aThreads.get(id)
    if (ref) ref.updatedAt = Date.now() - ms
  }
  const aGet = (p: string) => fetch(`${aBase}${p}`)

  beforeAll(async () => {
    const dir = mkdtempSync(join(tmpdir(), 'agentbox-gw-analysis-'))
    aThreads = new ThreadStore(join(dir, 'threads.json'))
    aBackend = new FakeBackend(aThreads, new InteractionRegistry(5_000))
    aServer = createGateway({
      ...asBackends(aBackend),
      threads: aThreads,
      interactions: new InteractionRegistry(5_000),
      analysisSources: SOURCES,
    })
    await new Promise<void>(r => aServer.listen(0, '127.0.0.1', r))
    const addr = aServer.address()
    aBase = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`
  })

  afterAll(async () => {
    await new Promise<void>(r => aServer.close(() => r()))
  })

  it('publishes which owners it will show', async () => {
    expect(await (await aGet('/analysis/sources')).json()).toEqual({
      sources: SOURCES,
    })
  })

  // The default, and what an open-source deployment gets. An allowlist that was
  // never configured must expose nothing rather than everything — the dashboard
  // reads this to decide whether the menu entry exists at all.
  it('exposes nothing when no owners are configured', async () => {
    const off = createGateway({
      ...asBackends(aBackend),
      threads: aThreads,
      interactions: new InteractionRegistry(50),
    })
    await new Promise<void>(r => off.listen(0, '127.0.0.1', r))
    const addr = off.address()
    const b = `http://127.0.0.1:${typeof addr === 'object' && addr ? addr.port : 0}`
    try {
      expect(await (await fetch(`${b}/analysis/sources`)).json()).toEqual({
        sources: [],
      })
      const body = (await (await fetch(`${b}/analysis/threads`)).json()) as {
        threads: ThreadInfo[]
      }
      expect(body.threads).toEqual([])
    } finally {
      await new Promise<void>(r => off.close(() => r()))
    }
  })

  it('lists the bots and nobody else', async () => {
    const bot = started('diag-bot', 'autotriage:triage:c1:Pod:ns/x')
    const human = started('alice', 'my private thread')
    const body = (await (await aGet('/analysis/threads')).json()) as {
      threads: (ThreadInfo & { source: string })[]
    }
    const ids = body.threads.map(t => t.id)
    expect(ids).toContain(bot)
    expect(ids).not.toContain(human)
    expect(body.threads.find(t => t.id === bot)?.source).toBe('diag-bot')
  })

  // A caller cannot widen the window past the cap, and cannot remove it by
  // sending nonsense — the failure mode of reading garbage as "unbounded" is the
  // entire store.
  it('clamps the window and drops what falls outside it', async () => {
    const fresh = started('diag-bot', 'fresh')
    const old = started('diag-bot', 'ten days ago')
    backdate(old, 10 * 24 * 60 * 60 * 1000)

    for (const [q, want] of [
      ['?days=99', 7],
      ['?days=abc', 1],
      ['?days=0', 1],
      ['?days=-3', 1],
      ['', 1],
    ] as const) {
      const body = (await (await aGet(`/analysis/threads${q}`)).json()) as {
        windowDays: number
        threads: ThreadInfo[]
      }
      expect(body.windowDays, `days query ${q || '(absent)'}`).toBe(want)
      expect(body.threads.map(t => t.id)).toContain(fresh)
      // Ten days back is outside even the widest window the cap allows.
      expect(body.threads.map(t => t.id)).not.toContain(old)
    }
  })

  // `source` narrows the allowlist; it never reaches past it. An unlisted name is
  // an empty result, not a lookup.
  it('intersects the requested source with the allowlist', async () => {
    const diag = started('diag-bot', 'diag row')
    const daily = started('daily-bot', 'daily row')
    const alice = started('alice', 'alice row')

    const only = (await (
      await aGet('/analysis/threads?source=daily-bot')
    ).json()) as { threads: ThreadInfo[] }
    expect(only.threads.map(t => t.id)).toContain(daily)
    expect(only.threads.map(t => t.id)).not.toContain(diag)

    const forged = (await (
      await aGet('/analysis/threads?source=alice')
    ).json()) as { threads: ThreadInfo[] }
    expect(forged.threads.map(t => t.id)).not.toContain(alice)
    expect(forged.threads).toEqual([])
  })

  it('hides threads nothing was ever said in', async () => {
    const empty = aThreads.create('diag-bot', 'claude-code', 'never ran')
    const body = (await (await aGet('/analysis/threads')).json()) as {
      threads: ThreadInfo[]
    }
    expect(body.threads.map(t => t.id)).not.toContain(empty.id)
  })

  // The reader is a guest. `list()` prunes as a side effect of walking the rows,
  // and a guest's read must not delete the host's data.
  it('does not prune while listing', async () => {
    const abandoned = aThreads.create('diag-bot', 'claude-code', 'stale')
    backdate(abandoned.id, 48 * 60 * 60 * 1000)
    await aGet('/analysis/threads?days=7')
    expect(aThreads.get(abandoned.id)).toBeDefined()
  })

  it('exports as the thread owner, never as the caller', async () => {
    const bot = started('diag-bot', 'exportable')
    const r = await aGet(`/analysis/threads/${bot}/export?userKey=someone-else`)
    expect(r.status).toBe(200)
    const body = (await r.json()) as { entries: TranscriptEntry[] }
    expect(body.entries.length).toBeGreaterThan(0)
    expect(aBackend.exportedFor.at(-1)).toEqual({
      userKey: 'diag-bot',
      threadId: bot,
    })
  })

  // Same answer for "does not exist" and "exists but is not a bot's", so this
  // cannot be used to probe whether a colleague's conversation is there.
  it('answers identically for a private thread and an unknown id', async () => {
    const alice = started('alice', 'private')
    const privateRes = await aGet(`/analysis/threads/${alice}/export`)
    const unknownRes = await aGet('/analysis/threads/th_deadbeef/export')
    expect(privateRes.status).toBe(404)
    expect(unknownRes.status).toBe(404)
    expect(await privateRes.json()).toEqual(await unknownRes.json())
  })

  // Read-only is structural: there is no verb here that could write.
  it('serves GET only', async () => {
    const r = await fetch(`${aBase}/analysis/threads`, { method: 'POST' })
    expect(r.status).toBe(404)
  })
})
