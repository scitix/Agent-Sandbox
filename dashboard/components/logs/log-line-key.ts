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

// A stable, URL-safe identity for one log line, so a selected line survives a
// reload, a shared link, and — the point of it — a re-query with a different time
// window or keyword.
//
// Deliberately NOT the array index: a re-query returns a different slice of the
// stream, so the same line almost always sits at a different offset. The key is
// `<ingest epoch ms>.<8 hex digits of the line text>`, reproducible from the entry
// alone and short enough to live in a query string.
//
// Resolution goes through a prebuilt index rather than a scan: hashing every line
// on every render is a per-keystroke cost proportional to the whole result (5000
// lines × 2KB is ~10MB of character work), which is felt as lag while paging
// through rows with the arrow keys.
import type { LogEntry } from "@/components/logs/types"

/** djb2 — small, fast, and stable across browsers (no crypto/async needed). */
function hash(s: string): string {
  let h = 5381
  for (let i = 0; i < s.length; i += 1) {
    h = ((h << 5) + h + s.charCodeAt(i)) | 0
  }
  return (h >>> 0).toString(16).padStart(8, "0")
}

function epochMs(timestamp: string | undefined): number {
  const ms = timestamp ? Date.parse(timestamp) : Number.NaN
  return Number.isNaN(ms) ? 0 : ms
}

/** Build the `sel` search-param value identifying this entry. */
export function logLineKey(entry: LogEntry): string {
  return `${epochMs(entry.timestamp)}.${hash(entry.log)}`
}

/** Lookup tables over one result set. Build once per result, not per render. */
export interface LineIndex {
  /** Full key → row. First writer wins, so duplicate lines resolve to the first. */
  exact: Map<string, number>
  /** Text hash → every row with that text, for the nearest-instant fallback. */
  byText: Map<string, { index: number; ms: number }[]>
}

export function buildLineIndex(entries: LogEntry[]): LineIndex {
  const exact = new Map<string, number>()
  const byText = new Map<string, { index: number; ms: number }[]>()
  for (let i = 0; i < entries.length; i += 1) {
    const entry = entries[i]
    const text = hash(entry.log)
    const ms = epochMs(entry.timestamp)
    const key = `${ms}.${text}`
    if (!exact.has(key)) exact.set(key, i)
    const bucket = byText.get(text)
    if (bucket) bucket.push({ index: i, ms })
    else byText.set(text, [{ index: i, ms }])
  }
  return { exact, byText }
}

/**
 * Locate a previously selected line in a result set. Exact key first; failing that
 * (the log service can re-ingest a line under a slightly different timestamp) the
 * same text at the nearest instant. Returns -1 when the line is not in this result
 * — the expected outcome when the line limit clipped it away, so callers must
 * handle it rather than assume a hit.
 */
export function findLineByKey(index: LineIndex, key: string): number {
  if (!key) return -1
  const exact = index.exact.get(key)
  if (exact !== undefined) return exact

  const dot = key.lastIndexOf(".")
  if (dot <= 0) return -1
  const wantMs = Number(key.slice(0, dot))
  if (!Number.isFinite(wantMs)) return -1
  const bucket = index.byText.get(key.slice(dot + 1))
  if (!bucket || bucket.length === 0) return -1

  let best = bucket[0]
  for (const candidate of bucket) {
    if (Math.abs(candidate.ms - wantMs) < Math.abs(best.ms - wantMs)) {
      best = candidate
    }
  }
  return best.index
}
