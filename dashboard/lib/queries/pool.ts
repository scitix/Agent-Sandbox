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

// Query options for Sandbox Pool resources. Pools are now read-only at the
// HTTP layer — creation / deletion / template-sync go through SandboxEnv,
// which materialises member Pools via the Env Reconciler.

import { currentApiClient } from "@/lib/api/client"
import { impersonationHeaders } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const poolsQueryOptions = () =>
  currentApiClient().queryOptions("get", "/sandboxpools", undefined, {
    select: (data) => data.items ?? [],
  })

// Env-scoped pool list: reuses /sandboxpools (which already carries `owningEnv`
// stamped by the Phase 1 PoolAdoption reconciler) and filters client-side, so
// the cache stays shared with the top-level pools query.
export const envPoolsQueryOptions = (envName: string) =>
  currentApiClient().queryOptions("get", "/sandboxpools", undefined, {
    select: (data) =>
      (data.items ?? [])
        .filter((p) => p.owningEnv === envName)
        .sort((a, b) => a.name.localeCompare(b.name)),
  })

export const poolQueryOptions = (name: string) =>
  currentApiClient().queryOptions("get", "/sandboxpools/{name}", {
    params: { path: { name } },
  })

// Admin impersonation variant: calls the regular /sandboxpools endpoint with
// X-Impersonate-Team and X-Impersonate-User headers.
export const adminUserPoolsQueryOptions = (team: string, user: string) =>
  currentApiClient().queryOptions(
    "get",
    "/sandboxpools",
    { headers: impersonationHeaders(team, user) },
    {
      select: (data) => data.items ?? [],
    },
  )
