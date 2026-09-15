import { describe, expect, it } from "vitest"
import { buildLineIndex, findLineByKey, logLineKey } from "@/components/logs/log-line-key"
import type { LogEntry } from "@/components/logs/types"

// The shape the sandbox stream actually produces: an RFC3339 timestamp with
// NANOSECOND precision, which is more digits than Date can represent.
const entries: LogEntry[] = [
  { timestamp: "2026-09-15T02:07:11.864000000Z", log: "cgroups disabled via --no-cgroups" },
  {
    timestamp: "2026-09-15T02:07:12.470078964Z",
    log: '{"level":"debug","message":"Process start"}',
  },
  {
    timestamp: "2026-09-15T02:07:12.470348009Z",
    log: '{"level":"info","message":"pid 63 started"}',
  },
]

describe("selecting a line from a nanosecond-timestamped stream", () => {
  it("resolves the key it just produced", () => {
    const index = buildLineIndex(entries)
    for (const e of entries) {
      expect(findLineByKey(index, logLineKey(e))).toBeGreaterThanOrEqual(0)
    }
  })

  // Two lines a fraction of a microsecond apart collapse to the same
  // millisecond. They must still be distinguishable, or selecting one selects
  // the other.
  it("keeps sub-millisecond neighbours apart", () => {
    const index = buildLineIndex(entries)
    expect(findLineByKey(index, logLineKey(entries[1]))).toBe(1)
    expect(findLineByKey(index, logLineKey(entries[2]))).toBe(2)
  })
})
