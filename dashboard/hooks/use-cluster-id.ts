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

/**
 * Returns the clusterID from the current route's [clusterID] dynamic segment.
 * Falls back to "default" when there is no [clusterID] segment (e.g. root-level
 * dashboard pages that haven't been migrated yet).
 */
export function useClusterID(): string {
  const params = useParams<{ clusterID?: string }>()
  return params?.clusterID ?? "default"
}
