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

// OpenCode as an ordinary AgentBackend.
//
// It used to be the exception: the gateway put a transparent HTTP/SSE proxy in
// front of `opencode serve` and the browser spoke OpenCode's own protocol. That
// cost more than it saved — the browser needed a second runtime just for this
// harness, and everything the gateway owns (thread identity, the page marker,
// the topic classifier, telemetry) applied to Claude Code only, i.e. to the path
// that was NOT in production.
//
// Two things are worth knowing before changing this file:
//
//   * THE SESSION IS CREATED LAZILY. `ThreadStore.backendThreadId` is documented
//     as "the harness's own session id, ONCE IT HAS ONE" — so a thread the user
//     opened but never spoke into has no OpenCode session at all. That is what
//     makes opencode behave like Claude Code from the browser's point of view,
//     and it is why the pod no longer needs a GC loop reaping blank sessions.
//   * THE EVENT STREAM IS SHARED, THE TURN IS NOT. `event.subscribe()` is one
//     firehose for the whole server, so `run()` filters by its own sessionID and
//     ends when `session.prompt()` resolves — not when the stream goes quiet,
//     which would end the turn on the first slow tool call.
//   * ...AND THE SUBSCRIPTION MUST BE ABORTED WHEN THE TURN ENDS. It is a live
//     HTTP connection that nothing else closes: the stream is server-wide so it
//     never completes, and the SDK's SSE client re-dials by itself if the socket
//     merely drops. Leaking one per turn is not a slow drip — every subscriber
//     gets a full copy of every event, so the per-event decode work grows with
//     the number of turns the pod has ever served, and the gateway's event loop
//     eventually cannot answer its own health probe.
import {
  type Event,
  type Message,
  type OpencodeClient,
  type Part,
  type ToolPart,
  createOpencodeClient,
} from '@opencode-ai/sdk/v2'
import { randomBytes } from 'node:crypto'
import { existsSync, mkdirSync } from 'node:fs'
import { homedir } from 'node:os'
import { join, resolve } from 'node:path'

import { proxyUrl } from '@scitix/agentbox-hands'
import { userDirectory } from '../attachments.ts'
import {
  type AgentBackend,
  type AgentEvent,
  type BackendCapabilities,
  type InteractionQuestion,
  type ModelInfo,
  NotSupportedError,
  type RunRequest,
  TITLE_BACKFILL_DELAYS_MS,
  type ThreadInfo,
  type TranscriptEntry,
  type TranscriptPart,
} from '../backend.ts'
import { attempt } from '../deadline.ts'
import type { InteractionRegistry } from '../interactions.ts'
import {
  bindSandboxIdentity,
  promptWithPage,
  releaseSandbox,
  systemPromptFile,
} from '../prompt.ts'
import { AsyncQueue } from '../queue.ts'
import type { ThreadStore } from '../threads.ts'

export interface OpenCodeConfig {
  /** Where `opencode serve` listens inside the pod. */
  baseUrl: string
  /** Optional default model, as `providerID/modelID`. */
  defaultModel?: string
  readyTimeoutMs: number
}

/**
 * Is the OpenCode harness offered at all?
 *
 * The SAME variable gates the entrypoint's `opencode serve`. One value for both,
 * because the alternative — the picker offering a harness whose server nobody
 * launched — is a 60s preflight timeout followed by an entry that never works.
 */
export function opencodeEnabled(): boolean {
  const raw = (process.env.ASSISTANT_OC_ENABLED ?? '1').trim().toLowerCase()
  return raw !== '0' && raw !== 'false'
}

export function openCodeConfigFromEnv(): OpenCodeConfig {
  const port = Number(process.env.OPENCODE_INTERNAL_PORT ?? 4096)
  return {
    baseUrl: process.env.OPENCODE_BASE_URL || `http://127.0.0.1:${port}`,
    defaultModel: process.env.ASSISTANT_OC_DEFAULT_MODEL || undefined,
    readyTimeoutMs: Number(process.env.OPENCODE_READY_TIMEOUT_MS ?? 60_000),
  }
}

/**
 * Titles OpenCode invents for a session it has not summarised yet.
 *
 * `session.create` with no title yields `New session - <ISO>`, replaced when the
 * harness's own titling call returns. Recognising the placeholder is what keeps it
 * out of the history list — the UI shows "no title yet" as a spinner instead.
 */
const PLACEHOLDER_TITLE = /^(new session\b|untitled\b)/i

/** How long one readiness probe may take before it is retried. Short: a server
 *  still bootstrapping does not answer at all, so waiting longer on any single
 *  attempt buys nothing and only eats the deadline. */
const READY_PROBE_TIMEOUT_MS = 3_000

/** Gap between readiness probes. */
const READY_PROBE_INTERVAL_MS = 500

/** Deadline for the remaining one-shot preflight calls. */
const PREFLIGHT_CALL_TIMEOUT_MS = 10_000

/** `providerID/modelID` → the pair the prompt API takes. */
function splitModel(
  id: string
): { providerID: string; modelID: string } | undefined {
  const slash = id.indexOf('/')
  if (slash <= 0 || slash === id.length - 1) return undefined
  return { providerID: id.slice(0, slash), modelID: id.slice(slash + 1) }
}

