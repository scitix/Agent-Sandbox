// Assistant client state.
//
// Extracted from the reference implementation's global store rather than
// copied whole: the rest of that file is its own product's domain, and an atom
// nothing here reads is an atom nobody maintains.
//
// These are all CLIENT state. Anything the server owns — the thread list, a
// turn's liveness — comes from the gateway through the runtime, not from here.

import { atom } from "jotai"
import { atomWithStorage } from "jotai/utils"

/** Where the harness pick is remembered across visits. */
const ASSISTANT_BACKEND_KEY = "abx.assistant.backend"

/**
 * Deploy-time front-end configuration.
 *
 * Only what the assistant reads. Kept as an atom rather than a module constant
 * because a deployment may fetch it at runtime, and every consumer should then
 * re-render rather than hold a value captured at import time.
 */
export interface LocalAppConfig {
  assistantEnabled?: boolean
  assistantBackend?: string
}

export const atomLocalAppConfig = atom<LocalAppConfig>({})

/** The harness the user picked, when they have picked one. */
export const atomAssistantBackendOverride = atomWithStorage<string | null>(
  ASSISTANT_BACKEND_KEY,
  null
)

export interface ColorAllocation {
  [key: string]: {
    // key is the component's group identifier
    [data: string]: number // data is the concrete value, number is the color index
  }
}

export interface BreadCrumbItem {
  label: string
  href: string
  children?: BreadCrumbItem[]
  /**
   * Kind label rendered as a Badge before the item's label (e.g. "Pod").
   * Used on the leaf crumb to annotate the current object's type. Rendered
   * verbatim — not passed through the translator.
   */
  badge?: string
  /** Full text shown on hover; used when `label` is an abbreviation. */
  title?: string
}

export interface AppConfig {
  apiBaseUrl: string
  multiClusterEnabled: boolean
  assistantEnabled: boolean
  /** Which agent harness the assistant pod runs, so the browser mounts the
   *  matching runtime on first render instead of probing and swapping. */
  assistantBackend?: 'opencode' | 'claude-code'
  // True when the hub chart was given `oneServiceDatasources` (the OneService
  // data layer). Gates the enterprise cluster-health / data-center nav entries,
  // which have no data without it. Derived in config.json from the Helm values.
  dataServiceEnabled: boolean
  /** The account page (API tokens). Served by the hub that exposes the navix API;
   *  elsewhere `enabled` is false and `externalUrl` points at the one that does, so
   *  the entry stays visible and sends the user somewhere real. */
  account?: {
    enabled: boolean
    externalUrl?: string
  }
  grafana?: {
    host: string
    dashboards: {
      scheduler: string
    }
  }
}

export type AssistantPageView = 'config'

/**
 * Which of those is open, or null for the conversation.
 *
 * Mirrored to `?page=` by the page, and the URL is what actually carries it: a
 * reload lands back where you were, a link opens the same view, and — the reason
 * this stopped being a plain toggle — picking a conversation from the history is
 * a request to READ it, so it simply drops the param instead of needing every
 * page to remember to close itself.
 *
 * An atom rather than reading `useSearch` at the point of use, because the
 * assistant surface also renders as the side panel on every OTHER route, where
 * `?page` belongs to that route's table pagination.
 */

export interface AskAttachment {
  name: string
  markdown: string
  /** Optional ASCII slug for the sandbox file name (proposal 0055). Lets an
   *  Ask AI attachment keep a meaningful, path-safe filename even when `name`
   *  (the chip label) is localized/non-ASCII. Falls back to a sanitized `name`. */
  slug?: string
}
/** Pending attachments to add to the assistant composer once the runtime is
 *  ready. Mirrors {@link atomAssistantPendingPrompt}. A list because the
 *  topic-switch move re-attaches everything the moved message carried, which may
 *  be more than one file; an Ask AI click queues exactly one. `null` = nothing
 *  queued (an empty array would make the auto-send gate spin on a no-op). */

