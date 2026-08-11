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
 * Short Go-style durations, as the sandbox form accepts them: `30s`, `5m`, `1h`.
 *
 * The native API takes these strings verbatim; the E2B API takes integer seconds
 * (`timeout`, and the startup-timeout metadata value). Validation and conversion
 * share this one pattern so a value the form accepts is always convertible.
 */
export const SHORT_DURATION_RE = /^(\d+[smh])?$/

const UNIT_SECONDS: Record<string, number> = { s: 1, m: 60, h: 3600 }

/**
 * Converts a short duration to whole seconds. Returns undefined for an empty or
 * unparseable value, so callers can simply omit the field.
 */
export function durationToSeconds(value?: string): number | undefined {
  const raw = value?.trim()
  if (!raw) return undefined

  const match = /^(\d+)([smh])$/.exec(raw)
  if (!match) return undefined

  const amount = Number(match[1])
  const unit = UNIT_SECONDS[match[2]!]
  if (!Number.isFinite(amount) || amount <= 0 || !unit) return undefined

  return amount * unit
}