/** The two ask-the-user events, narrowed off the SDK's event union so the
 *  handlers below read real fields instead of poking at an untyped bag. */
type PermissionAskedProps = Extract<
  Event,
  { type: 'permission.asked' }
>['properties']
type QuestionAskedProps = Extract<
  Event,
  { type: 'question.asked' }
>['properties']

/** A tool call's lifecycle, so `run()` emits start/args/end exactly once each
 *  even though OpenCode re-sends the whole part on every update. */
interface ToolTrack {
  started: boolean
  ended: boolean
  resulted: boolean
}

/**
 * Does this part belong to the agent's answer, or to the prompt we just sent?
 *
 * `message.part.updated` fires for the USER message too — the prompt is a message
 * with a text part like any other — and the firehose filter upstream only narrows
 * by session. Emitting those parts replayed the user's own prompt back as
 * assistant text, `<page …/>` marker and all (the browser strips that marker from
 * its own bubble, so its appearance is the tell). It also mis-anchored every tool
 * card: the wire attaches a tool call to the text message that is open when it
 * starts, so the echo became that message and the cards floated above the model's
 * real output. A reload looked correct because the transcript is read per role.
 *
 * Unknown ids emit. Roles come from `message.updated`, which OpenCode sends before
 * a message's parts (a part cannot attach to a message that does not exist), so an
 * unknown id means that assumption broke — and then echoing is the lesser evil
 * against silently swallowing the answer.
 *
 * `session.prompt()` does accept a caller-supplied `messageID`, which would make
 * this exact rather than inferred. Deliberately not used: OpenCode's ids are
 * time-sortable and its stored transcript order derives from them, so feeding it a
 * foreign id risks breaking the REPLAY path — which is the one that works today.
 */
export function isAgentPart(
  part: { messageID?: string },
  roles: Map<string, string>
): boolean {
  const id = part.messageID
  return !id || roles.get(id) !== 'user'
}

/**
 * OpenCode's GLOBAL rules file — the one instruction path that does not depend
 * on the session's cwd.
 *
 * OpenCode finds instructions three ways: `AGENTS.md` walking up from the
 * session directory, this global file, and `instructions` in opencode.json. The
 * gateway runs every session in the per-user workspace (`/home/agents/u/<user>`),
 * so the upward walk finds NOTHING — which is exactly how the product prompt got
 * lost when that root moved out of the image's home directory. Shipping the
 * prompt here instead makes it cwd-independent.
 */
export function globalRulesFile(): string {
  const configHome = process.env.XDG_CONFIG_HOME || join(homedir(), '.config')
  return join(configHome, 'opencode', 'AGENTS.md')
}

/** The shape `provider.list()` reports, narrowed to what the picker needs. */
interface ProviderEntry {
  id: string
  name: string
  models?: Record<string, { id: string; name: string }>
}

/**
 * The models the composer's picker may offer, given what the server reports.
 *
 * `declared` (what opencode.json configures) is the filter whenever it has
 * entries; `connected` is the fallback, and an empty result on both leaves the
 * list unfiltered — an over-long picker beats an empty one. Extracted from
 * `models()` because it is where a data-leak regression would land: see the
 * method's comment for why `connected` alone is not enough.
 */
export function pickModels(
  all: ProviderEntry[],
  connected: string[],
  declared: string[] | null
): ModelInfo[] {
  const allowed = declared?.length
    ? new Set(declared)
    : connected.length
      ? new Set(connected)
      : null
  const out: ModelInfo[] = []
  for (const provider of allowed ? all.filter(p => allowed.has(p.id)) : all) {
    for (const model of Object.values(provider.models ?? {})) {
      out.push({
        id: `${provider.id}/${model.id}`,
        name: `${model.name} (${provider.name})`,
      })
    }
  }
  return out
}

/** A turn in flight, from this backend's side: what it takes to cancel it, and
 *  the event queue the pump writes into. */
interface LiveRun {
  controller: AbortController
  events: AsyncQueue<AgentEvent>
}

export class OpenCodeBackend implements AgentBackend {
  readonly id = 'opencode' as const

  /**
   * Filled from what the SDK actually offers, NOT copied from Claude Code:
   *   - `compaction: true` — OpenCode emits a `compaction` message part, live
   *     and in the stored transcript.
   *   - `interaction: true` — backed by BOTH `question.reply` (the question
   *     cards) and `permission.reply` (tool permission), which is the same
   *     'question' | 'permission' union InteractionRequest already models.
   */
  readonly capabilities: BackendCapabilities = {
    interaction: true,
    threadList: true,
    fork: true,
    rename: true,
    compaction: true,
    transcriptExport: true,
    reasoningStream: true,
  }

  readonly sandboxing = 'native-file' as const

  private readonly clients = new Map<string, OpencodeClient>()
  private readonly running = new Map<string, LiveRun>()

  constructor(
    private readonly config: OpenCodeConfig,
    private readonly threads: ThreadStore,
    private readonly interactions: InteractionRegistry
  ) {}

  // --- lifecycle -------------------------------------------------------------

