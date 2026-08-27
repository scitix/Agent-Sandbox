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

// Read-only viewing of the unattended bots' conversations.
//
// The background flows (auto-triage, the daily digest) run as their own users
// and produce the reports everyone reads — but the WORK, which tools the agent
// reached for and what it saw, was visible to nobody. This module holds the
// policy for opening those transcripts to every dashboard user: which owners may
// be read, and how far back.
//
// It is deliberately pure. The routes that use it (`/analysis/*` in server.ts)
// stay short enough to audit at a glance, and the rules that decide what a
// stranger may see are testable without a server.
//
// The one rule that is NOT here, because it belongs at the call site: these are
// READ paths only. No `owner` is ever accepted by a route that writes — a viewer
// cannot post as a bot, rename its threads, or delete them.

/** Same shape a `userKey` has to satisfy everywhere else: it becomes a directory
 *  segment and a Langfuse user id. Anything else in the allowlist is a
 *  misconfiguration, and dropping it is safer than trusting it. */
const USER_KEY = /^[A-Za-z0-9._-]{1,128}$/

/** The window a request gets when it does not ask for one. Short on purpose —
 *  the common case is "what has the bot been doing today". */
export const ANALYSIS_DEFAULT_DAYS = 1

/** The furthest back any request can reach, whatever it asks for. The store is a
 *  single JSON file walked in memory, so this is about keeping the response (and
 *  the browser's list) bounded rather than about query cost. */
export const ANALYSIS_MAX_DAYS = 7

export const DAY_MS = 24 * 60 * 60 * 1000

/**
 * Which owners this deployment exposes, from `AGENTBOX_BOT_USERS`.
 *
 * Empty — the default, and what an OSS deployment gets — turns the whole feature
 * off: the routes still answer, with nothing in them, and the dashboard hides
 * its menu entry. That is the safe default for a config-driven allowlist: a
 * deployment that never opted in cannot leak a conversation by omission.
 */
export function analysisSourcesFromEnv(
  env: NodeJS.ProcessEnv = process.env
): string[] {
  return (env.AGENTBOX_BOT_USERS || '')
    .split(',')
    .map(s => s.trim())
    .filter(s => USER_KEY.test(s))
}

/** A day count from the query string, coerced into the supported range. Garbage,
 *  zero and negatives all mean "the caller did not choose", not "no window" —
 *  the failure mode of reading that as unbounded is the whole store. */
export function clampDays(raw: string | null | undefined): number {
  const n = Math.floor(Number(raw))
  if (!Number.isFinite(n) || n <= 0) return ANALYSIS_DEFAULT_DAYS
  return Math.min(n, ANALYSIS_MAX_DAYS)
}

/**
 * The owners a request may actually read: what it asked for, intersected with
 * what the deployment allows.
 *
 * An intersection rather than a lookup, so an unknown or unlisted `source`
 * narrows the result to nothing instead of widening it. No selection means the
 * whole allowlist, which is what the list page shows by default.
 */
export function resolveSources(
  allow: readonly string[],
  requested: string | null | undefined
): string[] {
  const asked = (requested || '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
  if (!asked.length) return [...allow]
  const wanted = new Set(asked)
  return allow.filter(s => wanted.has(s))
}
