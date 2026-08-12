// Claude Code backend.
//
// PROCESS MODEL — one CLI subprocess PER TURN, not per conversation.
// The SDK spawns a subprocess for each `query()` and the hosting guidance budgets
// from ~1 GiB per live session, so holding one per open conversation would let
// idle tabs consume the pod. Instead every run spawns a process with
// `resume: <session>`, streams the turn and exits. Measured: ~460 ms of spawn
// overhead per turn (≈22 ms behind a warm pool), and resuming reuses the prompt
// cache rather than re-paying for it. Idle conversations therefore cost nothing,
// and concurrency is bounded by turns in flight rather than tabs open.
//
// The turn ends when `query()` ends. A message the user sends while it is running
// is NOT pushed into it: the browser asks whether to interrupt, and a confirmed
// send is an ordinary new turn. That keeps this file's turn boundary the process's
// own lifetime, which is the one thing here that cannot be got subtly wrong.
//
// IDENTITY — the gateway's threadId is the sandbox key; the SDK's session id is
// an implementation detail kept in the thread store.
import {
  type CanUseTool,
  type PermissionResult,
  type SDKMessage,
  getSessionMessages,
  listSessions,
  query,
  renameSession,
} from '@anthropic-ai/claude-agent-sdk'
import { randomBytes } from 'node:crypto'
import { mkdirSync, readFileSync } from 'node:fs'

import { sandboxToolOptions } from '@scitix/agentbox-hands/claude-code'
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
  type Usage,
  sandboxCtxFor,
} from '../backend.ts'
import type { InteractionRegistry } from '../interactions.ts'
import {
  bindSandboxIdentity,
  promptWithPage,
  releaseSandbox,
  systemPromptFile,
} from '../prompt.ts'
import { AsyncQueue } from '../queue.ts'
import type { ThreadRef, ThreadStore } from '../threads.ts'

export interface ClaudeCodeConfig {
  /** Anthropic-format endpoint. Empty means Anthropic's own API. */
  baseURL?: string
  authToken?: string
  models: ModelInfo[]
  defaultModel?: string
  /** Model for the harness's own side tasks (titles and the like). */
  smallModel?: string
  effort?: 'low' | 'medium' | 'high' | 'xhigh' | 'max'
  /**
   * The deployment's runtime prompt, appended to the Claude Code preset.
   *
   * This is where an agent learns what it is: which sandbox it has, what the
   * deployment expects of it, what its own tools mean. Without it the model runs
   * on the bare coding-assistant preset and guesses — which is a working agent,
   * just a generic one.
   */
  systemPromptAppend?: string
  /** Plugin directories the image ships: skills, subagents, hooks. */
  pluginPaths?: string[]
}

