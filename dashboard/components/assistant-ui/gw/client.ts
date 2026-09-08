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

// Client for the agent gateway — the browser's only contract with the assistant,
// whichever harness runs behind it.
//
// Two things this deliberately does NOT do:
//   * branch on the backend. Everything conditional reads /capabilities, so a
//     backend that cannot fork threads simply has the affordance hidden rather
//     than the component knowing which backend is running.
//   * carry the run transport. That belongs to `@ag-ui/client`'s HttpAgent now
//     (see gw/agent.ts): a run IS its stream, one POST of AG-UI SSE, and the
//     protocol's runtime reduces it. What is left here is the REST around it —
//     capabilities, models, threads, and the parked-question listing.
import { basePathPrefix } from '@/lib/base-path'
import { getToken } from '@/lib/api/client'
import { impersonationAtom, store } from '@/lib/atoms'
import type {
  AgentUsage,
  InteractionRequest,
} from '@/lib/assistant/agent-events'

/** Derived from the runtime base path, never from a build-time constant — the
 *  deploy-time base path is injected as `<base href>` and baking it in pins the
 *  wrong prefix. */
export function gatewayBaseUrl(): string {
  return (
    process.env.NEXT_PUBLIC_ASSISTANT_GW_BASE_URL ||
    `${basePathPrefix()}api/assistant`.replace(/\/{2,}/g, '/')
  )
}

/** One turn's statistics, as the gateway reports them. */
export interface TurnStats {
  model?: string
  usage?: AgentUsage
  costUsd?: number
  stopReason?: string
}

/**
 * The agent state the gateway maintains, mirrored from gateway/wire.ts.
 *
 * Carried as AG-UI `STATE_SNAPSHOT` rather than `CUSTOM`, because the runtime
 * silently drops CUSTOM events while state is reduced, exposed via `useAgUiState`,
 * echoed back on the next run, and restorable on a thread switch. Both sides must
 * agree on this shape.
 */
export interface NavixAgentState {
  /** The gateway's own thread id — authoritative, since the id the browser
   *  generates for a new conversation is only a placeholder. */
  threadId?: string
  /** Per assistant-message statistics, keyed by message id. */
  stats?: Record<string, TurnStats>
  /** Events the gateway's replay log had already discarded when this run
   *  attached: the turn is shown from partway through, and saying so beats
   *  presenting a truncated reply as the whole one. */
  dropped?: number
}

/** Mirrors the gateway's own `BackendCapabilities`. Every field has a consumer
 *  that acts on it — see the gateway-side type for why that is a rule and not a
 *  coincidence. */
export interface BackendCapabilities {
  interaction: boolean
  threadList: boolean
  fork: boolean
  rename: boolean
  compaction: boolean
  transcriptExport: boolean
  reasoningStream: boolean
}

export interface BackendStatus {
  id: string
  available: boolean
  /** Why it cannot serve — shown on the disabled picker entry. */
  reason?: string
}

export interface GatewayInfo {
  backendId: string
  capabilities: BackendCapabilities
}

export interface ModelInfo {
  id: string
  name: string
}

export interface ThreadInfo {
  id: string
  /** A real title, or absent. The gateway normalises every harness placeholder
   *  away (OpenCode's `New session - <ISO>`, Claude Code's empty summary), so an
   *  absent title here always means "not named yet", never "named badly". */
  title?: string
  /** A harness session exists — i.e. someone has actually said something. A
   *  thread is created the moment the user clicks New, so this is what separates
   *  a conversation from an empty allocation. */
  started: boolean
  /** Started, but still waiting for its title. The row shows a spinner until the
   *  gateway pushes the real one. */
  titlePending: boolean
  updatedAt: number
  createdAt?: number
  /** The harness that owns it, stamped by the gateway as it merges the
   *  per-backend lists. */
  backendId?: string
  /** A turn is running in it right now — in ANY tab, or in none: a turn outlives
   *  the response that started it. This is what tells a freshly loaded page that
   *  it should attach to a stream rather than wait for the user to send. */
  live?: boolean
}

/** Where a reader would pick a running turn up. Mirrors the gateway's own
 *  `TurnSnapshot`. */
export interface LiveTurnInfo {
  inFlight: boolean
  /** Events produced so far, in cursor space. */
  seq: number
  /** Events already discarded from the replay log: a re-attach from 0 cannot show
   *  the whole turn, and the UI says so instead of quietly omitting them. */
  dropped: number
  startedAt?: number
  detachedSince?: number | null
}

/** A bot conversation in the read-only listing. `source` is the bot that owns it
 *  — the one identity a viewer needs, since none of these are theirs. */
export interface AnalysisThread extends ThreadInfo {
  source: string
}

/** What the thread-event stream reports. */
export type ThreadEvent =
  | { type: 'updated'; thread: ThreadInfo }
  | { type: 'deleted'; id: string }

