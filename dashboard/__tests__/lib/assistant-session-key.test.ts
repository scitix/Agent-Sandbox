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

// Which identity a conversation acts as, and which credential its sandbox gets.
//
// This decides whose namespace a sandbox lands in, whose quota it spends and
// whose vault its egress injection reads — so getting it wrong is not a display
// bug. Two directions matter and they fail differently: honouring a tenant's
// forged header would hand them somebody else's credential, and dropping an
// admin's real selection would quietly run the conversation as the admin while
// the rest of the console shows the selected user's data.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import type { AuthJWTPayload } from "@/lib/auth"
import {
  effectiveIdentity,
  getOrCreateSessionKey,
  resetSessionKeyCache,
  SESSION_KEY_DESCRIPTION,
  SessionKeyError,
} from "@/lib/server/assistant-session-key"

function payload(over: Partial<AuthJWTPayload> = {}): AuthJWTPayload {
  return { role: "tenant", team: "team1", user: "bob", ...over } as AuthJWTPayload
}

const IMPERSONATE = { "x-impersonate-team": "team2", "x-impersonate-user": "carol" }

describe("effectiveIdentity", () => {
  it("uses the session's own identity when nothing is selected", () => {
    expect(effectiveIdentity(payload(), new Headers())).toEqual({
      team: "team1",
      user: "bob",
      impersonating: false,
    })
  })

  it("honours an admin's selection", () => {
    expect(
      effectiveIdentity(payload({ role: "admin" }), new Headers(IMPERSONATE))
    ).toEqual({ team: "team2", user: "carol", impersonating: true })
  })

  it("IGNORES a tenant's selection", () => {
    // The headers come from a browser and are chosen by the person they would
    // name. Honouring them for a tenant is a credential handout.
    expect(effectiveIdentity(payload(), new Headers(IMPERSONATE))).toEqual({
      team: "team1",
      user: "bob",
      impersonating: false,
    })
  })

  it("ignores a half-specified selection", () => {
    // A team with no user resolves to a namespace belonging to nobody in
    // particular, so it is not a selection at all.
    const partials: Record<string, string>[] = [
      { "x-impersonate-team": "team2" },
      { "x-impersonate-user": "carol" },
      { "x-impersonate-team": "  ", "x-impersonate-user": "carol" },
    ]
    for (const partial of partials) {
      expect(
        effectiveIdentity(payload({ role: "admin" }), new Headers(partial))
      ).toMatchObject({ team: "team1", user: "bob", impersonating: false })
    }
  })

  it("reads the selection from the query string too", () => {
    // The thread event stream is an EventSource and cannot set a header. If it
    // resolved to a different identity than the runs do, the only symptom would
    // be conversation titles that never arrive.
    expect(
      effectiveIdentity(
        payload({ role: "admin" }),
        new Headers(),
        new URLSearchParams({ impersonateTeam: "team2", impersonateUser: "carol" })
      )
    ).toEqual({ team: "team2", user: "carol", impersonating: true })
  })

  it("does not honour a query selection for a tenant either", () => {
    expect(
      effectiveIdentity(
        payload(),
        new Headers(),
        new URLSearchParams({ impersonateTeam: "team2", impersonateUser: "carol" })
      )
    ).toMatchObject({ team: "team1", user: "bob", impersonating: false })
  })
})

