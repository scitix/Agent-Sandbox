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

// Transport to the loopback sandbox-proxy daemon. Bun and Node both ship fetch
// natively; no extra deps.
//
// Moved here from tools/_proxy.ts when the toolset became harness-neutral: the
// daemon call and the notice convention are shared by every binding, so they
// must not live under a directory owned by one harness.

/**
 * Loopback daemon base URL. Read per call rather than captured at module load:
 * the gateway may reconfigure it, and freezing it at import time also made the
 * value untestable (the module is imported before any test can set the env).
 */
export function proxyUrl(): string {
  return process.env.SBX_PROXY_URL || 'http://127.0.0.1:8765'
}

export async function call<T = unknown>(
  sessionKey: string,
  endpoint: string,
  body: Record<string, unknown>
): Promise<T> {
  const url = `${proxyUrl()}/sessions/${encodeURIComponent(sessionKey)}/${endpoint}`
  let res: Response
  try {
    res = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch (e) {
    throw new Error(
      `[sandbox-proxy] Failed to reach ${url}: ${(e as Error).message}\n` +
        `Is the daemon running? (cd <repo>; ./launch.sh)`
    )
  }
  const txt = await res.text()
  if (!res.ok) {
    throw new Error(`[sandbox-proxy ${res.status}] ${txt}`)
  }
  try {
    return JSON.parse(txt) as T
  } catch {
    return txt as unknown as T
  }
}

/**
 * The canonical session id behind whatever id this harness was handed.
 *
 * The gateway binds each thread on the daemon with the harness's own session id
 * as an ALIAS (see bindSandboxIdentity), so the daemon is the one process that
 * knows `ses_… -> th_…`. Anything that has to name a session to a party OUTSIDE
 * the harness — the proxy's analysis jobs are bound to the thread id, not to
 * OpenCode's session — has to ask for the canonical id first.
 *
 * Best effort: returns the input unchanged when the daemon is unreachable or has
 * no alias, which is exactly what the caller would have sent anyway.
 */
export async function canonicalSessionKey(sessionKey: string): Promise<string> {
  try {
    const res = await fetch(
      `${proxyUrl()}/sessions/${encodeURIComponent(sessionKey)}/canonical`
    )
    if (!res.ok) return sessionKey
    const body = (await res.json()) as { session_id?: string }
    return body.session_id || sessionKey
  } catch {
    return sessionKey
  }
}

// Daemon responses carry an optional one-shot `notice` (e.g. the sandbox was
// transparently rebuilt after an idle-timeout release). Surface it as a leading
// `note:` line — never fold it into the tool's data payload — matching the
// render.ts degrade-notice convention. Consumed once by the daemon, so only the
// first tool result after a rebuild carries it.
export function withNotice(
  notice: string | null | undefined,
  body: string
): string {
  return notice ? `note: ${notice}\n\n${body}` : body
}
