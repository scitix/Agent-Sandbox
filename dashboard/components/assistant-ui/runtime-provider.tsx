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

import {
  type Attachment,
  type AttachmentAdapter,
  type CompleteAttachment,
  CompositeAttachmentAdapter,
  type PendingAttachment,
  SimpleImageAttachmentAdapter,
  useAui,
  useAuiState,
} from '@assistant-ui/react'
import { useAgUiInterrupts } from '@assistant-ui/react-ag-ui'
import {
  useAssistantLoadState,
  useAssistantSessionStore,
} from '@/components/assistant-ui/backend-port'
import { CompactionToolUI } from '@/components/assistant-ui/compaction'
import { authHeaders, gatewayBaseUrl } from '@/components/assistant-ui/gw/client'
import { WorkspaceAutoOpenBridge } from '@/components/assistant-ui/workspace-auto-open'
import {
  NavigateToolUI,
  NavigationBridge,
} from '@/components/assistant-ui/navigate-tool-ui'
import {
  basePath,
  basePathPrefix,
} from '@/lib/base-path'
import {
  type AskAttachment,
  atomAssistantCurrentSessionId,
  atomAssistantModel,
  atomAssistantPendingAttachment,
  atomAssistantPendingAutoSend,
  atomAssistantPendingPrompt,
  atomAssistantRunActive,
  atomAssistantTopicNudge,
  atomAssistantTopicNudgeEnabled,
  atomAssistantTopicVerdicts,
  atomAssistantUserKey,
} from '@/lib/assistant/store'
import { userDirectory } from '@/lib/assistant/agent-workspace'
import {
  type AttachmentMarker,
  collectAttachmentMarkers,
  renderAttachmentMarker,
  sandboxNameFromPath,
  stripAttachmentMarkers,
} from '@/lib/assistant/attachment-marker'
import {
  clusterFromPath,
  matchCurrentPage,
} from '@/lib/assistant/current-page'
import {
  type PageContext,
  stripPageMarkers,
} from '@/lib/assistant/page-context'
import { getDefaultStore, useAtom, useAtomValue, useSetAtom } from 'jotai'
import {
  ReactNode,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'

/** localStorage key holding the last-viewed conversation for a scope. Neutral
 *  name because the same slot is used whichever harness runs; the legacy
 *  `opencode.`-prefixed key is migrated on read so nobody loses their place. */
export function storageKey(scope: string) {
  return `navix.assistant.sessionId.${scope}`
}

const LEGACY_STORAGE_PREFIX = 'opencode.sessionId.'

/** Read the persisted conversation id, migrating the pre-rename key once. */
export function readStoredSessionId(scope: string): string | null {
  const current = localStorage.getItem(storageKey(scope))
  if (current) return current
  const legacy = localStorage.getItem(`${LEGACY_STORAGE_PREFIX}${scope}`)
  if (legacy) localStorage.setItem(storageKey(scope), legacy)
  return legacy
}

// Per-user session namespace: the harness isolates a user's sessions by their
// working directory. The browser no longer creates it (the gateway mkdirs it when
// it binds a thread to a backend) but it still has to NAME it, for attachment
// staging and the workspace panel. The path is the product-wide constant, not a
// copy: the gateway hands the same string to the harness and the fs server accepts
// nothing else. An undefined userKey (OSS build) shares one "default" namespace.
export function sessionDirectory(userKey?: string): string {
  return userDirectory(userKey || 'default')
}

// The workspace-fs server (assistant pod): per-user mkdir, attachment staging,
// workspace listing. Its own prefix, separate from the gateway's, because nginx
// gives it a short read timeout and normal proxy buffering while the gateway
// streams. Keep in step with the `${basePath}/assistant-fs/` location in the
// dashboard nginx config and the dev proxy in vite.config.ts — the browser and
// those two are one contract, and pointing at a prefix nginx does not serve
// answers the SPA's index.html to a POST instead of failing loudly.
export const ASSISTANT_FS_BASE_URL =
  process.env.NEXT_PUBLIC_ASSISTANT_FS_BASE_URL ||
  `${basePathPrefix()}api/assistant-fs`.replace(/\/{2,}/g, '/')

// Read a staged attachment's content back from the assistant pod (the exact bytes
// the user uploaded) for download/preview of a sent message (proposal 0055).
// Throws on a miss (session reclaimed) so the caller can show an "expired" notice.
export async function readStagedAttachment(
  sessionID: string,
  sandboxName: string
): Promise<string> {
  const userKey = getDefaultStore().get(atomAssistantUserKey)
  const res = await fetch(`${ASSISTANT_FS_BASE_URL}/attach-read`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...authHeaders() },
    body: JSON.stringify({
      sessionID,
      dir: sessionDirectory(userKey),
      sandboxName,
    }),
  })
  if (!res.ok) throw new Error(`attach-read failed: ${res.status}`)
  const data = (await res.json()) as { content?: string }
  return data.content ?? ''
}

