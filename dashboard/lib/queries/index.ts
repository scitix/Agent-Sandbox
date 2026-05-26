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

// Unified re-export for all query options and mutations

export * from "./env"
export * from "./env-autoscaler"
export * from "./pool"
export * from "./sandbox"
export * from "./template"
export * from "./apikey"
export * from "./global-apikey"
export * from "./global-template"
export * from "./stats"
export * from "./prometheus"
export * from "./organization"
export * from "./auth"
export * from "./misc"
export * from "./sandbox-logs"

// ─── Invalidation helper ───────────────────────────────────────────────────────

import { useQueryClient } from "@tanstack/react-query"

/**
 * Returns helpers to invalidate specific resource caches.
 * Useful for page-level batch operations where you want to refresh
 * after multiple imperative calls finish.
 */
export function useInvalidate() {
  const qc = useQueryClient()
  return {
    sandboxes: () => void qc.invalidateQueries({ queryKey: ["get", "/sandboxes"] }),
    // envPools: refresh every env's pool list (no per-env filter — react-query
    // matches by key-prefix). For a single env, prefer the per-mutation
    // invalidate inside useCreate/Update/DeleteEnvPool.
    envPools: () => void qc.invalidateQueries({ queryKey: ["get", "/envs/{name}/sandboxpools"] }),
    envs: () => void qc.invalidateQueries({ queryKey: ["get", "/envs"] }),
    templates: () => void qc.invalidateQueries({ queryKey: ["get", "/sandbox-templates"] }),
    apiKeys: () => void qc.invalidateQueries({ queryKey: ["get", "/admin/api-keys"] }),
    globalApiKeys: () => void qc.invalidateQueries({ queryKey: ["get", "/api-keys"] }),
  }
}
