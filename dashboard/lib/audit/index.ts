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
 * Audit module entry point.
 *
 * Call `initAudit()` once before the first `writeAuditEvent()` call to register
 * the default FileAuditWriter.  Subsequent calls are no-ops (singleton guard).
 *
 * The log path defaults to /tmp/agentbox-dashboard.log and can be overridden
 * with the AUDIT_LOG_PATH environment variable.
 *
 * Usage:
 *   import { initAudit, writeAuditEvent } from "@/lib/audit"
 *
 *   initAudit()
 *   writeAuditEvent({ ... })
 *
 * To add a custom backend (e.g. database), import registerAuditWriter directly:
 *   import { registerAuditWriter } from "@/lib/audit/writer"
 *   registerAuditWriter(new MyDbWriter())
 */

import { registerAuditWriter } from "./writer"
import { FileAuditWriter } from "./file-writer"

const DEFAULT_LOG_PATH = "/tmp/agentbox-dashboard.log"

let initialized = false

/**
 * Initialize the audit system with the default FileAuditWriter.
 * Safe to call multiple times — only the first call has any effect.
 */
export function initAudit(): void {
  if (initialized) return
  initialized = true
  const logPath = process.env.AUDIT_LOG_PATH ?? DEFAULT_LOG_PATH
  registerAuditWriter(new FileAuditWriter(logPath))
}

export { writeAuditEvent, registerAuditWriter } from "./writer"
export type { AuditEvent, AuditWriter } from "./writer"
export type { AuditAction, AuditActor, AuditImpersonation } from "./types"
