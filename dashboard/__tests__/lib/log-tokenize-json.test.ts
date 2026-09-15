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

import { describe, expect, it } from "vitest"
import { tokenizeLogLine } from "@/components/logs/log-parse"

// The line shape envd actually emits, which is what this exists for.
const ENVD = `{"level":"debug","logger":"process","method":"POST /process.Process/Start","operation_id":"36","request":{"process":{"cmd":"/bin/bash","args":["-l","-c","echo hi"],"cwd":"/home/agents"},"stdin":false},"timestamp":"2026-09-14T12:42:23.828176581Z","message":"Process start (server stream start)"}`

describe("tokenizeLogLine on JSON", () => {
  it("colours top-level keys and values", () => {
    const toks = tokenizeLogLine(ENVD)
    const keys = toks.filter((t) => t.kind === "key").map((t) => ENVD.slice(t.start, t.end))
    expect(keys).toContain('"level"')
    expect(keys).toContain('"message"')
    // A key's text also appears inside a value; the walk must not mislocate it.
    expect(keys.filter((k) => k === '"message"')).toHaveLength(1)
  })

  it("keeps every token inside the line and in order", () => {
    const toks = tokenizeLogLine(ENVD)
    let prev = -1
    for (const t of toks) {
      expect(t.start).toBeGreaterThanOrEqual(prev)
      expect(t.end).toBeLessThanOrEqual(ENVD.length)
      expect(t.end).toBeGreaterThan(t.start)
      prev = t.start
    }
  })

  it("marks an error level so the row reads as one", () => {
    const line = `{"level":"error","msg":"boom"}`
    const toks = tokenizeLogLine(line)
    expect(toks.some((t) => t.kind === "errorValue")).toBe(true)
  })

  // The egress sidecar writes logfmt, not JSON — the other branch must still
  // handle it, or half the containers in a sandbox Pod lose their colouring.
  it("leaves a non-JSON line to the other branches", () => {
    const toks = tokenizeLogLine(`level=INFO msg="egress denied" host=169.254.169.254`)
    expect(toks.filter((t) => t.kind === "key")).not.toHaveLength(0)
  })

  it("does not throw on a truncated object", () => {
    expect(() => tokenizeLogLine('{"level":"info","msg":"cut off')).not.toThrow()
  })
})