export interface TopicVerdict {
  enabled: boolean
  isNewTopic?: boolean
  confidence?: number
  /** Did the conversation's subject (pod / node / cluster / team …) carry over? */
  objectCarriesOver?: boolean
  /** Did the line of enquiry carry over? A new topic needs BOTH to change. */
  goalCarriesOver?: boolean
  /** One-line justification, surfaced in the user message's More menu. */
  rationale?: string
  /** Langfuse trace for this check, so a verdict in the UI can be looked up. */
  traceId?: string
}

// Ask the gateway whether `newInput` starts a new topic vs continues the
// conversation. It runs on a model pinned in the deployment (the api key never
// reaches the browser) and is deliberately backend-independent — a one-shot cheap
// call whose cost should not move when the chat model does. Fails SAFE: any error
// → a disabled/negative verdict, so the classifier can never block or misfire.
// `sessionID`/`userKey` are observability only — they let the server file its
// Langfuse trace under the same session and user as the conversation itself.
export async function classifyTopic(
  context: string,
  newInput: string,
  model?: string,
  sessionID?: string | null,
  userKey?: string
): Promise<TopicVerdict> {
  try {
    const res = await fetch(`${gatewayBaseUrl()}/classify`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...authHeaders() },
      body: JSON.stringify({
        context,
        newInput,
        model,
        sessionID,
        // The gateway names these by their own vocabulary; sending both keys
        // costs nothing and keeps one call site.
        threadId: sessionID,
        userKey,
      }),
    })
    if (!res.ok) return { enabled: false }
    return (await res.json()) as TopicVerdict
  } catch {
    return { enabled: false }
  }
}

// Which catalog page the browser is on right now, for the `<page …/>` marker
// appended to outgoing prompts (see injectPageMarker below). Read at SEND time
// from window.location — not from a React hook — so it is the page as of the
// send, whatever route changes happened since the runtime mounted.
//
// A route the catalog does not claim still reports its CLUSTER. Without that
// fallback the most common send of all carried no marker: the cluster home
// redirects to the assistant page, which is deliberately not a catalog page (the
// agent has no business navigating the user to the chat they are already in), so
// the first question of every session — asked from the landing page — left the
// agent without even a cluster. A key is never invented for such a route; the
// marker just narrows to what the URL genuinely says.
//
// An EARLIER version of this feature injected the "current cluster" and it went
// wrong: the agent treated it as a SCOPE and searched only that cluster, when
// `navix search` deliberately spans every cluster and hub. The marker is now a
// referent for "this node" / "the current cluster" when the conversation has
// named none, and the assistant prompt says so in as many words. Sessions are
// still per-user (no cluster scope), and cluster attribution for observability
// still comes from the actual tool calls (see the langfuse plugin).
export function currentPageContext(): PageContext | undefined {
  if (typeof window === 'undefined') return undefined
  const { pathname } = window.location
  const page = matchCurrentPage(pathname, basePath())
  if (page) return page
  const cluster = clusterFromPath(pathname, basePath())
  return cluster ? { cluster } : undefined
}

// Attachment support (proposal 0055). Instead of inlining a text attachment's
// full content into the prompt (the stock SimpleTextAttachmentAdapter wraps it as
// `<attachment name=…>…full file…</attachment>` → the whole file lands in the
// model's context), we STAGE the content to the assistant pod (fs.py /attach) and
// put only a one-line reference into the message. The daemon flushes it into the
// agent's sandbox on the first tool call; the agent read/greps it on demand.
// Widen the accept list to the manifests/logs a scheduling user actually attaches.
const ATTACHMENT_ACCEPT =
  'text/*,application/json,application/yaml,application/x-yaml,application/xml,' +
  '.yaml,.yml,.json,.log,.txt,.md,.csv,.xml,.conf,.cfg,.ini,.env,.toml,.sh,.go,.ts,.tsx,.js,.py'

// Human-readable byte size for the marker (so the agent can judge read vs grep).
function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

// The compact marker a staged attachment leaves in the message. `name` is the
// display label (may be non-ASCII — it's data, not a path); `path` is the ASCII
// sandbox path; `size`/`lines` let the agent decide whether to read the whole
// file or grep it. Rendered by the headless module that also parses it back
// (attachment.tsx renders the chip from that parse), so the two can't drift.
function buildAttachmentMarker(displayName: string, up: StagedUpload): string {
  return renderAttachmentMarker({
    name: displayName,
    path: up.path,
    size: formatBytes(up.size),
    lines: String(up.lines),
  })
}