export function claudeCodeConfigFromEnv(): ClaudeCodeConfig {
  let models: ModelInfo[] = []
  try {
    models = JSON.parse(process.env.ASSISTANT_CC_MODELS || '[]') as ModelInfo[]
  } catch {
    models = []
  }
  const plugins = (process.env.ASSISTANT_CC_PLUGIN_PATHS || '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
  return {
    baseURL: process.env.ANTHROPIC_BASE_URL || undefined,
    authToken: process.env.ANTHROPIC_AUTH_TOKEN || undefined,
    models,
    defaultModel: process.env.ASSISTANT_CC_DEFAULT_MODEL || models[0]?.id,
    smallModel: process.env.ANTHROPIC_DEFAULT_HAIKU_MODEL || undefined,
    effort:
      (process.env.ASSISTANT_CC_EFFORT as ClaudeCodeConfig['effort']) ||
      'medium',
    // Inline wins; otherwise read the file the image ships. A large markdown
    // prompt does not belong in an env var.
    systemPromptAppend:
      process.env.ASSISTANT_SYSTEM_PROMPT_APPEND ||
      readPromptFile(systemPromptFile()),
    pluginPaths: plugins,
  }
}

/**
 * Read the runtime prompt from disk. A missing file is not fatal: the agent
 * still works, just without product knowledge — and failing startup over it
 * would make an unrelated packaging slip look like a credential problem.
 */
function readPromptFile(path: string): string | undefined {
  try {
    const text = readFileSync(path, 'utf-8').trim()
    return text || undefined
  } catch {
    console.error(
      `[claude-code] no runtime prompt at ${path}; running on the bare preset`
    )
    return undefined
  }
}

/** `<page …/>` marker: tells the agent which page the user was looking at.
 *  Folded into the prompt here rather than in a hook, because UserPromptSubmit
 *  can only add context and cannot alter the message body. */
interface AskUserQuestionInput {
  questions?: {
    question?: string
    header?: string
    multiSelect?: boolean
    options?: { label?: string; description?: string; preview?: string }[]
  }[]
}

/** Deadline for each outbound preflight call. A harness that cannot answer in
 *  this long is not one the pod should wait on: it is reported unavailable and
 *  the other harness still serves. */
const PREFLIGHT_CALL_TIMEOUT_MS = 10_000

/** A turn in flight: what it takes to cancel it. */
interface LiveRun {
  controller: AbortController
  events: AsyncQueue<AgentEvent>
}

export class ClaudeCodeBackend implements AgentBackend {
  readonly id = 'claude-code' as const

  readonly capabilities: BackendCapabilities = {
    interaction: true,
    threadList: true,
    fork: true,
    rename: true,
    // The SDK reports a compaction as a `compact_boundary` system message on the
    // run stream, and `getSessionMessages({ includeSystemMessages: true })`
    // replays it — so the marker survives a reload as well as arriving live.
    compaction: true,
    transcriptExport: true,
    reasoningStream: true,
  }

  readonly sandboxing = 'mcp' as const

  /** In-flight runs, so `interrupt` can cancel one. */
  private readonly running = new Map<string, LiveRun>()

  constructor(
    private readonly config: ClaudeCodeConfig,
    private readonly threads: ThreadStore,
    private readonly interactions: InteractionRegistry
  ) {}

  // --- lifecycle -------------------------------------------------------------

  async preflight(): Promise<void> {
    if (!this.config.models.length) {
      throw new Error(
        'claude-code backend: no models configured. Set assistant.claudeCode.models ' +
          '(rendered into ASSISTANT_CC_MODELS) — the SDK cannot enumerate models ' +
          'from a third-party endpoint, so the list must come from config.'
      )
    }
    if (!this.config.authToken) {
      throw new Error(
        'claude-code backend: no credential. Set assistant.claudeCode.credentials ' +
          '(rendered into ANTHROPIC_AUTH_TOKEN).'
      )
    }
    // Prove the endpoint speaks the Anthropic Messages dialect before serving a
    // single request. A gateway that only offers an OpenAI-shaped API answers
    // 400/404 here, which is far cheaper to diagnose at boot than as a failed
    // first message.
    const base = (this.config.baseURL || 'https://api.anthropic.com').replace(
      /\/+$/,
      ''
    )
    const res = await fetch(`${base}/v1/messages`, {
      method: 'POST',
      // Bounded, like every other outbound call on the startup path: an endpoint
      // that accepts the connection and then never answers would otherwise hold
      // preflight open forever, and preflight is what stands between the pod and
      // binding its readiness port.
      signal: AbortSignal.timeout(PREFLIGHT_CALL_TIMEOUT_MS),
      headers: {
        'content-type': 'application/json',
        authorization: `Bearer ${this.config.authToken}`,
        'anthropic-version': '2023-06-01',
      },
      body: JSON.stringify({
        model: this.config.smallModel || this.config.defaultModel,
        max_tokens: 1,
        messages: [{ role: 'user', content: 'ping' }],
      }),
    })
    if (!res.ok) {
      const body = await res.text().catch(() => '')
      throw new Error(
        `claude-code backend: ${base}/v1/messages answered ${res.status}. ` +
          `Claude Code speaks the Anthropic Messages API and cannot be routed to ` +
          `non-Claude models. Response: ${body.slice(0, 400)}`
      )
    }
    // The sandbox daemon is what makes the tool guarantee true; without it every
    // tool call would fail one at a time instead of once, loudly, here.
    const health = await fetch(`${proxyUrl()}/healthz`, {
      signal: AbortSignal.timeout(PREFLIGHT_CALL_TIMEOUT_MS),
    }).catch(() => null)
    if (!health?.ok) {
      throw new Error(
        `claude-code backend: sandbox daemon unreachable at ${proxyUrl()}.`
      )
    }
  }

  async models(): Promise<ModelInfo[]> {
    return this.config.models
  }

  // --- threads ---------------------------------------------------------------

  private userDir(userKey: string): string {
    const dir = userDirectory(userKey)
    mkdirSync(dir, { recursive: true })
    return dir
  }

  async listThreads(userKey: string): Promise<ThreadInfo[]> {
    // Own threads only: the gateway stamps each backend's answer with that
    // backend's id, so listing another harness's threads duplicates them.
    const refs = this.threads.list(userKey, this.id)
    // Backfill a title the harness summarised while nothing was watching (a
    // restart, or a thread predating the store's title cache). Stored rather than
    // merged into the response, so the push channel announces it too.
    if (refs.some(ref => ref.backendThreadId && !ref.title && !ref.autoTitle)) {
      await this.captureAutoTitles(userKey, refs)
    }
    return refs.map(ref => this.threads.toInfo(ref))
  }

  /**
   * Read the harness's own titles and store the real ones.
   *
   * Claude Code has no placeholder to filter: a session simply has no
   * `customTitle`/`summary` until it has been summarised, so absence IS "not named
   * yet". Writing through the store is what pushes the title to the browser.
   */
  private async captureAutoTitles(
    userKey: string,
    refs: ThreadRef[]
  ): Promise<void> {
    let harness: Awaited<ReturnType<typeof listSessions>> = []
    try {
      harness = await listSessions({ dir: this.userDir(userKey) })
    } catch {
      // A listing failure costs titles, not the thread list.
      return
    }
    const titleFor = new Map(
      harness.map(s => [s.sessionId, (s.customTitle || s.summary)?.trim()])
    )
    for (const ref of refs) {
      if (ref.title || ref.autoTitle || !ref.backendThreadId) continue
      const found = titleFor.get(ref.backendThreadId)
      if (found) this.threads.update(ref.id, { autoTitle: found })
    }
  }

  /**
   * Wait, briefly and off to the side, for the summary of a conversation that has
   * just had its first turn.
   *
   * Claude Code writes the summary into its session file some time after the turn,
   * so there is nothing to await — this re-reads a local file a few times and gives
   * up. Detached: the turn must never wait on a title.
   */
  private backfillAutoTitle(userKey: string, threadId: string): void {
    void (async () => {
      for (const delay of TITLE_BACKFILL_DELAYS_MS) {
        await new Promise(r => setTimeout(r, delay))
        const ref = this.threads.get(threadId)
        // Gone, renamed, or already named — nothing left to wait for.
        if (!ref || ref.title || ref.autoTitle || !ref.backendThreadId) return
        await this.captureAutoTitles(userKey, [ref])
      }
    })()
  }

  async createThread(userKey: string, title?: string): Promise<string> {
    return this.threads.create(userKey, this.id, title).id
  }

  async forkThread(userKey: string, threadId: string): Promise<string> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) throw new Error(`unknown thread ${threadId}`)
    // The fork gets its own gateway id immediately; the harness session is
    // forked lazily on the next run (forkSession is a run-time option), so a
    // fork that is never used costs nothing.
    const forked = this.threads.create(userKey, this.id, ref.title)
    this.threads.update(forked.id, {
      backendThreadId: ref.backendThreadId,
      // Mark it so the next run forks instead of continuing the parent.
      title: ref.title,
    })
    forkPending.add(forked.id)
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
    if (ref.backendThreadId) {
      await renameSession(ref.backendThreadId, title, {
        dir: this.userDir(userKey),
      }).catch(() => {
        // The gateway's title is what the UI shows; the harness copy is a nicety.
      })
    }
  }

  async deleteThread(userKey: string, threadId: string): Promise<void> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref) return
    this.threads.remove(threadId)
    // Release the sandbox too: the harness has no notion of our threads, and an
    // orphaned sandbox otherwise lingers until its idle timeout.
    await releaseSandbox(threadId)
  }

  async exportThread(
    userKey: string,
    threadId: string,
    opts: { until?: number } = {}
  ): Promise<TranscriptEntry[]> {
    const ref = this.threads.getForUser(threadId, userKey)
    if (!ref?.backendThreadId) return []
    const messages = await getSessionMessages(ref.backendThreadId, {
      dir: this.userDir(userKey),
      // Off by default, and the compact boundary is one of the system messages
      // it gates. Without it a reload silently drops every divider and the
      // conversation looks like the agent forgot the first half for no reason.
      includeSystemMessages: true,
    })
    // `until` stops the export before a turn that is still running: the CLI has
    // already written that turn's messages, and returning them as history renders
    // them twice against the live-turn replay.
    const visible = opts.until
      ? messages.filter(m => {
          const stamped = (m as { timestamp?: unknown }).timestamp
          const at =
            typeof stamped === 'string' ? Date.parse(stamped) : Number.NaN
          return Number.isNaN(at) || at < opts.until!
        })
      : messages
    return visible.map(m => ({
      role: (m.type === 'user' ? 'user' : 'assistant') as 'user' | 'assistant',
      uuid: m.uuid,
      parentAgentId: m.parent_agent_id ?? null,
      // The stored transcript entry carries a wall-clock timestamp that the
      // SDK's SessionMessage type does not declare. Read it defensively: with
      // it a reload keeps the send times, without it the UI simply shows none —
      // which is the behaviour we had before, not a regression.
      ...transcriptMeta(m as { timestamp?: unknown; message?: unknown }),
      parts: compactionPart(m) ?? transcriptParts(m.message),
    }))
  }

  // --- run -------------------------------------------------------------------

  async interrupt(threadId: string): Promise<void> {
    this.interactions.cancelThread(threadId)
    const live = this.running.get(threadId)
    live?.controller.abort()
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

      const prompt = promptWithPage(
        req.input.map(p => p.text).join('\n'),
        req.pageContext
      )
      const model = req.model || this.config.defaultModel
      const shouldFork = forkPending.delete(threadId)

      // Hand the daemon this thread's identity BEFORE any tool can run.
      // Without it the daemon reverse-looks-up an OpenCode-shaped API that
      // does not exist here, gets None, and silently degrades both the sandbox
      // cwd and the attachment flush. Done on every run rather than once at
      // creation so a daemon restart self-heals.
      await bindSandboxIdentity({
        threadId,
        directory: this.userDir(userKey),
      })

      events.push({ t: 'turn-start' })

      const stream = query({
        prompt,
        options: {
          cwd: this.userDir(userKey),
          model,
          effort: this.config.effort,
          systemPrompt: {
            type: 'preset',
            preset: 'claude_code',
            ...(this.config.systemPromptAppend
              ? { append: this.config.systemPromptAppend }
              : {}),
          },
          // Never read a developer's own user/project settings inside the pod.
          // Product-owned skills, subagents and hooks arrive via plugins, which
          // work regardless of settingSources.
          settingSources: [],
          ...(this.config.pluginPaths?.length
            ? {
                plugins: this.config.pluginPaths.map(path => ({
                  type: 'local' as const,
                  path,
                })),
              }
            : {}),
          strictMcpConfig: true,
          includePartialMessages: true,
          abortController: controller,
          ...(ref.backendThreadId
            ? {
                resume: ref.backendThreadId,
                ...(shouldFork ? { forkSession: true } : {}),
              }
            : {}),
          // AskUserQuestion is how the agent asks; the sandbox toolset is how it
          // does IO. Everything else stays off.
          ...this.toolOptions(threadId),
          ...(req.agent ? { agent: req.agent } : {}),
          toolConfig: { askUserQuestion: { previewFormat: 'html' } },
          canUseTool: this.canUseTool(threadId, events, req.signal),
          env: this.childEnv(),
        },
      })

      // Per-turn scratch for the bits no single SDK message carries (the model
      // name arrives on assistant messages, the turn ends on the result).
      const seen: { model?: string } = {}
      for await (const message of stream) {
        for (const event of translate(message, seen)) events.push(event)
        if (message.type === 'system' && message.subtype === 'init') {
          if (
            message.session_id &&
            message.session_id !== ref.backendThreadId
          ) {
            this.threads.update(threadId, {
              backendThreadId: message.session_id,
            })
          }
        }
      }
    } catch (e) {
      if (req.signal.aborted) {
        // The user pressed stop. That is not a failure, and labelling it one
        // paints a red error into the transcript of a client that did not itself
        // do the aborting (a second tab, or the /interrupt endpoint). End the
        // turn cleanly and say what happened instead.
        events.push({
          t: 'notice',
          level: 'info',
          text: 'The run was stopped.',
        })
        events.push({ t: 'turn-end', stopReason: 'interrupted' })
      } else {
        events.push({
          t: 'error',
          message: e instanceof Error ? e.message : String(e),
          retryable: false,
        })
      }
    } finally {
      if (threadId) {
        this.running.delete(threadId)
        this.threads.update(threadId, {})
        // The harness may still be summarising this conversation into a title.
        this.backfillAutoTitle(userKey, threadId)
      }
      events.close()
    }
  }

  /**
   * Tool wiring for one run: the sandbox toolset always, plus the Feishu
   * publishing tools ONLY when this run belongs to an analysis job.
   *
   * The gate is deliberately here rather than in a config file: under OpenCode
   * a global deny plus per-agent frontmatter kept these tools away from
   * interactive users, and a gateway that registered them once per process
   * would quietly hand every user the ability to post to the group.
   */
  /**
   * The tool set for one thread: the sandbox toolset plus the question card.
   *
   * Deliberately not extensible from here. An agent's own tools belong in
   * `spec.tools`, declared per agent and gated per scenario, because this backend
   * is shared by every agent the deployment serves — a tool wired in at this level
   * is granted to all of them at once, including the interactive users a
   * scenario-scoped grant is meant to withhold it from. That is why the tools an
   * earlier version stitched in here were removed rather than renamed, and why
   * adding one back is a bigger change than it looks.
   */
  private toolOptions(threadId: string) {
    return withAskUserQuestion(sandboxToolOptions(sandboxCtxFor(threadId)))
  }

  /** Environment for the CLI subprocess. Replaces, not merges, so credentials
   *  are explicit rather than inherited by accident. */
  private childEnv(): Record<string, string | undefined> {
    return {
      ...process.env,
      ...(this.config.baseURL
        ? { ANTHROPIC_BASE_URL: this.config.baseURL }
        : {}),
      ...(this.config.authToken
        ? { ANTHROPIC_AUTH_TOKEN: this.config.authToken }
        : {}),
      ...(this.config.smallModel
        ? { ANTHROPIC_DEFAULT_HAIKU_MODEL: this.config.smallModel }
        : {}),
    }
  }

  /**
   * The human-in-the-loop seam.
   *
   * `AskUserQuestion` reaches this callback even when allowed. Answering it is
   * NOT a matter of allow/deny: the selection is injected into the tool's input
   * as `answers`, keyed by the question text, and the tool then reports it back
   * to the model. Allowing without injecting yields "The user did not answer the
   * questions." — which looks like a working call and silently loses the answer.
   */
  private canUseTool(
    threadId: string,
    events: AsyncQueue<AgentEvent>,
    signal: AbortSignal
  ): CanUseTool {
    return async (toolName, input): Promise<PermissionResult> => {
      if (toolName !== 'AskUserQuestion') return { behavior: 'allow' }

      const asked = (input as AskUserQuestionInput).questions ?? []
      const questions: InteractionQuestion[] = asked.map((q, i) => ({
        key: q.question ?? `q${i}`,
        question: q.question ?? '',
        header: q.header,
        multiSelect: q.multiSelect,
        options: (q.options ?? []).map(o => ({
          label: o.label ?? '',
          description: o.description,
          preview: o.preview,
        })),
      }))

      const requestId = `int_${randomBytes(8).toString('hex')}`
      const request = { requestId, kind: 'question' as const, questions }
      events.push({ t: 'interaction', request })

      // The registry keeps the request too: this stream dies with the page, and a
      // reloaded browser recovers the card from GET /interactions.
      const answers = await this.interactions.park(
        requestId,
        threadId,
        request,
        signal
      )
      if (!answers) {
        // Nobody answered. Allow the call through unchanged so the tool reports
        // "not answered" and the agent moves on, rather than denying and making
        // the model treat it as a failure.
        return { behavior: 'allow' }
      }
      return {
        behavior: 'allow',
        updatedInput: { ...(input as object), answers },
      }
    }
  }
}