  /**
   * AN OPEN PORT IS NOT A READY SERVER.
   *
   * `opencode serve` binds before it has bootstrapped its first instance, and a
   * request that lands in that window can sit unanswered for minutes — the
   * server logs `creating instance` and then goes quiet. Readiness therefore has
   * to be a REAL call, bounded and retried, not a `connect()`: waiting on the
   * socket alone let the gateway issue its first request into a server that was
   * listening but could not yet reply, and since neither the SDK nor the
   * surrounding loop had a deadline, preflight never returned. The pod then
   * never bound :4099, failed its startup probe, and was killed and restarted on
   * a three-minute cycle — a cold-start race that only shows up on a slower node.
   *
   * Every call below is bounded for the same reason. The contract in registry.ts
   * is that a backend which cannot serve is reported unavailable, not fatal; a
   * backend that HANGS was fatal, because it took the whole pod with it.
   *
   * AND AN ANSWER IS NOT A READY ANSWER. The same lesson, one level in: the
   * server resolves its providers after it starts answering, so an early
   * `provider.list()` returns an EMPTY list rather than failing. Breaking out of
   * the loop on the first successful call and then rejecting an empty result
   * turned that window into a permanent verdict — the harness was withdrawn for
   * the pod's whole life over a list that was populated a second later. It shows
   * up as an environment difference, not a race: the same image serves fine where
   * provider resolution is fast and withdraws OpenCode where it is slow. So an
   * empty list is retried like any other not-ready state, and only the deadline
   * turns it into a verdict.
   */
  async preflight(): Promise<void> {
    const deadline = Date.now() + this.config.readyTimeoutMs
    let why = 'no attempt completed'
    /** Distinguishes "answered, but with nothing" from "did not answer". */
    let answeredEmpty = false
    for (;;) {
      // Cheap and total: it exercises the HTTP surface AND answers the question
      // the next check asks, so a ready server costs one round trip. Capped by
      // what is LEFT of the deadline as well as by its own timeout, or a single
      // slow probe would overrun the budget it is supposed to live inside.
      const budget = Math.min(READY_PROBE_TIMEOUT_MS, deadline - Date.now())
      const probe = await attempt(() => this.models(), Math.max(budget, 1))
      if (probe.ok && probe.value.length) break
      answeredEmpty = probe.ok
      why = probe.ok ? 'answered, but with no models yet' : probe.reason
      if (Date.now() >= deadline) {
        // Two different problems, two different messages: a server that never
        // answered is a readiness failure, while one that kept answering with an
        // empty list for the whole budget is a provider misconfiguration.
        throw new Error(
          answeredEmpty
            ? 'opencode backend: the server still reports no models after ' +
                `${this.config.readyTimeoutMs}ms. Check the provider block in ` +
                'opencode.json (assistant.provider in Helm).'
            : `opencode backend: ${this.config.baseUrl} was not ready within ` +
                `${this.config.readyTimeoutMs}ms (${why})`
        )
      }
      await new Promise(r => setTimeout(r, READY_PROBE_INTERVAL_MS))
    }
    const health = await fetch(`${proxyUrl()}/healthz`, {
      signal: AbortSignal.timeout(PREFLIGHT_CALL_TIMEOUT_MS),
    }).catch(() => null)
    if (!health?.ok) {
      throw new Error(
        `opencode backend: sandbox daemon unreachable at ${proxyUrl()}.`
      )
    }
    await this.assertPromptWired()
  }

  /**
   * Fail startup when the image ships a runtime prompt the harness will not read.
   *
   * This is the one misconfiguration that produces no error at all: the assistant
   * answers, fluently, as a generic coding agent that knows nothing about the
   * deployment it is serving — so it has to be caught here rather than by a user
   * noticing months later. Only
   * fires when a prompt file exists and NOTHING wires it, which is a packaging
   * mistake, never a deployment choice.
   */
  private async assertPromptWired(): Promise<void> {
    const prompt = systemPromptFile()
    if (!existsSync(prompt)) return
    if (existsSync(globalRulesFile())) return
    const declared = await attempt(
      () => this.clientFor('default').config.get(),
      PREFLIGHT_CALL_TIMEOUT_MS
    )
    // A config read that does not answer is not evidence that the prompt is
    // unwired, and refusing to start over it would turn a slow server into a
    // packaging error. Only a config we actually read can convict.
    if (!declared.ok) return
    const instructions = declared.value?.data?.instructions ?? []
    if (instructions.some(item => resolve(item) === resolve(prompt))) return
    throw new Error(
      `opencode backend: ${prompt} exists but nothing loads it. Sessions run in ` +
        `the per-user workspace, so OpenCode's upward AGENTS.md search never ` +
        `reaches it. Ship the prompt at ${globalRulesFile()} (the image does this) ` +
        `or list it under "instructions" in opencode.json.`
    )
  }

  /** One client per working directory: OpenCode scopes sessions by directory,
   *  and that is how per-user isolation is expressed. */
  private clientFor(userKey: string): OpencodeClient {
    const directory = this.userDir(userKey)
    const cached = this.clients.get(directory)
    if (cached) return cached
    const client = createOpencodeClient({
      baseUrl: this.config.baseUrl,
      directory,
    })
    this.clients.set(directory, client)
    return client
  }

  private userDir(userKey: string): string {
    const dir = userDirectory(userKey)
    mkdirSync(dir, { recursive: true })
    return dir
  }

