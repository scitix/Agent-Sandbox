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
import type { NextRequest } from "next/server"
import {
  buildLine,
  redactUrl,
  redactAbsoluteUrl,
  renderTextLine,
  impersonationFromHeaders,
} from "@/lib/server/access-log"

/** Minimal NextRequest stand-in — the logger only reads url, method and headers. */
function req(url: string, method = "GET", headers: Record<string, string> = {}): NextRequest {
  return { url, method, headers: new Headers(headers) } as unknown as NextRequest
}

describe("redactUrl", () => {
  it("keeps path and query, drops the origin", () => {
    expect(redactUrl("https://console.example.com/api/clusters/c1/v1/envs?limit=20")).toBe(
      "/api/clusters/c1/v1/envs?limit=20",
    )
  })

  it("masks credential-bearing query values", () => {
    expect(redactUrl("https://x/y?token=abc&apiKey=def&limit=1")).toBe(
      "/y?token=REDACTED&apiKey=REDACTED&limit=1",
    )
  })

  it("passes non-URLs through untouched", () => {
    expect(redactUrl("not a url")).toBe("not a url")
  })
})

describe("redactAbsoluteUrl", () => {
  it("keeps the origin so the dialled target is identifiable", () => {
    expect(redactAbsoluteUrl("http://10.0.0.1:30080/v1/envs?secret=s")).toBe(
      "http://10.0.0.1:30080/v1/envs?secret=REDACTED",
    )
  })
})

describe("impersonationFromHeaders", () => {
  it("is empty without the headers", () => {
    expect(impersonationFromHeaders(req("http://x/y"))).toEqual({})
  })

  it("picks up both headers", () => {
    const r = req("http://x/y", "GET", {
      "X-Impersonate-User": "bob",
      "X-Impersonate-Team": "org1",
    })
    expect(impersonationFromHeaders(r)).toEqual({ asUser: "bob", asTeam: "org1" })
  })
})

describe("buildLine", () => {
  it("records the upstream target that was actually dialled", () => {
    const r = req("https://console/agentbox/api/clusters/cluster-a/v1/envs", "GET", {
      "x-forwarded-for": "10.1.2.3, 10.4.5.6",
    })
    const line = buildLine(
      r,
      {
        scope: "cluster",
        cluster: "cluster-a",
        upstream: "http://10.0.0.1:30080/v1/envs",
        upstreamHost: "agentbox.example.internal",
        actor: { user: "alice", team: "k8s", role: "admin", authMethod: "oidc" },
      },
      404,
      12.5,
    )

    expect(line).toMatchObject({
      status: 404,
      method: "GET",
      path: "/agentbox/api/clusters/cluster-a/v1/envs",
      cluster: "cluster-a",
      upstream: "http://10.0.0.1:30080/v1/envs",
      upstreamHost: "agentbox.example.internal",
      clientIP: "10.1.2.3",
      user: "alice",
      role: "admin",
      auth: "oidc",
    })
  })

  it("omits fields the handler never learned", () => {
    const line = buildLine(req("http://x/api/hub/v1/whoami"), { scope: "hub" }, 401, 0.4)
    expect(line.user).toBeUndefined()
    expect(line.cluster).toBeUndefined()
    expect(line.upstream).toBeUndefined()
  })
})

describe("renderTextLine", () => {
  it("puts status, latency, caller, route and upstream on one line", () => {
    const text = renderTextLine({
      time: "2026-08-28T07:26:31.000Z",
      status: 404,
      durationMs: 12.5,
      scope: "cluster",
      method: "GET",
      path: "/api/clusters/cluster-a/v1/envs",
      cluster: "cluster-a",
      upstream: "http://10.0.0.1:30080/v1/envs",
      upstreamHost: "agentbox.example.internal",
      clientIP: "10.1.2.3",
      user: "alice",
      team: "k8s",
      role: "admin",
      auth: "oidc",
    })

    expect(text.split("\n")).toHaveLength(1)
    expect(text).toContain("[BFF] 404")
    expect(text).toContain("12.5ms")
    expect(text).toContain("GET    /api/clusters/cluster-a/v1/envs")
    expect(text).toContain("-> cluster-a http://10.0.0.1:30080/v1/envs")
    expect(text).toContain("(Host: agentbox.example.internal)")
    expect(text).toContain("user=alice team=k8s role=admin auth=oidc")
    expect(text).toContain("scope=cluster")
  })

  it("renders an unauthenticated early return without a caller section", () => {
    const text = renderTextLine({
      time: "2026-08-28T07:26:31.000Z",
      status: 401,
      durationMs: 0.4,
      scope: "hub",
      method: "GET",
      path: "/api/hub/v1/platform/users/count",
    })
    expect(text).toContain("[BFF] 401")
    expect(text).not.toContain("user=")
  })
})
