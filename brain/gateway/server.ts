// The agent gateway's HTTP surface — the only contract the browser knows.
//
// Everything harness-specific is behind AgentBackend; everything product-specific
// (the page marker, thread identity, interaction parking) is here or in a sibling
// module. A frontend must never need to know which backend is running: it reads
// /capabilities and hides what is unavailable.
import type { Interrupt } from '@ag-ui/core'
import { randomUUID } from 'node:crypto'
import { createServer } from 'node:http'
import type { IncomingMessage, Server, ServerResponse } from 'node:http'

import { DAY_MS, clampDays, resolveSources } from './analysis.ts'
import {
  type AttachmentStager,
  createAttachmentStager,
  userDirectory,
} from './attachments.ts'
import type {
  AgentBackend,
  BackendId,
  PromptPart,
  RunRequest,
  ThreadInfo,
} from './backend.ts'
import { UnavailableBackend } from './backends/unavailable.ts'
import type { Classifier } from './classify.ts'
import { withTimeout } from './deadline.ts'
import type { InteractionRegistry, ParkedInteraction } from './interactions.ts'
import { type BackendStatus, unavailableReason } from './registry.ts'
import { type LangfuseConfig, traceRun } from './telemetry.ts'
import type { ThreadStore } from './threads.ts'
import { LiveTurnRegistry } from './turns.ts'
import {
  HEARTBEAT_MS,
  type AgentState,
  SSE_HEADERS,
  encodeHeartbeat,
  streamRun,
} from './wire.ts'

/** Requests without an explicit user fall into one shared namespace, which is
 *  what the open-source build (no login) wants. */
const DEFAULT_USER_KEY = 'default'

const MAX_BODY_BYTES = 8 * 1024 * 1024

/** How long one harness gets to answer the history fan-out before its share of
 *  the list is dropped. The browser blocks its whole session initialization on
 *  `GET /threads`, and nginx gives this route a one-day read timeout — so a
 *  harness that hangs rather than fails (an SDK call that never settles, a stalled
 *  filesystem read) would otherwise wedge the assistant on "initializing session"
 *  with no recovery. A missing harness's rows are recoverable; a hung listing is
 *  not. */
const LIST_THREADS_TIMEOUT_MS = 5_000

export interface GatewayOptions {
  /** Every backend that passed preflight, keyed by id. Never empty. */
  backends: Map<BackendId, AgentBackend>
  defaultBackendId: BackendId
  /** Every backend the deployment could run, for the harness picker. */
  statuses: BackendStatus[]
  threads: ThreadStore
  interactions: InteractionRegistry
  /** Topic-switch classifier. Absent means the feature reports itself off. */
  classifier?: Classifier
  /** Whose conversations `/analysis/*` may show to anyone. The unattended bots,
   *  and nothing else. Empty (the default) turns that surface off entirely. */
  analysisSources?: string[]
  /** Langfuse target. Absent means no tracing; never a hard failure. */
  langfuse?: LangfuseConfig | null
  /** Injectable so tests do not need the workspace-fs server. */
  attachments?: AttachmentStager
  /** How long an unwatched turn may keep running before it is cancelled.
   *  Injectable so a test can assert that it fires. */
  detachedMaxMs?: number
  port?: number
  host?: string
}

async function readBody(req: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = []
  let size = 0
  for await (const chunk of req) {
    const buf = chunk as Buffer
    size += buf.length
    if (size > MAX_BODY_BYTES) throw new Error('request body too large')
    chunks.push(buf)
  }
  if (!chunks.length) return {}
  return JSON.parse(Buffer.concat(chunks).toString('utf-8'))
}

function json(res: ServerResponse, status: number, body: unknown): void {
  const text = JSON.stringify(body)
  res.writeHead(status, {
    'content-type': 'application/json; charset=utf-8',
    'content-length': Buffer.byteLength(text),
  })
  res.end(text)
}

/**
 * The body AG-UI's HttpAgent posts. Typed here rather than imported from
 * @ag-ui/core because the zod-inferred RunAgentInput is far wider than what this
 * endpoint reads, and stating the subset is what makes "we ignore client-side
 * history" reviewable instead of implicit.
 */
