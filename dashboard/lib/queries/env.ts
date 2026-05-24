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
// SandboxEnv is the user-facing primary: the API supports list / get /
// create / update / delete / sync-template. Member SandboxPools are
// materialised by the Env Reconciler and surface as read-only data on the
// /sandboxpools endpoint.

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
 * Create a new SandboxEnv. The Env Reconciler picks up the resulting CRD
 * and materialises one SandboxPool per entry in `members` (or a single
 * namesake pool when `members` is empty).
 */
export function useCreateEnv() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/envs", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/sandboxpools"])
    },
  })
}

/**
 * Patch one or more editable SandboxEnv spec fields: autoscaling, members,
 * overrides. Pool spec drift (e.g. overrides.image changed) is re-rendered
 * by the Env Reconciler on the next reconcile cycle.
 */
export function useUpdateEnv() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("patch", "/envs/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
      delayedInvalidate(qc, ["get", "/sandboxpools"])
    },
  })
}

/**
 * Back-compat alias used by the autoscaling-only edit sheet. New code should
 * call `useUpdateEnv` directly with the full patch body.
 */
export const useUpdateEnvAutoscaling = useUpdateEnv

/**
 * Delete a SandboxEnv. Member SandboxPools are cascade-deleted by Kubernetes
 * garbage collection via the controlling OwnerReference the Reconciler
 * stamps onto each Pool.
 */
export function useDeleteEnv() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("delete", "/envs/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/sandboxpools"])
    },
  })
}

/**
 * Re-render every member SandboxPool against the latest Template body plus
 * the Env's current overrides. Use this after an admin edits the underlying
 * SandboxTemplate — `useUpdateEnv` propagates `env.spec.overrides` edits
 * automatically through the next Reconcile cycle.
 */
export function useSyncEnvTemplate() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/envs/{name}/sync-template", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
      delayedInvalidate(qc, ["get", "/sandboxpools"])
    },
  })
}
