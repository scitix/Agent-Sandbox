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

"use client"

// The key a rendered documentation snippet is filled in with.
//
// One hook for every surface that shows docs with `${AGBX_API_KEY}` in it — the
// Env page, the Template page and the assistant's CLI guide — so the choice of
// key, and the memory of it, are the same fact everywhere: pick a key on one
// page and the next page already has it picked.

import { useCallback, useMemo, useSyncExternalStore } from "react"
import { useQuery } from "@tanstack/react-query"
import type { GlobalApiKeyItem } from "@/lib/api/client"
import { globalApiKeysQueryOptions } from "@/lib/queries"
import { fillApiKey, pickDocsApiKey, sortApiKeysNewestFirst } from "@/lib/utils/docs-api-key"

/** Where the chosen key is remembered. One id, not the key itself. */
const STORAGE_KEY = "agentbox.docs.apiKeyId"

/**
 * The remembered key id, read as an external store.
 *
 * `localStorage` is external state, so it is read the way React asks external
 * state to be read: a snapshot that is `null` on the server (there is no
 * storage there) and the stored id in the browser. Subscribing to the browser's
 * own `storage` event — and to a local one for the tab that made the choice —
 * is what keeps two docs panels on screen agreeing about which key is chosen.
 */
const listeners = new Set<() => void>()

function subscribeToStoredKey(listener: () => void): () => void {
  listeners.add(listener)
  window.addEventListener("storage", listener)
  return () => {
    listeners.delete(listener)
    window.removeEventListener("storage", listener)
  }
}

function readStoredKeyId(): string | null {
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    // A browser that refuses storage loses the memory, not the page.
    return null
  }
}

const noStoredKeyId = (): null => null

export interface DocsApiKey {
  /** Candidate keys, newest first. */
  keys: GlobalApiKeyItem[]
  /** The key snippets are rendered with, when the reader has one. */
  selected?: GlobalApiKeyItem
  /** Choose another one; remembered for the next page and the next visit. */
  select: (keyId: string) => void
  /** The document, with the placeholder replaced when a key is selected. */
  fill: (markdown: string) => string
  /** Keys are still being listed. */
  loading: boolean
}

export function useDocsApiKey(
  /** Which mode wins when the reader has not chosen: the default is
   *  `unrestricted` (a document for a person), and the assistant's guide asks
   *  for `agent` (a document for an unattended agent). */
  prefer: "unrestricted" | "agent" = "unrestricted",
): DocsApiKey {
  const { data, isLoading } = useQuery(globalApiKeysQueryOptions())
  const keys = useMemo(() => sortApiKeysNewestFirst(data ?? []), [data])

  const chosenId = useSyncExternalStore(subscribeToStoredKey, readStoredKeyId, noStoredKeyId)

  // A remembered key that no longer exists — revoked, deleted, or belonging to
  // another user on the same browser — falls back to the default rather than
  // leaving the picker pointing at nothing.
  const selected = useMemo(() => {
    const remembered = chosenId ? keys.find((k) => k.keyId === chosenId) : undefined
    return remembered ?? pickDocsApiKey(keys, prefer)
  }, [keys, chosenId, prefer])

  const select = useCallback((keyId: string) => {
    try {
      window.localStorage.setItem(STORAGE_KEY, keyId)
    } catch {
      // Same as above: the choice still applies to this page.
    }
    for (const listener of listeners) listener()
  }, [])

  const fill = useCallback(
    (markdown: string) => fillApiKey(markdown, selected?.rawToken),
    [selected?.rawToken],
  )

  return { keys, selected, select, fill, loading: isLoading }
}
