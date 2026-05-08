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
 * FileAuditWriter — appends human-readable audit records to a local file.
 *
 * Log format (two lines per record, separated by a blank line):
 *
 *   2026-04-02T08:15:33.421Z  [CREATE]  POST    cluster-prod  /v1/sandboxpools               → 201
 *     actor:        admin  alice  @  team-ops  [oidc]  <alice@example.com>
 *
 *   2026-04-02T08:22:11.003Z  [DELETE]  DELETE  cluster-prod  /v1/sandboxpools/pool-abc       → 200
 *     actor:        admin  alice  @  team-ops  [oidc]  <alice@example.com>
 *     impersonating: bob @ team-dev
 *
 *   2026-04-02T09:01:44.217Z  [CREATE]  POST    -             /api/global-api-keys            → 201
 *     actor:        tenant  bob  @  team-dev  [apikey]
 *
 * Rules:
 *   - action label: [CREATE] / [UPDATE] / [DELETE] / [ERROR]
 *   - clusterID "-" when absent (e.g. global-api-keys routes)
 *   - email portion omitted when empty
 *   - impersonating line omitted when no impersonation is active
 *   - fs.appendFileSync used for ordered, dependency-free writes
 *   - write errors are caught and printed to stderr; they never throw
 */

import * as fs from "fs"
import type { AuditWriter, AuditEvent } from "./writer"

// Map action → short display label
const ACTION_LABEL: Record<string, string> = {
  "api.create": "CREATE",
  "api.update": "UPDATE",
  "api.delete": "DELETE",
  "api.error": "ERROR ",
  "apikey.create": "CREATE",
  "apikey.delete": "DELETE",
}

function formatEvent(event: AuditEvent): string {
  const label = ACTION_LABEL[event.action] ?? event.action.toUpperCase().padEnd(6)
  const cluster = event.clusterID ?? "-"
  const statusArrow = `→ ${event.statusCode}`

  // ── line 1: summary ───────────────────────────────────────────────────────
  const line1 = `${event.timestamp}  [${label}]  ${event.method.padEnd(6)}  ${cluster.padEnd(14)}  ${event.path.padEnd(40)}  ${statusArrow}`

  // ── line 2: actor ─────────────────────────────────────────────────────────
  const { actor } = event
  const userAt = actor.user ? `${actor.user}` : "(unknown)"
  const teamPart = actor.team ? ` @ ${actor.team}` : ""
  const authPart = actor.authMethod ? `  [${actor.authMethod}]` : ""
  const emailPart = actor.email ? `  <${actor.email}>` : ""
  const namePart = actor.name && actor.name !== actor.user ? `  (${actor.name})` : ""
  const line2 = `  actor:        ${actor.role}  ${userAt}${teamPart}${authPart}${namePart}${emailPart}`

  // ── optional line 3: impersonation ────────────────────────────────────────
  let line3 = ""
  if (event.impersonation) {
    line3 = `\n  impersonating: ${event.impersonation.asUser} @ ${event.impersonation.asTeam}`
  }

  return `${line1}\n${line2}${line3}\n`
}

export class FileAuditWriter implements AuditWriter {
  private readonly logPath: string

  constructor(logPath: string) {
    this.logPath = logPath
  }

  write(event: AuditEvent): void {
    try {
      const line = formatEvent(event)
      fs.appendFileSync(this.logPath, line, "utf-8")
    } catch (e) {
      console.error(`[audit] failed to write to ${this.logPath}:`, e)
    }
  }
}
