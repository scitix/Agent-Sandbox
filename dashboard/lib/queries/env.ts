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
import { currentApiClient, currentFetchClient } from "@/lib/api/client"
import type { AgentCreateSandboxEnvRequest } from "@/lib/api/client"
import { apiFor, fetchFor, delayedInvalidate } from "./utils"

// ─── Query options ─────────────────────────────────────────────────────────────

/** List SandboxEnvs visible to the caller (filtered by team/user labels). */
export const envsQueryOptions = (clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/envs", undefined, {
    select: (data) => data.items ?? [],
  })

/** Fetch a single SandboxEnv by name. */
export const envQueryOptions = (name: string, clusterID?: string) =>
  apiFor(clusterID).queryOptions("get", "/envs/{name}", {
    params: { path: { name } },
  })

/**
 * Recent K8s Events for the Env and its member SandboxPools, sorted newest
 * first. Drives the activity timeline on the Env detail page.
 */
export const envEventsQueryOptions = (name: string, limit = 100, clusterID?: string) =>
  apiFor(clusterID).queryOptions(
    "get",
    "/envs/{name}/events",
    {
      params: { path: { name }, query: { limit } },
    },
    {
      select: (data) => data.items ?? [],
    },
  )

// ─── Mutations ─────────────────────────────────────────────────────────────────

/**
 * Create a new SandboxEnv shell — TemplateRef + Overrides + optional
 * ImagePullSecret only. Members are added via `useCreateEnvPool` after the
 * env exists; the matching autoscaling group is created automatically per
 * member and tuned via `useUpdateEnvAutoscalingGroup`.
 */
export function useCreateEnv() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("post", "/envs", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
    },
  })
}

/**
 * Patch the env shell (overrides + image-pull-secret). Members and
 * autoscaling groups are managed through their own dedicated mutations.
 */
export function useUpdateEnv() {
  const qc = useQueryClient()
  return currentApiClient().useMutation("patch", "/envs/{name}", {
    onSuccess: () => {
      delayedInvalidate(qc, ["get", "/envs"])
      delayedInvalidate(qc, ["get", "/envs/{name}"])
      delayedInvalidate(qc, ["get", "/envs/{name}/sandboxpools"])
    },
  })
}

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
      delayedInvalidate(qc, ["get", "/envs/{name}/sandboxpools"])
    },
  })
}

/**
 * Imperative single-env delete for batch operations (the multi-select toolbar
 * deletes each selected row outside the React render tree). Mirrors
 * `deleteSandboxImperative`. Cache invalidation is the caller's responsibility.
 */
export async function deleteEnvImperative(name: string): Promise<void> {
  const client = currentFetchClient()
  const { error } = await client.DELETE("/envs/{name}", {
    params: { path: { name } },
  })
  if (error) throw error
}

/**
 * Imperative create against a named cluster, for extending an Env to clusters
 * the user is not currently viewing. A hook cannot serve this: the set of target
 * clusters is chosen at click time, so there is no fixed number of `useMutation`
 * calls to make. Cache invalidation is the caller's responsibility.
 */
export async function createEnvImperative(
  body: AgentCreateSandboxEnvRequest,
  clusterID: string,
): Promise<void> {
  const { error } = await fetchFor(clusterID).POST("/envs", { body })
  if (error) throw error
}
