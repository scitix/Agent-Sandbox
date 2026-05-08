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
 * Audit writer abstraction.
 *
 * Any number of AuditWriter implementations can be registered; every call to
 * writeAuditEvent dispatches to all of them in registration order.
 *
 * Writers are synchronous by design — the BFF uses fire-and-forget semantics
 * so audit I/O never blocks request handlers.  Failures are swallowed and
 * printed to stderr so the application continues operating.
 */

import type { AuditEvent } from "./types"

export type { AuditEvent }

export interface AuditWriter {
  write(event: AuditEvent): void
}

const writers: AuditWriter[] = []

/**
 * Register a new audit writer.  Call this once during application startup.
 * Calling it multiple times with the same writer instance will result in
 * duplicate log entries.
 */
export function registerAuditWriter(w: AuditWriter): void {
  writers.push(w)
}

/**
 * Dispatch an audit event to all registered writers.
 *
 * This function is intentionally synchronous and never throws.  Any error
 * produced by a writer is caught and printed to stderr.
 */
export function writeAuditEvent(event: AuditEvent): void {
  for (const w of writers) {
    try {
      w.write(event)
    } catch (e) {
      console.error("[audit] writer error:", e)
    }
  }
}
