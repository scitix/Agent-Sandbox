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

// Langfuse tracing, emitted by the gateway rather than by a harness plugin.
//
// WHY HERE AND NOT IN THE HARNESS:
//   * The gateway sees BOTH backends' event streams, so telemetry is written once
//     instead of once per harness. The vendored opencode Langfuse fork exists only
//     because the official plugin's userId is process-global; owning this here
//     removes the fork rather than porting it.
//   * Claude Code's own Langfuse integration is a Stop hook that re-reads the
//     .jsonl transcript and reaches into Langfuse SDK internals to backdate
//     observations. It also never sets a userId. We already hold richer data
//     live — usage and cost per turn — so re-reading a transcript would be a
//     roundabout way to get less.
//   * Claude Code's native OTel is not the path either: its spans are named
//     `claude_code.*`, which matches neither the GenAI semantic conventions nor
//     Langfuse's own namespace, so everything lands in metadata and nothing
//     becomes a generation.
//
// THE SHAPE IS COPIED ON PURPOSE. Both official integrations produce
// turn -> generation -> nested tool spans, and the existing dashboards are built
// on it, so emitting the same shape is what keeps an A/B comparison legible.
//
// Uses the ingestion API rather than OTLP: it lets us state model, usage, cost,
// sessionId and userId exactly, instead of hoping an attribute mapping infers them.
import { randomUUID } from 'node:crypto'

import type { AgentEvent, Usage } from './backend.ts'

export interface LangfuseConfig {
  baseUrl: string
  publicKey: string
  secretKey: string
  environment?: string
}

export function langfuseConfigFromEnv(): LangfuseConfig | null {
  const baseUrl = (process.env.LANGFUSE_BASEURL || '')
    .trim()
    .replace(/\/+$/, '')
  const publicKey = (process.env.LANGFUSE_PUBLIC_KEY || '').trim()
  const secretKey = (process.env.LANGFUSE_SECRET_KEY || '').trim()
  if (!baseUrl || !publicKey || !secretKey) return null
  return {
    baseUrl,
    publicKey,
    secretKey,
    environment: (process.env.LANGFUSE_ENVIRONMENT || '').trim() || undefined,
  }
}

const TIMEOUT_MS = 5_000

function nowIso(): string {
  return new Date().toISOString()
}

/** Post one ingestion batch. Swallows everything: telemetry must never affect a
 *  turn, and this runs off the request path. */
async function post(cfg: LangfuseConfig, batch: unknown[]): Promise<void> {
  if (!batch.length) return
  const token = Buffer.from(`${cfg.publicKey}:${cfg.secretKey}`).toString(
    'base64'
  )
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS)
  try {
    await fetch(`${cfg.baseUrl}/api/public/ingestion`, {
      method: 'POST',
      signal: controller.signal,
      headers: {
        'content-type': 'application/json',
        authorization: `Basic ${token}`,
      },
      body: JSON.stringify({ batch }),
    })
  } catch {
    // Best effort by contract.
  } finally {
    clearTimeout(timer)
  }
}

export interface TurnIdentity {
  threadId: string
  userKey: string
  backendId: string
  model?: string
}

interface ToolSpan {
  id: string
  name: string
  args?: unknown
  startTime: string
  endTime?: string
  result?: string
  isError?: boolean
}

/**
 * Accumulates one turn's events and emits a trace on completion.
 *
 * Buffered rather than streamed because Langfuse wants an observation's end time
 * and usage at creation, and because a turn is short — holding it costs a few KB
 * and avoids one HTTP call per token.
 */
export class TurnTrace {
  private readonly traceId = randomUUID().replace(/-/g, '')
  private readonly generationId = randomUUID()
  private readonly startTime = nowIso()
  private readonly tools = new Map<string, ToolSpan>()
  private text = ''
  private thinking = ''
  private usage?: Usage
  private costUsd?: number
  private model?: string
  private error?: string

  constructor(
    private readonly cfg: LangfuseConfig | null,
    private readonly identity: TurnIdentity,
    private readonly input: string
  ) {}

