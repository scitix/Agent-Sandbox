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
 * One-line access log for every BFF proxy request — the Gin `[GIN]` line's
 * counterpart for the Next.js side.
 *
 * Why this exists and not just the audit log: `lib/audit` records *mutations*
 * for compliance and deliberately skips GETs, and it records the caller and the
 * backend path but not the URL that was actually dialled. When a proxy answers
 * 404 there is no way to tell from the logs whether the dashboard rejected the
 * cluster id, whether the upstream ingress had no matching rule, or which URL
 * was tried — the pod prints nothing at all. That has cost real debugging time
 * (a cluster entry whose `url` was missing the ingress path prefix looks
 * identical, from the browser, to a broken route). So: log every proxied
 * request, with the resolved upstream target, on one line.
 *
 * Output goes to stdout (stderr for 5xx) so `kubectl logs` collects it.
 *
 * Env:
 *   BFF_ACCESS_LOG=text|json|off   default "text"
 */

import type { NextRequest, NextResponse } from "next/server"

export type AccessLogFormat = "text" | "json" | "off"

/** Who made the call, as far as the proxy could tell. Absent before auth runs. */
export interface AccessLogActor {
  user?: string
  team?: string
  role?: string
  authMethod?: string
  /** Set when the request carried X-Impersonate-User / -Team. */
  asUser?: string
  asTeam?: string
}

/**
 * Mutable per-request context. The handler fills in whatever it has learned by
 * the time it returns; every field is optional so an early return (401, unknown
 * cluster) still produces a useful line.
 */
export interface AccessLogContext {
  /** Which proxy emitted the line: "cluster" | "e2b" | "hub" | "agent" | … */
  scope: string
  /** Cluster id from the URL, for cluster-scoped routes. */
  cluster?: string
  /** Full upstream URL actually dialled, query included. */
  upstream?: string
  /** Host header sent upstream, when it overrides the URL's own host. */
  upstreamHost?: string
  actor?: AccessLogActor
  /** Short free-text reason, e.g. "cluster not found" or an exception message. */
  note?: string
}

interface AccessLogLine {
  time: string
  status: number
  durationMs: number
  scope: string
  method: string
  path: string
  cluster?: string
  upstream?: string
  upstreamHost?: string
  clientIP?: string
  user?: string
  team?: string
  role?: string
  auth?: string
  asUser?: string
  asTeam?: string
  note?: string
}

/** Query params whose values never belong in a log line. */
const SENSITIVE_QUERY_KEYS = /^(token|api[-_]?key|key|secret|password|signature|access[-_]?token)$/i

function format(): AccessLogFormat {
  const raw = (process.env.BFF_ACCESS_LOG ?? "text").toLowerCase()
  return raw === "json" || raw === "off" ? raw : "text"
}

/**
 * Path + query with sensitive values masked. Relative form only — the public
 * origin adds nothing and varies with the ingress.
 */
export function redactUrl(rawUrl: string): string {
  let parsed: URL
  try {
    parsed = new URL(rawUrl)
  } catch {
    return rawUrl
  }
  for (const key of [...parsed.searchParams.keys()]) {
    if (SENSITIVE_QUERY_KEYS.test(key)) parsed.searchParams.set(key, "REDACTED")
  }
  const qs = parsed.searchParams.toString()
  return `${parsed.pathname}${qs ? `?${qs}` : ""}`
}

/** Same masking, but keeping the origin — used for the upstream target. */
export function redactAbsoluteUrl(rawUrl: string): string {
  let parsed: URL
  try {
    parsed = new URL(rawUrl)
  } catch {
    return rawUrl
  }
  for (const key of [...parsed.searchParams.keys()]) {
    if (SENSITIVE_QUERY_KEYS.test(key)) parsed.searchParams.set(key, "REDACTED")
  }
  return parsed.toString()
}

/** First hop of X-Forwarded-For, else X-Real-IP. */
function clientIP(request: NextRequest): string | undefined {
  const fwd = request.headers.get("x-forwarded-for")
  if (fwd) {
    const first = fwd.split(",")[0]?.trim()
    if (first) return first
  }
  return request.headers.get("x-real-ip") ?? undefined
}