function randSuffix(): string {
  const a = new Uint8Array(2)
  crypto.getRandomValues(a)
  return Array.from(a, b => b.toString(16).padStart(2, '0')).join('')
}

function extForType(type: string): string {
  if (type.includes('json')) return '.json'
  if (type.includes('markdown')) return '.md'
  if (type.includes('yaml')) return '.yaml'
  if (type.includes('csv')) return '.csv'
  return '.txt'
}

// A File carrying an explicit ASCII slug (set by Ask-AI prefill so the sandbox
// file gets a meaningful name even when the display label is localized/Chinese).
type SluggedFile = File & { navixSlug?: string }

// Split a picked/generated File into a user-facing display name (kept verbatim in
// the chip + marker) and an ASCII, deduped sandbox file name (the on-disk path
// segment the backend validates). Non-ASCII display names collapse to a safe
// base; a `navixSlug` (translation-key-derived) takes precedence when present.
function deriveAttachmentNames(file: File): {
  displayName: string
  sandboxName: string
} {
  const displayName = file.name || 'attachment'
  // The slug (if any) is an ASCII name that may carry its own extension; else
  // fall back to the display name. Extension resolves from that name, else type.
  const nameForBase = (file as SluggedFile).navixSlug || displayName
  const dot = nameForBase.lastIndexOf('.')
  const rawExt = dot > 0 ? nameForBase.slice(dot + 1) : ''
  const ext = /^[A-Za-z0-9]{1,8}$/.test(rawExt)
    ? `.${rawExt.toLowerCase()}`
    : extForType(file.type)
  const stem = dot > 0 ? nameForBase.slice(0, dot) : nameForBase
  let base = stem
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/^[-.]+|[-.]+$/g, '')
    .slice(0, 64)
  if (!base) base = 'attachment'
  return { displayName, sandboxName: `${base}-${randSuffix()}${ext}` }
}

/** Turn a staged sandbox file name back into a slug for a RE-stage, dropping the
 *  4-hex dedup suffix {@link deriveAttachmentNames} added — otherwise every
 *  carry-over stacks another one (`nodes-19bf-a3c2.json`). */
function slugFromSandboxName(sandboxName: string): string {
  const dot = sandboxName.lastIndexOf('.')
  const stem = dot > 0 ? sandboxName.slice(0, dot) : sandboxName
  const ext = dot > 0 ? sandboxName.slice(dot) : ''
  return `${stem.replace(/-[0-9a-f]{4}$/, '')}${ext}`
}

/**
 * Read a sent message's staged attachments back off the pod so they can be
 * re-attached in ANOTHER session — the topic-switch move, which branches to a
 * fresh session and re-sends the message there.
 *
 * A new session means a new sandbox, so the old marker's path resolves to nothing
 * for the agent: the reference has to be re-staged, not copied. The pod still
 * holds the exact bytes the user uploaded (keyed by the OLD session id), so
 * re-attaching costs one read per file and no re-upload by the user.
 *
 * Attachments whose staged copy is gone (session reclaimed / pod restarted) are
 * skipped: moving the question is still better than refusing to move it, and the
 * user can re-attach.
 */
export async function readAttachmentsForCarryOver(
  fromSessionID: string | null | undefined,
  attachments: { name: string; path?: string }[]
): Promise<AskAttachment[]> {
  if (!fromSessionID) return []
  const out: AskAttachment[] = []
  for (const a of attachments) {
    const sandboxName = a.path ? sandboxNameFromPath(a.path) : undefined
    if (!sandboxName) continue
    try {
      out.push({
        name: a.name,
        markdown: await readStagedAttachment(fromSessionID, sandboxName),
        slug: slugFromSandboxName(sandboxName),
      })
    } catch (e) {
      console.warn('[opencode] attachment carry-over failed:', sandboxName, e)
    }
  }
  return out
}

interface StagedUpload {
  path: string
  sandboxName: string
  size: number
  lines: number
}

// Keyed by the PendingAttachment id: the in-flight (or settled) staging upload.
// add() starts it in the background so the (possibly slow) round-trip overlaps
// the user still typing; send() awaits it to emit the marker. Cleared on
// send/remove.
const attachmentUploads = new Map<string, Promise<StagedUpload | null>>()

/** Stages a text attachment to the assistant pod and references it by sandbox path
 *  instead of inlining its content (proposal 0055). Falls back to the inline form
 *  if staging is unavailable so content is never dropped. */
class SandboxTextAttachmentAdapter implements AttachmentAdapter {
  accept = ATTACHMENT_ACCEPT

