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
  return requestAt(`${baseUrl(ctx)}${path}`, ctx, method, body)
}

/** The same request, against an absolute URL that is not under `/v1`. */
export async function requestAt<T = unknown>(
  url: string,
  ctx: Context,
  method: string,
  body?: unknown,
): Promise<T> {
  let res: Response
  try {
    res = await fetch(url, {
      method,
      headers: {
        ...headers(ctx),
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch (err) {
    // A TLS or DNS failure arrives as a runtime string ("unknown certificate
    // verification error") with no indication of which address produced it —
    // and the address is the part the caller can do something about. Said
    // plainly, and said as retryable, because most of these are.
    const why = err instanceof Error ? err.message : String(err)
    throw new CliError(
      `could not reach ${url}: ${why}`,
      'nothing was sent. Check the address and that the platform is up, then run it again — ' +
        '`abx context list` shows which deployment this command resolved to.',
    )
  }

  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : {}
  } catch {
    parsed = { error: text }
  }

  if (res.ok) return parsed as T

  const payload = parsed as {
    error?: string
    errorCode?: string
    detail?: unknown
    approval?: { url?: string }
  }
  const msg = payload.error ?? `${res.status} ${res.statusText}`

  // The gate answers with APPROVAL_REQUIRED and a `detail` block carrying the
  // link a person needs. Reading it from `payload.approval` (which the server
  // has never sent) threw the whole thing away and printed the doubled
  // "this write is held for approval: approval required: …" instead.
  if (payload.errorCode === 'APPROVAL_REQUIRED' || res.status === 428) {
    throw new CliError(
      `this write is waiting for a person: ${approvalSummary(payload.detail, msg)}`,
      approvalHint(payload.detail),
    )
  }
  // Never going to be granted, and deliberately not an approval: an agent that
  // cannot tell this from the case above polls for a decision nobody will ever
  // be asked to make.
  if (payload.errorCode === 'FORBIDDEN_FOR_AGENT') {
    const detail = payload.detail as { summary?: string; url?: string } | undefined
    const what = detail?.summary ?? msg
    throw new CliError(
      `this credential cannot do that at all: ${what}`,
      'there is no request to wait on here, and nothing an agent key can do to change that — ' +
        'a person does it as themselves' +
        (detail?.url ? `:\n  ${detail.url}` : ' in the console.'),
    )
  }
  if (res.status === 403) {
    throw new CliError(msg, hintFor403(msg))
  }
  if (res.status === 404) {
    const e = new CliError(msg, 'check the name, or list what exists first')
    e.status = 404
    throw e
  }
  throw new CliError(msg, typeof payload.detail === 'object' ? JSON.stringify(payload.detail) : undefined)
}

/** What the person is being asked, from the 428's structured detail. */
function approvalSummary(detail: unknown, fallback: string): string {
  const d = detail as { summary?: string } | undefined
  return d?.summary ?? fallback.replace(/^approval required:\s*/, '')
}

/**
 * Everything the caller needs to know about a held write, in the order it
 * needs it: where a person releases it, which request this is, and that there
 * is nothing to retry until they do.
 */
function approvalHint(detail: unknown): string {
  const d = (detail ?? {}) as {
    approvalId?: string
    operation?: string
    onceOnly?: boolean
    url?: string
    expiresAt?: string
  }
  const lines: string[] = []
  lines.push(
    d.url
      ? `a person releases it here:\n  ${d.url}`
      : 'this deployment publishes no console address, so ask whoever runs it to release the request.',
  )
  const facts = [d.operation ? `${d.operation}` : '', d.onceOnly ? 'single-use' : ''].filter(Boolean)
  if (d.approvalId || facts.length) {
    lines.push(
      `approval ${d.approvalId ?? '(unknown id)'}${facts.length ? ` (${facts.join(', ')})` : ''}` +
        `${d.expiresAt ? ` expires at ${d.expiresAt}.` : '.'}`,
    )
  }
  lines.push(
    're-running this command after they decide is how it goes through: there is nothing to\n' +
      'retry before then, and an agent key cannot decide its own request.',
  )
  return lines.join('\n')
}

function hintFor403(msg: string): string | undefined {
  // An admin route reached with a tenant key. This one has nothing to do with
  // approvals or with what an agent key may mint: it is a role, and the answer
  // is either an admin key or the console.
  if (/admin\b/i.test(msg)) {
    return 'that is an admin surface: use an admin key, or do it in the console as an admin'
  }
  if (/impersonat/i.test(msg)) {
    // Not a permission problem, and not one this CLI offers a flag for. It
    // speaks as one tenant, whoever the key belongs to; acting as somebody
    // means holding their key, not asking this one to pretend. An admin with
    // work to do across tenants does it in the console.
    return 'this read is per-tenant and this credential names no tenant — use that user\'s own API key, or do it in the console'
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
