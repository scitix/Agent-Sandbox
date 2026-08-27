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

// The agent-backend contract.
//
// One interface, several harnesses behind it. The rules that keep this honest:
//
//   * CAPABILITIES ARE NEGOTIATED, NOT ASSUMED. A backend declares what it can
//     do; the gateway forwards that to the browser, which hides affordances it
//     cannot support. The alternative — a lowest-common-denominator interface —
//     would drag every backend down to the weakest one.
//   * BACKENDS DO NOT SPEAK THE WIRE PROTOCOL. They emit `AgentEvent`, and
//     exactly one file (wire.ts) turns that into what the browser reads. Without
//     this split every adapter would have to know about message ids, event
//     pairing and chunk merging, three times over.
//   * IDENTITY IS THE GATEWAY'S. Backends map the gateway's threadId onto their
//     own session concept; nothing above this layer learns a harness session id.
import type {
  AgentEvent,
  AgentUsage as Usage,
} from './agent-events.ts'

import type { SandboxCtx } from '@scitix/agentbox-hands'

export type BackendId = 'claude-code' | 'opencode' | 'codex'

/**
 * What a backend can do. Optional-by-absence rather than boolean-by-false where
 * a capability carries data, so a consumer writes `if (!caps.models) return null`
 * instead of branching on a flag and then guessing what is available.
 */
/**
 * What a backend can do, AS THE BROWSER SEES IT.
 *
 * Every field here has a consumer that acts on it — that is the entry
 * requirement, not a coincidence. Facts about HOW a backend is built (which
 * model dialect it speaks, where its model list comes from, how it overrides the
 * sandbox toolset) are not capabilities: nothing above the backend branches on
 * them, and keeping them here made four of ten fields inert while looking
 * exactly as load-bearing as `threadList`. They live on the backend or on its
 * config instead.
 */
export interface BackendCapabilities {
  /** Can the agent ask the user mid-turn and receive an answer? */
  interaction: boolean
  threadList: boolean
  fork: boolean
  rename: boolean
  /** Does this backend report context compaction, so the transcript can mark
   *  where the conversation was folded into a summary? */
  compaction: boolean
  transcriptExport: boolean
  reasoningStream: boolean
}

/**
 * How a backend keeps the agent's file and shell IO off the pod.
 *
 * NOT a capability: the browser never sees it and nothing renders differently
 * because of it. It is an admission requirement — the registry refuses to serve
 * a backend that declares `'none'` rather than quietly running with the sandbox
 * guarantee broken — so it belongs on the backend, next to `preflight`.
 */
export type SandboxBinding = 'mcp' | 'native-file' | 'none'

export interface ModelInfo {
  id: string
  name: string
}

export interface ThreadInfo {
  id: string
  /**
   * A REAL title, or absent — never a harness placeholder.
   *
   * OpenCode names a fresh session `New session - <ISO>` and replaces it once its
   * auto-titling task finishes; Claude Code has no title at all until its summary
   * lands. Both are "no title yet", and normalising them here is what lets the UI
   * show one loading state instead of encoding each harness's placeholder.
   */
  title?: string
  /**
   * A harness session exists, i.e. at least one turn has run.
   *
   * The authoritative "this conversation has messages" signal: the harness session
   * is created lazily by the first run, never by `createThread`. The UI keeps
   * unstarted threads out of the history list — a conversation nobody has spoken
   * in is not history.
   */
  started: boolean
  /** Started, but the real title has not arrived yet. The UI shows a spinner and
   *  waits for the push (see the thread-event stream). */
  titlePending: boolean
  /** Epoch ms of the last activity, for newest-first ordering. */
  updatedAt: number
  createdAt?: number
  /** Which harness owns this thread. Stamped by the gateway when it merges the
   *  per-backend lists — a backend has no reason to know its own id, and asking
   *  each one to fill it in is how the field would eventually be wrong. */
  backendId?: BackendId
  /** A turn is running here right now. Stamped by the gateway from the live-turn
   *  registry (a harness's own listing cannot know, since a turn survives the
   *  response that started it), so a history row can show it and a client that
   *  reconnects knows to attach rather than to send. */
  live?: boolean
}

export interface TranscriptEntry {
  role: 'user' | 'assistant'
  /** Stable id, so an export can be diffed or resumed against. */
  uuid?: string
  timestamp?: string
  /** Present when this entry came from a subagent rather than the main thread. */
  parentAgentId?: string | null
  parts: TranscriptPart[]
  /**
   * What the live stream reported in `turn-end`, persisted per message.
   *
   * Without these an export is strictly worse than the stream it replaces: a
   * reload replays the transcript, and the send time, the model that answered
   * and the token/cost figures all vanish from a conversation the user was
   * looking at a second earlier. They are optional because a harness may not
   * record them, not because they are decoration.
   */
  model?: string
  usage?: Usage
  costUsd?: number
}

