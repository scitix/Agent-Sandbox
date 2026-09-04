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

// Query options and mutations for API Key resources (admin, per-cluster)

import { useQueryClient } from "@tanstack/react-query"
import { currentApiClient } from "@/lib/api/client"
import { delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const apiKeysQueryOptions = (namespace?: string) =>
  currentApiClient().queryOptions(
    "get",
    "/admin/api-keys",
    {
      params: { query: namespace ? { namespace } : undefined },
    },
    {
      // ListAPIKeysResult is a bare APIKeyItem[] (the endpoint returns the
      // full list without pagination), not an envelope with `.items`.
      select: (data) => data ?? [],
    },
  )

// ─── Admin mutations ───────────────────────────────────────────────────────────

export function useCreateApiKey() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/admin/api-keys", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/admin/api-keys"]),
  })
}

export function useDeleteApiKey() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/admin/api-keys/{name}", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/admin/api-keys"]),
  })
}

export function usePromoteApiKey() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/admin/api-keys/{name}/promote", {
    onSuccess: () => delayedInvalidate(qc, ["get", "/admin/api-keys"]),
  })
}

// ─── Key selection ─────────────────────────────────────────────────────────────

/**
 * The key a browser-side caller should authenticate an E2B request with: the
 * newest one whose plaintext the platform still holds.
 *
 * The E2B surface takes API keys only — never the session JWT — so anything the
 * console drives through it has to pick a key on the user's behalf. Legacy keys
 * are stored as a hash alone and cannot be sent upstream, so they are not
 * candidates however recent they are.
 */
export function pickUsableApiKey<T extends { rawToken?: string; issuedAt?: string }>(
  keys: T[] | undefined,
): T | undefined {
  return (keys ?? [])
    .filter((k) => !!k.rawToken)
    .sort((a, b) => (b.issuedAt ?? "").localeCompare(a.issuedAt ?? ""))
    .at(0)
}
