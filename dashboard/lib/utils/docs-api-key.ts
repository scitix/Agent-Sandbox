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

// Filling in the one placeholder the server refuses to.
//
// `${AGBX_API_KEY}` is the only docs variable the API leaves alone, and
// deliberately: a response carrying a live credential cannot be cached, logged,
// echoed by `abx`, or relayed by an agent without leaking it. The console is
// the one client that can substitute safely, because it is already holding the
// reader's own keys — so the placeholder is filled here, in the browser, from
// the key the reader picked.
//
// Everything else in the document (env name, pool name, cluster id, gateway
// URLs) is already real by the time it arrives; this is not a template engine.

import type { GlobalApiKeyItem } from "@/lib/api/client"

/** The placeholder, spelled exactly as `handlers/env_docs.go` leaves it. */
export const API_KEY_PLACEHOLDER = "${AGBX_API_KEY}"

/**
 * Replace `${AGBX_API_KEY}` with `apiKey` — **inside fenced code blocks only**.
 *
 * A document is read by people as well as executed, and prose that carried a
 * live credential would put it in a screenshot, a quote in a chat, or the
 * middle of a paragraph somebody forwards. So the substitution is confined to
 * the one place a key is meant to be: a code block that gets copied and run.
 * Everywhere else the placeholder stays spelled out, which reads as the
 * instruction it is.
 *
 * Fences are tracked line by line rather than parsed: four backticks, tildes
 * and info strings all have to survive untouched, and a real Markdown parser
 * here would be a dependency to keep in step with the renderer's.
 */
export function fillApiKey(markdown: string, apiKey?: string): string {
  if (!apiKey || !markdown.includes(API_KEY_PLACEHOLDER)) return markdown

  let fence: "`" | "~" | null = null
  return markdown
    .split("\n")
    .map((line) => {
      const marker = /^\s*(`{3,}|~{3,})/.exec(line)
      if (marker) {
        const char = marker[1][0] as "`" | "~"
        // Opening a fence, closing the one that is open, or a longer run of the
        // same character closing a shorter one — all three are the marker
        // changing state, and none of them is code.
        if (!fence) fence = char
        else if (fence === char) fence = null
        return line
      }
      if (!fence) return line
      return line.split(API_KEY_PLACEHOLDER).join(apiKey)
    })
    .join("\n")
}

/** Newest first — the order a picker shows keys in. */
export function sortApiKeysNewestFirst<T extends { issuedAt?: string }>(keys: T[]): T[] {
  return [...keys].sort((a, b) => (b.issuedAt ?? "").localeCompare(a.issuedAt ?? ""))
}

/**
 * Which key a docs snippet should use when the reader has not chosen one.
 *
 * `unrestricted` for a document a person is going to paste into their own
 * terminal — that is the credential that acts as them, which is what the
 * snippet implies. `agent` for a document whose whole purpose is to be handed
 * to an unattended agent. Neither is a rule about safety: an agent key starts
 * sandboxes, runs commands and moves files exactly as an unrestricted one does.
 */
export function pickDocsApiKey(
  keys: GlobalApiKeyItem[] | undefined,
  prefer: "unrestricted" | "agent",
): GlobalApiKeyItem | undefined {
  const usable = sortApiKeysNewestFirst(keys ?? [])
  const preferred = usable.find((k) => (k.mode ?? "unrestricted") === prefer)
  return preferred ?? usable[0]
}
