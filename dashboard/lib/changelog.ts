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
 * Changelog data — edit ONLY this file when publishing a new release.
 * Keep entries sorted: newest version first.
 */

export interface ChangelogEntry {
  /** Semantic version string, e.g. "1.3.0" */
  version: string
  /** ISO date string, e.g. "2026-03-27" */
  date: string
  /** Short bullet-point highlights shown in the dialog summary */
  highlights: string[]
  /** Full Markdown content rendered on the /changelog page and in the dialog body */
  content: string
}

export const CHANGELOG: ChangelogEntry[] = []

/** Returns the latest (first) changelog entry, or undefined if the changelog is empty */
export function latestEntry(): ChangelogEntry | undefined {
  return CHANGELOG[0]
}

/** Returns the latest version string, or empty string if the changelog is empty */
export function latestVersion(): string {
  return CHANGELOG[0]?.version ?? ""
}