  async add({ file }: { file: File }): Promise<PendingAttachment> {
    const id = crypto.randomUUID()
    const { displayName, sandboxName } = deriveAttachmentNames(file)
    // Stage in the background (pod-local write is instant; it does NOT spin up the
    // sandbox — that happens lazily on the first tool call).
    attachmentUploads.set(
      id,
      (async (): Promise<StagedUpload | null> => {
        try {
          const sessionID = getDefaultStore().get(atomAssistantCurrentSessionId)
          if (!sessionID) return null
          const userKey = getDefaultStore().get(atomAssistantUserKey)
          const content = await file.text()
          const res = await fetch(`${ASSISTANT_FS_BASE_URL}/attach`, {
            method: 'POST',
            headers: { 'content-type': 'application/json', ...authHeaders() },
            body: JSON.stringify({
              sessionID,
              dir: sessionDirectory(userKey),
              sandboxName,
              content,
            }),
          })
          if (!res.ok) return null
          const data = (await res.json()) as {
            path?: string
            sandboxName?: string
          }
          if (!data.path) return null
          return {
            path: data.path,
            sandboxName: data.sandboxName || sandboxName,
            size: new Blob([content]).size,
            lines: content ? content.split('\n').length : 0,
          }
        } catch {
          return null
        }
      })()
    )
    return {
      id,
      type: 'document',
      name: displayName,
      contentType: file.type,
      file,
      status: { type: 'requires-action', reason: 'composer-send' },
    }
  }

  async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
    const up = await attachmentUploads.get(attachment.id)
    attachmentUploads.delete(attachment.id)
    const text = up
      ? buildAttachmentMarker(attachment.name, up)
      : // Degrade to the inline form so the user's content is never lost.
        `<attachment name=${attachment.name}>\n${await attachment.file.text()}\n</attachment>`
    return {
      ...attachment,
      status: { type: 'complete' },
      content: [{ type: 'text', text }],
    }
  }

  async remove(attachment: Attachment): Promise<void> {
    attachmentUploads.delete(attachment.id)
  }
}

export const ATTACHMENT_ADAPTER = new CompositeAttachmentAdapter([
  new SimpleImageAttachmentAdapter(),
  new SandboxTextAttachmentAdapter(),
])

interface SessionState {
  sessionId: string | null
  error: Error | null
}

// Filled by whichever host is mounted, so every consumer of the open
// conversation is harness-neutral without knowing it.
const AssistantSessionContext = createContext<SessionState>({
  sessionId: null,
  error: null,
})

/** Provide the open conversation to the tree. Used by both hosts. */
export function AssistantSessionProvider({
  value,
  children,
}: {
  value: SessionState
  children: ReactNode
}) {
  return (
    <AssistantSessionContext.Provider value={value}>
      {children}
    </AssistantSessionContext.Provider>
  )
}

/** The open conversation, whichever harness produced it. */
export function useAssistantSession() {
  return useContext(AssistantSessionContext)
}

// Force-reconnect the live `/event` SSE stream (aborts it so react-opencode
// reconnects + re-syncs). Provided by AssistantHost; consumed by the watchdog
// bridge and the in-thread recovery banner so they can heal a silently-dead
// stream without prop-drilling the abort ref through the whole app.
const AssistantReconnectContext = createContext<() => void>(() => {})

/** The transport-reset callback. Named for the product, not a harness: the
 *  gateway passes a no-op because a run IS its own stream there. */
export function useAssistantReconnect() {
  return useContext(AssistantReconnectContext)
}

/** Provide a transport-reset callback. The gateway passes a no-op: a run IS its
 *  own stream there, so there is no long-lived connection to heal. */
export function AssistantReconnectProvider({
  value,
  children,
}: {
  value: () => void
  children: ReactNode
}) {
  return (
    <AssistantReconnectContext.Provider value={value}>
      {children}
    </AssistantReconnectContext.Provider>
  )
}

export interface AssistantHostProps {
  children: ReactNode
  scope?: string
  // Whether the assistant is currently visible — the panel is open or the user
  // is on the dedicated /assistant page (computed by the layout). Drives the
  // lazy connection: the runtime (and its stream) is only mounted while the
  // assistant is wanted, not held open in the background all day.
  active?: boolean
}

export function AssistantBridges() {
  return (
    <>
      <RunActiveBridge />
      <AskAiFreshSessionBridge />
      <ComposerPrefillBridge />
      <ComposerPrefillAttachmentBridge />
      <ComposerAutoSendBridge />
      <TopicNudgeBridge />
      <CompactionToolUI />
      <WorkspaceAutoOpenBridge />
      {/* Acts on `open_page`. Here rather than in the tool card because a
          collapsed tool group never mounts its children. */}
      <NavigationBridge />
      <NavigateToolUI />
    </>
  )
}

