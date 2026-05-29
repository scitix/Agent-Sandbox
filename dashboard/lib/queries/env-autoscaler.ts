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

// Query options + mutations for the env-scoped autoscaler config CRUD.
// Mirrors the env-scoped pool helpers (pool.ts) — `Spec` reads the full
// EnvAutoscalingSpec, and per-group operations (Add / Update / Delete /
// List / Get) live under `/envs/{name}/autoscaling/groups`.

import { useQueryClient } from "@tanstack/react-query"
import { currentApiClient } from "@/lib/api/client"
import { delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

export const envAutoscalingQueryOptions = (envName: string) =>
  currentApiClient().queryOptions(
    "get",
    "/envs/{name}/autoscaling",
    { params: { path: { name: envName } } },
    { select: (data) => data.spec },
  )

export const envAutoscalingGroupsQueryOptions = (envName: string) =>
  currentApiClient().queryOptions(
    "get",
    "/envs/{name}/autoscaling/groups",
    { params: { path: { name: envName } } },
    { select: (data) => data.items ?? [] },
  )

// ─── Mutations ────────────────────────────────────────────────────────────────

// Groups are created automatically when a member declaring the matching
// ScalingGroup is added (POST /envs/{name}/sandboxpools) and garbage-collected
// by the Env reconciler once unreferenced — there is no standalone "add group"
// mutation. Only update / delete are exposed here.

/** Patch an existing autoscaling group. Policy objects are REPLACED wholesale. */
export function useUpdateEnvAutoscalingGroup(envName: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("put", "/envs/{name}/autoscaling/groups/{groupName}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs/{name}/autoscaling/groups", { params: { path: { name: envName } } }])
      delayedInvalidate(qc, ["get", "/envs/{name}/autoscaling", { params: { path: { name: envName } } }])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
    },
  })
}

/** Remove an autoscaling group. */
export function useDeleteEnvAutoscalingGroup(envName: string) {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/envs/{name}/autoscaling/groups/{groupName}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs/{name}/autoscaling/groups", { params: { path: { name: envName } } }])
      delayedInvalidate(qc, ["get", "/envs/{name}/autoscaling", { params: { path: { name: envName } } }])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
    },
  })
}