/** Threads whose next run should fork the parent session rather than continue it. */
const forkPending = new Set<string>()

/** Keep AskUserQuestion available while the sandbox options take everything else. */
function withAskUserQuestion(
  opts: ReturnType<typeof sandboxToolOptions>
): ReturnType<typeof sandboxToolOptions> {
  return {
    ...opts,
    // `tools: []` removes the built-in base set; name the ones we want back.
    tools: ['AskUserQuestion', 'Task', 'Agent'],
    allowedTools: [...opts.allowedTools, 'Task', 'Agent'],
  }
}

/**
 * Turn one SDK message into zero or more internal events.
 *
 * `seen` carries the little state a single message cannot: the model name appears
 * only on assistant messages, while the turn ends on the result message. Without
 * threading it, `turn-end` has no model — which blanks the per-message model row
 * in the UI and leaves Langfuse unable to attribute cost to a model.
 */
export function translate(
  message: SDKMessage,
  seen: { model?: string } = {}
): AgentEvent[] {
  const out: AgentEvent[] = []

  if (message.type === 'stream_event') {
    const ev = message.event as {
      type?: string
      delta?: { type?: string; text?: string; partial_json?: string }
      content_block?: { type?: string; id?: string; name?: string }
      index?: number
    }
    if (ev.type === 'content_block_start' && ev.content_block) {
      const block = ev.content_block
      if (block.type === 'tool_use' && block.id) {
        out.push({ t: 'tool-start', id: block.id, name: block.name ?? '' })
      }
    } else if (ev.type === 'content_block_delta' && ev.delta) {
      if (ev.delta.type === 'text_delta' && ev.delta.text) {
        out.push({ t: 'text', delta: ev.delta.text })
      } else if (ev.delta.type === 'thinking_delta' && ev.delta.text) {
        out.push({ t: 'thinking', delta: ev.delta.text })
      }
      // input_json_delta carries streaming tool args but without a block id on
      // the delta itself; args are emitted whole on tool-end instead, which is
      // what the UI needs to render a tool card.
    }
    return out
  }

  // The harness folded the context. It arrives on the run stream, in position,
  // which is exactly where the divider belongs — no hook needed (PreCompact
  // fires before the summary exists, and PostCompact is out-of-band).
  if (message.type === 'system' && message.subtype === 'compact_boundary') {
    out.push({
      t: 'compaction',
      auto: message.compact_metadata?.trigger !== 'manual',
    })
    return out
  }

  if (message.type === 'assistant') {
    if (message.message.model) seen.model = message.message.model
    for (const block of message.message.content ?? []) {
      if (typeof block === 'string') continue
      if (block.type === 'tool_use') {
        out.push({ t: 'tool-end', id: block.id, args: block.input })
      }
    }
    return out
  }

  if (message.type === 'user') {
    const content = message.message?.content
    if (Array.isArray(content)) {
      for (const block of content) {
        if (typeof block === 'string') continue
        if (block.type === 'tool_result') {
          out.push({
            t: 'tool-result',
            id: block.tool_use_id,
            content:
              typeof block.content === 'string'
                ? block.content
                : JSON.stringify(block.content),
            isError: block.is_error,
          })
        }
      }
    }
    return out
  }

  if (message.type === 'result') {
    out.push({
      t: 'turn-end',
      usage: {
        inputTokens: message.usage?.input_tokens,
        outputTokens: message.usage?.output_tokens,
        cacheReadTokens: message.usage?.cache_read_input_tokens,
        cacheCreationTokens: message.usage?.cache_creation_input_tokens,
      },
      costUsd: message.total_cost_usd,
      model: seen.model,
      stopReason: message.subtype,
    })
    if (message.subtype !== 'success') {
      out.push({
        t: 'error',
        message: `run ended: ${message.subtype}`,
        retryable: true,
      })
    }
    return out
  }

  return out
}