/** Mirror the runtime's run state (a reply streaming) into an atom so
 *  AssistantHost — which lives above the runtime — can keep the connection alive
 *  across a panel close that happens mid-reply (and for background agent runs).
 *  Resets to false when the run ends or the runtime unmounts, so the connection
 *  can drop once nothing needs it.
 *
 *  A pending interrupt counts as active. AG-UI ends the run when the agent asks a
 *  question, so `isRunning` goes false while the user is still expected to answer
 *  — and letting the panel's grace window expire there would unmount the runtime
 *  and discard the (in-memory) interrupt. */
function RunActiveBridge() {
  const isRunning = useAuiState(s => s.thread.isRunning)
  const awaitingUser = useAgUiInterrupts().length > 0
  const setRunActive = useSetAtom(atomAssistantRunActive)
  const active = isRunning || awaitingUser
  useEffect(() => {
    setRunActive(active)
  }, [active, setRunActive])
  // Separate unmount-only cleanup so a value change doesn't flicker false→true.
  useEffect(() => () => setRunActive(false), [setRunActive])
  return null
}

/** Consumes the Ask-AI "prefer a fresh session" flag once the runtime is mounted:
 *  if the current thread already holds a conversation, branch to a fresh session
 *  so the queued question doesn't bleed into it; then clear the flag, which
 *  unblocks the composer pre-fill bridges to fill the (now fresh) thread. Runs
 *  the create-then-prefill ordering Ask-AI used to do at click time, but here —
 *  so Ask-AI no longer needs the runtime mounted when clicked (the panel may be
 *  closed; clicking is what opens it). */
function AskAiFreshSessionBridge() {
  const [pendingFresh, setPendingFresh] = useAtom(
    atomAssistantPendingPrompt
  )
  const loadState = useAssistantLoadState()
  const isEmpty = useAuiState(s => s.thread.messages.length === 0)
  const { create } = useCreateAssistantSession()

  // Refs so churn in `create` (its identity changes when its internal `busy`
  // toggles) or `isEmpty` (flips the instant we switch to the new empty session)
  // does NOT re-run the effect. If it did, the re-run would either no-op `create`
  // (busy) yet still clear the flag early — landing the prefill in the OLD thread
  // just before the real switch wipes the view — or cancel the in-flight switch.
  const createRef = useRef(create)
  createRef.current = create
  const isEmptyRef = useRef(isEmpty)
  isEmptyRef.current = isEmpty
  // One branch decision per pending-fresh cycle.
  const handledRef = useRef(false)

  useEffect(() => {
    if (!pendingFresh) {
      handledRef.current = false
      return
    }
    // Wait for the resumed thread to finish loading before judging emptiness —
    // messages arrive asynchronously, so acting during load would treat a
    // populated conversation as empty and append the ask into it.
    if (loadState !== 'ready' && loadState !== 'error') return
    if (handledRef.current) return
    handledRef.current = true
    void (async () => {
      // Branch to a fresh session only when the current one has content; await
      // the switch fully before clearing the flag, so the composer prefill
      // bridges (which wait on it) fill the NEW thread, not the old one.
      if (!isEmptyRef.current) {
        try {
          await createRef.current()
        } catch {
          // Fall through — fill the current session rather than dropping the ask.
        }
      }
      setPendingFresh(null)
    })()
  }, [pendingFresh, loadState, setPendingFresh])

  return null
}

/** Eager-create a new OpenCode session and switch to it. Bypasses assistant-ui's
 *  lazy-init flow (which leaves the composer disabled until first send). */
/**
 * Eager-create a conversation and switch to it.
 *
 * Bypasses assistant-ui's lazy-init flow, which leaves the composer disabled
 * until the first send. Backed by the port, so "New session" works under either
 * harness; a backend that cannot enumerate conversations has no store and the
 * call is a no-op rather than a crash.
 */
export function useCreateAssistantSession() {
  const store = useAssistantSessionStore()
  const [busy, setBusy] = useState(false)

  const create = useCallback(async () => {
    if (busy || !store) return
    setBusy(true)
    try {
      const id = await store.create()
      if (id) store.switchTo(id)
    } finally {
      setBusy(false)
    }
  }, [busy, store])

  return { create, busy }
}

/** When something pushes a pending prompt into the atom (e.g. an Ask AI button),
 *  drop it into the composer and clear the atom. Runs once the runtime mounts. */
