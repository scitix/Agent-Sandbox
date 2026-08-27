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

// Thread identity: the gateway's own id, and what it maps to in a backend.
//
// WHY NOT JUST USE THE BACKEND'S SESSION ID: it is not ours to depend on.
//   * Claude Code stores sessions under a directory whose name is derived from
//     the cwd path, so a change to the mount layout would orphan every history.
//   * The sandbox is addressed by this id, so keeping it stable is what lets a
//     conversation (and its staged attachments) survive a backend switch.
//   * The Feishu report path used to join on a harness session id and failed
//     silently when it did not match; a gateway-owned id plus an explicit
//     mapping removes that class of bug.
//
// The store is a small JSON file rewritten atomically. That is adequate because
// the assistant is single-replica by construction (the sandbox map is in-process)
// and the volume is thousands of rows, not millions.
import { randomBytes } from 'node:crypto'
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'

import type { BackendId, ThreadInfo } from './backend.ts'

export interface ThreadRef {
  /** The gateway's id — the only one anything above this layer sees. */
  id: string
  userKey: string
  backendId: BackendId
  /** The harness's own session id, once it has one. Absent until the first run:
   *  `createThread` deliberately creates no harness session, which makes this the
   *  authoritative "has this conversation started" flag. */
  backendThreadId?: string
  /** An EXPLICIT title: what a caller passed at creation, or a rename. Wins over
   *  whatever the harness came up with, because a user who renamed a conversation
   *  does not want the harness's summary back. */
  title?: string
  /** The title the HARNESS generated, once it is a real one (placeholders are
   *  never stored). Kept apart from `title` so a rename and an auto-title cannot
   *  overwrite each other. */
  autoTitle?: string
  createdAt: number
  updatedAt: number
}

/** Notified after any change to a thread. `undefined` means "gone". */
export type ThreadChangeListener = (
  userKey: string,
  ref: ThreadRef | undefined,
  id: string
) => void

/** How long an unstarted thread is kept before the list prunes it. "New session"
 *  creates a real thread immediately, so abandoning one leaves a row nothing will
 *  ever show (the UI hides unstarted threads) and nothing will ever clean up. */
const UNSTARTED_TTL_MS = 24 * 60 * 60 * 1000

/** `th_` + 16 hex chars. Safe as a path segment and as the sandbox key (the
 *  workspace-fs daemon validates session ids against `[A-Za-z0-9._-]{1,200}`). */
export function newThreadId(): string {
  return `th_${randomBytes(8).toString('hex')}`
}

function defaultStorePath(): string {
  const base =
    process.env.ASSISTANT_THREAD_STORE ||
    join(
      process.env.CLAUDE_CONFIG_DIR ||
        join(process.env.HOME || '/tmp', '.agentbox'),
      'gateway',
      'threads.json'
    )
  return base
}

interface Persisted {
  version: 1
  threads: ThreadRef[]
}

export class ThreadStore {
  private readonly path: string
  private readonly byId = new Map<string, ThreadRef>()
  private readonly listeners = new Set<ThreadChangeListener>()

  constructor(path: string = defaultStorePath()) {
    this.path = path
    this.load()
  }

  /**
   * Watch every change to every thread. Returns an unsubscribe.
   *
   * This is what makes the browser's history list push-driven: a rename, the first
   * run adopting a harness session, an auto-title landing and a delete all funnel
   * through the mutators below, so one listener covers them for every backend —
   * rather than each harness having to remember to announce itself.
   */
  onChange(listener: ThreadChangeListener): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  private announce(userKey: string, ref: ThreadRef | undefined, id: string) {
    for (const listener of this.listeners) {
      try {
        listener(userKey, ref, id)
      } catch (e) {
        // A subscriber (an SSE response that just went away) must never break the
        // mutation that notified it.
        console.warn('[gateway] thread change listener threw:', e)
      }
    }
  }

  private load(): void {
    try {
      const raw = readFileSync(this.path, 'utf-8')
      const parsed = JSON.parse(raw) as Persisted
      for (const t of parsed.threads ?? []) this.byId.set(t.id, t)
    } catch {
      // No store yet, or an unreadable one. Starting empty is correct: the
      // backend still holds the transcripts, and a lost mapping costs history
      // visibility rather than data.
    }
  }

  private flush(): void {
    const body: Persisted = { version: 1, threads: [...this.byId.values()] }
    mkdirSync(dirname(this.path), { recursive: true })
    // Write-then-rename so a crash mid-write cannot leave a truncated file that
    // would silently drop every mapping on the next start.
    const tmp = `${this.path}.tmp`
    writeFileSync(tmp, JSON.stringify(body), 'utf-8')
    renameSync(tmp, this.path)
  }

