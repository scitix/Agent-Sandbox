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

import { baseUrl, CliError, headers, type Context } from './context'

/**
 * One request, and an error that explains itself.
 *
 * The approval case is the one worth reading: an Agent-mode key has its writes
 * held for a person to release, and the response carries where that happens. A
 * plain 403 would send an agent looking for a permission to fix; the link tells
 * it what it is actually waiting for.
 *
 * Minting credentials is different again and deliberately so. There is no
 * approval to wait for, because an agent that can issue a key can issue one
 * without the Agent restriction and walk out of the gate entirely — so the CLI
 * hands back a console link and stops, rather than opening a request nobody
 * should be able to wave through from a prompt.
 */
export async function request<T = unknown>(
  ctx: Context,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const url = `${baseUrl(ctx)}${path}`
  const res = await fetch(url, {
    method,
    headers: {
      ...headers(ctx),
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : {}
  } catch {
    parsed = { error: text }
  }

  if (res.ok) return parsed as T

  const payload = parsed as { error?: string; detail?: unknown; approval?: { url?: string } }
  const msg = payload.error ?? `${res.status} ${res.statusText}`

  if (res.status === 428 || payload.approval?.url) {
    throw new CliError(
      `this write is held for approval: ${msg}`,
      payload.approval?.url
        ? `a person releases it here:\n  ${payload.approval.url}\nthe command succeeds once they do — re-run it then.`
        : undefined,
    )
  }
  if (res.status === 403) {
    throw new CliError(msg, hintFor403(msg))
  }
  if (res.status === 404) {
    throw new CliError(msg, 'check the name, or list what exists first')
  }
  throw new CliError(msg, typeof payload.detail === 'object' ? JSON.stringify(payload.detail) : undefined)
}

function hintFor403(msg: string): string | undefined {
  if (/impersonat/i.test(msg)) {
    // Not a permission problem: the read is tenant-scoped and an admin key has
    // no tenant. Saying which flags supply one turns the refusal into an answer.
    return 'this read is per-tenant and an admin key names no tenant — add --as-team <t> --as-user <u>'
  }
  if (/api key|credential/i.test(msg)) {
    // Not an approval path: this one is never granted to an agent at all.
    return 'issuing credentials is not something an agent key can do, with or without approval — open the console and do it as yourself'
  }
  if (/belongs to/i.test(msg)) {
    return 'a warm pool is capacity its owner already paid for; create in an env of your own'
  }
  return undefined
}
