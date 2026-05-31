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

/**
 * Tests for lib/audit/ — writer abstraction, FileAuditWriter formatting,
 * and integration with the cluster-proxy / global-api-keys routes.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest"
import * as fs from "fs"
import { SignJWT } from "jose"

// ─── Helpers ─────────────────────────────────────────────────────────────────

const JWT_SECRET = "test-secret-32-bytes-xxxxxxxxxxx"
process.env.JWT_SECRET = JWT_SECRET

async function makeJWT(payload: Record<string, unknown>): Promise<string> {
  const secret = new TextEncoder().encode(JWT_SECRET)
  return new SignJWT(payload)
    .setProtectedHeader({ alg: "HS256" })
    .setIssuedAt()
    .setExpirationTime("7d")
    .sign(secret)
}

// ─── 1. Writer abstraction ─────────────────────────────────────────────────

describe("AuditWriter abstraction", () => {
  it("dispatches events to all registered writers", async () => {
    // Use a fresh dynamic import so the module-level `writers` array starts empty
    const { registerAuditWriter, writeAuditEvent } = await import("@/lib/audit/writer")

    const calls1: unknown[] = []
    const calls2: unknown[] = []
    registerAuditWriter({ write: (e) => calls1.push(e) })
    registerAuditWriter({ write: (e) => calls2.push(e) })

    const event = {
      timestamp: "2026-04-02T08:00:00.000Z",
      action: "api.create" as const,
      method: "POST",
      path: "/v1/envs/env-prod/sandboxpools",
      clusterID: "cluster-prod",
      statusCode: 201,
      actor: { role: "admin" as const, user: "alice", team: "team-ops", authMethod: "oidc" },
    }

    writeAuditEvent(event)

    expect(calls1).toHaveLength(1)
    expect(calls2).toHaveLength(1)
    expect(calls1[0]).toEqual(event)
  })

  it("catches writer errors and does not rethrow", async () => {
    const { registerAuditWriter, writeAuditEvent } = await import("@/lib/audit/writer")

    registerAuditWriter({
      write: () => {
        throw new Error("disk full")
      },
    })

    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {})
    expect(() =>
      writeAuditEvent({
        timestamp: "2026-04-02T08:00:00.000Z",
        action: "api.delete" as const,
        method: "DELETE",
        path: "/v1/envs/env-1/sandboxpools/x",
        statusCode: 200,
        actor: { role: "tenant" as const },
      }),
    ).not.toThrow()
    expect(consoleError).toHaveBeenCalledWith(expect.stringContaining("[audit]"), expect.anything())
    consoleError.mockRestore()
  })
})

// ─── 2. FileAuditWriter formatting ────────────────────────────────────────

describe("FileAuditWriter formatting", () => {
  const LOG_PATH = "/tmp/test-audit-vitest.log"

  beforeEach(() => {
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
  })

  afterEach(() => {
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
  })

  async function makeWriter() {
    const { FileAuditWriter } = await import("@/lib/audit/file-writer")
    return new FileAuditWriter(LOG_PATH)
  }

  function readLog(): string {
    return fs.existsSync(LOG_PATH) ? fs.readFileSync(LOG_PATH, "utf-8") : ""
  }

  it("writes a basic CREATE event to file", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T08:15:33.421Z",
      action: "api.create",
      method: "POST",
      path: "/v1/envs/env-prod/sandboxpools",
      clusterID: "cluster-prod",
      statusCode: 201,
      actor: {
        role: "admin",
        user: "alice",
        team: "team-ops",
        authMethod: "oidc",
        email: "alice@example.com",
      },
    })

    const log = readLog()
    expect(log).toContain("[CREATE]")
    expect(log).toContain("POST")
    expect(log).toContain("cluster-prod")
    expect(log).toContain("/v1/envs/env-prod/sandboxpools")
    expect(log).toContain("→ 201")
    expect(log).toContain("admin")
    expect(log).toContain("alice")
    expect(log).toContain("team-ops")
    expect(log).toContain("[oidc]")
    expect(log).toContain("<alice@example.com>")
    expect(log).not.toContain("impersonating")
  })

  it("includes impersonation line when impersonation is present", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T08:22:11.003Z",
      action: "api.delete",
      method: "DELETE",
      path: "/v1/envs/env-prod/sandboxpools/pool-abc",
      clusterID: "cluster-prod",
      statusCode: 200,
      actor: {
        role: "admin",
        user: "alice",
        team: "team-ops",
        authMethod: "oidc",
        email: "alice@example.com",
      },
      impersonation: { asUser: "bob", asTeam: "team-dev" },
    })

    const log = readLog()
    expect(log).toContain("[DELETE]")
    expect(log).toContain("impersonating")
    expect(log).toContain("bob")
    expect(log).toContain("team-dev")
  })

  it("omits email when not present", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T09:01:44.217Z",
      action: "apikey.create",
      method: "POST",
      path: "/api/global-api-keys",
      statusCode: 201,
      actor: { role: "tenant", user: "bob", team: "team-dev", authMethod: "apikey" },
    })

    const log = readLog()
    expect(log).toContain("bob")
    expect(log).not.toContain("<")
    expect(log).not.toContain(">")
  })

  it("shows '-' for clusterID when absent", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T09:05:00.000Z",
      action: "apikey.delete",
      method: "DELETE",
      path: "/api/global-api-keys/my-key",
      statusCode: 204,
      actor: { role: "admin", user: "alice", authMethod: "oidc" },
    })

    const log = readLog()
    // clusterID column should be '-'
    expect(log).toMatch(/\[DELETE\]\s+DELETE\s+-\s+/)
  })

  it("writes ERROR action for 4xx responses", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T09:10:00.000Z",
      action: "api.error",
      method: "POST",
      path: "/v1/sandboxes",
      clusterID: "cluster-dev",
      statusCode: 403,
      actor: { role: "tenant", user: "charlie", authMethod: "apikey" },
    })

    const log = readLog()
    expect(log).toContain("[ERROR")
    expect(log).toContain("→ 403")
  })

  it("appends multiple events sequentially", async () => {
    const w = await makeWriter()
    w.write({
      timestamp: "2026-04-02T10:00:00.000Z",
      action: "api.create",
      method: "POST",
      path: "/v1/envs/env-prod/sandboxpools",
      clusterID: "c1",
      statusCode: 201,
      actor: { role: "admin", user: "u1" },
    })
    w.write({
      timestamp: "2026-04-02T10:01:00.000Z",
      action: "api.delete",
      method: "DELETE",
      path: "/v1/envs/env-1/sandboxpools/p1",
      clusterID: "c1",
      statusCode: 200,
      actor: { role: "admin", user: "u1" },
    })

    const log = readLog()
    const createIdx = log.indexOf("[CREATE]")
    const deleteIdx = log.indexOf("[DELETE]")
    expect(createIdx).toBeGreaterThanOrEqual(0)
    expect(deleteIdx).toBeGreaterThan(createIdx)
  })

  it("does not throw when the log path is unwritable", async () => {
    const { FileAuditWriter } = await import("@/lib/audit/file-writer")
    const w = new FileAuditWriter("/no-such-dir/nope/audit.log")

    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {})
    expect(() =>
      w.write({
        timestamp: "2026-04-02T10:00:00.000Z",
        action: "api.create",
        method: "POST",
        path: "/v1/x",
        statusCode: 201,
        actor: { role: "tenant" },
      }),
    ).not.toThrow()
    expect(consoleError).toHaveBeenCalled()
    consoleError.mockRestore()
  })
})

// ─── 3. Cluster proxy route integration ───────────────────────────────────

vi.mock("undici", () => ({ request: vi.fn() }))

import { request as undiciRequest } from "undici"
const mockUndici = vi.mocked(undiciRequest)

function fakeUndiciResponse(statusCode: number, body: unknown = {}) {
  const bodyStr = JSON.stringify(body)
  return {
    statusCode,
    headers: { "content-type": "application/json" },
    body: {
      json: async () => JSON.parse(bodyStr),
      text: async () => bodyStr,
      arrayBuffer: async () => new TextEncoder().encode(bodyStr).buffer as ArrayBuffer,
      dump: async () => undefined,
    },
    trailers: {},
    opaque: null,
    context: {},
  } as unknown as Awaited<ReturnType<typeof undiciRequest>>
}

describe("Cluster proxy route — audit integration", () => {
  const LOG_PATH = "/tmp/test-audit-proxy-vitest.log"

  beforeEach(() => {
    process.env.AUDIT_LOG_PATH = LOG_PATH
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
    vi.resetModules()
    mockUndici.mockReset()
  })

  afterEach(() => {
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
    delete process.env.AUDIT_LOG_PATH
  })

  async function getClusterRoute() {
    const clusterConfig = await import("@/lib/cluster-config")
    vi.spyOn(clusterConfig, "listClusters").mockReturnValue([
      { id: "cluster-prod", name: "Prod Cluster", url: "http://localhost:8080" },
    ])
    vi.spyOn(clusterConfig, "getClusterConfig").mockReturnValue({
      id: "cluster-prod",
      name: "Prod Cluster",
      url: "http://localhost:8080",
    })
    return import("@/app/api/clusters/[clusterID]/[...path]/route")
  }

  function readLog(): string {
    return fs.existsSync(LOG_PATH) ? fs.readFileSync(LOG_PATH, "utf-8") : ""
  }

  it("writes audit log for POST (api.create)", async () => {
    const token = await makeJWT({
      apiKey: "agbx_k",
      role: "admin",
      user: "alice",
      team: "t1",
      authMethod: "apikey",
    })
    mockUndici.mockResolvedValueOnce(fakeUndiciResponse(201, { name: "new-pool" }))

    const { POST } = await getClusterRoute()
    // Pass signal directly in the Request constructor — Object.assign cannot
    // override the read-only signal property of the native Request class.
    const req = new Request(
      "http://localhost/api/clusters/cluster-prod/v1/envs/env-prod/sandboxpools",
      {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ spec: {} }),
        signal: new AbortController().signal,
      },
    )
    const res = await POST(req as never, {
      params: Promise.resolve({
        clusterID: "cluster-prod",
        path: ["v1", "envs", "env-prod", "sandboxpools"],
      }),
    })
    expect(res.status).toBe(201)

    const log = readLog()
    expect(log).toContain("[CREATE]")
    expect(log).toContain("POST")
    expect(log).toContain("cluster-prod")
    expect(log).toContain("/v1/envs/env-prod/sandboxpools")
    expect(log).toContain("→ 201")
    expect(log).toContain("alice")
    expect(log).toContain("t1")
  })

  it("writes audit log for DELETE (api.delete)", async () => {
    const token = await makeJWT({
      apiKey: "agbx_k",
      role: "admin",
      user: "alice",
      team: "t1",
      authMethod: "apikey",
    })
    mockUndici.mockResolvedValueOnce(fakeUndiciResponse(200, {}))

    const { DELETE } = await getClusterRoute()
    const req = new Request(
      "http://localhost/api/clusters/cluster-prod/v1/envs/env-prod/sandboxpools/pool-abc",
      {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
        signal: new AbortController().signal,
      },
    )
    await DELETE(req as never, {
      params: Promise.resolve({
        clusterID: "cluster-prod",
        path: ["v1", "envs", "env-prod", "sandboxpools", "pool-abc"],
      }),
    })

    const log = readLog()
    expect(log).toContain("[DELETE]")
    expect(log).toContain("/v1/envs/env-prod/sandboxpools/pool-abc")
  })

  it("writes api.error for 4xx backend response", async () => {
    const token = await makeJWT({
      apiKey: "agbx_k",
      role: "tenant",
      user: "bob",
      team: "t2",
      authMethod: "apikey",
    })
    mockUndici.mockResolvedValueOnce(fakeUndiciResponse(403, { error: "forbidden" }))

    const { POST } = await getClusterRoute()
    const req = new Request("http://localhost/api/clusters/cluster-prod/v1/sandboxes", {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({}),
      signal: new AbortController().signal,
    })
    await POST(req as never, {
      params: Promise.resolve({ clusterID: "cluster-prod", path: ["v1", "sandboxes"] }),
    })

    const log = readLog()
    expect(log).toContain("[ERROR")
    expect(log).toContain("→ 403")
  })

  it("records impersonation when X-Impersonate headers are present", async () => {
    const token = await makeJWT({
      role: "admin",
      user: "alice",
      team: "team-ops",
      authMethod: "oidc",
    })
    mockUndici.mockResolvedValueOnce(fakeUndiciResponse(201, { name: "sb1" }))

    const { POST } = await getClusterRoute()
    const req = new Request("http://localhost/api/clusters/cluster-prod/v1/sandboxes", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "X-Impersonate-Team": "team-dev",
        "X-Impersonate-User": "bob",
      },
      body: JSON.stringify({}),
      signal: new AbortController().signal,
    })
    await POST(req as never, {
      params: Promise.resolve({ clusterID: "cluster-prod", path: ["v1", "sandboxes"] }),
    })

    const log = readLog()
    expect(log).toContain("alice")
    expect(log).toContain("impersonating")
    expect(log).toContain("bob")
    expect(log).toContain("team-dev")
  })

  it("does NOT write audit log for GET requests", async () => {
    const token = await makeJWT({
      apiKey: "agbx_k",
      role: "tenant",
      user: "bob",
      authMethod: "apikey",
    })
    mockUndici.mockResolvedValueOnce(fakeUndiciResponse(200, { items: [] }))

    const { GET } = await getClusterRoute()
    const req = new Request(
      "http://localhost/api/clusters/cluster-prod/v1/envs/env-prod/sandboxpools",
      {
        headers: { Authorization: `Bearer ${token}` },
        signal: new AbortController().signal,
      },
    )
    await GET(req as never, {
      params: Promise.resolve({
        clusterID: "cluster-prod",
        path: ["v1", "envs", "env-prod", "sandboxpools"],
      }),
    })

    const log = readLog()
    expect(log).toBe("")
  })
})

// ─── 4. Hub proxy route integration (API keys) ──────────────────────────────

describe("Hub proxy routes — API keys audit integration", () => {
  const LOG_PATH = "/tmp/test-audit-apikeys-vitest.log"

  beforeEach(() => {
    process.env.AUDIT_LOG_PATH = LOG_PATH
    process.env.WSPROXY_INTERNAL_URL = "http://localhost:9004"
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
    vi.resetModules()
  })

  afterEach(() => {
    try {
      fs.unlinkSync(LOG_PATH)
    } catch {
      /* no-op */
    }
    delete process.env.AUDIT_LOG_PATH
  })

  function readLog(): string {
    return fs.existsSync(LOG_PATH) ? fs.readFileSync(LOG_PATH, "utf-8") : ""
  }

  it("writes api.create on successful POST /api/hub/v1/api-keys", async () => {
    const token = await makeJWT({
      role: "admin",
      user: "alice",
      team: "team-ops",
      authMethod: "oidc",
    })

    // Mock global fetch for wsproxy call
    const mockFetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ apiKey: "agbx_newkey", keyId: "agbx_ne" }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    )
    vi.stubGlobal("fetch", mockFetch)

    const { POST } = await import("@/app/api/hub/[...path]/route")
    const req = new Request("http://localhost/api/hub/v1/api-keys", {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({ description: "my key" }),
    })
    const res = await POST(req as never, {
      params: Promise.resolve({ path: ["v1", "api-keys"] }),
    })
    expect(res.status).toBe(201)

    const log = readLog()
    expect(log).toContain("[CREATE]")
    expect(log).toContain("/v1/api-keys")
    expect(log).toContain("→ 201")
    expect(log).toContain("alice")
    expect(log).toContain("team-ops")

    vi.unstubAllGlobals()
  })

  it("writes api.delete on successful DELETE /api/hub/v1/api-keys/{name}", async () => {
    const token = await makeJWT({
      role: "admin",
      user: "alice",
      team: "team-ops",
      authMethod: "oidc",
    })

    const mockFetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal("fetch", mockFetch)

    const { DELETE } = await import("@/app/api/hub/[...path]/route")
    const req = new Request("http://localhost/api/hub/v1/api-keys/agbx_mykey", {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
    })
    const res = await DELETE(req as never, {
      params: Promise.resolve({ path: ["v1", "api-keys", "agbx_mykey"] }),
    })
    expect(res.status).toBe(204)

    const log = readLog()
    expect(log).toContain("[DELETE]")
    expect(log).toContain("/v1/api-keys/agbx_mykey")
    expect(log).toContain("→ 204")
    expect(log).toContain("alice")

    vi.unstubAllGlobals()
  })

  it("writes api.error on failed DELETE /api/hub/v1/api-keys/{name}", async () => {
    const token = await makeJWT({ role: "admin", user: "alice", authMethod: "oidc" })

    const mockFetch = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ error: "not found" }), { status: 404 }))
    vi.stubGlobal("fetch", mockFetch)

    const { DELETE } = await import("@/app/api/hub/[...path]/route")
    const req = new Request("http://localhost/api/hub/v1/api-keys/no-such-key", {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
    })
    const res = await DELETE(req as never, {
      params: Promise.resolve({ path: ["v1", "api-keys", "no-such-key"] }),
    })
    expect(res.status).toBe(404)

    const log = readLog()
    expect(log).toContain("[ERROR ]")
    expect(log).toContain("/v1/api-keys/no-such-key")
    expect(log).toContain("→ 404")

    vi.unstubAllGlobals()
  })
})
