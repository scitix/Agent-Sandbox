import { describe, expect, it } from "vitest"

import type { LogEntry } from "@/components/logs/types"

import { buildLineIndex, findLineByKey, logLineKey } from "./log-line-key"

function entry(timestamp: string, log: string): LogEntry {
  return { timestamp, log }
}

/** Resolve a key against a result set, the way the viewer does. */
function find(entries: LogEntry[], key: string): number {
  return findLineByKey(buildLineIndex(entries), key)
}

const A = entry("2026-07-29T04:13:08.156Z", "first line")
const B = entry("2026-07-29T04:13:09.000Z", "second line")
const C = entry("2026-07-29T04:13:10.000Z", "third line")

describe("logLineKey", () => {
  it("is `<ingest epoch ms>.<8 hex digits>`", () => {
    // The shape is a contract: it goes in a URL and is parsed back by splitting on
    // the LAST dot, so the hash half must always be exactly 8 hex digits.
    expect(logLineKey(A)).toMatch(/^\d+\.[0-9a-f]{8}$/)
    expect(logLineKey(A).split(".")[0]).toBe(String(Date.parse(A.timestamp)))
  })

  it("is stable — the same entry always yields the same key", () => {
    expect(logLineKey(B)).toBe(logLineKey({ ...B }))
  })

  it("separates identical text at different instants", () => {
    const early = entry("2026-07-29T04:13:08.000Z", "same text")
    const late = entry("2026-07-29T04:14:08.000Z", "same text")
    expect(logLineKey(early)).not.toBe(logLineKey(late))
  })

  it("keeps the 8-digit hash shape for awkward payloads", () => {
    const awkward = [
      "",
      ".",
      "a.b.c.d",
      "[31mcolored[0m",
      "CJK 中文 mixed",
      "x".repeat(20000),
      '{"nested":{"json":"with \\"escapes\\""}}',
    ]
    for (const log of awkward) {
      const key = logLineKey(entry("2026-07-29T04:13:08.000Z", log))
      expect(key, log.slice(0, 20)).toMatch(/^\d+\.[0-9a-f]{8}$/)
    }
  })

  it("round-trips every line of a result set back to its own row", () => {
    // The guarantee the selection depends on: for any line in the result, the key
    // built from it resolves to exactly that line. Exercised over payloads chosen
    // to stress the key format (dots, escapes, unicode, empty, huge).
    const entries: LogEntry[] = [
      A,
      B,
      C,
      entry("2026-07-29T04:13:11.000Z", ""),
      entry("2026-07-29T04:13:12.000Z", "trailing dot."),
      entry("2026-07-29T04:13:13.000Z", "1785311031761.29189481"),
      entry("2026-07-29T04:13:14.000Z", "x".repeat(5000)),
      entry("2026-07-29T04:13:15.000Z", "CJK 中文 · punctuation"),
      entry("not-a-date", "no parseable timestamp"),
      ...Array.from({ length: 200 }, (_, i) =>
        entry(
          new Date(Date.parse("2026-07-29T05:00:00.000Z") + i * 37).toISOString(),
          `I0729 05:00:00.0000${i} 7 server.go:${i}] "line" seq=${i}`,
        ),
      ),
    ]
    const index = buildLineIndex(entries)
    entries.forEach((e, i) => {
      expect(findLineByKey(index, logLineKey(e)), `row ${i}`).toBe(i)
    })
  })
})

describe("findLineByKey", () => {
  it("finds the same line after a re-query moved it", () => {
    const key = logLineKey(B)
    const widened = [entry("2026-07-29T04:13:00.000Z", "earlier"), A, B, C]
    expect(find(widened, key)).toBe(2)
  })

  it("returns -1 when the line is not in this result", () => {
    // The whole point of the -1: a clipped window must not silently select a
    // different line.
    expect(find([A, C], logLineKey(B))).toBe(-1)
    expect(find([], logLineKey(B))).toBe(-1)
  })

  it("falls back to the same text at the nearest instant", () => {
    // Re-ingestion can shift a line's timestamp; the text hash still matches.
    const reingested = entry("2026-07-29T04:13:09.400Z", "second line")
    expect(find([A, reingested, C], logLineKey(B))).toBe(1)
  })

  it("picks the nearest instant when the text repeats", () => {
    const rows = [
      entry("2026-07-29T04:13:00.000Z", "repeated"),
      entry("2026-07-29T04:13:09.100Z", "repeated"),
      entry("2026-07-29T04:13:30.000Z", "repeated"),
    ]
    expect(find(rows, logLineKey(entry("2026-07-29T04:13:09.000Z", "repeated")))).toBe(1)
  })

  it("prefers the exact key over a nearer sibling", () => {
    const rows = [
      entry("2026-07-29T04:13:09.000Z", "repeated"),
      entry("2026-07-29T04:13:09.001Z", "repeated"),
    ]
    expect(find(rows, logLineKey(rows[1]))).toBe(1)
  })

  it("resolves duplicates to the first occurrence", () => {
    const dup = entry("2026-07-29T04:13:09.000Z", "identical")
    expect(find([A, dup, { ...dup }, C], logLineKey(dup))).toBe(1)
  })

  it("rejects malformed keys instead of guessing", () => {
    for (const key of ["", "nonsense", ".abc", "1785298369000"]) {
      expect(find([A, B, C], key)).toBe(-1)
    }
  })

  it("handles an entry with no parseable timestamp", () => {
    const noTs = entry("not-a-date", "orphan line")
    const key = logLineKey(noTs)
    expect(key.startsWith("0.")).toBe(true)
    expect(find([A, noTs], key)).toBe(1)
  })
})