export interface TranscriptPart {
  type: 'text' | 'thinking' | 'tool-call' | 'tool-result' | 'compaction'
  text?: string
  id?: string
  name?: string
  args?: unknown
  content?: string
  isError?: boolean
  /** `compaction` only: whether the harness compacted on its own. */
  auto?: boolean
}

export interface TranscriptEntry {
  role: 'user' | 'assistant'
  uuid?: string
  timestamp?: string
  parentAgentId?: string | null
  parts: TranscriptPart[]
  /** What the live turn reported, persisted so a reload does not lose it. */
  model?: string
  usage?: AgentUsage
  costUsd?: number
}

/**
 * The session token and the acting identity, for the BFF in front of the
 * gateway.
 *
 * Every call goes through that proxy, and the proxy is what authenticates —
 * the gateway itself takes the caller's word for who is asking. So a request
 * without the token is not "unauthenticated to the gateway", it is a 401 from
 * the proxy, which is the whole point.
 *
 * The impersonation pair is sent for the same reason every other API call
 * sends it: an admin acting as someone else expects the assistant to act as
 * that person too — its sandbox is created with that person's key, so without
 * these headers the conversation would quietly operate as the admin while the
 * rest of the console shows the selected user's data. The proxy re-derives the
 * decision from the verified session and discards what is sent here, so this is
 * a hint about the selector's state, not a grant.
 */