export type AskAiButtonAction = 'ask' | 'copy'

export type AssistantNavPhase = 'opening' | 'opened'

export interface AssistantNavSignal {
  id: string
  /** Human-readable destination, e.g. "Nodes (Faulty)" — from buildDestination. */
  summary: string
  phase: AssistantNavPhase
}

export interface AssistantModelRef {
  providerID: string
  modelID: string
}

export interface AssistantTopicVerdict {
  isNewTopic?: boolean
  confidence?: number
  objectCarriesOver?: boolean
  goalCarriesOver?: boolean
  rationale?: string
  traceId?: string
}

/** Classifier verdicts keyed by user-message id — including the ones that did
 *  NOT nudge, so a user message's More menu can explain why it was read as a
 *  continuation. Diagnostic chrome only: plain (non-persisted) atom, so it
 *  resets on reload and never grows across sessions. */

export const atomAssistantEnabled = atom(get => {
  const config = get(atomLocalAppConfig)
  return config.assistantEnabled ?? false
})

// Which agent harness backs the assistant. Deploy-time truth from config.json,
// so the browser mounts one runtime and never swaps it mid-session.
/** The harness NEW conversations start under.
 *
 *  config.json supplies the deployment's default; the picker in the assistant
 *  toolbar overrides it per browser, because the pod now serves every backend it
 *  is configured for and switching is a test affordance rather than a redeploy.
 *  An existing thread is unaffected — its backend is fixed server-side at
 *  creation, so switching only decides what the NEXT conversation uses. */

export const atomAssistantBackend = atom(
  get =>
    get(atomAssistantBackendOverride) ??
    get(atomLocalAppConfig).assistantBackend ??
    // claude-code, not opencode: OpenCode is withdrawn here (its tool
    // overrides register alongside the built-ins rather than replacing them,
    // which would run the agent's shell on the pod). A default naming a
    // harness the gateway does not serve shows the wrong name in the picker
    // while every reply comes from the other one.
    'claude-code',
  (_get, set, next: string | null) => {
    set(atomAssistantBackendOverride, next)
    if (typeof localStorage === 'undefined') return
    if (next) localStorage.setItem(ASSISTANT_BACKEND_KEY, next)
    else localStorage.removeItem(ASSISTANT_BACKEND_KEY)
  }
)

// OneService data layer toggle -- from config.json dataServiceEnabled (derived
// from whether the hub chart was given oneServiceDatasources). Gates the
// enterprise cluster-health / data-center nav entries.

export const atomAssistantOpen = atom(false)
// Agent workspace file browser panel — shown/hidden on the dedicated assistant
// full page (the navigator toggle replaces the "open assistant" button there).
// Defaults CLOSED: most turns produce no files, so the panel opens on demand —
// either from the navigator toggle or automatically once the agent actually
// writes a file (see WorkspaceAutoOpenBridge).

export const atomWorkspaceOpen = atom(false)
/** Whether an agent file write may auto-open the workspace panel. Manually
 *  closing the panel clears this (the user deliberately collapsed it, so a later
 *  write must not fight that); manually opening it re-arms it. */

export const atomWorkspaceAutoOpen = atom(true)

/**
 * Whether the user has collapsed or expanded the assistant page's conversation
 * menu DURING THIS VISIT, or `null` for "hasn't touched it".
 *
 * Tri-state rather than a boolean, because there is no single right default: a
 * menu is a list of conversations, and on a first visit there are none — so the
 * page would open with an empty column taking a fifth of the screen to say
 * nothing. `null` therefore defers to the history itself (see
 * `useAssistantMenuOpen`): closed until the user has talked to the assistant,
 * out from then on.
 *
 * Deliberately NOT persisted, and cleared when the page unmounts. Collapsing a
 * panel to get it out of the way for a minute is not a preference — it is a
 * gesture about what is on screen right now, and remembering it forever turns
 * one impatient click into a setting the user never knowingly changed. What DOES
 * survive is the URL: the page mirrors this to `?menu=`, so a link (or the side
 * panel's menu button, which navigates) carries the state to exactly where it
 * was meant to apply, and the back button restores it.
 */