/**
 * The per-message metadata an export must carry: when it was sent, and (for an
 * assistant turn) which model answered and what it cost.
 *
 * Read off the RAW stored message rather than a typed field because that is
 * where Claude Code keeps it — `SessionMessage.message` is the Anthropic message
 * verbatim, so `model` and `usage` live inside it. A missing field yields an
 * absent property, never a zero: a wrong number here is worse than no number.
 */
export function transcriptMeta(m: { timestamp?: unknown; message?: unknown }): {
  timestamp?: string
  model?: string
  usage?: Usage
  costUsd?: number
} {
  const out: { timestamp?: string; model?: string; usage?: Usage } = {}
  if (typeof m.timestamp === 'string') out.timestamp = m.timestamp
  const msg = m.message as
    | {
        model?: unknown
        usage?: {
          input_tokens?: number
          output_tokens?: number
          cache_read_input_tokens?: number
          cache_creation_input_tokens?: number
        }
      }
    | undefined
  if (typeof msg?.model === 'string') out.model = msg.model
  const u = msg?.usage
  if (u) {
    out.usage = {
      inputTokens: u.input_tokens,
      outputTokens: u.output_tokens,
      cacheReadTokens: u.cache_read_input_tokens,
      cacheCreationTokens: u.cache_creation_input_tokens,
    }
  }
  return out
}