function ComposerPrefillBridge() {
  const aui = useAui()
  const [pending, setPending] = useAtom(atomAssistantPendingPrompt)
  // Two gates before the prompt may land, so it fills the SETTLED thread rather
  // than one still being switched (which would reset the composer and silently
  // drop the text — setText doesn't throw, so the old retry never re-fired):
  //   - pendingFresh false — the AskAiFreshSessionBridge branch has settled;
  //   - loadState ready/error — the resumed thread finished loading (mirrors
  //     AskAiFreshSessionBridge), so the composer belongs to the final thread.
  const pendingFresh = useAtomValue(atomAssistantPendingPrompt)
  const loadState = useAssistantLoadState()
  const loadReady = loadState === 'ready' || loadState === 'error'

  useEffect(() => {
    if (!pending || pendingFresh || !loadReady) return
    // Event-driven, not time-boxed: the pending atom is KEPT until setText
    // verifiably takes, and this effect re-runs whenever the gates change — so a
    // slow cold start still gets the prefill once it settles, instead of the old
    // 6s budget expiring and dropping it. The short retry only covers the
    // sub-render gap where the composer element trails the ready thread.
    let cancelled = false
    let attempts = 15
    const tick = () => {
      if (cancelled) return
      try {
        aui.composer().setText(pending)
      } catch {
        if (attempts-- > 0) setTimeout(tick, 100)
        return
      }
      setPending(null)
    }
    tick()
    return () => {
      cancelled = true
    }
  }, [pending, pendingFresh, loadReady, aui, setPending])

  return null
}

/** Attachment counterpart to {@link ComposerPrefillBridge}: when an Ask AI
 *  button (or the topic-switch move) pushes content payloads into the atom, wrap
 *  each in a `text/markdown` File and add it to the composer as an attachment
 *  (SandboxTextAttachmentAdapter accepts it), then clear the atom. */
function ComposerPrefillAttachmentBridge() {
  const aui = useAui()
  const [pending, setPending] = useAtom(atomAssistantPendingAttachment)
  // Same two gates as ComposerPrefillBridge: hold until the fresh-session branch
  // settles AND the thread finished loading, so the attachment lands in the
  // settled thread's composer.
  const pendingFresh = useAtomValue(atomAssistantPendingPrompt)
  const loadState = useAssistantLoadState()
  const loadReady = loadState === 'ready' || loadState === 'error'
  // Third gate, specific to attachments: the adapter stages to whatever session
  // atomAssistantCurrentSessionId names, so that mirror must have caught up with
  // the thread on screen. Right after a branch (the topic-switch move) it still
  // names the OLD session for a render or two, and staging there would put the
  // file in a sandbox the new session's agent never sees. Both values come from
  // the same source (ActiveSessionPersistence), so they converge immediately.
  const threadSession = useAuiState(
    s => s.threadListItem?.externalId ?? s.threadListItem?.remoteId
  )
  const stagingSession = useAtomValue(atomAssistantCurrentSessionId)
  const sessionSettled = !!threadSession && stagingSession === threadSession

  useEffect(() => {
    if (!pending?.length || pendingFresh || !loadReady || !sessionSettled)
      return
    // Event-driven like ComposerPrefillBridge: keep the pending atom until every
    // addAttachment resolves; re-run on gate changes rather than timing out. A
    // retry only re-adds what actually failed, so a partial failure can't
    // duplicate the files that already landed.
    let cancelled = false
    let attempts = 15
    let todo = pending
    const tick = async () => {
      if (cancelled) return
      const composer = aui.composer()
      const failed: AskAttachment[] = []
      for (const a of todo) {
        const file: SluggedFile = new File([a.markdown], a.name, {
          type: 'text/markdown',
        })
        // Carry an ASCII slug so the staged sandbox file gets a meaningful name
        // even when the display label is localized (proposal 0055).
        if (a.slug) file.navixSlug = a.slug
        try {
          await composer.addAttachment(file)
        } catch {
          failed.push(a)
        }
      }
      if (cancelled) return
      todo = failed
      // Clear even when some file never made it: the auto-send bridge WAITS on
      // this atom, so holding a permanently-failing attachment here would wedge
      // the message it belongs to. Better to send the question without the file.
      if (!todo.length || attempts-- <= 0) setPending(null)
      else setTimeout(() => void tick(), 100)
    }
    void tick()
    return () => {
      cancelled = true
    }
  }, [pending, pendingFresh, loadReady, sessionSettled, aui, setPending])

  return null
}

/** Auto-send counterpart to {@link ComposerPrefillBridge}: consumes
 *  {@link atomAssistantPendingAutoSend} — a prompt that should be SENT (not just
 *  pre-filled) into the settled thread. Same readiness gates as the prefill
 *  bridges (fresh-session branch settled + thread loaded). It fills the composer,
 *  then sends as soon as the composer reports it can (text non-empty, no run in
 *  flight), and clears the atom. Used by the topic-switch "move to a new session"
 *  flow so the user's already-sent message is re-sent in the fresh session
 *  without a second click. */