export const atomAssistantMenuOpen = atom<boolean | null>(null)

/** The non-conversation pages the assistant's main column can show. */

export const atomAssistantPage = atom<AssistantPageView | null>(null)

/** Who is logged in, for the assistant's greeting.
 *
 * The dashboard core has no user API of its own (the profile endpoint is an
 * enterprise concern), so the enterprise build publishes the display name here
 * and the greeting degrades to a nameless variant when nothing does. An atom
 * rather than a prop because the greeting renders in two places mounted by
 * different owners — the assistant page and the layout's side panel. */

export const atomAssistantPendingPrompt = atom<string | null>(null)

/** A content-heavy payload (a serialized table, a diagnosis report) that an
 *  Ask AI button hands to the assistant as a composer *attachment* — distinct
 *  from the prompt text, which carries the question. `markdown` becomes a
 *  `text/markdown` File; `name` is the user-visible chip label. Pre-fill only. */

export const atomAssistantPendingAttachment = atom<AskAttachment[] | null>(null)

/** Set by an Ask AI click so that, once the runtime mounts, a queued prompt
 *  lands in a FRESH session when the current one already has messages (so the
 *  question doesn't bleed into an unrelated conversation). The runtime-side
 *  bridge branches the session if needed, then clears this; the composer
 *  pre-fill bridges wait until it's cleared so they fill the right thread.
 *  Decouples Ask AI from needing the runtime mounted at click time (the panel
 *  may be closed — clicking is what opens it). */

export const atomAssistantAutoOpenPage = atomWithStorage(
  'assistant.autoOpenPage',
  true
)

/** Which action the shared Ask AI split-button performs on its primary click:
 *  `'ask'` hands the prompt + attachment to the assistant, `'copy'` copies that
 *  same context to the clipboard instead. A single GLOBAL preference (not
 *  per-button) — toggled from any Ask AI button's dropdown and persisted, so a
 *  user who prefers copying keeps that choice across pages and reloads. */

export const atomAskAiButtonAction = atomWithStorage<AskAiButtonAction>(
  'assistant.askAiButtonAction',
  'ask'
)

/** A live, agent-initiated dashboard navigation. Set by NavigationBridge the
 *  moment the assistant's `open_page` fires, cleared shortly after the jump
 *  lands. Drives the "the assistant is navigating" affordances (top progress
 *  bar + inset frame glow) so it's obvious the page changed because the agent
 *  acted, not because the user clicked. `phase` goes 'opening' (deliberate beat
 *  before the jump) → 'opened' (jump landed). */

export const atomAssistantStreamStuck = atom(false)

/** True while a reply is in flight (the opencode run is streaming/busy). Mirrored
 *  out of the runtime by RunActiveBridge so AssistantHost — which lives above the
 *  runtime — can keep the connection alive across a panel close that happens
 *  mid-reply. Reset to false when the run ends or the runtime unmounts. Part of
 *  the lazy-connection model: the /event stream is only held while the assistant
 *  is visible OR a run is active (+ a short grace), never idle in the background. */

export const atomAssistantRunActive = atom(false)

/** The model the assistant sends with. Discovered at runtime from opencode's
 *  own config (`client.config.providers()` → the models in `opencode.json`,
 *  which the Helm chart renders from `assistant.provider.models`). `null` means
 *  "not chosen yet — use the discovered default (opencode's `default`, else the
 *  first available)". Persisted so a user's pick survives reloads. This replaces
 *  the old build-time model pin entirely — the model is no longer baked into the
 *  frontend bundle. */

export const atomAssistantModel = atomWithStorage<AssistantModelRef | null>(
  'assistant.model',
  null
)