/** Flatten a stored message into transcript parts for export. */
/**
 * A stored compact boundary, as its one transcript part — or undefined when this
 * record is an ordinary message.
 *
 * `SessionMessage` declares only `type` / `uuid` / `message`, so the subtype and
 * metadata are read off both the record and its `message`: the stored JSONL line
 * IS the SDK message, but which of the two levels the fields land on is not part
 * of the SDK's declared surface, and guessing one would mean the divider quietly
 * stops replaying after an SDK bump.
 */
function compactionPart(record: {
  type?: string
  message?: unknown
}): TranscriptPart[] | undefined {
  if (record.type !== 'system') return undefined
  const shape = (candidate: unknown) =>
    candidate as
      | { subtype?: string; compact_metadata?: { trigger?: string } }
      | undefined
  const outer = shape(record)
  const inner = shape(record.message)
  const boundary =
    outer?.subtype === 'compact_boundary'
      ? outer
      : inner?.subtype === 'compact_boundary'
        ? inner
        : undefined
  if (!boundary) return undefined
  return [
    {
      type: 'compaction',
      auto: boundary.compact_metadata?.trigger !== 'manual',
    },
  ]
}

function transcriptParts(message: unknown): TranscriptPart[] {
  const parts: TranscriptPart[] = []
  const content = (message as { content?: unknown })?.content
  if (typeof content === 'string') {
    if (content) parts.push({ type: 'text', text: content })
    return parts
  }
  if (!Array.isArray(content)) return parts
  for (const block of content) {
    if (typeof block === 'string') {
      parts.push({ type: 'text', text: block })
      continue
    }
    const b = block as {
      type?: string
      text?: string
      thinking?: string
      id?: string
      name?: string
      input?: unknown
      tool_use_id?: string
      content?: unknown
      is_error?: boolean
    }
    if (b.type === 'text' && b.text) parts.push({ type: 'text', text: b.text })
    else if (b.type === 'thinking' && b.thinking)
      parts.push({ type: 'thinking', text: b.thinking })
    else if (b.type === 'tool_use' && b.id)
      parts.push({
        type: 'tool-call',
        id: b.id,
        name: b.name ?? '',
        args: b.input,
      })
    else if (b.type === 'tool_result' && b.tool_use_id)
      parts.push({
        type: 'tool-result',
        id: b.tool_use_id,
        content:
          typeof b.content === 'string' ? b.content : JSON.stringify(b.content),
        isError: b.is_error,
      })
  }
  return parts
}
