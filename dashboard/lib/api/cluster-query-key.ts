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
 * Cluster-scoped React Query keys.
 *
 * openapi-react-query derives a key from the operation alone — `[method, path,
 * init]` — while the only thing separating one cluster's client from another is
 * its baseUrl (`/api/clusters/{id}/v1`). Two clusters asking for the same path
 * therefore shared a single cache entry: navigating from cluster A to cluster B
 * rendered A's data under B's URL, and anything derived from it (a pool name
 * read out of the cached Env, say) was then requested from B, which 404s.
 *
 * Appending the cluster gives every cluster its own entry.
 */

/** `[method, path, init, clusterID]` — see the invariants on `clusterQueryKey`. */
export type ClusterQueryKey = [string, string, unknown, string]

/**
 * Builds the cluster-scoped key for one operation.
 *
 * Two invariants, both load-bearing:
 *
 *   - **The cluster goes last.** Every invalidation in `lib/queries` matches by
 *     key prefix (`["get", "/envs"]`, `["get", "/envs/{name}/sandboxpools",
 *     {params}]`, …), and a prefix only keeps matching if nothing is inserted
 *     ahead of it. Those invalidations now span every cluster rather than just
 *     the current one — a harmless superset, where a prefixed key would have
 *     silently stopped matching and left the UI stale.
 *
 *   - **Index 2 always holds `init`**, filled with `null` when the operation
 *     takes none. openapi-react-query's shared `queryFn` destructures
 *     `[method, path, init]` and spreads `init` into the fetch options. Omitting
 *     index 2 for parameterless operations would put the cluster id there, and
 *     spreading a string yields `{0: "a", 1: "r", …}` — corrupting every such
 *     request. `{...null}` is `{}`, so `null` is the safe filler.
 */
export function clusterQueryKey(
  method: string,
  path: string,
  init: unknown,
  clusterID: string,
): ClusterQueryKey {
  return [method, path, init ?? null, clusterID]
}