  create(userKey: string, backendId: BackendId, title?: string): ThreadRef {
    const now = Date.now()
    const ref: ThreadRef = {
      id: newThreadId(),
      userKey,
      backendId,
      title,
      createdAt: now,
      updatedAt: now,
    }
    this.byId.set(ref.id, ref)
    this.flush()
    this.announce(userKey, ref, ref.id)
    return ref
  }

  get(id: string): ThreadRef | undefined {
    return this.byId.get(id)
  }

  /** Look up a thread and assert it belongs to this user. Returns undefined for
   *  both "unknown" and "someone else's", so a caller cannot leak existence. */
  getForUser(id: string, userKey: string): ThreadRef | undefined {
    const ref = this.byId.get(id)
    return ref && ref.userKey === userKey ? ref : undefined
  }

  update(
    id: string,
    patch: Partial<Omit<ThreadRef, 'id'>>
  ): ThreadRef | undefined {
    const ref = this.byId.get(id)
    if (!ref) return undefined
    Object.assign(ref, patch, { updatedAt: Date.now() })
    this.flush()
    this.announce(ref.userKey, ref, ref.id)
    return ref
  }

  remove(id: string): boolean {
    const ref = this.byId.get(id)
    const had = this.byId.delete(id)
    if (had) {
      this.flush()
      this.announce(ref?.userKey ?? '', undefined, id)
    }
    return had
  }

  /**
   * Newest first, scoped to one user — and, when given, to one harness.
   *
   * `backendId` is not optional decoration: the gateway builds `GET /threads` by
   * asking EVERY serving backend for the user's threads and stamping each answer
   * with the answering backend's id. A backend that returns threads it does not
   * own therefore puts the same conversation in the list once per harness, with a
   * different `backendId` on each copy — duplicated history, and a UI that reads
   * the open thread's harness (hence its capabilities) off whichever copy sorted
   * first. Every backend passes its own id.
   */
  list(userKey: string, backendId?: BackendId): ThreadRef[] {
    this.pruneUnstarted()
    return [...this.byId.values()]
      .filter(
        t =>
          t.userKey === userKey &&
          (backendId === undefined || t.backendId === backendId)
      )
      .sort((a, b) => b.updatedAt - a.updatedAt)
  }

  /**
   * Newest first, across several owners at once — the read-only viewer's listing.
   *
   * Two deliberate differences from `list`. It does NOT prune: a read must never
   * delete rows, least of all rows belonging to someone the reader is only
   * looking at. And the time window is applied HERE rather than left to the
   * caller, so there is no shape of request that returns the whole store.
   *
   * `since` is a floor on `updatedAt`. An empty `owners` returns nothing, which
   * is what makes an unconfigured deployment serve an empty list rather than
   * everything.
   */
  listOwned(owners: readonly string[], since: number): ThreadRef[] {
    const wanted = new Set(owners)
    if (!wanted.size) return []
    return [...this.byId.values()]
      .filter(t => wanted.has(t.userKey) && t.updatedAt >= since)
      .sort((a, b) => b.updatedAt - a.updatedAt)
  }

  /**
   * Forget threads that were created and never spoken in.
   *
   * Opening the assistant and clicking "new conversation" creates a real thread
   * before there is anything in it, and the UI hides those — so without this they
   * accumulate in the store forever, invisible. Done on `list` rather than on a
   * timer: it is the one call that already walks every row, and a store with no
   * readers has nothing to clean up.
   */
  private pruneUnstarted(): void {
    const cutoff = Date.now() - UNSTARTED_TTL_MS
    const stale = [...this.byId.values()].filter(
      t => !t.backendThreadId && t.updatedAt < cutoff
    )
    if (!stale.length) return
    for (const t of stale) this.byId.delete(t.id)
    this.flush()
    for (const t of stale) this.announce(t.userKey, undefined, t.id)
  }

  /**
   * The public view of a thread — and the ONE place the three derived facts are
   * computed, so the list endpoint and the push channel cannot disagree about
   * whether a conversation has started or is still waiting for its title.
   */
  toInfo(ref: ThreadRef): ThreadInfo {
    const title = ref.title || ref.autoTitle
    const started = !!ref.backendThreadId
    return {
      id: ref.id,
      ...(title ? { title } : {}),
      started,
      titlePending: started && !title,
      updatedAt: ref.updatedAt,
      createdAt: ref.createdAt,
      backendId: ref.backendId,
    }
  }
}
