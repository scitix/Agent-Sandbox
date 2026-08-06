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

import { describe, it, expect, vi, beforeEach } from "vitest"
import { SignJWT } from "jose"

// ─── Helpers ────────────────────────────────────────────────────────────────

const JWT_SECRET = "test-secret-32-bytes-xxxxxxxxxxx"
process.env.JWT_SECRET = JWT_SECRET

async function makeJWT(payload: Record<string, unknown>, expiry = "7d"): Promise<string> {
  const secret = new TextEncoder().encode(JWT_SECRET)
  return new SignJWT(payload)
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime(expiry)
    .sign(secret)
}

// ─── Mock undici ─────────────────────────────────────────────────────────────
// The BFF routes use undici.request() directly (not global fetch) so that the
// Host header can be overridden. We mock the entire undici module here.

vi.mock("undici", () => ({
  request: vi.fn(),
}))

import { request as undiciRequest } from "undici"
const mockUndiciRequest = vi.mocked(undiciRequest)

type UndiciMockResponse = { statusCode: number; body: unknown }

function makeUndiciBody(data: unknown) {
  const bodyStr = JSON.stringify(data)
  return {
    json: async () => JSON.parse(bodyStr),
    text: async () => bodyStr,
    arrayBuffer: async () => new TextEncoder().encode(bodyStr).buffer as ArrayBuffer,
    dump: async () => undefined,
  }
}

function makeFakeUndiciResponse(r: UndiciMockResponse) {
  return {
    statusCode: r.statusCode,
    headers: { "content-type": "application/json" },
    body: makeUndiciBody(r.body),
    trailers: {},
    opaque: null,
    context: {},
  } as unknown as Awaited<ReturnType<typeof undiciRequest>>
}

function mockUndici(responses: Array<UndiciMockResponse>) {
  let callIndex = 0
  mockUndiciRequest.mockImplementation(async () => {
    const r = responses[callIndex] ?? responses[responses.length - 1]
    callIndex++
    return makeFakeUndiciResponse(r)
  })
}

// ─── Import route handlers ───────────────────────────────────────────────────
// We import the handlers directly and construct synthetic Request objects.

// ─── /api/auth/me ────────────────────────────────────────────────────────────

describe("GET /api/auth/me", () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockUndiciRequest.mockReset()
  })

  it("returns user info for a valid JWT", async () => {
    const token = await makeJWT({
      apiKey: "agbx_testkey",
      role: "tenant",
      user: "alice",
    })

    const { GET } = await import("@/app/api/auth/me/route")
    const req = new Request("http://localhost/api/auth/me", {
      headers: { Authorization: `Bearer ${token}` },
    })
    const res = await GET(req)
    expect(res.status).toBe(200)
    const body = await res.json()
    expect(body.role).toBe("tenant")
    expect(body.namespace).toBeUndefined()
    expect(body.user).toBe("alice")
    expect(body.apiKey).toBeUndefined()
  })

  it("returns 401 for missing Authorization header", async () => {
    const { GET } = await import("@/app/api/auth/me/route")
    const req = new Request("http://localhost/api/auth/me")
    const res = await GET(req)
    expect(res.status).toBe(401)
  })

  it("returns 401 for an expired JWT", async () => {
    // Use a timestamp in the past (1 second ago)
    const expiredAt = Math.floor(Date.now() / 1000) - 1
    const secret = new TextEncoder().encode(JWT_SECRET)
    const token = await new SignJWT({ apiKey: "agbx_testkey", role: "admin" })
      .setProtectedHeader({ alg: "HS256" })
      .setIssuedAt(expiredAt - 10)
      .setExpirationTime(expiredAt)
      .sign(secret)

    const { GET } = await import("@/app/api/auth/me/route")
    const req = new Request("http://localhost/api/auth/me", {
      headers: { Authorization: `Bearer ${token}` },
    })
    const res = await GET(req)
    expect(res.status).toBe(401)
  })

  it("returns 401 for a token signed with wrong secret", async () => {
    const wrongSecret = new TextEncoder().encode("wrong-secret-xxxxxxxxxxxxxxxxxxxxxxx")
    const token = await new SignJWT({ apiKey: "agbx_x", role: "tenant" })
      .setProtectedHeader({ alg: "HS256" })
      .setIssuedAt()
      .setExpirationTime("7d")
      .sign(wrongSecret)

    const { GET } = await import("@/app/api/auth/me/route")
    const req = new Request("http://localhost/api/auth/me", {
      headers: { Authorization: `Bearer ${token}` },
    })
    const res = await GET(req)
    expect(res.status).toBe(401)
  })
})

// ─── /api/auth/login ─────────────────────────────────────────────────────────

