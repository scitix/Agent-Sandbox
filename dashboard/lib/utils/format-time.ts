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
 * An ISO timestamp as `YYYY-MM-DD HH:mm:ss`, in the viewer's own timezone.
 *
 * Log lines are read against wall-clock memory — "the deploy was around ten
 * past" — so a UTC rendering makes the reader do arithmetic on every line. The
 * getters used here are the local ones, which is what does the conversion.
 *
 * Never throws: an unparseable value comes back unchanged rather than as
 * "Invalid Date", because the raw text is still the most useful thing to show.
 */
export function formatLocalTimestamp(iso: string, opts?: { withMillis?: boolean }): string {
  if (!iso) return ""
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number, w = 2) => String(n).padStart(w, "0")
  const base =
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  return opts?.withMillis ? `${base}.${pad(d.getMilliseconds(), 3)}` : base
}