interface AgUiRunBody {
  threadId?: string
  runId?: string
  messages?: { role?: string; content?: unknown }[]
  /** Answers to the interrupts the PREVIOUS run ended with. Its presence is what
   *  makes this a resume rather than a new turn. A first-class field of
   *  `RunAgentInput`, set by the browser's `submitInterruptResponses`. */
  resume?: {
    interruptId?: string
    status?: 'resolved' | 'cancelled'
    payload?: unknown
  }[]
  /** The agent state the client is holding, echoed so our `STATE_SNAPSHOT` stays
   *  cumulative rather than forgetting every previous turn's stats. */
  state?: AgentState | null
  /** Everything AG-UI has no field for; set by the browser's runtime. */
  forwardedProps?: {
    userKey?: string
    model?: string
    pageContext?: Record<string, string>
    /** Which harness a NEW conversation should start under. Ignored once the
     *  thread exists — its backend is fixed at creation. */
    backend?: string
    /**
     * Rejoin the turn already in flight instead of starting one.
     *
     * The browser sends this after anything that separated it from its stream: a
     * reload, a tab it came back to, a socket an intermediary dropped, a runtime
     * that remounted. There is no new prompt — this run exists only to carry the
     * rest of a turn that never stopped running.
     */
    attach?: boolean
    /** Where to resume from, in `turn.seq` space. Omitted means "from the start of
     *  the turn", which is what a client with no prior position wants. */
    cursor?: number
  }
}

/**
 * The new user turn: the text of the LAST user message.
 *
 * AG-UI resends the whole conversation on every run. Feeding all of it to a
 * backend that already has the history would duplicate every previous turn, so
 * only the tail is taken — the harness session is the source of truth for what
 * came before.
 */
function lastUserText(messages: AgUiRunBody['messages']): string[] {
  for (let i = (messages?.length ?? 0) - 1; i >= 0; i--) {
    const m = messages?.[i]
    if (m?.role !== 'user') continue
    if (typeof m.content === 'string') return [m.content]
    // AG-UI allows structured content; take the text parts in order.
    if (Array.isArray(m.content)) {
      return m.content
        .map(part =>
          part && typeof part === 'object' && 'text' in part
            ? String((part as { text?: unknown }).text ?? '')
            : typeof part === 'string'
              ? part
              : ''
        )
        .filter(Boolean)
    }
    return []
  }
  return []
}

/**
 * A parked question, as an AG-UI interrupt.
 *
 * `id` MUST be the park's `requestId`: the browser echoes it back as
 * `resume[].interruptId`, and that is what identifies the waiter to settle.
 *
 * The whole question array rides in `metadata.agentbox` — the protocol's
 * `responseSchema` describes the answer shape for a generic AG-UI client, but our
 * cards need the labelled options, and `key` (the answer map's key) has to survive
 * the round trip or answers land under the wrong question.
 *
 * `expiresAt` is deliberately NOT set. The browser's `submitInterruptResponses`
 * throws before any network call on an expired interrupt — including for a
 * `cancelled` response — so publishing the park's deadline would make a stale card
 * impossible to dismiss and wedge the composer. The server-side resume is
 * forgiving instead.
 */
function toInterrupt(parked: ParkedInteraction): Interrupt {
  return {
    id: parked.requestId,
    reason:
      parked.request.kind === 'permission' ? 'confirmation' : 'input_required',
    ...(parked.request.questions[0]?.question
      ? { message: parked.request.questions[0].question }
      : {}),
    responseSchema: {
      type: 'object',
      properties: {
        answers: {
          type: 'object',
          description: 'One entry per question, keyed by the question text.',
          additionalProperties: { type: 'string' },
        },
      },
      required: ['answers'],
    },
    metadata: {
      agentbox: {
        kind: parked.request.kind,
        createdAt: parked.createdAt,
        questions: parked.request.questions,
      },
    },
  }
}

/** The answer map out of a resume entry. A `cancelled` status and a missing or
 *  malformed payload both mean "the user declined", which the backends already
 *  understand as an empty map. */
function answersOf(entry: {
  status?: 'resolved' | 'cancelled'
  payload?: unknown
}): Record<string, string> {
  if (entry.status !== 'resolved') return {}
  const payload = entry.payload
  if (!payload || typeof payload !== 'object') return {}
  const answers = (payload as { answers?: unknown }).answers
  if (!answers || typeof answers !== 'object') return {}
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(answers as Record<string, unknown>))
    if (typeof v === 'string') out[k] = v
  return out
}

/**
 * A header that PINS the end user, overriding anything the request says.
 *
 * The gateway otherwise takes the caller's word for `userKey`, which is right for
 * a trusted integration acting on behalf of many of its own users — an API-key
 * caller naming which of its users is asking. It is wrong for a browser: there the
 * value is chosen by the person it identifies, so a user could read any other
 * user's threads by editing a query string.
 *
 * A front door that authenticates the person sets this instead, and whatever the
 * request body or query says is ignored. It is safe to trust only because nothing
 * outside the cluster can reach this port: the header has to be set by the hop
 * that authenticated, and that hop strips any inbound copy.
 */
const USER_KEY_HEADER = 'x-agentbox-user'