export function authHeaders(): Record<string, string> {
  const token = getToken()
  const out: Record<string, string> = token
    ? { authorization: `Bearer ${token}` }
    : {}
  const impersonation = store.get(impersonationAtom)
  if (impersonation?.team && impersonation?.user) {
    out['x-impersonate-team'] = impersonation.team
    out['x-impersonate-user'] = impersonation.user
  }
  return out
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${gatewayBaseUrl()}${path}`, {
    ...init,
    headers: {
      'content-type': 'application/json',
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  })
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`gateway ${path} -> ${res.status}: ${body.slice(0, 300)}`)
  }
  return (await res.json()) as T
}

export const gateway = {
  async info(backend?: string): Promise<GatewayInfo> {
    return json<GatewayInfo>(
      backend
        ? `/capabilities?backend=${encodeURIComponent(backend)}`
        : '/capabilities'
    )
  },

  /** Every harness this deployment could run, serving or not. Drives the picker;
   *  an unavailable entry keeps its `reason` so the UI can say why. */
  async backends(): Promise<BackendStatus[]> {
    return (await json<{ backends: BackendStatus[] }>('/backends')).backends
  },

  async models(backend?: string): Promise<ModelInfo[]> {
    return (
      await json<{ models: ModelInfo[] }>(
        backend ? `/models?backend=${encodeURIComponent(backend)}` : '/models'
      )
    ).models
  },

  async listThreads(userKey: string): Promise<ThreadInfo[]> {
    return (
      await json<{ threads: ThreadInfo[] }>(
        `/threads?userKey=${encodeURIComponent(userKey)}`
      )
    ).threads
  },

  /** `backend` picks the harness for the NEW thread; an existing one keeps its
   *  own, so switching the picker never re-homes a conversation. */
  async createThread(
    userKey: string,
    title?: string,
    backend?: string
  ): Promise<string> {
    return (
      await json<{ threadId: string }>('/threads', {
        method: 'POST',
        body: JSON.stringify({ userKey, title, backend }),
      })
    ).threadId
  },

  async renameThread(
    userKey: string,
    threadId: string,
    title: string
  ): Promise<void> {
    await json(`/threads/${encodeURIComponent(threadId)}`, {
      method: 'PATCH',
      body: JSON.stringify({ userKey, title }),
    })
  },

  async deleteThread(userKey: string, threadId: string): Promise<void> {
    await json(`/threads/${encodeURIComponent(threadId)}`, {
      method: 'DELETE',
      body: JSON.stringify({ userKey }),
    })
  },

  async exportThread(
    userKey: string,
    threadId: string
  ): Promise<TranscriptEntry[]> {
    return (
      await json<{ entries: TranscriptEntry[] }>(
        `/threads/${encodeURIComponent(threadId)}/export?userKey=${encodeURIComponent(userKey)}`
      )
    ).entries
  },

  /**
   * Is a turn still running in this conversation?
   *
   * Asked before hydrating one, and again whenever the browser suspects it lost
   * its stream. Never throws: a reconnect that cannot reach the gateway must fall
   * back to "assume not running" and let the next attempt correct it, rather than
   * surfacing an error over a conversation the user can still read.
   */
  async live(threadId: string): Promise<LiveTurnInfo> {
    try {
      return (
        await json<{ live: LiveTurnInfo }>(
          `/threads/${encodeURIComponent(threadId)}/live`
        )
      ).live
    } catch {
      return { inFlight: false, seq: 0, dropped: 0 }
    }
  },

  // --- read-only viewing of the unattended bots' conversations --------------
  //
  // None of these take a `userKey`, and that is the design rather than an
  // omission: the identity that decides what comes back is the thread's OWNER,
  // which only the server knows. There is no counterpart that writes.

  /** Which bots this deployment will show. Empty = the feature is off, and the
   *  menu entry does not exist. */
  async analysisSources(): Promise<string[]> {
    return (await json<{ sources: string[] }>('/analysis/sources')).sources
  },

  /** The bots' conversations in the last `days` (the server caps it), newest
   *  first. Metadata only — the transcript is a second call, made when one is
   *  opened. */
  async analysisThreads(opts: {
    days: number
    sources?: string[]
  }): Promise<{ windowDays: number; threads: AnalysisThread[] }> {
    const params = new URLSearchParams({ days: String(opts.days) })
    if (opts.sources?.length) params.set('source', opts.sources.join(','))
    return json<{ windowDays: number; threads: AnalysisThread[] }>(
      `/analysis/threads?${params}`
    )
  },

  async analysisTranscript(threadId: string): Promise<TranscriptEntry[]> {
    return (
      await json<{ entries: TranscriptEntry[] }>(
        `/analysis/threads/${encodeURIComponent(threadId)}/export`
      )
    ).entries
  },

  async interrupt(userKey: string, threadId: string): Promise<void> {
    await json(`/threads/${encodeURIComponent(threadId)}/interrupt`, {
      method: 'POST',
      body: JSON.stringify({ userKey }),
    })
  },

  /** Answer a parked question. A 409 means it is already settled — expected, not
   *  exceptional, so it resolves rather than throwing. */
  async pendingInteractions(): Promise<
    { requestId: string; threadId: string; request: InteractionRequest }[]
  > {
    try {
      return (
        await json<{
          pending: {
            requestId: string
            threadId: string
            request: InteractionRequest
          }[]
        }>('/interactions')
      ).pending
    } catch {
      // A failed recovery must not block opening the conversation.
      return []
    }
  },

  /**
   * Watch this user's conversations. Returns an unsubscribe.
   *
   * Two facts about a conversation arrive after the request that caused them: the
   * harness's auto-generated title (a separate model call that can outlive the
   * answer) and the first run adopting a harness session. Neither fits on the run
   * stream — AG-UI rejects events after `RUN_FINISHED` — so they are pushed here
   * and the browser never polls for them.
   *
   * `EventSource` rather than a `fetch` reader: it reconnects on its own, which is
   * the entire behaviour we would otherwise have to write.
   */
  watchThreads(userKey: string, onEvent: (e: ThreadEvent) => void): () => void {
    // EventSource cannot set a header, and this stream still has to reach an
    // authenticating proxy — so the session travels as a query parameter that
    // the proxy accepts HERE ONLY and strips before forwarding, keeping it out
    // of the upstream's access log. The alternative, a fetch reader, would
    // mean writing the reconnect behaviour EventSource already has.
    // The acting identity travels the same way and for the same reason: the
    // proxy recomputes `userKey` from it, so a stream opened without it
    // subscribes to the admin's own threads while their runs go to the selected
    // user's. The only symptom would be titles that never arrive.
    const token = getToken()
    const impersonation = store.get(impersonationAtom)
    const acting =
      impersonation?.team && impersonation?.user
        ? `&impersonateTeam=${encodeURIComponent(impersonation.team)}` +
          `&impersonateUser=${encodeURIComponent(impersonation.user)}`
        : ''
    const source = new EventSource(
      `${gatewayBaseUrl()}/threads/events?userKey=${encodeURIComponent(userKey)}` +
        (token ? `&access_token=${encodeURIComponent(token)}` : '') +
        acting
    )
    source.onmessage = e => {
      try {
        onEvent(JSON.parse(e.data as string) as ThreadEvent)
      } catch {
        // A frame we cannot parse is a frame we ignore; the list is still correct,
        // just staler.
      }
    }
    // No onerror handler on purpose: EventSource retries by itself, and logging
    // every reconnect (a pod restart, a proxy timeout) would be pure noise.
    return () => source.close()
  },

  async classify(input: {
    userKey: string
    threadId?: string | null
    context?: string
    newInput: string
  }): Promise<{
    enabled: boolean
    isNewTopic?: boolean
    confidence?: number
    objectCarriesOver?: boolean
    goalCarriesOver?: boolean
    rationale?: string
  }> {
    try {
      return await json('/classify', {
        method: 'POST',
        body: JSON.stringify(input),
      })
    } catch {
      // Fails safe: the nudge must never block a send or nag on an error.
      return { enabled: false }
    }
  },
}
