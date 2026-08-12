// The agent gateway's INTERNAL event contract.
//
// This is what a backend adapter emits and what `gateway/wire.ts` translates into
// AG-UI. It is deliberately not the wire format: exactly one file knows what the
// browser reads, so swapping the wire again is a change to that file alone.
//
// It used to carry a reducer as well — event stream to chat messages — from when
// the browser decoded AG-UI back into these events and projected them itself. The
// dashboard now runs the protocol's own runtime (`@assistant-ui/react-ag-ui`), so
// that reduction was a second implementation of something the protocol specifies,
// and it is gone. What the browser still shares with this file is the question
// payload (`InteractionQuestion`), which travels inside an AG-UI interrupt.
//
// Framework-free on purpose: no React, no assistant-ui imports.

/** Monotonic per stream, so a client can tell a gap from a pause. */
export interface WireMeta {
  seq: number
}

export interface AgentUsage {
  inputTokens?: number
  outputTokens?: number
  cacheReadTokens?: number
  cacheCreationTokens?: number
}

export interface InteractionOption {
  label: string
  description?: string
  /** Sanitised HTML, when the tool offered a visual preview. */
  preview?: string
}

export interface InteractionQuestion {
  /** The answer map's key — the question text as the agent phrased it. */
  key: string
  question: string
  header?: string
  multiSelect?: boolean
  options: InteractionOption[]
}

export interface InteractionRequest {
  requestId: string
  kind: 'question' | 'permission'
  questions: InteractionQuestion[]
}

export type AgentEvent =
  | { t: 'thread'; threadId: string }
  | { t: 'turn-start' }
  | {
      t: 'turn-end'
      usage?: AgentUsage
      costUsd?: number
      model?: string
      stopReason?: string
    }
  | { t: 'text'; delta: string }
  | { t: 'thinking'; delta: string }
  | { t: 'tool-start'; id: string; name: string }
  | { t: 'tool-args'; id: string; delta: string }
  | { t: 'tool-end'; id: string; args?: unknown }
  | { t: 'tool-result'; id: string; content: string; isError?: boolean }
  | { t: 'interaction'; request: InteractionRequest }
  /**
   * The harness folded everything above this point into a summary.
   *
   * Positional, not statistical: it marks a place in the conversation, so it is
   * an event rather than something on `turn-end`. `auto` separates a compaction
   * the harness ran on its own (the context window filled up) from one asked for
   * through its API — there is no manual action in this product, so in practice
   * every one is automatic, which is exactly why the marker earns its place: the
   * agent otherwise just appears to have forgotten the earlier conversation.
   */
  | { t: 'compaction'; auto: boolean }
  | { t: 'notice'; level: 'info' | 'warn'; text: string }
  | { t: 'error'; message: string; retryable: boolean }

export type WireEvent = AgentEvent & WireMeta

/**
 * The tool name a compaction travels under on the wire.
 *
 * AG-UI has no event for "something happened here that is not a message", and
 * the runtime materialises exactly three kinds of part — text, reasoning and
 * tool call. `CUSTOM` is dropped by the aggregator and `ACTIVITY_SNAPSHOT` is
 * reserved for `mcp-apps`, so a synthetic tool call is the only way to put a
 * marker at the right POSITION in the transcript. It is never a tool the model
 * called or can call: the gateway mints it on the way out and the browser has a
 * dedicated renderer for it.
 *
 * Shared from here, alongside the event union itself, for the same reason the
 * union is: the gateway writes this name and the dashboard matches on it, and a
 * typo in either would show up as a missing divider rather than as an error.
 */
export const COMPACTION_TOOL_NAME = 'context_compacted'