function ComposerAutoSendBridge() {
  const aui = useAui()
  const [pending, setPending] = useAtom(atomAssistantPendingAutoSend)
  const pendingFresh = useAtomValue(atomAssistantPendingPrompt)
  // A third gate the prefill bridges don't need: attachments queued alongside the
  // prompt must be IN the composer before it sends, or the re-send goes out
  // without the files it was supposed to carry over.
  const pendingAttachments = useAtomValue(atomAssistantPendingAttachment)
  const loadState = useAssistantLoadState()
  const loadReady = loadState === 'ready' || loadState === 'error'

  useEffect(() => {
    if (!pending || pendingFresh || !loadReady || pendingAttachments?.length)
      return
    let cancelled = false
    let attempts = 20
    const tick = () => {
      if (cancelled) return
      const composer = aui.composer()
      try {
        composer.setText(pending)
      } catch {
        if (attempts-- > 0) setTimeout(tick, 100)
        return
      }
      // Text is in; send once the composer is ready (non-empty, no in-flight
      // run). A fresh session has no run, so canSend flips true almost at once;
      // retry briefly to cover the render gap, then clear (leaving the text in
      // the composer if send never became available, so it's never lost).
      if (composer.getState().canSend) {
        composer.send()
        setPending(null)
      } else if (attempts-- > 0) {
        setTimeout(tick, 100)
      } else {
        setPending(null)
      }
    }
    tick()
    return () => {
      cancelled = true
    }
  }, [pending, pendingFresh, loadReady, pendingAttachments, aui, setPending])

  return null
}

// Topic-switch nudge tuning. The verdict is only as good as the history it can
// see — a window that clips the conversation drops the subject and makes a
// follow-up read as a fresh question — and the models in play have very large
// context windows, so both caps are generous. They exist to bound a pathological
// session, not to save tokens; the server caps the assembled string again.
const TOPIC_CONFIDENCE_THRESHOLD = 0.6
const TOPIC_CONTEXT_TURNS = 60
const TOPIC_PER_TURN_CHARS = 4000
// Tool ARGUMENTS carry the subject when the assistant's prose does not (it says
// "found it" and the pod name lives only in the call), so they go in. Tool
// RESULTS stay out: they are the bulk of a turn, can reach megabytes, and add no
// information about what the user is asking.
const TOPIC_PER_TOOL_ARGS_CHARS = 400

/** A locally-sent user message that opencode has not echoed back yet. react-opencode
 *  projects it optimistically (id `local:<clientId>`, this metadata flag), then
 *  REPLACES it with the server message under its real id — so anything keyed on the
 *  message id sees the same send twice. */
const isPendingUserMessage = (message: {
  metadata?: { custom?: Record<string, unknown> }
}) =>
  (message.metadata?.custom as { opencode?: { pending?: boolean } } | undefined)
    ?.opencode?.pending === true

/** Watches for a freshly-sent user message and, when the topic-switch setting is
 *  on, asks the server-side classifier (which reuses opencode's model config)
 *  whether it starts a NEW topic. On a confident yes it sets
 *  {@link atomAssistantTopicNudge} → the in-thread TopicSwitchNudge offers a
 *  one-click "move to a new session". Never blocks the send (this runs after the
 *  message is already in the thread); classifies each user message at most once;
 *  skips the first message of a session (no prior topic) — which also means the
 *  auto-sent message in a freshly-branched session never re-triggers it. */