  /**
   * The models a user may pick — the CONFIGURED ones, not OpenCode's catalogue.
   *
   * `provider.list()` returns `all`: every provider OpenCode knows of, which is
   * a models.dev catalogue of a hundred models this deployment cannot reach. The
   * one the deployment actually configured (assistant.provider in Helm, rendered
   * into opencode.json) is a single entry somewhere in that list, so an unfiltered
   * list buries it — the picker offered every foreign model first and ours last.
   *
   * `connected` is NOT that filter: OpenCode counts its own hosted "opencode"
   * (Zen) free models as connected because they need no credentials, so a
   * connected-only filter offers a third-party endpoint this deployment never
   * configured — cluster data leaving with the prompt. The filter is what
   * opencode.json declares: `enabled_providers` when the deployment pinned one,
   * otherwise the keys of its `provider` block. Only when the config declares
   * neither (a hand-written file that authenticates providers out of band) does
   * this fall back to `connected`, and then to the full list — an over-long
   * picker beats an empty one.
   *
   * This is the picker's view only. The server-side allowlist is
   * `enabled_providers` in opencode.json (rendered by the chart); without it an
   * unconfigured provider stays reachable by model id even when hidden here.
   */
  async models(): Promise<ModelInfo[]> {
    const res = await this.clientFor('default').provider.list()
    return pickModels(
      res.data?.all ?? [],
      res.data?.connected ?? [],
      await this.declaredProviders()
    )
  }

  /**
   * The provider ids opencode.json declares, or null when it declares none.
   *
   * `enabled_providers` wins when present: it is the server's own allowlist, so
   * honouring it keeps the picker from offering something the server refuses to
   * run. A config read failure is null too — a lost filter is a long picker, not
   * a broken assistant.
   */
  private async declaredProviders(): Promise<string[] | null> {
    const cfg = await this.clientFor('default')
      .config.get()
      .catch(() => null)
    const ids = cfg?.data?.enabled_providers?.length
      ? cfg.data.enabled_providers
      : Object.keys(cfg?.data?.provider ?? {})
    return ids.length ? ids : null
  }

  // --- threads ---------------------------------------------------------------

  async listThreads(userKey: string): Promise<ThreadInfo[]> {
    // Own threads only: the gateway stamps each backend's answer with that
    // backend's id, so listing another harness's threads duplicates them.
    const refs = this.threads.list(userKey, this.id)
    // Backfill: a title the harness produced while this process was not watching
    // (a restart mid-turn, a thread from before the store cached titles). Stored
    // rather than merged into the response, so the push channel reports it too and
    // the next listing needs no harness call.
    if (refs.some(ref => ref.backendThreadId && !ref.title && !ref.autoTitle)) {
      try {
        const res = await this.clientFor(userKey).session.list()
        const titles = new Map((res.data ?? []).map(s => [s.id, s.title]))
        for (const ref of refs) {
          if (ref.title || ref.autoTitle || !ref.backendThreadId) continue
          this.rememberAutoTitle(ref.id, titles.get(ref.backendThreadId))
        }
      } catch {
        // A listing failure costs titles, not the thread list.
      }
    }
    return refs.map(ref => this.threads.toInfo(ref))
  }

  /**
   * Store a harness-generated title, if it is a real one.
   *
   * OpenCode names a brand-new session `New session - <ISO>` and replaces that
   * once its titling task finishes, so the placeholder has to be recognised and
   * dropped here — it is this harness's private convention, and letting it reach
   * the browser is how "New session - 2026-08-01T05:48:00.651Z" ends up in a
   * history list. Writing through the store is deliberate: its change listeners
   * are what push the title to the browser.
   */
  private rememberAutoTitle(threadId: string, title?: string): void {
    const clean = title?.trim()
    if (!clean || PLACEHOLDER_TITLE.test(clean)) return
    const ref = this.threads.get(threadId)
    if (!ref || ref.autoTitle === clean) return
    this.threads.update(threadId, { autoTitle: clean })
  }

  /**
   * Wait, briefly and off to the side, for a title that lands after the turn.
   *
   * The live pump catches the common case, but OpenCode's titling is a separate
   * model call that can outlive the answer — and by then the event subscription is
   * closed (it must be: it is a server-wide firehose, see `drive`). So this asks a
   * few times and gives up. Detached on purpose: the turn must not wait on it, and
   * every attempt is a local call to the harness in the same pod.
   */
  private backfillAutoTitle(userKey: string, threadId: string): void {
    void (async () => {
      for (const delay of TITLE_BACKFILL_DELAYS_MS) {
        await new Promise(r => setTimeout(r, delay))
        const ref = this.threads.get(threadId)
        // Gone, renamed, or already named by the pump — nothing left to wait for.
        if (!ref || ref.title || ref.autoTitle || !ref.backendThreadId) return
        try {
          const res = await this.clientFor(userKey).session.list()
          const found = (res.data ?? []).find(
            s => s.id === ref.backendThreadId
          )?.title
          this.rememberAutoTitle(threadId, found)
          if (this.threads.get(threadId)?.autoTitle) return
        } catch {
          // Keep trying the remaining attempts; a title is a nicety.
        }
      }
    })()
  }

