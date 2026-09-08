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

// The API key a conversation's sandbox is created with — server-only.
//
// A sandbox created with the deployment's own key lands in the DEPLOYMENT's
// namespace, is billed to its quota, and shows the whole cluster to `abx`. The
// assistant instead creates each conversation's sandbox with a key belonging to
// the person talking to it, so the sandbox lands in their namespace, counts
// against their quota, and everything the agent lists is theirs.
//
// The key is always a TENANT key, including for an admin. That is not enforced
// here — it is a property of how the hub mints it (see identityHeaders below) —
// which is why this module never inspects or asserts a role.

import type { AuthJWTPayload } from "@/lib/auth"

/** Where the hub's own API lives, as seen from this server. */
const WSPROXY_INTERNAL_URL =
  process.env.WSPROXY_INTERNAL_URL ?? "http://localhost:9004"

/**
 * Marks the key this module owns, so it is found again on the next request
 * instead of minting a second one, and so a person can recognise it in the
 * dashboard's own key list. Changing it orphans every key already issued —
 * they keep working, but a fresh one gets minted alongside.
 */
export const SESSION_KEY_DESCRIPTION = "agentbox-assistant"

/** Who a conversation acts as. */
export interface EffectiveIdentity {
  team: string
  user: string
  /** True when an admin selected someone other than themselves. */
  impersonating: boolean
}

/** Query parameters carrying the same selection the headers do. */
export const IMPERSONATE_TEAM_PARAM = "impersonateTeam"
export const IMPERSONATE_USER_PARAM = "impersonateUser"

/**
 * The identity a request acts under.
 *
 * The browser sends its impersonation selection in headers, which are not
 * trustworthy — so they are honoured only for a session whose VERIFIED role is
 * admin, mirroring what the API's own middleware does with the same two headers.
 * For everyone else they are ignored rather than rejected: a tenant who forges
 * them gets their own identity back, which is the same answer they would get by
 * not sending them.
 *
 * Read from the query string as well, because the thread event stream is an
 * `EventSource` and cannot set a header. One decision covering both transports
 * is the point: computing it from headers alone would make the push channel
 * resolve to the admin's own identity while their runs resolve to the selected
 * user's, and the visible symptom is only that conversation titles stop
 * arriving — nothing that names the cause.
 */
export function effectiveIdentity(
  payload: AuthJWTPayload,
  headers: Headers,
  searchParams?: URLSearchParams
): EffectiveIdentity {
  const team = (payload.team ?? "").trim()
  const user = (payload.user ?? "").trim()

  if (payload.role === "admin") {
    const impTeam = (
      headers.get("x-impersonate-team") ??
      searchParams?.get(IMPERSONATE_TEAM_PARAM) ??
      ""
    ).trim()
    const impUser = (
      headers.get("x-impersonate-user") ??
      searchParams?.get(IMPERSONATE_USER_PARAM) ??
      ""
    ).trim()
    // Both or neither: a half-specified target would resolve to a namespace
    // belonging to nobody in particular.
    if (impTeam && impUser) {
      return { team: impTeam, user: impUser, impersonating: true }
    }
  }
  return { team, user, impersonating: false }
}

/**
 * The headers that make the hub mint a TENANT key for `id`.
 *
 * Sent ALWAYS, naming the caller themselves when nobody was selected — not only
 * when impersonating. The hub's key creation copies the caller's own role onto
 * the key it mints and only forces `tenant` when these headers are present
 * (pkg/wsproxy/syncmgr/handlers/apikey.go). So an admin who sends nothing gets
 * an ADMIN key, and their sandbox would then see the whole cluster and be
 * refused by the quota endpoint, which rejects a non-impersonating admin
 * outright. Naming yourself is what makes the outcome uniform for every caller.
 *
 * Harmless for a tenant: the hub ignores these headers unless the caller is an
 * admin, and a tenant's own identity is what it would have used anyway.
 */
function identityHeaders(id: EffectiveIdentity): Record<string, string> {
  return { "X-Impersonate-Team": id.team, "X-Impersonate-User": id.user }
}

interface HubKeyItem {
  keyId: string
  description?: string
  rawToken?: string
  role?: string
  user?: string
  team?: string
  mode?: string
}

/**
 * The hub answers `GET /v1/api-keys` with a BARE ARRAY (`ListAPIKeysResult =
 * []APIKeyItem`), not with an `{items}` envelope like most list endpoints on
 * this API. Reading `.items` off it yields `undefined`, no key is ever found
 * reusable, and every cache miss mints another one until the hub's per-user cap
 * refuses — which is what the person sees, a 409 in the middle of a
 * conversation, nowhere near the shape mismatch that caused it.
 *
 * Both shapes are accepted so that a future envelope does not reintroduce it.
 */
