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

// Vendor transcript records → assistant-ui messages.
//
// Its own module because it has TWO callers with very different needs. The live
// runtime replays history into a conversation you can talk to; the read-only
// analysis viewer renders a bot's transcript with no agent behind it at all. This
// file is pure — no React, no `GatewayAgent`, no `useAgUiRuntime` — so the viewer
// can reuse the conversion without importing the whole live stack to get at it.
//
// Both paths therefore produce the SAME message shapes, which is what lets the
// viewer render with the ordinary message components rather than a second,
// slowly-diverging renderer.
import type { ThreadMessageLike } from '@assistant-ui/react'
import type { AgUiInterrupt } from '@assistant-ui/react-ag-ui'
import { COMPACTION_TOOL_NAME } from '@/lib/assistant/agent-events'

import type { TurnStats } from './client'

/** Structural, not the client's exact `TranscriptEntry`: `type` widens to string
 *  so a part kind added on the server does not have to land here before this
 *  compiles. Unknown kinds are dropped, which is the right failure. */
export interface TranscriptLike {
  role: 'user' | 'assistant'
  uuid?: string
  timestamp?: string
  model?: string
  usage?: TurnStats['usage']
  costUsd?: number
  parentAgentId?: string | null
  parts: {
    type: string
    text?: string
    id?: string
    name?: string
    args?: unknown
    content?: string
    isError?: boolean
    auto?: boolean
  }[]
}

/** A parked question, in the shape the library persists and restores. */
export interface PersistableInterrupt {
  requestId: string
  kind: 'question' | 'permission'
  questions: unknown
}

/** The per-turn model / token / cost facts, or nothing when the record carries
 *  none. Exported because hydration files them separately, keyed by record id,
 *  into the agent state the message footer reads. */
export function statsOf(entry: TranscriptLike): TurnStats | undefined {
  if (!entry.model && !entry.usage && entry.costUsd === undefined)
    return undefined
  return {
    ...(entry.model ? { model: entry.model } : {}),
    ...(entry.usage ? { usage: entry.usage } : {}),
    ...(entry.costUsd !== undefined ? { costUsd: entry.costUsd } : {}),
  }
}

type Part = Exclude<ThreadMessageLike['content'], string>[number]
type ToolPart = Extract<Part, { type: 'tool-call' }>

/**
 * Rebuild conversational turns from vendor transcript records.
 *
 * Claude emits tool results as synthetic user records and can split one assistant
 * turn over several records; rendering those records 1:1 creates empty bubbles and
 * a separate tool group for every stream fragment.
 *
 * `stillParked` is the questions the gateway is STILL waiting on. They are stamped
 * onto the last assistant message under the namespace the AG-UI runtime restores
 * from, so the card comes back after a reload — but only those, because a card for
 * a park the server has forgotten can never be dismissed and blocks every
 * subsequent send. A read-only viewer passes none: there is no composer to unblock
 * and nobody who could answer.
 */
export function transcriptToMessages(
  entries: TranscriptLike[],
  stillParked: readonly PersistableInterrupt[] = []
): ThreadMessageLike[] {
  const messages: ThreadMessageLike[] = []
  const calls = new Map<string, ToolPart>()

  entries.forEach((entry, entryIndex) => {
    const parts: Part[] = []
    entry.parts.forEach(part => {
      if (part.type === 'text' && part.text) {
        parts.push({ type: 'text', text: part.text })
      } else if (part.type === 'thinking' && part.text) {
        parts.push({ type: 'reasoning', text: part.text })
      } else if (part.type === 'tool-call') {
        const args = (part.args ?? {}) as ToolPart['args']
        const call: ToolPart = {
          type: 'tool-call',
          toolCallId: part.id ?? `replay-${entryIndex}-${parts.length}`,
          toolName: part.name ?? 'unknown',
          args,
          // Every tool card renders its one-line preview AND its args block from
          // `argsText`, never from `args` — omitting it shows an empty card.
          argsText: JSON.stringify(args),
        }
        parts.push(call)
        calls.set(call.toolCallId as string, call)
      } else if (part.type === 'compaction') {
        // Replayed as the same synthetic tool call the live wire emits, so one
        // renderer covers both paths. Given its own id: unlike a real call there
        // is no result to match up, and two dividers in a turn must not collide.
        const args = { auto: part.auto !== false }
        parts.push({
          type: 'tool-call',
          toolCallId: `compaction-${entryIndex}-${parts.length}`,
          toolName: COMPACTION_TOOL_NAME,
          args,
          argsText: JSON.stringify(args),
          result: '',
        })
      } else if (part.type === 'tool-result' && part.id) {
        const call = calls.get(part.id)
        if (call) {
          // `ToolPart` is readonly in the public type; the object is ours until
          // it is handed to the runtime.
          Object.assign(call, { result: part.content, isError: part.isError })
        }
      }
    })

    // A record containing only tool results has already updated its matching
    // calls above and must not become an empty user message.
    if (!parts.length) return

    const previous = messages.at(-1)
    if (entry.role === 'assistant' && previous?.role === 'assistant') {
      ;(previous.content as Part[]).push(...parts)
      return
    }
    const stats = statsOf(entry)
    messages.push({
      id: entry.uuid ?? `replay-${entryIndex}`,
      role: entry.role,
      content: parts,
      // A missing timestamp used to become 0 — i.e. 1970 — which rendered as a
      // real send time. NaN is the honest sentinel: `formatClock` suppresses an
      // invalid Date, so the row shows no time rather than a false one. (Date.now()
      // would be worse than 0: "just now" looks right.)
      createdAt: new Date(entry.timestamp ? Date.parse(entry.timestamp) : NaN),
      ...(stats ? { metadata: { custom: { agentbox: stats } } } : {}),
    })
  })

  if (stillParked.length) {
    const at = messages.reduce(
      (found, m, i) => (m.role === 'assistant' ? i : found),
      -1
    )
    if (at >= 0) {
      const last = messages[at]
      const custom = (last.metadata?.custom ?? {}) as Record<string, unknown>
      messages[at] = {
        ...last,
        metadata: {
          ...last.metadata,
          custom: {
            ...custom,
            agui: { interrupts: stillParked.map(asInterrupt) },
          },
        },
      }
    }
  }

  return messages
}

/** Mirror of the gateway's own mapping (see `toInterrupt` in gateway/server.ts):
 *  both sides have to agree, because the card reads `metadata.agentbox`. */
function asInterrupt(request: PersistableInterrupt): AgUiInterrupt {
  return {
    id: request.requestId,
    reason: request.kind === 'permission' ? 'confirmation' : 'input_required',
    metadata: { agentbox: { kind: request.kind, questions: request.questions } },
  }
}
