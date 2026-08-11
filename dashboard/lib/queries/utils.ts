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

import { useQueryClient } from "@tanstack/react-query"
import {
  currentApiClient,
  currentFetchClient,
  getApiClient,
  getFetchClient,
} from "@/lib/api/client"

/**
 * Client accessors for the query layer, in the two shapes a call site needs.
 *
 * Omitting `clusterID` targets the cluster in the current browser URL — what
 * nearly every page wants. Passing one targets that cluster explicitly, for the
 * cross-cluster cases: probing whether an Env of the same name exists elsewhere,
 * reading a peer cluster's Pool behind a foreign-cluster link, creating an Env
 * on a cluster the user is not currently viewing.
 *
 * Read factories in this directory follow the convention of taking `clusterID`
 * as an optional LAST parameter, so existing call sites keep working untouched:
 *
 *     envQueryOptions("slimedev")           // current cluster
 *     envQueryOptions("slimedev", "foo")  // that cluster
 *
 * Each cluster keeps its own cache entries (see `lib/api/cluster-query-key.ts`),
 * so the two forms above never overwrite one another.
 */
export const apiFor = (clusterID?: string) =>
  clusterID ? getApiClient(clusterID) : currentApiClient()

export const fetchFor = (clusterID?: string) =>
  clusterID ? getFetchClient(clusterID) : currentFetchClient()

const DELAY_MS = 300

export function delayedInvalidate(qc: ReturnType<typeof useQueryClient>, key: unknown[]) {
  setTimeout(() => void qc.invalidateQueries({ queryKey: key }), DELAY_MS)
}

/**
 * Returns headers for admin impersonation.
 * When an admin calls a regular endpoint with these headers, the backend
 * resolves the target user's namespace and executes the request on their behalf.
 * Non-admin callers that provide these headers have them silently ignored.
 */
export const impersonationHeaders = (team: string, user: string): Record<string, string> => ({
  "X-Impersonate-Team": team,
  "X-Impersonate-User": user,
})