  async createThread(userKey: string, title?: string): Promise<string> {
    // No OpenCode session here on purpose — see the file header. The thread is
    // real (it has an id, a sandbox key and a place in the list); the harness
    // session appears on the first run.
    return this.threads.create(userKey, this.id, title).id
  }

  async forkThread(userKey: string, threadId: string): Promise<string> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) throw new Error(`unknown thread ${threadId}`)
    if (!ref.backendThreadId) {
      // Nothing was ever said in it, so a fork is just a new thread.
      return this.createThread(userKey, ref.title)
    }
    const res = await this.clientFor(userKey).session.fork({
      sessionID: ref.backendThreadId,
    })
    const forkedId = res.data?.id
    if (!forkedId)
      throw new Error(`opencode: fork of ${threadId} returned no session`)
    const forked = this.threads.create(userKey, this.id, ref.title)
    this.threads.update(forked.id, { backendThreadId: forkedId })
    return forked.id
  }

  async renameThread(
    userKey: string,
    threadId: string,
    title: string
  ): Promise<void> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) throw new Error(`unknown thread ${threadId}`)
    this.threads.update(threadId, { title })
    if (!ref.backendThreadId) return
    await this.clientFor(userKey)
      .session.update({ sessionID: ref.backendThreadId, title })
      .catch(() => {
        // The gateway's title is what the UI shows; the harness copy is a nicety.
      })
  }

  async deleteThread(userKey: string, threadId: string): Promise<void> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) return
    this.threads.remove(threadId)
    if (ref.backendThreadId) {
      await this.clientFor(userKey)
        .session.delete({ sessionID: ref.backendThreadId })
        .catch(() => undefined)
    }
    // The harness has no notion of our threads, so an orphaned sandbox would
    // otherwise linger until its idle timeout.
    await releaseSandbox(threadId)
  }

  async exportThread(
    userKey: string,
    threadId: string,
    opts: { until?: number } = {}
  ): Promise<TranscriptEntry[]> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref?.backendThreadId) return []
    const res = await this.clientFor(userKey).session.messages({
      sessionID: ref.backendThreadId,
    })
    const entries = res.data ?? []
    // `until` cuts the export at a turn that is still running: OpenCode stores a
    // message as soon as it starts, so an in-flight turn IS in this listing, and
    // returning it as history duplicates the stream that is still producing it.
    // Keyed on creation time because that is the only ordering the stored
    // messages share with the live-turn registry.
    const visible = opts.until
      ? entries.filter(e => {
          const created = (e.info as Message).time?.created
          return !created || created < opts.until!
        })
      : entries
    return visible.map(entry => {
      const info = entry.info as Message
      // An assistant turn carries what the live stream reported in `turn-end`;
      // replaying without it is what made a reload drop the model and the cost.
      const a = info.role === 'assistant' ? info : undefined
      return {
        role: info.role === 'user' ? 'user' : 'assistant',
        uuid: info.id,
        ...(info.time?.created
          ? { timestamp: new Date(info.time.created).toISOString() }
          : {}),
        ...(a
          ? {
              model: `${a.providerID}/${a.modelID}`,
              ...(typeof a.cost === 'number' ? { costUsd: a.cost } : {}),
              ...(a.tokens
                ? {
                    usage: {
                      inputTokens: a.tokens.input,
                      outputTokens: a.tokens.output,
                      cacheReadTokens: a.tokens.cache?.read,
                      cacheCreationTokens: a.tokens.cache?.write,
                    },
                  }
                : {}),
            }
          : {}),
        parts: transcriptParts(entry.parts ?? []),
      }
    })
  }

  // --- run -------------------------------------------------------------------

  async interrupt(threadId: string): Promise<void> {
    this.interactions.cancelThread(threadId)
    this.running.get(threadId)?.controller.abort()
    const ref = this.threads.get(threadId)
    if (ref?.backendThreadId) {
      await this.clientFor(ref.userKey)
        .session.abort({ sessionID: ref.backendThreadId })
        .catch(() => undefined)
    }
  }

  async answer(
    requestId: string,
    answers: Record<string, string>
  ): Promise<void> {
    if (!this.interactions.answer(requestId, answers)) {
      throw new NotSupportedError(
        `interaction ${requestId} is not awaiting an answer (already answered, ` +
          `timed out, or from a previous process)`
      )
    }
  }

  run(req: RunRequest): AsyncIterable<AgentEvent> {
    const events = new AsyncQueue<AgentEvent>()
    void this.drive(req, events)
    return events
  }

  private async drive(
    req: RunRequest,
    events: AsyncQueue<AgentEvent>
  ): Promise<void> {
    const userKey = req.userKey
    let threadId = req.threadId
    const client = this.clientFor(userKey)
    // Declared out here so the `finally` can close the event subscription even
    // when the turn dies before `pump.drain()` is ever reached — a throw from
    // `session.prompt()` used to leave the stream open forever.
    let subscription: AbortController | undefined
    try {
      if (!threadId) {
        threadId = await this.createThread(userKey)
        events.push({ t: 'thread', threadId })
      }
      const ref = this.threads.getForUser(threadId, userKey)
      if (!ref) throw new Error(`unknown thread ${threadId}`)

      const controller = new AbortController()
      const live: LiveRun = { controller, events }
      this.running.set(threadId, live)
      req.signal.addEventListener('abort', () => controller.abort(), {
        once: true,
      })

      // THE lazy session: created on the first turn, never on thread creation.
      let sessionID = ref.backendThreadId
      if (!sessionID) {
        const created = await client.session.create({ title: ref.title })
        sessionID = created.data?.id
        if (!sessionID)
          throw new Error('opencode: session.create returned no id')
        this.threads.update(threadId, { backendThreadId: sessionID })
      }

      // Hand the daemon this thread's identity BEFORE any tool can run — which is
      // why it sits after session.create (that call runs no tools) and before
      // prompt(). The alias is what keeps ONE sandbox per thread: the tools are
      // handed OpenCode's session id, not ours.
      await bindSandboxIdentity({
        threadId,
        directory: this.userDir(userKey),
        aliases: [sessionID],
      })

      events.push({ t: 'turn-start' })

      // Subscribe BEFORE prompting, or the first deltas of a fast turn are lost.
      //
      // The subscription owns an HTTP connection to the OpenCode server and MUST
      // be closed when the turn ends — see `subscription` below and the `finally`
      // that aborts it no matter how the turn exits.
      subscription = new AbortController()
      // The signal rides in the SECOND argument: the first is the endpoint's own
      // query parameters (directory/workspace), the second the request options,
      // which the generated client spreads straight into `fetch`.
      const stream = await client.event.subscribe(
        {},
        { signal: subscription.signal }
      )
      const pump = this.pump(
        stream,
        sessionID,
        threadId,
        live,
        controller.signal,
        client,
        subscription
      )

      const model = req.model ? splitModel(req.model) : undefined
      const promptResult = await client.session.prompt({
        sessionID,
        ...(model ? { model } : {}),
        ...(req.agent ? { agent: req.agent } : {}),
        parts: [
          {
            type: 'text',
            text: promptWithPage(
              req.input.map(p => p.text).join('\n'),
              req.pageContext
            ),
          },
        ],
      })

      // The turn is over when prompt() resolves. Give the pump a moment to drain
      // the trailing parts the server emitted just before responding, then stop:
      // waiting on the shared stream to go quiet would hang on the next user's
      // activity.
      await pump.drain()

      // THEN RECONCILE AGAINST THE RESPONSE, WHICH CANNOT LOSE THE RACE.
      //
      // The events above arrive on a SEPARATE connection from the one
      // `session.prompt()` answered on, and nothing orders the two. A part the
      // server emitted in the last moments of the turn can therefore still be in
      // the socket when the prompt returns — and `drain()` then aborts that
      // subscription, discarding it. The 50ms grace only widens the window; it
      // cannot close it, because the loser of a race is not chosen by how long
      // you wait.
      //
      // The symptom was an answer that vanished: reasoning rendered, then the run
      // finished with no text at all, while a reload showed the full reply
      // (the transcript is read back over HTTP, so it never had the race). Models
      // that emit their whole answer in one burst at the end of the turn lose
      // this coin flip often; ones that stream token by token almost never do.
      //
      // `prompt()`'s own response carries the finished message's parts, so it is
      // the authoritative version of what the pump was trying to observe.
      // Replaying it through the SAME emitters and the SAME per-part bookkeeping
      // makes this idempotent by construction: a part already emitted in full
      // produces nothing, a partially-emitted one produces only its tail, and a
      // missed one produces all of it.
      pump.reconcile(promptResult.data?.parts ?? [])

      const info = promptResult.data?.info
      events.push({
        t: 'turn-end',
        ...(info?.modelID
          ? { model: `${info.providerID}/${info.modelID}` }
          : {}),
        ...(info?.tokens
          ? {
              usage: {
                inputTokens: info.tokens.input,
                outputTokens: info.tokens.output,
                cacheReadTokens: info.tokens.cache?.read,
                cacheCreationTokens: info.tokens.cache?.write,
              },
            }
          : {}),
        ...(typeof info?.cost === 'number' ? { costUsd: info.cost } : {}),
      })
    } catch (e) {
      events.push({
        t: 'error',
        message: e instanceof Error ? e.message : String(e),
        // An aborted turn is the user hanging up, not a failure to retry.
        retryable: !req.signal.aborted,
      })
    } finally {
      // The turn is over: drop the event subscription. This is the ONLY thing
      // that returns the connection — the stream is server-wide and never ends
      // on its own, and the SDK's SSE client re-dials on its own if the socket
      // merely drops, so nothing short of aborting it lets go.
      subscription?.abort()
      if (threadId) {
        this.running.delete(threadId)
        // The harness may still be naming this conversation. Watch for it off to
        // the side — the turn is over either way.
        this.backfillAutoTitle(userKey, threadId)
      }
      events.close()
    }
  }

  /**
   * Translate the server's event firehose into this turn's AgentEvents.
   *
   * OpenCode re-sends a whole part on every update rather than a delta, so text
   * is diffed against what was already emitted and tool calls are tracked to
   * emit start/end once. Returns a `drain()` the caller awaits after the prompt
   * resolves, and a `reconcile()` that replays the prompt response's own parts
   * through the same bookkeeping — see the call site for why that is not
   * belt-and-braces but the thing that actually makes the turn complete.
   */
  private pump(
    stream: { stream: AsyncIterable<Event> },
    sessionID: string,
    threadId: string,
    live: LiveRun,
    signal: AbortSignal,
    client: OpencodeClient,
    subscription: AbortController
  ): {
    drain: () => Promise<void>
    reconcile: (parts: Part[]) => void
  } {
    const events = live.events
    const emittedText = new Map<string, string>()
    const tools = new Map<string, ToolTrack>()
    // Compaction markers already emitted, by part id. Unlike text and tools,
    // a compaction carries no state to diff against, so without this a second
    // update of the same part — or the reconcile pass below — would put a second
    // divider in the conversation.
    const compactions = new Set<string>()
    // messageID → role, so a part can be attributed to the agent or to the prompt.
    const roles = new Map<string, string>()
    let settled = false

    // Aborting the subscription rejects the in-flight read, which surfaces here
    // as an AbortError. That is the normal way this task ends, so it is swallowed
    // rather than reported — and it MUST be caught, or ending a turn would raise
    // an unhandled rejection on every single run.
    const task = (async () => {
      for await (const event of stream.stream) {
        if (signal.aborted || settled) return
        // One firehose for the whole server: everything not addressed to this
        // turn's session belongs to somebody else.
        const props: unknown = (event as { properties?: unknown }).properties
        if (
          props &&
          typeof props === 'object' &&
          'sessionID' in props &&
          (props as { sessionID?: string }).sessionID !== sessionID
        ) {
          continue
        }

        switch (event.type) {
          // Roles first: this is what tells the prompt's own parts apart from the
          // agent's. OpenCode announces a message before streaming its parts.
          case 'message.updated': {
            const info = event.properties.info
            if (info?.id && info.role) roles.set(info.id, info.role)
            break
          }
          case 'message.part.updated':
            if (isAgentPart(event.properties.part, roles)) {
              this.emitPart(
                event.properties.part,
                events,
                emittedText,
                tools,
                compactions
              )
            }
            break
          // The harness names a session with its own model call, in parallel with
          // the turn. Catching it here is free — the firehose is already open —
          // and storing it pushes the new title straight to the browser through
          // the thread store's change listeners. What lands after the turn is
          // picked up by `backfillAutoTitle` instead.
          case 'session.updated':
            // Checked against OUR session explicitly: the firehose filter above
            // keys on `properties.sessionID`, which this event does not carry (it
            // nests the whole session under `info`), so without this the title of
            // somebody else's conversation would be stored as ours.
            if (event.properties.info?.id === sessionID) {
              this.rememberAutoTitle(threadId, event.properties.info.title)
            }
            break
          case 'permission.asked':
            void this.askPermission(
              event.properties,
              threadId,
              events,
              signal,
              client
            )
            break
          case 'question.asked':
            void this.askQuestion(
              event.properties,
              threadId,
              events,
              signal,
              client
            )
            break
          default:
            break
        }
      }
    })().catch(() => {})

    return {
      drain: async () => {
        // One macrotask is enough for parts the server flushed with the response.
        await new Promise(r => setTimeout(r, 50))
        settled = true
        // Close the subscription, and note that `settled` alone cannot do it: the
        // flag is only read at the TOP of the loop, so on a quiet server the task
        // stays parked on `await` — holding the connection — until some unrelated
        // session happens to emit. Aborting is what unparks it, by failing the
        // in-flight read; the iterator itself never ends, because the stream is
        // server-wide.
        subscription.abort()
        // Now that it can actually finish, wait for it, so a turn never overlaps
        // its own pump. `task` never rejects (see above), so this cannot throw.
        await task
      },
      // The finished message, as the prompt response reported it. Same emitters,
      // same maps, so anything the stream already delivered is a no-op and only
      // what it missed goes out. Must run AFTER `drain()`: the two would
      // otherwise interleave and could emit the same tail twice.
      reconcile: (parts: Part[]) => {
        for (const part of parts) {
          this.emitPart(part, events, emittedText, tools, compactions)
        }
      },
    }
  }

  private emitPart(
    part: Part,
    events: AsyncQueue<AgentEvent>,
    emittedText: Map<string, string>,
    tools: Map<string, ToolTrack>,
    compactions: Set<string>
  ): void {
    switch (part.type) {
      case 'text': {
        const full = part.text ?? ''
        const already = emittedText.get(part.id) ?? ''
        if (full.length > already.length) {
          events.push({ t: 'text', delta: full.slice(already.length) })
          emittedText.set(part.id, full)
        }
        break
      }
      case 'reasoning': {
        const full = part.text ?? ''
        const already = emittedText.get(part.id) ?? ''
        if (full.length > already.length) {
          events.push({ t: 'thinking', delta: full.slice(already.length) })
          emittedText.set(part.id, full)
        }
        break
      }
      case 'tool': {
        this.emitTool(part, events, tools)
        break
      }
      case 'compaction': {
        // A `notice` used to go out here, and `notice` translates to nothing —
        // so the capability said compaction was observable while the browser
        // could not observe it. It is its own event now, and the wire puts a
        // divider where it happened. Once per part: it has no content to diff,
        // so "already sent" has to be remembered explicitly.
        if (compactions.has(part.id)) break
        compactions.add(part.id)
        events.push({ t: 'compaction', auto: part.auto })
        break
      }
      default:
        break
    }
  }

  private emitTool(
    part: ToolPart,
    events: AsyncQueue<AgentEvent>,
    tools: Map<string, ToolTrack>
  ): void {
    const id = part.callID || part.id
    const track = tools.get(id) ?? {
      started: false,
      ended: false,
      resulted: false,
    }
    tools.set(id, track)

    if (!track.started) {
      events.push({ t: 'tool-start', id, name: part.tool })
      track.started = true
    }
    const state = part.state
    if (
      !track.ended &&
      (state.status === 'running' ||
        state.status === 'completed' ||
        state.status === 'error')
    ) {
      const input = 'input' in state ? state.input : undefined
      events.push({ t: 'tool-end', id, ...(input ? { args: input } : {}) })
      track.ended = true
    }
    if (!track.resulted && state.status === 'completed') {
      events.push({ t: 'tool-result', id, content: state.output ?? '' })
      track.resulted = true
    }
    if (!track.resulted && state.status === 'error') {
      events.push({
        t: 'tool-result',
        id,
        content: 'error' in state ? String(state.error) : 'tool failed',
        isError: true,
      })
      track.resulted = true
    }
  }

  /** A tool wants permission. Same card, same registry, same `/answer` as CC. */
  private async askPermission(
    props: PermissionAskedProps,
    threadId: string,
    events: AsyncQueue<AgentEvent>,
    signal: AbortSignal,
    client: OpencodeClient
  ): Promise<void> {
    const requestID = props.id
    const patterns = props.patterns?.length
      ? ` (${props.patterns.join(', ')})`
      : ''
    const title = `Allow ${props.permission}${patterns}?`
    const request = {
      requestId: requestID,
      kind: 'permission' as const,
      questions: [
        {
          key: 'reply',
          question: title,
          options: [
            { label: 'once', description: 'Allow this one time' },
            {
              label: 'always',
              description: 'Allow for the rest of the session',
            },
            { label: 'reject', description: 'Do not allow' },
          ],
        },
      ],
    }
    events.push({ t: 'interaction', request })
    const answers = await this.interactions.park(
      requestID,
      threadId,
      request,
      signal
    )
    const reply = answers?.reply
    await client.permission
      .reply({
        requestID,
        reply: reply === 'always' || reply === 'reject' ? reply : 'once',
      })
      .catch(() => undefined)
  }

  /** The agent asked the user a question (OpenCode's first-class question API). */
  private async askQuestion(
    props: QuestionAskedProps,
    threadId: string,
    events: AsyncQueue<AgentEvent>,
    signal: AbortSignal,
    client: OpencodeClient
  ): Promise<void> {
    const requestID = props.id
    const asked = props.questions ?? []
    const questions: InteractionQuestion[] = asked.map((q, i) => ({
      // The answer map is keyed by the question text, as everywhere else.
      key: q.question || `q${i}`,
      question: q.question,
      ...(q.header ? { header: q.header } : {}),
      ...(q.multiple ? { multiSelect: true } : {}),
      options: (q.options ?? []).map(o => ({
        label: o.label,
        ...(o.description ? { description: o.description } : {}),
      })),
    }))
    const request = {
      requestId: requestID,
      kind: 'question' as const,
      questions,
    }
    events.push({ t: 'interaction', request })
    const answers = await this.interactions.park(
      requestID,
      threadId,
      request,
      signal
    )
    if (!answers) {
      // Nobody answered: reject so the agent is told rather than left hanging.
      await client.question.reject({ requestID }).catch(() => undefined)
      return
    }
    // QuestionAnswer is a string[] of chosen labels, and the array is POSITIONAL
    // against `questions` — not a map. Rebuild it in the asked order so a
    // multi-question request cannot answer the wrong one.
    await client.question
      .reply({
        requestID,
        answers: questions.map(q => {
          const picked = answers[q.key]
          return picked === undefined ? [] : [picked]
        }),
      })
      .catch(() => undefined)
  }
}

/** Transcript projection: the same part vocabulary the UI reduces, flattened. */
function transcriptParts(parts: Part[]): TranscriptPart[] {
  const out: TranscriptPart[] = []
  for (const part of parts) {
    switch (part.type) {
      case 'text':
        if (part.text) out.push({ type: 'text', text: part.text })
        break
      case 'reasoning':
        if (part.text) out.push({ type: 'thinking', text: part.text })
        break
      case 'tool': {
        const id = part.callID || part.id
        const state = part.state
        out.push({
          type: 'tool-call',
          id,
          name: part.tool,
          args: 'input' in state ? state.input : undefined,
        })
        if (state.status === 'completed') {
          out.push({ type: 'tool-result', id, content: state.output ?? '' })
        } else if (state.status === 'error') {
          out.push({
            type: 'tool-result',
            id,
            content: 'error' in state ? String(state.error) : 'tool failed',
            isError: true,
          })
        }
        break
      }
      case 'compaction':
        out.push({ type: 'compaction', auto: part.auto })
        break
      default:
        break
    }
  }
  return out
}

export function newInteractionId(): string {
  return `int_${randomBytes(8).toString('hex')}`
}
