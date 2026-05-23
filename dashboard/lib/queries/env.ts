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

// Query options and mutations for SandboxEnv resources.
//
// MVP scope: list, get by name, and patch the autoscaling block. Other Env
// fields (templateRef, mode, members) are managed by the Phase 1 adopter
// and are read-only at this layer.

import { useQueryClient } from "@tanstack/react-query"
import { currentApiClient } from "@/lib/api/client"
import { delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

/** List SandboxEnvs visible to the caller (filtered by team/user labels). */
export const envsQueryOptions = () =>
  currentApiClient().queryOptions("get", "/envs", undefined, {
    select: (data) => data.items ?? [],
  })

/** Fetch a single SandboxEnv by name. */
export const envQueryOptions = (name: string) =>
  currentApiClient().queryOptions("get", "/envs/{name}", {
    params: { path: { name } },
  })

// ─── Mutations ─────────────────────────────────────────────────────────────────

/**
 * Patch the autoscaling block of a SandboxEnv.
 *
 * Only the `autoscaling` field is accepted by the API; supplying other fields
 * has no effect. On success, both the list and the per-name query cache are
 * invalidated so the table and detail sheet pick up the new state.
 */
export function useUpdateEnvAutoscaling() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("patch", "/envs/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
    },
  })
}