describe("getOrCreateSessionKey", () => {
  const identity = { team: "team1", user: "bob", impersonating: false }
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    resetSessionKeyCache()
    fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    resetSessionKeyCache()
  })

  function ok(body: unknown) {
    return { ok: true, status: 200, json: async () => body, text: async () => "" }
  }

  /**
   * The hub's list response, in the shape it actually sends: a BARE ARRAY.
   *
   * Every test here once passed an `{items}` envelope, which no version of the
   * hub has ever returned — so the reuse branch was exercised on a shape that
   * only existed in this file, and in production nothing was ever reused. Keys
   * accumulated one per cache miss until the per-user cap answered 409 in the
   * middle of a conversation. Build list responses through this helper so a
   * test cannot invent the shape again.
   */
  function keyList(...items: Record<string, unknown>[]) {
    return ok(items)
  }

  function assistantKey(over: Record<string, unknown> = {}) {
    return {
      keyId: "k1",
      description: SESSION_KEY_DESCRIPTION,
      rawToken: "agbx_existing",
      role: "tenant",
      // Agent mode is what makes a key reusable here. A key carrying the right
      // description without it is an unrestricted credential, and handing that
      // to the assistant is the thing the approval gate exists to prevent.
      mode: "agent",
      team: identity.team,
      user: identity.user,
      ...over,
    }
  }

  it("reuses the key it already minted for this person", () => {
    fetchMock.mockResolvedValueOnce(keyList(assistantKey()))
    return expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_existing")
  })

  it("asks the hub only for this identity's keys", async () => {
    // LIST does not read the impersonation headers CREATE does: it reads
    // ?team=&user=, and for an admin who sends neither it returns every key in
    // the namespace.
    fetchMock.mockResolvedValueOnce(keyList(assistantKey()))
    await getOrCreateSessionKey("jwt", identity)

    const [url] = fetchMock.mock.calls[0]
    const params = new URL(url, "http://x").searchParams
    expect(params.get("team")).toBe(identity.team)
    expect(params.get("user")).toBe(identity.user)
  })

  it("never reuses a key belonging to someone else", async () => {
    // What an unscoped list returns for an admin. Taking the first matching
    // description out of it runs one person's conversation on another
    // person's credential — in their namespace, against their quota.
    fetchMock
      .mockResolvedValueOnce(
        keyList(assistantKey({ team: "team9", user: "eve", rawToken: "agbx_eve" }))
      )
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("never reuses a key that is not in agent mode", async () => {
    // A key with the right description but no mode is an unrestricted
    // credential: it would change things without anyone being asked, which is
    // exactly what the gate exists to prevent.
    fetchMock
      .mockResolvedValueOnce(keyList(assistantKey({ mode: "unrestricted" })))
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("never reuses a key that is not a tenant key", async () => {
    // This module's whole promise is that the sandbox runs as a tenant. A key
    // with a wider role was not minted here, and using it would restore the
    // whole-cluster access the identity exists to avoid.
    fetchMock
      .mockResolvedValueOnce(keyList(assistantKey({ role: "admin", rawToken: "agbx_admin" })))
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("mints one when there is none", async () => {
    fetchMock.mockResolvedValueOnce(keyList()).mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")

    const [, init] = fetchMock.mock.calls[1]
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body)).toEqual({
      description: SESSION_KEY_DESCRIPTION,
      mode: "agent",
    })
  })

  it("mints at most one key for concurrent first requests", async () => {
    // A conversation opens its event stream and posts its run at the same
    // moment. Two independent resolutions spend two of an allowance of three.
    fetchMock.mockResolvedValue(ok({ apiKey: "agbx_new" }))
    fetchMock.mockResolvedValueOnce(keyList())
    const [a, b] = await Promise.all([
      getOrCreateSessionKey("jwt", identity),
      getOrCreateSessionKey("jwt", identity),
    ])
    expect([a, b]).toEqual(["agbx_new", "agbx_new"])
    const posts = fetchMock.mock.calls.filter(([, init]) => init.method === "POST")
    expect(posts).toHaveLength(1)
  })

  it("skips a key it cannot reuse rather than failing", async () => {
    // A key minted before plaintext storage has no rawToken. Minting alongside
    // it is recoverable; refusing to start the conversation is not.
    fetchMock
      .mockResolvedValueOnce(keyList(assistantKey({ rawToken: undefined })))
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("ignores keys the person made for something else", async () => {
    fetchMock
      .mockResolvedValueOnce(
        keyList(assistantKey({ keyId: "ci", description: "my ci pipeline", rawToken: "agbx_ci" }))
      )
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("names the person, not the cap, when the allowance is spent", async () => {
    // The hub says "exceeded max keys per user (3)", which names a limit but
    // not who can act on it. Reaching it with no reusable key means the
    // person's own keys are the ones in the way.
    fetchMock
      .mockResolvedValueOnce(keyList())
      .mockResolvedValueOnce({
        ok: false,
        status: 409,
        text: async () => '{"error":"exceeded max keys per user (3)"}',
        json: async () => ({}),
      })
      // The re-read a conflict triggers. Still nothing to reuse, so the
      // refusal stands.
      .mockResolvedValueOnce(keyList())
    await expect(getOrCreateSessionKey("jwt", identity)).rejects.toThrow(/API Keys page/)
  })

  it("adopts the key the other request just made", async () => {
    // Two requests can both find nothing and both try to create; the hub allows
    // one agent key per person and refuses the loser. Failing there would end a
    // conversation over a key that now exists, made for the same person by the
    // same code a moment earlier.
    fetchMock
      .mockResolvedValueOnce(keyList())
      .mockResolvedValueOnce({
        ok: false,
        status: 409,
        text: async () => '{"error":"this user already has an agent key (k1)"}',
        json: async () => ({}),
      })
      .mockResolvedValueOnce(keyList(assistantKey()))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_existing")
  })

  it("ALWAYS names the identity, so the hub mints a tenant key", async () => {
    // The hub copies the caller's own role onto the key unless these headers are
    // present, and only forces `tenant` when they are. An admin who sends
    // nothing therefore gets an ADMIN key — whose sandbox sees the whole cluster
    // and whose quota lookup is refused outright. Naming yourself is what makes
    // the result the same for everyone.
    fetchMock.mockResolvedValueOnce(keyList()).mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await getOrCreateSessionKey("jwt", { team: "t", user: "u", impersonating: false })

    for (const [, init] of fetchMock.mock.calls) {
      expect(init.headers["X-Impersonate-Team"]).toBe("t")
      expect(init.headers["X-Impersonate-User"]).toBe("u")
    }
  })

  it("caches per identity, not per conversation", async () => {
    fetchMock.mockResolvedValue(keyList(assistantKey({ rawToken: "agbx_x" })))
    await getOrCreateSessionKey("jwt", identity)
    await getOrCreateSessionKey("jwt", identity)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("does not confuse two identities", async () => {
    fetchMock
      .mockResolvedValueOnce(keyList(assistantKey({ keyId: "b", rawToken: "agbx_bob" })))
      .mockResolvedValueOnce(
        keyList(
          assistantKey({
            keyId: "c",
            team: "team2",
            user: "carol",
            rawToken: "agbx_carol",
          })
        )
      )
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_bob")
    await expect(
      getOrCreateSessionKey("jwt", { team: "team2", user: "carol", impersonating: true })
    ).resolves.toBe("agbx_carol")
  })

  it("refuses an identity with no team or user", async () => {
    // An administrator whose account is not itself a tenant. The quota endpoint
    // would refuse that identity anyway, so saying "pick a user" here beats
    // letting the agent hit a 403 inside the sandbox and guess at why.
    for (const bad of [
      { team: "", user: "bob", impersonating: false },
      { team: "team1", user: "", impersonating: false },
    ]) {
      await expect(getOrCreateSessionKey("jwt", bad)).rejects.toThrow(SessionKeyError)
    }
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("reports a hub that will not mint, rather than returning nothing", async () => {
    fetchMock
      .mockResolvedValueOnce(keyList())
      .mockResolvedValueOnce({
        ok: false,
        status: 503,
        text: async () => "key store not configured",
        json: async () => ({}),
      })
    await expect(getOrCreateSessionKey("jwt", identity)).rejects.toThrow(
      /could not create a sandbox api key/i
    )
  })
})