export type TranscriptPart =
  | { type: 'text'; text: string }
  | { type: 'thinking'; text: string }
  | { type: 'tool-call'; id: string; name: string; args: unknown }
  | { type: 'tool-result'; id: string; content: string; isError?: boolean }
  /** Where the harness folded the conversation. Exported as well as streamed:
   *  a divider that vanishes on reload is worse than none, because the gap in
   *  the agent's memory stays and the explanation for it does not. */
  | { type: 'compaction'; auto: boolean }

/** A part of an outgoing prompt. Attachments are already staged, so only their
 *  one-line marker travels — never the file's content. */
export type PromptPart = { type: 'text'; text: string }

export interface PageContext {
  key?: string
  cluster?: string
  [param: string]: string | undefined
}

export interface RunRequest {
  /** null starts a new thread; the id is reported back via a `thread` event. */
  threadId: string | null
  userKey: string
  input: PromptPart[]
  /** Which page the user was on; the gateway folds this into the prompt. */
  pageContext?: PageContext
  model?: string
  /**
   * The proxy's analysis-job key, present only for a background bot run.
   * It is BOTH the report tools' join key and the gate that registers them:
   * no jobKey means no publishing tools, so an interactive user can never
   * reach the Feishu group. Never supplied by the model.
   */
  jobKey?: string
  /** Named subagent to run the turn as (the bot flows: autotriage, …). */
  agent?: string
  signal: AbortSignal
}

// The wire contract is defined ONCE, in agent-events.ts, because it has two
// consumers that must never disagree: this gateway emits the events and the
// dashboard reduces them into messages. Defining the union in both places would
// make a drift invisible — a backend adding a field the UI ignores looks like a
// missing tool card, not like an error.
export type {
  AgentEvent,
  AgentUsage as Usage,
  InteractionOption,
  InteractionQuestion,
  InteractionRequest,
} from './agent-events.ts'

/**
 * How long after a turn a backend keeps asking its harness for the conversation's
 * title, and how spaced out the attempts are.
 *
 * Every harness names a conversation with its own model call, which can outlive the
 * answer, and none of them can be awaited. Shared so both backends give up at the
 * same point: a title is a nicety, and a thread whose titling failed must not leave
 * a task retrying forever.
 */
export const TITLE_BACKFILL_DELAYS_MS = [1_000, 3_000, 6_000, 10_000]

/** Thrown by a backend that cannot serve a capability it does not declare. */
export class NotSupportedError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'NotSupportedError'
  }
}

export interface AgentBackend {
  readonly id: BackendId
  readonly capabilities: BackendCapabilities
  /** Checked by the registry before this backend is allowed to serve. */
  readonly sandboxing: SandboxBinding

  /**
   * Fail fast on a misconfiguration: credentials, the model endpoint's dialect,
   * the sandbox daemon. The registry calls this at startup and refuses to serve
   * if it throws — a protocol mismatch must surface at boot, not on a user's
   * first message.
   */
  preflight(): Promise<void>
  models(): Promise<ModelInfo[]>

  listThreads(userKey: string): Promise<ThreadInfo[]>
  createThread(userKey: string, title?: string): Promise<string>
  forkThread(userKey: string, threadId: string): Promise<string>
  renameThread(userKey: string, threadId: string, title: string): Promise<void>
  deleteThread(userKey: string, threadId: string): Promise<void>
  /**
   * The conversation so far.
   *
   * `until` (epoch ms) stops the export before a turn that is still running: that
   * turn's output is replayed from the live-turn log instead, and returning it
   * from both places renders it twice on a reload.
   */
  exportThread(
    userKey: string,
    threadId: string,
    opts?: { until?: number }
  ): Promise<TranscriptEntry[]>

  /** The only streaming entry point. */
  run(req: RunRequest): AsyncIterable<AgentEvent>
  interrupt(threadId: string): Promise<void>
  /** Answer an {@link InteractionRequest} previously emitted by `run`. */
  answer(requestId: string, answers: Record<string, string>): Promise<void>
}

/** How a backend addresses the sandbox for a thread. Always the gateway's
 *  threadId, so a sandbox (and its staged attachments) survives a backend
 *  switch and is never tied to a harness's own session id. */
export function sandboxCtxFor(threadId: string): SandboxCtx {
  return { sessionKey: threadId }
}