function parseKeyList(body: unknown): HubKeyItem[] {
  if (Array.isArray(body)) return body as HubKeyItem[]
  const items = (body as { items?: unknown } | null)?.items
  return Array.isArray(items) ? (items as HubKeyItem[]) : []
}

/**
 * The one key belonging to `id` that this module may hand out.
 *
 * Matched on team+user as well as description, because the hub's LIST does not
 * scope itself the way its CREATE does: create honours the impersonation
 * headers, while list honours `?team=&user=` and, for an admin who sends
 * neither, returns every key in the namespace. Picking the first matching
 * description out of that would hand one person's conversation another person's
 * credential — an admin's sandbox running as whoever happened to sort first.
 *
 * The role check keeps the module's promise that this is always a tenant key.
 * A key carrying this description but a wider role was not minted here; using
 * it would silently restore the whole-cluster access the identity exists to
 * avoid. Skipping it mints a correct one alongside.
 */
function reusable(items: HubKeyItem[], id: EffectiveIdentity): string | undefined {
  const match = items.find(
    (k) =>
      // Agent mode is the load-bearing half of the match, not the description.
      // The description is a label anyone can type; the mode is what actually
      // holds this key's platform writes for a person, and reusing a key
      // without it would hand the assistant a credential that changes things
      // unattended — the exact thing the gate exists to prevent.
      k.mode === "agent" &&
      k.description === SESSION_KEY_DESCRIPTION &&
      // A key minted before plaintext storage has no rawToken and cannot be
      // reused — skipping it mints a usable one alongside rather than failing.
      !!k.rawToken &&
      (k.team ?? id.team) === id.team &&
      (k.user ?? id.user) === id.user &&
      (k.role ?? "tenant") === "tenant"
  )
  return match?.rawToken
}

/**
 * Keyed by identity, NOT by session: the key belongs to the person.
 *
 * Entries expire. A revoked key cannot be noticed here — it is spent by the
 * daemon creating a sandbox, and that failure surfaces inside the conversation,
 * never on this hop — so there is nothing to invalidate on. An expiring entry
 * means a person who deletes this key on the key page recovers by waiting
 * rather than by restarting the console, and the authority stays the hub's key
 * list, which a revoked key drops out of.
 */
const CACHE_TTL_MS = 120_000

const cache = new Map<string, { key: string; expires: number }>()

function cacheKey(id: EffectiveIdentity): string {
  return `${id.team}/${id.user}`
}

function cached(id: EffectiveIdentity): string | undefined {
  const hit = cache.get(cacheKey(id))
  if (!hit) return undefined
  if (hit.expires <= Date.now()) {
    cache.delete(cacheKey(id))
    return undefined
  }
  return hit.key
}

function remember(id: EffectiveIdentity, key: string): void {
  cache.set(cacheKey(id), { key, expires: Date.now() + CACHE_TTL_MS })
}

/**
 * Resolutions already running, so concurrent first-requests share one.
 *
 * A conversation opens its event stream and posts its run at the same moment,
 * and both need the key. Without this they both miss the cache, both list, both
 * find nothing, and both create — two keys for one person, out of an allowance
 * of three. The pair this produced was visible in the hub's key list as two
 * entries issued in the same second.
 */
const inflight = new Map<string, Promise<string>>()

/** Only for tests — the cache is process-wide and would leak between them. */
export function resetSessionKeyCache(): void {
  cache.clear()
  inflight.clear()
}

async function hubFetch(
  path: string,
  jwt: string,
  id: EffectiveIdentity,
  init?: { method: string; body?: string }
): Promise<Response> {
  return fetch(`${WSPROXY_INTERNAL_URL}${path}`, {
    method: init?.method ?? "GET",
    headers: {
      authorization: `Bearer ${jwt}`,
      "content-type": "application/json",
      ...identityHeaders(id),
    },
    ...(init?.body ? { body: init.body } : {}),
    // No caching layer between us and a credential.
    cache: "no-store",
  })
}

export class SessionKeyError extends Error {
  constructor(
    message: string,
    readonly status: number
  ) {
    super(message)
    this.name = "SessionKeyError"
  }
}

