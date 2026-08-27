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

// Backend selection, and the startup check that makes a bad selection loud.
//
// The rule this file exists to enforce: a misconfigured backend must fail at BOOT,
// not on a user's first message. Running `codex exec` against an endpoint that
// speaks the wrong dialect taught the lesson — the failure surfaced as a 400 deep
// inside a turn, which is far more expensive to diagnose than a refusal to start.
//
// The pod now serves EVERY backend it is configured for, not just one, so a
// tester can switch harness from the UI instead of redeploying. Two consequences
// worth stating, because they are what make that safe:
//
//   * A backend that fails preflight is marked UNAVAILABLE rather than killing
//     the pod — one missing credential must not take the assistant down. That
//     holds even when it is EVERY credential: the pod also serves the workspace-fs
//     server, which touches no model, and the entrypoint kills every child together
//     when one exits. A boot failure would therefore turn a missing model key into a
//     CrashLoopBackOff that also takes down attachment upload and the file browser,
//     with the reason visible only in a log. See backends/unavailable.ts.
//   * A thread remembers the backend that created it (`ThreadRef.backendId`), so
//     switching the picker only affects NEW conversations. Replaying a Claude
//     Code thread through OpenCode would silently produce a different agent's
//     history, which is exactly the kind of quiet wrongness this codebase keeps
//     designing against.
import { analysisSourcesFromEnv } from './analysis.ts'
import { type AgentBackend, type BackendId } from './backend.ts'
import {
  ClaudeCodeBackend,
  claudeCodeConfigFromEnv,
} from './backends/claude-code.ts'
import { CodexBackend } from './backends/codex.ts'
import {
  OpenCodeBackend,
  openCodeConfigFromEnv,
  opencodeEnabled,
} from './backends/opencode.ts'
import { type Classifier, createClassifier } from './classify.ts'
import { InteractionRegistry } from './interactions.ts'
import { classifierModelFromEnv } from './model/one-shot.ts'
import { type LangfuseConfig, langfuseConfigFromEnv } from './telemetry.ts'
import { ThreadStore } from './threads.ts'

export const BACKEND_IDS: BackendId[] = ['claude-code', 'opencode', 'codex']

export function backendIdFromEnv(): BackendId {
  const raw = (process.env.ASSISTANT_BACKEND || 'opencode').trim()
  if (!BACKEND_IDS.includes(raw as BackendId)) {
    throw new Error(
      `ASSISTANT_BACKEND="${raw}" is not one of ${BACKEND_IDS.join(', ')}`
    )
  }
  return raw as BackendId
}

/** What the browser's harness picker renders. */
export interface BackendStatus {
  id: BackendId
  available: boolean
  /** Why not, when unavailable — shown as the disabled entry's tooltip. */
  reason?: string
}

export interface GatewayDeps {
  /**
   * Every backend that passed preflight, keyed by id.
   *
   * MAY BE EMPTY: a deployment whose model credentials are all wrong still serves
   * this port, so the reasons in `statuses` reach the browser (and the pod's
   * non-model services keep running). Consumers reach a harness through
   * `pick()`, which substitutes {@link UnavailableBackend} — nothing indexes this
   * map directly and assumes a hit.
   */
  backends: Map<BackendId, AgentBackend>
  /** The one a request without an explicit choice gets (ASSISTANT_BACKEND when
   *  it is serving, else whichever else came up). */
  defaultBackendId: BackendId
  /** Every backend the deployment could have, serving or not, for the picker. */
  statuses: BackendStatus[]
  threads: ThreadStore
  interactions: InteractionRegistry
  classifier?: Classifier
  /** Whose conversations anyone may read through `/analysis/*`. Config-driven
   *  and empty by default, so a deployment that never opted in exposes nothing. */
  analysisSources: string[]
  langfuse?: LangfuseConfig | null
}

/**
 * Is this backend configured at all?
 *
 * Deliberately CHEAP and network-free: it answers "did the deployment supply
 * what this harness needs", which is the question the picker asks. Whether it
 * actually works is what preflight decides, once, at boot.
 */
