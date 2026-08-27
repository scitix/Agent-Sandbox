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

// Product behaviour every backend needs, defined once.
//
// These used to live inside the Claude Code adapter, which was fine while it was
// the only real backend. With two of them the risk is the classic one: the second
// adapter reimplements "fold the page marker in" slightly differently, and the
// two harnesses answer the same question differently for reasons nobody can see.
import { homedir } from 'node:os'
import { join } from 'node:path'

import { proxyUrl } from '@scitix/agentbox-hands'
import type { PageContext } from './backend.ts'

/**
 * The runtime prompt the image ships, read by every harness.
 *
 * This is where an agent learns what it is — which sandbox it has, what the
 * deployment expects of it — so both backends must read the SAME file. They used
 * to reach it through a literal written out in each one, which is the arrangement
 * where a moved file leaves one harness on the bare preset and the other not.
 *
 * Resolved from HOME rather than written out, because a literal has to agree with
 * the image's runtime user and there is nothing to notice when it stops agreeing:
 * a missing file means "no product knowledge", and the agent then answers
 * plausibly and generically instead of failing.
 */
export function systemPromptFile(): string {
  return process.env.ASSISTANT_SYSTEM_PROMPT_FILE || join(homedir(), 'AGENTS.md')
}

/**
 * The `<page … />` marker the dashboard's own prompt carries: which page the user
 * was looking at when they hit send. Appended to the prompt rather than sent as a
 * separate field because it must survive into the transcript — an answer that
 * resolved "this node" is only readable later if the marker is still there.
 */
export function renderPageMarker(page: PageContext | undefined): string {
  if (!page) return ''
  const attrs = Object.entries(page)
    .filter(([, v]) => typeof v === 'string' && v.length > 0)
    .map(([k, v]) => `${k}="${String(v).replace(/"/g, '&quot;')}"`)
  return attrs.length ? `<page ${attrs.join(' ')} />` : ''
}

/** Prompt text + marker, in the one order both backends must use. */
export function promptWithPage(
  text: string,
  page: PageContext | undefined
): string {
  const marker = renderPageMarker(page)
  const body = text.trim()
  return marker ? `${body}\n\n${marker}` : body
}

/**
 * Tell the sandbox daemon which thread (and therefore which sandbox, cwd and
 * staged attachments) the next tool call belongs to.
 *
 * Done on every run rather than once at thread creation so a daemon restart
 * self-heals. Failure is swallowed on purpose: the daemon logs it, and a turn
 * must not die because the bind call raced a restart — but note that a MISSING
 * bind degrades three things silently at once (sandbox cwd, attachment flush, UI
 * mode), which is why it is called before any tool can run rather than lazily.
 *
 * `aliases` are ids the HARNESS will address the daemon by instead of the thread
 * id. OpenCode's tools get OpenCode's session id and cannot translate it, so
 * without the alias that id becomes a second session with a second sandbox: the
 * thread's workspace panel answers `inactive` while the agent works somewhere
 * nobody can see, and attachments staged under the thread id never flush.
 */
export async function bindSandboxIdentity(opts: {
  threadId: string
  directory: string
  aliases?: string[]
}): Promise<void> {
  try {
    await fetch(
      `${proxyUrl()}/sessions/${encodeURIComponent(opts.threadId)}/bind`,
      {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          directory: opts.directory,
          ...(opts.aliases?.length ? { aliases: opts.aliases } : {}),
        }),
      }
    )
  } catch {
    // See above: best effort by design.
  }
}

/** Release the sandbox bound to a thread (thread deletion). */
export async function releaseSandbox(threadId: string): Promise<void> {
  await fetch(`${proxyUrl()}/sessions/${encodeURIComponent(threadId)}`, {
    method: 'DELETE',
  }).catch(() => undefined)
}
