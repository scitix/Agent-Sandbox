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

// A single model call, independent of the agent backend.
//
// WHY THIS EXISTS SEPARATELY: the topic classifier used to steal its provider
// config out of opencode.json. That coupling is invisible until you change
// harness — under Claude Code there is no such file, the provider lookup returns
// nothing, and the classifier answers `{enabled:false}`, which the UI renders by
// hiding the affordance. The feature disappears and nobody files a bug.
//
// It is also the right shape on its own terms: the classifier wants a cheap,
// pinned, non-reasoning model regardless of what the user is chatting with, so
// tying it to the chat backend would make its cost and behaviour move whenever
// the chat model does.

export interface OneShotRequest {
  system: string
  user: string
  maxTokens: number
  timeoutMs: number
  /** Deterministic by default: this is a classifier, not a writer. */
  temperature?: number
}

export interface OneShotResult {
  /** The model's text. Empty when the call failed — callers must fail safe. */
  content: string
  model: string
  usage?: { input?: number; output?: number; total?: number }
  latencyMs: number
  error?: string
}

export interface OneShotModel {
  readonly model: string
  complete(req: OneShotRequest): Promise<OneShotResult>
}

export type ModelWire = 'openai-chat' | 'anthropic-messages'

export interface OneShotConfig {
  wire: ModelWire
  baseURL: string
  apiKey: string
  model: string
}

async function withTimeout<T>(
  ms: number,
  fn: (signal: AbortSignal) => Promise<T>
): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), ms)
  try {
    return await fn(controller.signal)
  } finally {
    clearTimeout(timer)
  }
}

class OpenAiChatModel implements OneShotModel {
  constructor(private readonly cfg: OneShotConfig) {}
  get model(): string {
    return this.cfg.model
  }

  async complete(req: OneShotRequest): Promise<OneShotResult> {
    const began = Date.now()
    try {
      const res = await withTimeout(req.timeoutMs, signal =>
        fetch(`${this.cfg.baseURL.replace(/\/+$/, '')}/chat/completions`, {
          method: 'POST',
          signal,
          headers: {
            'content-type': 'application/json',
            authorization: `Bearer ${this.cfg.apiKey}`,
          },
          body: JSON.stringify({
            model: this.cfg.model,
            messages: [
              { role: 'system', content: req.system },
              { role: 'user', content: req.user },
            ],
            temperature: req.temperature ?? 0,
            max_tokens: req.maxTokens,
          }),
        })
      )
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const body = (await res.json()) as {
        choices?: {
          message?: { content?: string; reasoning_content?: string }
        }[]
        usage?: {
          prompt_tokens?: number
          completion_tokens?: number
          total_tokens?: number
        }
      }
      const message = body.choices?.[0]?.message
      // A reasoning model that exhausts its budget answers inside the chain of
      // thought and leaves `content` empty; reading the reasoning field recovers
      // the verdict instead of silently reporting a continuation.
      const content = message?.content || message?.reasoning_content || ''
      return {
        content,
        model: this.cfg.model,
        usage: {
          input: body.usage?.prompt_tokens,
          output: body.usage?.completion_tokens,
          total: body.usage?.total_tokens,
        },
        latencyMs: Date.now() - began,
      }
    } catch (e) {
      return {
        content: '',
        model: this.cfg.model,
        latencyMs: Date.now() - began,
        error: e instanceof Error ? `${e.name}: ${e.message}` : String(e),
      }
    }
  }
}

class AnthropicMessagesModel implements OneShotModel {
  constructor(private readonly cfg: OneShotConfig) {}
  get model(): string {
    return this.cfg.model
  }

  async complete(req: OneShotRequest): Promise<OneShotResult> {
    const began = Date.now()
    try {
      const res = await withTimeout(req.timeoutMs, signal =>
        fetch(`${this.cfg.baseURL.replace(/\/+$/, '')}/v1/messages`, {
          method: 'POST',
          signal,
          headers: {
            'content-type': 'application/json',
            authorization: `Bearer ${this.cfg.apiKey}`,
            'anthropic-version': '2023-06-01',
          },
          body: JSON.stringify({
            model: this.cfg.model,
            system: req.system,
            max_tokens: req.maxTokens,
            temperature: req.temperature ?? 0,
            messages: [{ role: 'user', content: req.user }],
          }),
        })
      )
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const body = (await res.json()) as {
        content?: { type?: string; text?: string }[]
        usage?: { input_tokens?: number; output_tokens?: number }
      }
      const content = (body.content ?? [])
        .map(b => (b.type === 'text' ? (b.text ?? '') : ''))
        .join('')
      const input = body.usage?.input_tokens
      const output = body.usage?.output_tokens
      return {
        content,
        model: this.cfg.model,
        usage: {
          input,
          output,
          total:
            typeof input === 'number' && typeof output === 'number'
              ? input + output
              : undefined,
        },
        latencyMs: Date.now() - began,
      }
    } catch (e) {
      return {
        content: '',
        model: this.cfg.model,
        latencyMs: Date.now() - began,
        error: e instanceof Error ? `${e.name}: ${e.message}` : String(e),
      }
    }
  }
}

export function createOneShotModel(cfg: OneShotConfig): OneShotModel {
  return cfg.wire === 'anthropic-messages'
    ? new AnthropicMessagesModel(cfg)
    : new OpenAiChatModel(cfg)
}

/**
 * Build the classifier's model from config. Deliberately separate from the chat
 * backend's config so the two can point at different endpoints — the classifier
 * wants a cheap pinned model whatever the user is chatting with.
 */
export function classifierModelFromEnv(): OneShotModel | null {
  const baseURL = process.env.ASSISTANT_CLASSIFIER_BASE_URL
  const apiKey = process.env.ASSISTANT_CLASSIFIER_API_KEY
  const model = process.env.ASSISTANT_CLASSIFIER_MODEL
  const wire = (process.env.ASSISTANT_CLASSIFIER_WIRE ||
    'openai-chat') as ModelWire
  if (!baseURL || !apiKey || !model) return null
  return createOneShotModel({ wire, baseURL, apiKey, model })
}