  observe(event: AgentEvent): void {
    switch (event.t) {
      case 'text':
        this.text += event.delta
        break
      case 'thinking':
        this.thinking += event.delta
        break
      case 'tool-start':
        this.tools.set(event.id, {
          id: event.id,
          name: event.name,
          startTime: nowIso(),
        })
        break
      case 'tool-end': {
        const span = this.tools.get(event.id)
        if (span) span.args = event.args
        break
      }
      case 'tool-result': {
        const span = this.tools.get(event.id)
        if (span) {
          span.endTime = nowIso()
          // Tool results can be large; the head is enough to see what happened.
          span.result = event.content.slice(0, 4000)
          span.isError = event.isError
        }
        break
      }
      case 'turn-end':
        this.usage = event.usage
        this.costUsd = event.costUsd
        this.model = event.model
        break
      case 'error':
        this.error = event.message
        break
      default:
        break
    }
  }

  /** Emit the trace. Never throws. */
  async flush(): Promise<void> {
    if (!this.cfg) return
    const endTime = nowIso()
    const metadata: Record<string, unknown> = {
      backend: this.identity.backendId,
      threadId: this.identity.threadId,
      ...(this.costUsd != null ? { costUsd: this.costUsd } : {}),
    }

    const batch: unknown[] = [
      {
        id: randomUUID(),
        type: 'trace-create',
        timestamp: this.startTime,
        body: {
          id: this.traceId,
          name: 'assistant-turn',
          input: this.input,
          output: this.text,
          // The two identities the dashboards filter on. userId is a real user
          // here, not a directory basename.
          sessionId: this.identity.threadId,
          userId: this.identity.userKey,
          ...(this.cfg.environment
            ? { environment: this.cfg.environment }
            : {}),
          metadata,
          tags: ['assistant', this.identity.backendId],
          timestamp: this.startTime,
        },
      },
      {
        id: randomUUID(),
        type: 'generation-create',
        timestamp: this.startTime,
        body: {
          id: this.generationId,
          traceId: this.traceId,
          name: 'assistant-generation',
          model: this.model || this.identity.model,
          input: this.input,
          output: this.thinking
            ? `${this.thinking}\n\n${this.text}`
            : this.text,
          startTime: this.startTime,
          endTime,
          metadata,
          ...(this.usage
            ? {
                usageDetails: {
                  input: this.usage.inputTokens,
                  output: this.usage.outputTokens,
                  cache_read_input_tokens: this.usage.cacheReadTokens,
                  cache_creation_input_tokens: this.usage.cacheCreationTokens,
                },
              }
            : {}),
          ...(this.costUsd != null
            ? { costDetails: { total: this.costUsd } }
            : {}),
          ...(this.error ? { level: 'ERROR', statusMessage: this.error } : {}),
        },
      },
    ]

    // Tool spans nest under the generation that triggered them, which is the
    // shape both official integrations produce and the dashboards expect.
    for (const span of this.tools.values()) {
      batch.push({
        id: randomUUID(),
        type: 'span-create',
        timestamp: span.startTime,
        body: {
          id: randomUUID(),
          traceId: this.traceId,
          parentObservationId: this.generationId,
          name: `Tool: ${span.name}`,
          input: span.args,
          output: span.result,
          startTime: span.startTime,
          endTime: span.endTime || endTime,
          ...(span.isError ? { level: 'ERROR' } : {}),
        },
      })
    }

    await post(this.cfg, batch)
  }
}

/** Wrap an event stream so a turn is traced as it flows past. Telemetry never
 *  alters the stream: every event is forwarded unchanged. */
export function traceRun(
  cfg: LangfuseConfig | null,
  identity: TurnIdentity,
  input: string,
  events: AsyncIterable<AgentEvent>
): AsyncIterable<AgentEvent> {
  if (!cfg) return events
  const trace = new TurnTrace(cfg, identity, input)
  return (async function* traced() {
    try {
      for await (const event of events) {
        trace.observe(event)
        yield event
      }
    } finally {
      void trace.flush()
    }
  })()
}
