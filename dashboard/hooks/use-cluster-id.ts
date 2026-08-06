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

import { useParams } from "next/navigation"
import { useAtomValue } from "jotai"
import { clusterIDAtom, clustersAtom } from "@/lib/atoms"

/**
 * Returns the clusterID from the current route's [clusterID] dynamic segment.
 * Falls back to "default" when there is no [clusterID] segment — the BFF proxy
 * resolves "default" to the first configured cluster, so API calls from the
 * cluster-agnostic pages still work.
 */
export function useClusterID(): string {
  const params = useParams<{ clusterID?: string }>()
  return params?.clusterID ?? "default"
}

/**
 * The clusterID to build navigation links with.
 *
 * Same as `useClusterID()` on a cluster-scoped route. On the cluster-agnostic
 * pages (`/overview`, `/admin`) there is no route segment to read, and linking
 * to the literal "default" would put a placeholder in the address bar and in
 * every shared link — so it resolves to the session's cluster, then to the
 * first available one.
 */
export function useNavClusterID(): string {
  const params = useParams<{ clusterID?: string }>()
  const sessionClusterID = useAtomValue(clusterIDAtom)
  const clusters = useAtomValue(clustersAtom).clusters

  if (params?.clusterID) return params.clusterID
  if (sessionClusterID && clusters.some((c) => c.id === sessionClusterID)) return sessionClusterID
  return clusters[0]?.id ?? "default"
}