/**
 * The tenant key for `id`, reused across conversations and across sessions.
 *
 * One long-lived key per person rather than one per conversation: a
 * per-conversation key needs someone to delete it, and nobody does after a pod
 * restart or an abandoned thread — the residue is a growing pile of live
 * credentials. This key is visible to its owner on the dashboard's key page,
 * and revoking it there takes effect once the cache entry above expires.
 *
 * Never logged and never returned to a browser.
 */
export async function getOrCreateSessionKey(
  jwt: string,
  id: EffectiveIdentity
): Promise<string> {
  if (!id.team || !id.user) {
    throw new SessionKeyError(
      "This account has no team/user identity, so a sandbox cannot be created " +
        "on its behalf. An administrator should pick a user with the " +
        "impersonation selector in the sidebar.",
      409
    )
  }

  const hit = cached(id)
  if (hit) return hit

  const key = cacheKey(id)
  const running = inflight.get(key)
  if (running) return running

  const attempt = resolveSessionKey(jwt, id).finally(() => {
    if (inflight.get(key) === attempt) inflight.delete(key)
  })
  inflight.set(key, attempt)
  return attempt
}

/**
 * Mints the one agent key this identity gets, tolerating the race.
 *
 * Two requests can both find no key and both try to make one — a conversation
 * opens its event stream and posts its run at the same moment. The hub allows
 * only one agent key per person and answers the loser with a conflict, so the
 * loser re-reads rather than failing: the key it wanted now exists, and it was
 * made for the same person by the same code a few milliseconds earlier.
 */
async function createAgentKey(
  jwt: string,
  id: EffectiveIdentity
): Promise<Response> {
  const created = await hubFetch("/v1/api-keys", jwt, id, {
    method: "POST",
    // `agent`: this key is handed to something that acts while nobody is
    // watching, so the platform holds its writes until the person in the
    // conversation approves them. Its sandbox work is unaffected — same key,
    // and the gate does not sit on that surface.
    body: JSON.stringify({
      description: SESSION_KEY_DESCRIPTION,
      mode: "agent",
    }),
  })
  if (created.status !== 409) return created

  const listed = await hubFetch(listPathFor(id), jwt, id)
  if (!listed.ok) return created
  const existing = reusable(
    parseKeyList(await listed.json().catch(() => null)),
    id
  )
  if (!existing) return created
  return new Response(JSON.stringify({ apiKey: existing }), {
    status: 201,
    headers: { "content-type": "application/json" },
  })
}

/**
 * The hub's LIST scopes itself by query parameter; its CREATE scopes itself by
 * the impersonation headers. `hubFetch` sends the headers on everything, so
 * omitting these parameters does not narrow the read — for an admin it returns
 * every key in the namespace, and the first matching one would be somebody
 * else's.
 */
function listPathFor(id: EffectiveIdentity): string {
  return (
    `/v1/api-keys?team=${encodeURIComponent(id.team)}` +
    `&user=${encodeURIComponent(id.user)}`
  )
}

async function resolveSessionKey(
  jwt: string,
  id: EffectiveIdentity
): Promise<string> {
  const listed = await hubFetch(listPathFor(id), jwt, id)
  if (listed.status === 401 || listed.status === 403) {
    throw new SessionKeyError(
      "Not allowed to read the API keys for this identity.",
      listed.status
    )
  }
  if (listed.ok) {
    const existing = reusable(
      parseKeyList(await listed.json().catch(() => null)),
      id
    )
    if (existing) {
      remember(id, existing)
      return existing
    }
  }

  const created = await createAgentKey(jwt, id)
  if (!created.ok) {
    const detail = await created.text().catch(() => "")
    // The cap is reached with no reusable key only when the person's whole
    // allowance is spent on keys of their own, so the way out is theirs to
    // take on the API keys page. Saying so beats reporting the hub's wording,
    // which names a limit without naming who can act on it.
    const capped = created.status === 409 && detail.includes("max keys per user")
    throw new SessionKeyError(
      capped
        ? `${id.team}/${id.user} has used their whole API key allowance, and ` +
          `none of those keys is the assistant's own ("${SESSION_KEY_DESCRIPTION}"). ` +
          `Delete an unused key on the API Keys page and start the conversation again.`
        : `Could not create a sandbox API key for ${id.team}/${id.user}: ` +
          `hub returned ${created.status} ${detail.slice(0, 200)}`,
      created.status === 409 ? 409 : 503
    )
  }
  const body = (await created.json().catch(() => null)) as {
    apiKey?: string
  } | null
  if (!body?.apiKey) {
    throw new SessionKeyError(
      "The hub created a key but returned no token for it.",
      503
    )
  }
  remember(id, body.apiKey)
  return body.apiKey
}