/** Per-user key for soft session isolation. opencode has no multi-tenancy, but
 *  sessions can be created/listed under a per-user `directory`, so each user
 *  only sees their own. Set by the enterprise build from the logged-in user
 *  (e.g. /opuser/userInfo name); undefined in the OSS build → a single shared
 *  "default" namespace. */

export const atomAssistantUserKey = atom<string | undefined>(undefined)

/** The opencode session id (`ses_…`) currently bound to the runtime. Mirrored out
 *  of the runtime by ActiveSessionPersistence so non-runtime code (e.g. the
 *  attachment adapter, which is a plain object created at module load) can learn
 *  which session — and thus which sandbox — an attachment belongs to. `null` when
 *  no session is active. See proposal 0055. */

export const atomAssistantCurrentSessionId = atom<string | null>(null)

/** A prompt queued to be AUTO-SENT into the (fresh) thread once it settles — the
 *  send-it-for-me counterpart to {@link atomAssistantPendingPrompt} (which only
 *  pre-fills, leaving the user to hit Send). Used by the topic-switch "move to a
 *  new session" flow: the user already sent the message once, so after branching
 *  to a fresh session we re-send it there without a second click. Consumed by
 *  ComposerAutoSendBridge under the same readiness gates as the prefill bridges
 *  (fresh-session settled + thread loaded), then cleared. `null` = nothing queued. */

export const atomAssistantPendingAutoSend = atom<string | null>(null)

/** User setting: show a non-blocking "this looks like a new topic — move it to a
 *  new session?" nudge after sending, when a lightweight classifier judges the
 *  new message unrelated to the current conversation. On by default (the send is
 *  never blocked and the nudge is dismissible, so the cost of a false positive is
 *  one ignored alert); the classifier reuses opencode's own model config
 *  server-side. Persisted.
 *
 *  The storage key is versioned: the setting shipped off-by-default, so every
 *  existing user carries a stored `false` that would otherwise pin them to the
 *  old behaviour forever. A new key makes the new default actually reach them;
 *  the cost is that anyone who had deliberately turned it ON also restarts from
 *  the default — which is now the same thing. */

export const atomAssistantTopicNudgeEnabled = atomWithStorage(
  'assistant.topicNudge.v2',
  true
)

/** What the topic classifier concluded about one user message. Mirrors the
 *  workspace-fs `/classify` response: a new topic requires BOTH axes to change,
 *  so `objectCarriesOver`/`goalCarriesOver` explain a verdict far better than the
 *  boolean alone. `traceId` points at the Langfuse trace for the same run. */

export const atomAssistantTopicVerdicts = atom<
  Record<string, AssistantTopicVerdict>
>({})

/** The currently-shown topic-switch nudge, or null. Set by TopicNudgeBridge when
 *  a sent message is classified as a new topic; carries the message text so
 *  "move to a new session" can re-send it there (via {@link
 *  atomAssistantPendingAutoSend}), plus the staged attachments it carried so they
 *  can be re-attached in the new session — a new session means a new sandbox, so
 *  the old message's sandbox paths would resolve to nothing there. Cleared when
 *  the user acts or dismisses. */

export const atomAssistantTopicNudge = atom<{
  newInput: string
  /** Which conversation raised it. The nudge speaks about ONE send in ONE
   *  conversation ("move THIS question out"), but the atom is a singleton and
   *  every Thread — the side panel, the full page, and the empty landing —
   *  renders the same footer, so without an owner the alert followed the user
   *  onto a landing screen that has no such message. The renderer shows it only
   *  while this session is the one on screen. */
  sessionId?: string | null
  /** Display name + sandbox path of each attachment on the moved message. */
  attachments?: { name: string; path?: string }[]
} | null>(null)

/**
 * Client-only "what-if" overrides for the network-topology graph: maps a node
 * name to a simulated target group value. Applied when building grouping trees
 * so a node visually moves to another bucket without any backend write. Plain
 * (non-persisted) atom — resets on reload; clear it to drop the simulation.
 */