describe("POST /api/auth/login", () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockUndiciRequest.mockReset()
  })

  it("returns 400 for missing apiKey", async () => {
    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    })
    const res = await POST(req)
    expect(res.status).toBe(400)
  })

  // An API key is not bound to a cluster — the JWT carries only the key and the
  // per-cluster proxy injects it wherever the URL points — so omitting
  // clusterID is valid and just picks whichever backend answers whoami.
  it("falls back to the first cluster when clusterID is omitted", async () => {
    mockUndici([{ statusCode: 200, body: { role: "tenant", user: "alice", team: "eng" } }])

    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "listClusters").mockReturnValue([
      { id: "cluster-1", name: "Test Cluster", url: "http://localhost:8080" },
    ])
    vi.spyOn(clusterConfig, "getClusterConfig").mockReturnValue({
      id: "cluster-1",
      name: "Test Cluster",
      url: "http://localhost:8080",
    })

    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ apiKey: "agbx_somekey" }),
    })
    const res = await POST(req)
    expect(res.status).toBe(200)
    const body = await res.json()
    expect(body.clusterID).toBe("cluster-1")
    expect(typeof body.token).toBe("string")
  })

  it("returns 503 when no clusters are configured", async () => {
    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "listClusters").mockReturnValue([])

    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ apiKey: "agbx_somekey" }),
    })
    const res = await POST(req)
    expect(res.status).toBe(503)
  })

  it("returns 401 when backend ping fails", async () => {
    mockUndici([{ statusCode: 401, body: { error: "invalid api key" } }])

    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ apiKey: "agbx_badkey", clusterID: "cluster-1" }),
    })
    const res = await POST(req)
    // clusterID "cluster-1" is not in clusters.yaml → 404 from getClusterConfig
    expect(res.status).toBe(404)
  })

  it("returns JWT with role=admin when whoami returns admin", async () => {
    mockUndici([{ statusCode: 200, body: { role: "admin", user: "", team: "" } }])

    // Mock the cluster config lookup so the route finds the cluster
    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "getClusterConfig").mockReturnValue({
      id: "cluster-1",
      name: "Test Cluster",
      url: "http://localhost:8080",
    })

    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ apiKey: "agbx_adminkey", clusterID: "cluster-1" }),
    })
    const res = await POST(req)
    expect(res.status).toBe(200)
    const body = await res.json()
    expect(body.role).toBe("admin")
    expect(typeof body.token).toBe("string")
    expect(body.token.length).toBeGreaterThan(0)
    expect(body.namespace).toBeUndefined()
  })

  it("returns JWT with role=tenant when whoami returns tenant", async () => {
    mockUndici([{ statusCode: 200, body: { role: "tenant", user: "", team: "" } }])

    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "getClusterConfig").mockReturnValue({
      id: "cluster-1",
      name: "Test Cluster",
      url: "http://localhost:8080",
    })

    const { POST } = await import("@/app/api/auth/login/route")
    const req = new Request("http://localhost/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ apiKey: "agbx_tenantkey", clusterID: "cluster-1" }),
    })
    const res = await POST(req)
    expect(res.status).toBe(200)
    const body = await res.json()
    expect(body.role).toBe("tenant")
    expect(typeof body.token).toBe("string")
    expect(body.namespace).toBeUndefined()
  })
})

// ─── BFF cluster-aware proxy /api/clusters/[clusterID]/[...path] ─────────────

describe("BFF cluster proxy", () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockUndiciRequest.mockReset()
  })

  it("returns 401 for requests without Authorization header", async () => {
    const { GET } = await import("@/app/api/clusters/[clusterID]/[...path]/route")
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const req = new Request("http://localhost/api/clusters/default/v1/sandboxes") as any
    const res = await GET(req, {
      params: Promise.resolve({ clusterID: "default", path: ["v1", "sandboxes"] }),
    })
    expect(res.status).toBe(401)
  })

  it("forwards request with AGENTBOX-API-KEY injected from JWT", async () => {
    // JWT without authMethod defaults to API key mode
    const token = await makeJWT({ apiKey: "agbx_realkey", role: "tenant" })

    let capturedHeaders: Record<string, string> | undefined
    mockUndiciRequest.mockImplementationOnce(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      async (_url: any, init?: any) => {
        capturedHeaders = init?.headers as Record<string, string> | undefined
        return makeFakeUndiciResponse({ statusCode: 200, body: { items: [] } })
      },
    )

    // Mock listClusters to return a cluster for the "default" fallback path
    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "listClusters").mockReturnValue([
      { id: "cluster-1", name: "Test Cluster", url: "http://localhost:8080" },
    ])

    const { GET } = await import("@/app/api/clusters/[clusterID]/[...path]/route")
    const req = new Request("http://localhost/api/clusters/default/v1/sandboxes", {
      headers: { Authorization: `Bearer ${token}` },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    }) as any

    const res = await GET(req, {
      params: Promise.resolve({ clusterID: "default", path: ["v1", "sandboxes"] }),
    })
    expect(res.status).toBe(200)
    expect(capturedHeaders?.["agentbox-api-key"] ?? capturedHeaders?.["AGENTBOX-API-KEY"]).toBe(
      "agbx_realkey",
    )
  })

  it("returns 401 for an invalid JWT in cluster proxy", async () => {
    const { GET } = await import("@/app/api/clusters/[clusterID]/[...path]/route")
    const req = new Request("http://localhost/api/clusters/default/v1/sandboxes", {
      headers: { Authorization: "Bearer invalidtoken" },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    }) as any
    const res = await GET(req, {
      params: Promise.resolve({ clusterID: "default", path: ["v1", "sandboxes"] }),
    })
    expect(res.status).toBe(401)
  })
})