function userKeyOf(
  url: URL,
  body: unknown,
  headers?: NodeJS.Dict<string | string[]>
): string {
  const pinned = headers?.[USER_KEY_HEADER]
  const fromHeader = Array.isArray(pinned) ? pinned[0] : pinned
  if (typeof fromHeader === 'string' && fromHeader) {
    return sanitizeUserKey(fromHeader)
  }
  const fromBody =
    body && typeof body === 'object'
      ? (body as { userKey?: unknown }).userKey
      : undefined
  return sanitizeUserKey(
    (typeof fromBody === 'string' ? fromBody : undefined) ||
      url.searchParams.get('userKey') ||
      DEFAULT_USER_KEY
  )
}

/** The user key becomes a directory segment and a Langfuse user id, so it is
 *  constrained the same way the workspace-fs daemon constrains it. Applied to the
 *  pinned header too: being trusted to name the user is not the same as being
 *  trusted to name a path. */
function sanitizeUserKey(raw: string): string {
  return /^[A-Za-z0-9._-]{1,128}$/.test(raw) ? raw : DEFAULT_USER_KEY
}

export function createGateway(opts: GatewayOptions): Server {
  const {
    backends,
    defaultBackendId,
    statuses,
    threads,
    interactions,
    classifier,
    langfuse,
  } = opts
  const analysisSources = opts.analysisSources ?? []

  /**
   * Which backend serves this request.
   *
   * A thread's harness is FIXED at creation (`ThreadRef.backendId`) — replaying
   * a Claude Code conversation through OpenCode would quietly produce a
   * different agent's history. Only a request with no thread yet honours the
   * caller's `backend` pick, and an unknown or not-serving id falls back to the
   * default rather than erroring: the picker is a convenience, not a gate.
   */
  const pick = (sel: {
    threadId?: string | null
    requested?: string | null
  }): AgentBackend => {
    const ofThread = sel.threadId
      ? threads.get(sel.threadId)?.backendId
      : undefined
    const wanted = ofThread ?? sel.requested ?? undefined
    return (
      (wanted ? backends.get(wanted as BackendId) : undefined) ??
      backends.get(defaultBackendId) ??
      // Nothing serves. Returning the stand-in rather than `undefined as
      // AgentBackend` is what keeps every route on this one path: each would
      // otherwise throw a TypeError on a missing method and answer with an
      // "undefined is not a function" that says nothing about the credential
      // that is actually wrong.
      unavailable
    )
  }
  const unavailable = new UnavailableBackend(
    defaultBackendId,
    unavailableReason(statuses)
  )
  const attachments = opts.attachments ?? createAttachmentStager()
  // In-flight turns, whose lifetime is deliberately NOT the lifetime of the
  // response reading them: an interrupt ends the run while the harness stays
  // parked. See turns.ts.
  const turns = new LiveTurnRegistry({
    cancelInteractions: threadId => interactions.cancelThread(threadId),
    ...(opts.detachedMaxMs !== undefined
      ? { detachedMaxMs: opts.detachedMaxMs }
      : {}),
  })
  /** Stamp the one fact a harness listing cannot know. Shared by the listing and
   *  the push channel for the same reason `ThreadStore.toInfo` exists: two views
   *  of a conversation must not be able to disagree about it. */
  const withLive = (t: ThreadInfo): ThreadInfo => ({
    ...t,
    live: turns.isBusy(t.id),
  })
  // A finished background run used to be announced to a chat system from here.
  // That was the gateway doing one tenant's product work, so it is gone: the run
  // already reports completion through `jobKey`, and what a deployment wants done
  // with that belongs on its side of the callback.

  return createServer((req, res) => {
    void handle(req, res).catch(e => {
      if (!res.headersSent)
        json(res, 500, { error: e instanceof Error ? e.message : String(e) })
      else res.end()
    })
  })

  async function handle(
    req: IncomingMessage,
    res: ServerResponse
  ): Promise<void> {
    const url = new URL(req.url ?? '/', 'http://gateway.local')
    const path = url.pathname.replace(/\/+$/, '') || '/'
    const method = req.method ?? 'GET'

    // --- liveness ----------------------------------------------------------
    if (path === '/healthz') return json(res, 200, { ok: true })

    // --- capability negotiation --------------------------------------------
    // The frontend reads this instead of branching on a backend name, so a
    // backend that cannot do something simply has the affordance hidden.
    if (path === '/capabilities' && method === 'GET') {
      // `backend=` lets the browser read the capabilities of the harness it is
      // ABOUT to switch to, before any thread exists under it.
      const b = pick({ requested: url.searchParams.get('backend') })
      return json(res, 200, {
        backendId: b.id,
        capabilities: b.capabilities,
        defaultBackendId,
      })
    }

    // The harness picker. Everything the deployment could run, whether or not it
    // came up, so the UI can grey out a broken one WITH its reason instead of
    // pretending it does not exist.
    if (path === '/backends' && method === 'GET') {
      return json(res, 200, { backends: statuses, defaultBackendId })
    }

    if (path === '/models' && method === 'GET') {
      return json(res, 200, {
        models: await pick({
          requested: url.searchParams.get('backend'),
        }).models(),
      })
    }

    // --- threads -----------------------------------------------------------
    if (path === '/threads' && method === 'GET') {
      const userKey = userKeyOf(url, {}, req.headers)
      // Threads from EVERY serving backend: the list is the user's history, not
      // the current pick's history, and hiding the other harness's threads would
      // look like data loss right after a switch.
      const lists = await Promise.all(
        [...backends.values()].map(b =>
          withTimeout(
            b
              .listThreads(userKey)
              // Stamped here, where the owning backend is known for certain.
              .then(ts => ts.map(t => ({ ...t, backendId: b.id })))
              .catch(() => [] as ThreadInfo[]),
            LIST_THREADS_TIMEOUT_MS,
            []
          )
        )
      )
      const all: ThreadInfo[] = lists.flat()
      return json(res, 200, {
        threads: all.map(withLive).sort((a, b) => b.updatedAt - a.updatedAt),
      })
    }

    // Live thread metadata, pushed. The browser needs it because two facts about a
    // conversation arrive AFTER the request that caused them: the harness's
    // auto-generated title (a separate model call, which may finish after the turn)
    // and the first run adopting a harness session. Neither can ride on the run
    // stream — AG-UI rejects anything after `RUN_FINISHED`, and holding the run
    // open to wait would keep the "generating" spinner up for seconds after the
    // answer is complete. So they come down here instead, and the client never
    // polls.
    if (path === '/threads/events' && method === 'GET') {
      const userKey = userKeyOf(url, {}, req.headers)
      res.writeHead(200, SSE_HEADERS)
      res.flushHeaders?.()
      const send = (payload: unknown) => {
        if (res.writableEnded) return
        res.write(`data: ${JSON.stringify(payload)}\n\n`)
      }
      const unsubscribe = threads.onChange((owner, ref, id) => {
        // Scoped to one user: the store is shared, the stream is personal.
        if (owner !== userKey) return
        send(
          ref
            ? { type: 'updated', thread: withLive(threads.toInfo(ref)) }
            : { type: 'deleted', id }
        )
      })
      // Same cadence and reasoning as the run stream's: a comment frame keeps an
      // intermediary with a read timeout from deciding an idle stream is dead.
      const beat = setInterval(() => {
        if (!res.writableEnded) res.write(encodeHeartbeat())
      }, HEARTBEAT_MS)
      const stop = () => {
        clearInterval(beat)
        unsubscribe()
      }
      res.on('close', stop)
      res.on('error', stop)
      return
    }

    if (path === '/threads' && method === 'POST') {
      const body = (await readBody(req)) as { title?: string }
      const userKey = userKeyOf(url, body, req.headers)
      const id = await pick({
        requested:
          (body as { backend?: string }).backend ??
          url.searchParams.get('backend'),
      }).createThread(userKey, body.title)
      return json(res, 201, { threadId: id })
    }

    // --- analysis: the unattended bots' conversations, read-only -----------
    //
    // Everything above this point answers "what is MINE", with the caller naming
    // themselves. These three answer "what did the bots do", and the identity
    // that matters is the thread's OWNER — which the store knows and the caller
    // cannot influence. `?userKey=` is ignored here on purpose: there is nothing
    // for it to unlock, and honouring it would put a caller-supplied string on an
    // authorization path.
    //
    // They are GET-only and live outside `/bot/`, which nginx blackholes. The
    // separation is the whole security model: reading a bot's transcript is open
    // to everyone, acting as one is open to nobody with a browser.
    if (path === '/analysis/sources' && method === 'GET') {
      return json(res, 200, { sources: analysisSources })
    }

    if (path === '/analysis/threads' && method === 'GET') {
      const windowDays = clampDays(url.searchParams.get('days'))
      const owners = resolveSources(
        analysisSources,
        url.searchParams.get('source')
      )
      // Metadata only, straight out of the store — no backend fan-out. That
      // keeps this route off `LIST_THREADS_TIMEOUT_MS` (a wedged harness cannot
      // stall the list) and, more importantly, away from `list()`, whose prune
      // would delete rows on behalf of a reader who is only looking.
      const rows = threads
        .listOwned(owners, Date.now() - windowDays * DAY_MS)
        .map(ref => ({ ...threads.toInfo(ref), source: ref.userKey }))
      return json(res, 200, {
        windowDays,
        // Unstarted threads are noise everywhere else too — a row nothing was
        // ever said in.
        threads: rows.filter(t => t.started),
      })
    }

    const analysisExport = /^\/analysis\/threads\/([^/]+)\/export$/.exec(path)
    if (analysisExport && method === 'GET') {
      const threadId = decodeURIComponent(analysisExport[1])
      const ref = threads.get(threadId)
      // One response for "no such thread" and for "not one of the bots'", so
      // this cannot be used to probe whether a private conversation exists.
      if (!ref || !analysisSources.includes(ref.userKey)) {
        return json(res, 404, { error: 'unknown thread' })
      }
      const backend = pick({ threadId })
      if (!backend.capabilities.transcriptExport) {
        return json(res, 501, { error: 'this backend cannot export' })
      }
      // The OWNER's key, not the caller's: it selects both the store row and the
      // harness working directory the transcript lives in.
      return json(res, 200, {
        entries: await backend.exportThread(ref.userKey, threadId),
      })
    }

    const threadMatch = /^\/threads\/([^/]+)(\/[^/]+)?$/.exec(path)
    if (threadMatch) {
      const threadId = decodeURIComponent(threadMatch[1])
      const sub = threadMatch[2]
      const body = method === 'GET' ? {} : await readBody(req)
      const userKey = userKeyOf(url, body, req.headers)

      if (!sub && method === 'PATCH') {
        const { title } = body as { title?: string }
        if (!title) return json(res, 400, { error: 'title is required' })
        await pick({ threadId }).renameThread(userKey, threadId, title)
        return json(res, 200, { ok: true })
      }
      if (!sub && method === 'DELETE') {
        await pick({ threadId }).deleteThread(userKey, threadId)
        return json(res, 200, { ok: true })
      }
      if (sub === '/fork' && method === 'POST') {
        if (!pick({ threadId }).capabilities.fork)
          return json(res, 501, { error: 'this backend cannot fork threads' })
        return json(res, 201, {
          threadId: await pick({ threadId }).forkThread(userKey, threadId),
        })
      }
      // Is a turn still running here, and where would a reader pick it up?
      //
      // The browser asks before it hydrates a conversation and again whenever it
      // suspects it lost its stream. Cheap and side-effect-free on purpose: it is
      // polled by a reconnect that must not disturb the turn it is asking about.
      if (sub === '/live' && method === 'GET') {
        const snapshot = turns.describe(threadId)
        return json(res, 200, {
          live: snapshot ?? { inFlight: false, seq: 0, dropped: 0 },
        })
      }
      if (sub === '/export' && method === 'GET') {
        if (!pick({ threadId }).capabilities.transcriptExport)
          return json(res, 501, { error: 'this backend cannot export' })
        // A turn still in flight is replayed from the live-turn log, not from the
        // harness's own record: returning it here as well renders it twice on a
        // reload, once as history and once as the stream it is still producing.
        const live = turns.describe(threadId)
        const entries = await pick({ threadId }).exportThread(
          userKey,
          threadId,
          live?.inFlight ? { until: live.startedAt } : undefined
        )
        const format = url.searchParams.get('format') ?? 'json'
        if (format === 'md') {
          const md = entries
            .map(e => {
              const text = e.parts
                .map(p =>
                  p.type === 'text'
                    ? p.text
                    : p.type === 'tool-call'
                      ? `\`${p.name}(${JSON.stringify(p.args)})\``
                      : ''
                )
                .filter(Boolean)
                .join('\n\n')
              return `## ${e.role}\n\n${text}`
            })
            .join('\n\n')
          res.writeHead(200, { 'content-type': 'text/markdown; charset=utf-8' })
          res.end(md)
          return
        }
        return json(res, 200, { entries })
      }
      if (sub === '/interrupt' && method === 'POST') {
        // Stop means stop: release anything parked and kill the turn in the
        // registry too, or the harness keeps producing into a log nobody reads.
        turns.cancel(threadId)
        await pick({ threadId }).interrupt(threadId)
        return json(res, 200, { ok: true })
      }
      return json(res, 404, { error: `no route for ${method} ${path}` })
    }

    // --- topic-switch classifier -------------------------------------------
    // A gateway concern, not an agent one: it is a one-shot call to a pinned
    // cheap model, and tying it to the chat backend would make its cost and
    // behaviour move whenever the chat model does.
    if (path === '/classify' && method === 'POST') {
      const body = (await readBody(req)) as {
        context?: string
        newInput?: string
        threadId?: string | null
      }
      if (!classifier) return json(res, 200, { enabled: false })
      if (!body.newInput)
        return json(res, 400, { error: 'newInput is required' })
      const userKey = userKeyOf(url, body, req.headers)
      return json(
        res,
        200,
        await classifier.classify({
          context: body.context,
          newInput: body.newInput,
          threadId: body.threadId ?? null,
          userKey,
        })
      )
    }

    // Parked questions, for RECONCILIATION. Answers no longer come through here
    // (they ride on the next run's `resume`), but the browser persists interrupts
    // in its own message metadata, and after a gateway restart those are
    // unanswerable — a card nobody can dismiss, with the composer wedged behind
    // it. Hydration cross-checks against this list and drops what is gone.
    if (path === '/interactions' && method === 'GET') {
      return json(res, 200, { pending: interactions.pending() })
    }

    // --- background bot API ------------------------------------------------
    // The proxy drives three unattended flows (auto-triage, daily digest,
    // Feishu Q&A) with no browser involved. It never reads the agent's output:
    // the only completion signal is the agent calling back with a report. So a
    // streaming endpoint buys nothing here, and what these need instead is an
    // explicit identity (userKey, jobKey), file staging BEFORE the first turn,
    // and fire-and-forget dispatch that tolerates being abandoned.
    if (path === '/bot/threads' && method === 'POST') {
      const body = (await readBody(req)) as {
        title?: string
        jobKey?: string
        backend?: string
      }
      const userKey = userKeyOf(url, body, req.headers)
      // A background bot has no picker, so its caller may pin the harness — and
      // production does. Otherwise a change to the BROWSER's default harness
      // would silently move the unattended analysis flows onto it, which is
      // exactly the kind of change nobody would make on purpose. Falls back to
      // the default when the pinned one is not serving: a degraded bot beats a
      // silent one. The thread remembers this choice, so `/bot/run` follows it
      // without the caller repeating itself.
      const threadId = await pick({ requested: body.backend }).createThread(
        userKey,
        body.title
      )
      return json(res, 201, { threadId })
    }

    // Staging BEFORE the first prompt is the contract: the daemon flushes on the
    // first tool call, so anything staged later is invisible for that turn.
    if (path === '/bot/attachments' && method === 'POST') {
      const body = (await readBody(req)) as {
        threadId?: string
        sandboxName?: string
        content?: string
      }
      const userKey = userKeyOf(url, body, req.headers)
      if (!body.threadId || !threads.getForUser(body.threadId, userKey))
        return json(res, 404, { error: 'unknown thread' })
      if (!body.sandboxName || typeof body.content !== 'string')
        return json(res, 400, {
          error: 'sandboxName and content are required',
        })
      const staged = await attachments.stage({
        // The sandbox key is the thread id, so the staging key matches what the
        // backend binds on the daemon.
        sessionKey: body.threadId,
        directory: userDirectory(userKey),
        sandboxName: body.sandboxName,
        content: body.content,
      })
      return json(res, 201, staged)
    }

    if (path === '/bot/run' && method === 'POST') {
      const body = (await readBody(req)) as {
        threadId?: string
        input?: string
        agent?: string
        jobKey?: string
        model?: string
      }
      const userKey = userKeyOf(url, body, req.headers)
      if (!body.threadId || !threads.getForUser(body.threadId, userKey))
        return json(res, 404, { error: 'unknown thread' })
      if (!body.input?.trim())
        return json(res, 400, { error: 'input must contain non-empty text' })
      if (!body.jobKey)
        return json(res, 400, {
          error:
            "jobKey is required: it is the report tools' join key and the gate " +
            'that registers them',
        })

      // Fire and forget. The caller has its own reconcile loop and never waits
      // on us, so draining the stream here (rather than streaming it back)
      // keeps the turn alive after the HTTP request is long gone.
      const controller = new AbortController()
      const run = pick({ threadId: body.threadId }).run({
        threadId: body.threadId,
        userKey,
        input: [{ type: 'text', text: body.input }],
        model: body.model,
        jobKey: body.jobKey,
        agent: body.agent,
        signal: controller.signal,
      })
      void (async () => {
        const traced = traceRun(
          langfuse ?? null,
          {
            threadId: body.threadId as string,
            userKey,
            backendId: pick({ threadId: body.threadId }).id,
            model: body.model,
          },
          body.input as string,
          run
        )
        let finalText = ''
        let lastError = ''
        try {
          for await (const event of traced) {
            if (event.t === 'text') finalText += event.delta
            if (event.t === 'error') {
              lastError = event.message
              console.error(
                `[gateway] bot run ${body.jobKey} error: ${event.message}`
              )
            }
          }
        } catch (e) {
          lastError = String(e)
          console.error(`[gateway] bot run ${body.jobKey} threw: ${String(e)}`)
        }
      })()
      return json(res, 202, { ok: true })
    }

    // --- the run stream ----------------------------------------------------
    if (path === '/run' && method === 'POST') {
      // AG-UI's HttpAgent posts a RunAgentInput: the WHOLE conversation plus a
      // threadId/runId it generated. Our backends are stateful (they resume a
      // harness session by threadId), so only the last user message is new —
      // the rest is the client's copy of history and is deliberately ignored.
      // The fields AG-UI has no slot for (which page the user was on, the model
      // pick, the user key) ride in `forwardedProps`.
      const body = (await readBody(req)) as AgUiRunBody
      const forwarded = body.forwardedProps ?? {}
      const userKey = userKeyOf(url, forwarded, req.headers)
      // A brand-new conversation: AG-UI mints a threadId client-side, but ours
      // are gateway-owned, so an id we do not know means "start a new thread".
      const knownThread =
        body.threadId && threads.getForUser(body.threadId, userKey)
          ? body.threadId
          : null
      const clientState = body.state ?? undefined
      // Every response is exactly one run, and the browser's aggregator adopts
      // the first message id the run reports — so allocate it here and never
      // share it with another run of the same turn (reusing it makes the browser
      // drop its placeholder and then silently discard everything that follows).
      const messageId = randomUUID()
      const runId = body.runId || randomUUID()

      /** Publish one run over the response, then let the turn outlive it. */
      const streamTurn = async (
        turn: ReturnType<LiveTurnRegistry['start']>,
        cursor: number
      ): Promise<void> => {
        // `close` fires on a clean `res.end()` too, so a flag — not the socket
        // state — is what distinguishes "we are ending the run to publish a
        // question" from "the tab went away". Getting this wrong cancels the very
        // question being published, with no symptom at all.
        let detachingForInterrupt = false
        // What this reader missed before it arrived. Reported in the state so a
        // truncated replay is visible rather than passing for the whole turn.
        //
        // The client echoes state back, so a stale `dropped` would make every
        // later turn claim to be truncated: it is dropped from what the client
        // sent and re-added only when it is true of THIS run.
        const dropped = Math.max(0, turn.dropped - cursor)
        const carried: AgentState = { ...clientState }
        delete carried.dropped
        const state: AgentState | undefined =
          dropped > 0
            ? { ...carried, dropped }
            : Object.keys(carried).length
              ? carried
              : undefined
        res.writeHead(200, SSE_HEADERS)
        // Flush headers immediately so the browser's stream opens even if the
        // first model token is seconds away.
        res.flushHeaders?.()
        // Scoped to THIS response, never the turn: ending the run must not end the
        // turn (that is the whole mechanism behind interrupts).
        const reading = new AbortController()
        const attached = turns.attach(turn, cursor, reading.signal)
        // The reader's own position. The pump runs ahead of it, so a resume has to
        // continue from HERE rather than from the turn's write cursor, or the
        // events produced between the question and the end of this response are
        // silently dropped.
        let consumed = cursor
        const events = (async function* counted() {
          for await (const event of attached) {
            consumed++
            yield event
          }
        })()
        // A hang-up has to END THE READER, not cancel the turn. Without this the
        // loop below waits on a socket nobody is listening to: writes to a dead
        // response silently succeed, so it would never notice, the reader would
        // never be released, and the registry's no-reader policy would never get a
        // chance to stop the turn. Releasing it starts the grace window instead,
        // which is what eventually cancels an abandoned run.
        res.on('close', () => reading.abort())
        try {
          for await (const frame of streamRun(events, {
            threadId: turn.threadId,
            runId,
            messageId,
            ...(state ? { state } : {}),
            signal: turn.controller.signal,
            onInteraction: () => {
              const parked = interactions.byThread(turn.threadId)
              if (!parked.length) return []
              detachingForInterrupt = true
              turns.markResume(turn, consumed)
              return parked.map(toInterrupt)
            },
          })) {
            if (res.writableEnded) break
            res.write(frame)
          }
        } finally {
          await events.return(undefined)
          turns.releaseReader(turn, { forInterrupt: detachingForInterrupt })
        }
        res.end()
      }

      /** A run that carries no events, well-formed. See the call sites for why a
       *  4xx would be worse: the browser already has a run in flight, and an
       *  error response leaves the thread unable to send again. */
      const streamNothing = async (): Promise<void> => {
        res.writeHead(200, SSE_HEADERS)
        res.flushHeaders?.()
        for await (const frame of streamRun((async function* () {})(), {
          threadId: knownThread ?? body.threadId ?? 'pending',
          runId,
          messageId,
          ...(clientState ? { state: clientState } : {}),
        })) {
          if (res.writableEnded) break
          res.write(frame)
        }
        res.end()
      }

      // --- attach: rejoin a turn that never stopped running -------------------
      //
      // The browser lost its stream (a reload, a tab it came back to, a socket an
      // intermediary dropped) while the harness kept working — which it now does
      // for as long as the detached lease allows. This run is a cursor over that
      // turn's log and then its live tail; `turns.attach` builds a FRESH
      // translator over both, so protocol validity is structural rather than
      // something the replay path has to remember.
      if (forwarded.attach) {
        const turn = knownThread ? turns.get(knownThread) : undefined
        // Not in flight after all: it finished between the browser asking and this
        // request landing. An empty run lets the client settle without an error.
        if (!turn || turn.done) return await streamNothing()
        return await streamTurn(turn, forwarded.cursor ?? 0)
      }

      // --- resume: answer the parked questions, rejoin the same turn ---------
      if (body.resume?.length) {
        const turn = knownThread ? turns.get(knownThread) : undefined
        for (const entry of body.resume) {
          if (!entry.interruptId) continue
          // Stale is the common case, not an error: the park may have timed out,
          // another tab may have answered first, or the gateway may have
          // restarted. The browser has already cleared the card either way.
          if (!interactions.has(entry.interruptId)) {
            console.warn(
              `[gateway] resume for unknown interrupt ${entry.interruptId}`
            )
            continue
          }
          try {
            await pick({ threadId: knownThread }).answer(
              entry.interruptId,
              answersOf(entry)
            )
          } catch (e) {
            console.warn(
              `[gateway] resume could not settle ${entry.interruptId}:`,
              e
            )
          }
        }
        // Nothing to rejoin (restart, or the turn aged out). The browser has
        // already dropped the interrupts locally, so it needs a run that ends
        // cleanly rather than an error.
        if (!turn) return await streamNothing()
        await streamTurn(turn, turn.resumeCursor ?? turn.seq)
        return
      }

      // --- a new turn -------------------------------------------------------
      const input: PromptPart[] = lastUserText(body.messages)
        .map(text => ({ type: 'text' as const, text }))
        .filter(p => p.text.trim().length > 0)
      // A blank prompt is a client bug; starting a turn on it burns a model call
      // and leaves an empty message in the transcript.
      if (!input.length)
        return json(res, 400, { error: 'input must contain non-empty text' })

      const liveTurn = knownThread ? turns.get(knownThread) : undefined
      const isLive = !!liveTurn && !liveTurn.done

      // One turn per thread. Two tabs sending at once used to start two harness
      // turns over one session, where only the last cancellation handle survived.
      //
      // `code` is what makes this recoverable rather than a dead end: a client
      // that sees it attaches to the live turn and runs its message once that turn
      // is over, instead of showing an error for a conversation that is working
      // fine. A user who does not want to wait interrupts first — that is a
      // decision the browser asks about, not one this endpoint can make.
      if (isLive && liveTurn)
        return json(res, 409, {
          code: 'turn_in_flight',
          error: 'this conversation already has a turn in flight',
          threadId: liveTurn.threadId,
          seq: liveTurn.seq,
        })

      // An existing thread keeps its own harness; a new one takes the picker's.
      const runBackend = pick({
        threadId: knownThread,
        requested: forwarded.backend,
      })

      const controller = new AbortController()
      const request: RunRequest = {
        threadId: knownThread,
        userKey,
        input,
        pageContext: forwarded.pageContext,
        model: forwarded.model,
        // Owned by the registry, NOT by this request: that decoupling is what
        // lets a run end for an interrupt without ending the turn.
        signal: controller.signal,
      }

      // Tracing wraps the stream and forwards every event unchanged, so a
      // telemetry backend being down can never affect a turn.
      const traced = traceRun(
        langfuse ?? null,
        {
          threadId: knownThread ?? 'pending',
          userKey,
          backendId: runBackend.id,
          model: request.model,
        },
        input.map(p => p.text).join('\n'),
        runBackend.run(request)
      )
      // For a new conversation the real thread id is not known until the backend
      // mints it; the client's id stands in until a `thread` event corrects it
      // through the agent state.
      const turnKey = knownThread ?? body.threadId ?? 'pending'
      const turn = turns.start(turnKey, userKey, controller, traced)
      await streamTurn(turn, 0)
      return
    }

    return json(res, 404, { error: `no route for ${method} ${path}` })
  }
}

export async function listenGateway(opts: GatewayOptions): Promise<Server> {
  const server = createGateway(opts)
  const port = opts.port ?? Number(process.env.ASSISTANT_GATEWAY_PORT ?? 4099)
  const host = opts.host ?? '0.0.0.0'
  await new Promise<void>(resolve => server.listen(port, host, resolve))
  return server
}
