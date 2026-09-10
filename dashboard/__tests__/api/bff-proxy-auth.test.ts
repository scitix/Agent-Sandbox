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

import { describe, it, expect } from "vitest"
import { SignJWT } from "jose"

const JWT_SECRET = "test-secret-32-bytes-xxxxxxxxxxx"
process.env.JWT_SECRET = JWT_SECRET

import { requireProxyAuth } from "@/lib/server/bff-auth"

async function makeJWT(payload: Record<string, unknown>): Promise<string> {
  return new SignJWT(payload)
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime("7d")
    .sign(new TextEncoder().encode(JWT_SECRET))
}

describe("requireProxyAuth", () => {
  it("accepts a platform API key without verifying it as a JWT", async () => {
    // This is what a sandbox sends: `abx` reaches every cluster through the
    // path-routed BFF address, and the only credential injected into it is the
    // platform key. Running it through verifyJWT rejects a valid credential.
    const key = "agbx_" + "a".repeat(43)
    const result = await requireProxyAuth(`Bearer ${key}`)
    expect("error" in result).toBe(false)
    if ("error" in result) return
    expect(result.identity).toEqual({ kind: "platformKey", apiKey: key })
  })

  it("still accepts a browser session JWT", async () => {
    const token = await makeJWT({ user: "ylli", team: "k8s", role: "admin", authMethod: "oidc" })
    const result = await requireProxyAuth(`Bearer ${token}`)
    expect("error" in result).toBe(false)
    if ("error" in result) return
    expect(result.identity.kind).toBe("jwt")
    if (result.identity.kind !== "jwt") return
    expect(result.identity.payload.user).toBe("ylli")
    expect(result.identity.token).toBe(token)
  })

  it("rejects a bearer value that is neither a platform key nor a valid JWT", async () => {
    const result = await requireProxyAuth("Bearer not-a-jwt-and-not-a-key")
    expect("error" in result).toBe(true)
  })

  it("rejects a missing or non-Bearer Authorization header", async () => {
    expect("error" in (await requireProxyAuth(null))).toBe(true)
    expect("error" in (await requireProxyAuth("Basic abc"))).toBe(true)
  })

  it("does not treat a key-shaped value as a JWT even when the secret would reject it", async () => {
    // Guards the ordering: the prefix check must run BEFORE verifyJWT, or a
    // platform key comes back as "Invalid or expired token" — which reads like
    // the key is wrong rather than like the door only accepts one kind.
    const key = "agbx_short"
    const result = await requireProxyAuth(`Bearer ${key}`)
    expect("error" in result).toBe(false)
  })
})