function TopicNudgeBridge() {
  const enabled = useAtomValue(atomAssistantTopicNudgeEnabled)
  const messages = useAuiState(s => s.thread.messages)
  const model = useAtomValue(atomAssistantModel)
  const setNudge = useSetAtom(atomAssistantTopicNudge)
  const setVerdicts = useSetAtom(atomAssistantTopicVerdicts)
  const sessionId = useAtomValue(atomAssistantCurrentSessionId)
  const userKey = useAtomValue(atomAssistantUserKey)
  const lastCheckedRef = useRef<string | null>(null)

  useEffect(() => {
    if (!enabled) return
    // The last user message and its position. idx <= 0 means there is no user
    // message yet, or it's the very first message — no prior topic to leave.
    let idx = -1
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'user') {
        idx = i
        break
      }
    }
    if (idx <= 0) return
    const userMsg = messages[idx]
    // Wait for opencode's own message. The optimistic projection carries a
    // throwaway `local:` id, so classifying it would fire a SECOND, identical
    // request the moment the real id lands (two Langfuse traces per send) and
    // file the verdict under an id that no longer exists — TopicVerdictMenu
    // looks it up by the server id.
    if (isPendingUserMessage(userMsg)) return
    if (userMsg.id === lastCheckedRef.current) return
    // Already classified in an earlier mount. The ref only spans one mount, and
    // this bridge unmounts whenever the assistant closes (the lazy-connection
    // gate), so without this a reopen re-asks about the same message — a second
    // model call, a second Langfuse trace, and a nudge the user already
    // dismissed coming back. Read without subscribing: the verdict map is
    // written from inside this effect.
    if (getDefaultStore().get(atomAssistantTopicVerdicts)[userMsg.id]) return
    lastCheckedRef.current = userMsg.id
    // A new turn — any nudge from the previous message is stale.
    setNudge(null)

    // Every message carries the `<page …/>` marker; the classifier must never
    // see it. Walking from the node list to a pod detail is not a change of
    // subject, and leaving the markers in makes BOTH axes (object / goal) look
    // like they moved when only the browser did. Stripped on the new input AND
    // on the prior turns, since those carry their own markers too.
    //
    // Attachment markers go too, and for a second reason besides noise: this same
    // text is what "move to a new session" re-sends, and a marker flattened into
    // prose would arrive as raw text pointing at the OLD session's sandbox. The
    // files travel as attachments instead (see {@link attachmentsOf}).
    const textOf = (m: (typeof messages)[number]) =>
      stripAttachmentMarkers(
        stripPageMarkers(
          m.parts
            .flatMap(p => (p.type === 'text' ? [p.text] : []))
            .join(' ')
            .trim()
        )
      )

    /** The staged attachments a message carries, so the topic-switch move can
     *  re-attach them in the new session rather than lose them. */
    const attachmentsOf = (m: (typeof messages)[number]): AttachmentMarker[] =>
      m.parts.flatMap(p =>
        p.type === 'text' ? collectAttachmentMarkers(p.text) : []
      )

    // What the user can actually see, plus the calls the assistant made to get
    // there — never tool results and never reasoning, which are bulk without
    // signal about what is being asked.
    const transcriptOf = (m: (typeof messages)[number]) =>
      stripAttachmentMarkers(
        stripPageMarkers(
          m.parts
            .flatMap(p => {
              if (p.type === 'text') return [p.text]
              if (p.type !== 'tool-call') return []
              const args = (p.argsText ?? '').slice(
                0,
                TOPIC_PER_TOOL_ARGS_CHARS
              )
              return [`[tool] ${p.toolName}${args ? ` ${args}` : ''}`]
            })
            .join('\n')
            .trim()
        )
      )

    const newInput = textOf(userMsg)
    if (!newInput) return
    const context = messages
      .slice(0, idx)
      .slice(-TOPIC_CONTEXT_TURNS)
      .map(m => `${m.role}: ${transcriptOf(m).slice(0, TOPIC_PER_TURN_CHARS)}`)
      .join('\n')

    // NOT cancelled on cleanup. This effect depends on `messages`, which changes
    // on every streaming delta of the reply — so a cleanup-scoped `cancelled`
    // flag threw the verdict away a few milliseconds after the request left,
    // and the id guard above then refused to ask again. The classifier answered
    // in well under a second (the Langfuse trace shows it) and the UI still
    // showed nothing until a remount happened to re-ask. Staleness is handled by
    // identity instead: the verdict is filed under the message id it belongs to,
    // and only the nudge — the one piece of UI that speaks about "this send" —
    // is dropped when a newer message has since been sent.
    void (async () => {
      const verdict = await classifyTopic(
        context,
        newInput,
        model?.modelID,
        sessionId,
        userKey
      )
      if (!verdict.enabled) return
      // Keep every verdict, not just the ones that nudge: the user message's
      // More menu shows why a message was judged a continuation too.
      setVerdicts(prev => ({ ...prev, [userMsg.id]: verdict }))
      // A newer user message has been sent while this was in flight — its own
      // classification owns the nudge now, and that turn already cleared it.
      if (lastCheckedRef.current !== userMsg.id) return
      if (
        verdict.isNewTopic &&
        (verdict.confidence ?? 0) >= TOPIC_CONFIDENCE_THRESHOLD
      ) {
        const attachments = attachmentsOf(userMsg)
        setNudge({
          newInput,
          // Stamped with the conversation it belongs to. The atom is a
          // singleton and every Thread renders the same footer, so the renderer
          // needs this to tell "the nudge for the conversation on screen" from
          // "a nudge left behind by the one before it".
          sessionId,
          ...(attachments.length ? { attachments } : {}),
        })
      }
    })()
  }, [enabled, messages, model, setNudge, setVerdicts, sessionId, userKey])

  return null
}