export function configuredReason(id: BackendId): string | undefined {
  switch (id) {
    case 'claude-code': {
      const cfg = claudeCodeConfigFromEnv()
      if (!cfg.authToken) {
        // Named both ways out on purpose. The chart lends the shared provider key
        // to this harness ONLY when a baseURL points it at that provider — an
        // unconfigured harness would otherwise reach api.anthropic.com with a
        // credential issued by someone else, which is a 401 at best and a key
        // disclosed to a third party at worst.
        return (
          'no credential: set assistant.claudeCode.credentials, or ' +
          'assistant.claudeCode.baseURL to reuse the shared provider key'
        )
      }
      if (!cfg.models.length) {
        return 'no models configured (assistant.claudeCode.models is empty)'
      }
      return undefined
    }
    case 'opencode':
      // Its server runs in this pod, launched by the entrypoint from this very
      // variable — reading the same one here is what keeps the picker from
      // offering a harness nobody started. Whether it actually came up is
      // preflight's business.
      if (!opencodeEnabled()) {
        return 'withdrawn (assistant.opencode.enabled=false)'
      }
      return undefined
    case 'codex':
      return 'codex is a placeholder; see backends/codex.ts'
  }
}

/** Construct one backend. Does NOT preflight — the caller decides when. */
export function buildBackend(
  id: BackendId,
  threads: ThreadStore,
  interactions: InteractionRegistry
): AgentBackend {
  switch (id) {
    case 'claude-code':
      return new ClaudeCodeBackend(
        claudeCodeConfigFromEnv(),
        threads,
        interactions
      )
    case 'opencode':
      return new OpenCodeBackend(openCodeConfigFromEnv(), threads, interactions)
    case 'codex':
      return new CodexBackend()
  }
}

function reasonOf(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/** One line naming every harness that is not serving, and why. */
export function unavailableReason(statuses: BackendStatus[]): string {
  return statuses
    .filter(s => !s.available)
    .map(s => `${s.id}: ${s.reason ?? 'unknown'}`)
    .join('; ')
}

/**
 * Build and verify every configured backend.
 *
 * Never throws for a backend's sake: each one either serves or is reported
 * unavailable with its reason. An empty result is a valid — and loudly logged —
 * outcome, not a boot failure.
 */
export async function startBackends(): Promise<GatewayDeps> {
  const threads = new ThreadStore()
  const interactions = new InteractionRegistry()
  // Both optional by design: no classifier model means the frontend hides the
  // affordance, and no Langfuse config means no tracing. Neither is a reason to
  // refuse traffic.
  const classifier = createClassifier(classifierModelFromEnv())
  const langfuse = langfuseConfigFromEnv()

  const backends = new Map<BackendId, AgentBackend>()
  const statuses: BackendStatus[] = []

  for (const id of BACKEND_IDS) {
    const unconfigured = configuredReason(id)
    if (unconfigured) {
      statuses.push({ id, available: false, reason: unconfigured })
      continue
    }
    let backend: AgentBackend
    try {
      backend = buildBackend(id, threads, interactions)
    } catch (e) {
      statuses.push({ id, available: false, reason: reasonOf(e) })
      continue
    }
    // A backend that cannot keep the sandbox guarantee is refused outright
    // rather than warned about: its file/shell IO might land on the pod.
    if (backend.sandboxing === 'none') {
      statuses.push({
        id,
        available: false,
        reason:
          'declares sandboxing="none": it cannot guarantee that file and ' +
          'shell IO stay inside the sandbox',
      })
      continue
    }
    try {
      await backend.preflight()
    } catch (e) {
      // Loud in the log, but not fatal — the other harness may be fine.
      console.error(`[gateway] backend ${id} is unavailable: ${reasonOf(e)}`)
      statuses.push({ id, available: false, reason: reasonOf(e) })
      continue
    }
    backends.set(id, backend)
    statuses.push({ id, available: true })
  }

  const preferred = backendIdFromEnv()
  if (!backends.size) {
    // Loud, because this pod is now up and answering with an empty model list —
    // which reads like "no models configured" unless the real reason is right
    // here. The same text goes to the browser through `statuses`.
    console.error(
      `[gateway] NO HARNESS CAN SERVE: ${unavailableReason(statuses)}`
    )
    console.error(
      '[gateway] serving the API anyway so the reason above reaches the UI; ' +
        'every request that needs a harness will fail with it'
    )
  }
  const defaultBackendId = backends.has(preferred)
    ? preferred
    : // ASSISTANT_BACKEND still expresses the deployment's intent; falling back
      // keeps the pod useful when that one harness is the broken one. With none
      // serving there is nothing to fall back TO, so the intent stands — and it
      // is the id the unavailable stand-in reports.
      ([...backends.keys()][0] ?? preferred)
  if (defaultBackendId !== preferred && backends.size) {
    console.error(
      `[gateway] ASSISTANT_BACKEND=${preferred} is not serving; ` +
        `defaulting to ${defaultBackendId}`
    )
  }

  return {
    backends,
    defaultBackendId,
    statuses,
    threads,
    interactions,
    classifier,
    analysisSources: analysisSourcesFromEnv(),
    langfuse,
  }
}
