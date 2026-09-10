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

// A stored layout is read back straight into a layout engine, so a value that
// is merely *shaped* wrong has to be rejected here rather than there: handed a
// bad layout the group does not fall back to its defaults, it misbehaves.

import { describe, expect, it } from "vitest"
import { readLayout } from "@/hooks/use-persisted-layout"

describe("readLayout", () => {
  it("reads back what was written", () => {
    expect(readLayout(JSON.stringify({ page: 68, assistant: 32 }))).toEqual({
      page: 68,
      assistant: 32,
    })
  })

  it.each([
    ["nothing stored", null],
    ["not JSON", "{oops"],
    ["an array", "[68,32]"],
    ["null", "null"],
    ["a string size", JSON.stringify({ page: "68%" })],
    ["a NaN size", '{"page":null}'],
    ["Infinity, which JSON.stringify writes as null", '{"page":1e999}'],
  ])("refuses %s", (_why, raw) => {
    expect(readLayout(raw)).toBeNull()
  })
})