/** Renders the Gin-ish single line. Exported for tests. */
export function renderTextLine(l: AccessLogLine): string {
  const parts = [
    "[BFF]",
    String(l.status),
    "|",
    `${l.durationMs.toFixed(1).padStart(8)}ms`,
    "|",
    (l.clientIP ?? "-").padEnd(15),
    "|",
    l.method.padEnd(6),
    l.path,
  ]

  const target = l.cluster ? `${l.cluster} ${l.upstream ?? "-"}` : (l.upstream ?? "")
  if (target) parts.push("->", target)
  if (l.upstreamHost) parts.push(`(Host: ${l.upstreamHost})`)

  const who: string[] = []
  if (l.user) who.push(`user=${l.user}`)
  if (l.team) who.push(`team=${l.team}`)
  if (l.role) who.push(`role=${l.role}`)
  if (l.auth) who.push(`auth=${l.auth}`)
  if (l.asUser) who.push(`as=${l.asTeam ?? "-"}/${l.asUser}`)
  if (who.length > 0) parts.push("|", who.join(" "))

  parts.push("|", `scope=${l.scope}`)
  if (l.note) parts.push("|", l.note)

  return parts.join(" ")
}

/** Builds the structured line. Exported for tests. */
export function buildLine(
  request: NextRequest,
  ctx: AccessLogContext,
  status: number,
  durationMs: number,
): AccessLogLine {
  return {
    time: new Date().toISOString(),
    status,
    durationMs,
    scope: ctx.scope,
    method: request.method,
    path: redactUrl(request.url),
    ...(ctx.cluster ? { cluster: ctx.cluster } : {}),
    ...(ctx.upstream ? { upstream: redactAbsoluteUrl(ctx.upstream) } : {}),
    ...(ctx.upstreamHost ? { upstreamHost: ctx.upstreamHost } : {}),
    ...(clientIP(request) ? { clientIP: clientIP(request) } : {}),
    ...(ctx.actor?.user ? { user: ctx.actor.user } : {}),
    ...(ctx.actor?.team ? { team: ctx.actor.team } : {}),
    ...(ctx.actor?.role ? { role: ctx.actor.role } : {}),
    ...(ctx.actor?.authMethod ? { auth: ctx.actor.authMethod } : {}),
    ...(ctx.actor?.asUser ? { asUser: ctx.actor.asUser } : {}),
    ...(ctx.actor?.asTeam ? { asTeam: ctx.actor.asTeam } : {}),
    ...(ctx.note ? { note: ctx.note } : {}),
  }
}

function emit(line: AccessLogLine): void {
  const mode = format()
  if (mode === "off") return
  const text =
    mode === "json" ? JSON.stringify({ msg: "bff.access", ...line }) : renderTextLine(line)
  // 5xx to stderr so it stands out in `kubectl logs`; everything else to stdout.
  if (line.status >= 500) console.error(text)
  else console.log(text)
}

/**
 * Wraps a proxy handler so exactly one access line is written per request,
 * whatever path the handler takes out (early 401, upstream response, throw).
 *
 * The handler receives a mutable context to annotate as it resolves the cluster
 * and the upstream URL:
 *
 *   return withAccessLog(request, "cluster", async (log) => {
 *     log.cluster = clusterID
 *     …
 *     log.upstream = targetUrl
 *     return new NextResponse(...)
 *   })
 */
export async function withAccessLog(
  request: NextRequest,
  scope: string,
  handler: (log: AccessLogContext) => Promise<NextResponse>,
): Promise<NextResponse> {
  const ctx: AccessLogContext = { scope }
  const started = performance.now()
  try {
    const res = await handler(ctx)
    emit(buildLine(request, ctx, res.status, performance.now() - started))
    return res
  } catch (err) {
    ctx.note = err instanceof Error ? `${err.name}: ${err.message}` : String(err)
    emit(buildLine(request, ctx, 500, performance.now() - started))
    throw err
  }
}

/** Reads the impersonation headers into the shape the log line wants. */
export function impersonationFromHeaders(
  request: NextRequest,
): Pick<AccessLogActor, "asUser" | "asTeam"> {
  const asUser = request.headers.get("X-Impersonate-User")
  const asTeam = request.headers.get("X-Impersonate-Team")
  return {
    ...(asUser ? { asUser } : {}),
    ...(asTeam ? { asTeam } : {}),
  }
}
