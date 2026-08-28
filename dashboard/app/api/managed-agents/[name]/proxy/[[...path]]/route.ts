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

// Agent conversation proxy: /api/managed-agents/<name>/proxy/<path>
//   → WSPROXY_INTERNAL_URL/internal/managedagents/<name>/proxy/<path>
//   → the agent's Brain (its gateway, or its workspace-fs server under /_fs)
//
// THIS ROUTE STREAMS. It exists as its own file rather than as more cases in the
// CRUD proxy next door because that one reads the whole upstream response into an
// ArrayBuffer before answering — correct for a JSON object, fatal here. An agent
// turn is a Server-Sent Events stream that stays open for as long as the agent
// works, so buffering it shows the user nothing until the turn ends and then
// everything at once. For a turn that runs minutes, that is indistinguishable from
// a hang, and the "stop" button has nothing to stop.
//
// So the body is piped through untouched, and the two headers that make a proxied
// SSE stream behave are set explicitly. Everything else about the agent's surface
// stays whatever the Brain serves: this layer authenticates and forwards, and
// deliberately knows nothing about threads, runs or AG-UI.

import { NextResponse, type NextRequest } from "next/server"
import { requireAuth } from "@/lib/server/bff-auth"
import { impersonationFromHeaders, withAccessLog } from "@/lib/server/access-log"
import type { AccessLogContext } from "@/lib/server/access-log"

const WSPROXY_INTERNAL_URL = process.env.WSPROXY_INTERNAL_URL ?? "http://localhost:9004"

type RouteContext = { params: Promise<{ name: string; path?: string[] }> }

// Node, not edge: the stream has no deadline of its own and an agent turn can run
// far past an edge function's budget.
export const runtime = "nodejs"
// A conversation is per-user and per-moment. Caching any of it would serve one
// user's transcript to the next.
export const dynamic = "force-dynamic"
export const fetchCache = "force-no-store"

function proxy(request: NextRequest, ctx: RouteContext) {
  return withAccessLog(request, "agent-proxy", (log) => doProxy(request, ctx, log))
}

async function doProxy(request: NextRequest, ctx: RouteContext, log: AccessLogContext) {
  const { name, path } = await ctx.params

  const authResult = await requireAuth(request.headers.get("Authorization"))
  if ("error" in authResult) return authResult.error
  const { payload } = authResult
  log.actor = {
    user: payload.user,
    team: payload.team,
    role: payload.role,
    authMethod: payload.authMethod,
    ...impersonationFromHeaders(request),
  }

  const suffix = (path ?? []).map(encodeURIComponent).join("/")
  const search = new URL(request.url).searchParams.toString()
  const target =
    `${WSPROXY_INTERNAL_URL}/internal/managedagents/${encodeURIComponent(name)}/proxy` +
    `${suffix ? `/${suffix}` : ""}${search ? `?${search}` : ""}`
  log.upstream = target

  const headers = new Headers()
  const auth = request.headers.get("Authorization")
  if (auth) headers.set("Authorization", auth)
  for (const h of ["Content-Type", "Accept", "Last-Event-ID"]) {
    const v = request.headers.get(h)
    if (v) headers.set(h, v)
  }

  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: request.method,
      headers,
      body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
      // Required by undici whenever a request carries a streaming body, which an
      // attachment upload does.
      ...({ duplex: "half" } as RequestInit),
      // The turn decides when it is over, not us. An AbortSignal here would cut a
      // long turn off mid-answer with no way to tell that from a crash.
      signal: request.signal,
    })
  } catch (e) {
    // A client that navigated away aborts the request; that is not an error worth
    // logging as one, and there is nobody left to answer.
    if (request.signal.aborted) return new NextResponse(null, { status: 499 })
    console.error("[agent proxy] hub unavailable:", e)
    log.note = "hub unavailable"
    return NextResponse.json({ error: "Hub unavailable" }, { status: 503 })
  }

  const responseHeaders = new Headers()
  for (const h of ["Content-Type", "Cache-Control", "Content-Disposition"]) {
    const v = upstream.headers.get(h)
    if (v) responseHeaders.set(h, v)
  }
  if (upstream.headers.get("Content-Type")?.includes("text/event-stream")) {
    // Two hops between here and the browser buffer by default, and each one is
    // enough to hold the whole turn: nginx-family proxies until told otherwise,
    // and any cache that thinks a 200 text/* is storable.
    responseHeaders.set("Cache-Control", "no-cache, no-transform")
    responseHeaders.set("X-Accel-Buffering", "no")
  }

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: responseHeaders,
  })
}

export const GET = (req: NextRequest, ctx: RouteContext) => proxy(req, ctx)
export const POST = (req: NextRequest, ctx: RouteContext) => proxy(req, ctx)
export const PUT = (req: NextRequest, ctx: RouteContext) => proxy(req, ctx)
export const PATCH = (req: NextRequest, ctx: RouteContext) => proxy(req, ctx)
export const DELETE = (req: NextRequest, ctx: RouteContext) => proxy(req, ctx)
