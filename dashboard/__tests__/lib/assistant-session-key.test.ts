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

  it("reuses the key it already minted for this person", () => {
    fetchMock.mockResolvedValueOnce(
      ok({ items: [{ keyId: "k1", description: SESSION_KEY_DESCRIPTION, rawToken: "agbx_existing" }] })
    )
    return expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_existing")
  })

  it("mints one when there is none", async () => {
    fetchMock
      .mockResolvedValueOnce(ok({ items: [] }))
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")

    const [, init] = fetchMock.mock.calls[1]
    expect(init.method).toBe("POST")
    expect(JSON.parse(init.body)).toEqual({ description: SESSION_KEY_DESCRIPTION })
  })

  it("skips a key it cannot reuse rather than failing", async () => {
    // A key minted before plaintext storage has no rawToken. Minting alongside
    // it is recoverable; refusing to start the conversation is not.
    fetchMock
      .mockResolvedValueOnce(
        ok({ items: [{ keyId: "old", description: SESSION_KEY_DESCRIPTION }] })
      )
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("ignores keys the person made for something else", async () => {
    fetchMock
      .mockResolvedValueOnce(
        ok({ items: [{ keyId: "ci", description: "my ci pipeline", rawToken: "agbx_ci" }] })
      )
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await expect(getOrCreateSessionKey("jwt", identity)).resolves.toBe("agbx_new")
  })

  it("ALWAYS names the identity, so the hub mints a tenant key", async () => {
    // The hub copies the caller's own role onto the key unless these headers are
    // present, and only forces `tenant` when they are. An admin who sends
    // nothing therefore gets an ADMIN key — whose sandbox sees the whole cluster
    // and whose quota lookup is refused outright. Naming yourself is what makes
    // the result the same for everyone.
    fetchMock
      .mockResolvedValueOnce(ok({ items: [] }))
      .mockResolvedValueOnce(ok({ apiKey: "agbx_new" }))
    await getOrCreateSessionKey("jwt", { team: "t", user: "u", impersonating: false })

    for (const [, init] of fetchMock.mock.calls) {
      expect(init.headers["X-Impersonate-Team"]).toBe("t")
      expect(init.headers["X-Impersonate-User"]).toBe("u")
    }
  })

  it("caches per identity, not per conversation", async () => {
    fetchMock.mockResolvedValue(
      ok({ items: [{ keyId: "k1", description: SESSION_KEY_DESCRIPTION, rawToken: "agbx_x" }] })
    )
    await getOrCreateSessionKey("jwt", identity)
    await getOrCreateSessionKey("jwt", identity)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("does not confuse two identities", async () => {
    fetchMock
      .mockResolvedValueOnce(
        ok({ items: [{ keyId: "b", description: SESSION_KEY_DESCRIPTION, rawToken: "agbx_bob" }] })
      )
      .mockResolvedValueOnce(
        ok({ items: [{ keyId: "c", description: SESSION_KEY_DESCRIPTION, rawToken: "agbx_carol" }] })
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
      .mockResolvedValueOnce(ok({ items: [] }))
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
