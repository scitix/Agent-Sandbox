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

// Query options for miscellaneous endpoints (quotas, oidc config)

import { queryOptions } from "@tanstack/react-query"
import { currentApiClient } from "@/lib/api/client"
import { impersonationHeaders } from "./utils"

const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

export const serverVersionQueryOptions = (clusterID: string) =>
  queryOptions({
    queryKey: ["serverVersion", clusterID],
    queryFn: () =>
      fetch(`${basePath}/api/clusters/${clusterID}/ping`).then(
        (res) => res.json() as Promise<{ serverVersion: string }>,
      ),
    staleTime: 60 * 1000,
    refetchOnWindowFocus: false,
  })

/**
 * Returns quota items for the current user (or an impersonated user).
 *
 * When `options.impersonate` is provided, X-Impersonate-Team and
 * X-Impersonate-User headers are attached so the backend resolves the target
 * user's quotas. Admins must provide impersonation context because the admin
 * API key itself (User="admin", Team="admin") is not a valid quota subject.
 */
export const quotasQueryOptions = (options?: {
  enabled?: boolean
  impersonate?: { team: string; user: string }
}) =>
  currentApiClient().queryOptions(
    "get",
    "/quotas",
    options?.impersonate
      ? { headers: impersonationHeaders(options.impersonate.team, options.impersonate.user) }
      : undefined,
    {
      enabled: options?.enabled ?? true,
      select: (data) => data.items ?? [],
    },
  )

export const oidcConfigQueryOptions = () =>
  queryOptions({
    queryKey: ["oidcConfig"],
    queryFn: () =>
      fetch(`${basePath}/api/auth/oidc/config`).then((res) => {
        if (!res.ok) return { enabled: false }
        return res.json() as Promise<{ enabled: boolean }>
      }),
    refetchOnWindowFocus: false,
  })

/**
 * InstanceType catalog. Use only when `useFeatureGates().instanceType` is
 * true — on noop deployments the endpoint still returns 200 with an empty
 * items array, but consumers should branch on the gate to avoid rendering
 * empty UI.
 */
/**
 * PersistentVolumeClaims the caller may mount into a sandbox.
 *
 * Scoped to the caller's own namespace by the server — the operation takes no
 * namespace parameter, and that derivation is the authorisation boundary, not a
 * filter. Uses the worker client (never the hub): claims are a per-cluster,
 * per-namespace fact.
 *
 * Use only when `useFeatureGates().volumes` is true; while the feature is off
 * the endpoint returns 200 with an empty list.
 */
export const volumesQueryOptions = (options?: { enabled?: boolean }) =>
  currentApiClient().queryOptions("get", "/volumes", undefined, {
    enabled: options?.enabled ?? true,
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
    select: (data) => data.items ?? [],
  })

export const instanceTypesQueryOptions = (options?: { enabled?: boolean }) =>
  currentApiClient().queryOptions("get", "/instancetypes", undefined, {
    enabled: options?.enabled ?? true,
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
    select: (data) => data.items ?? [],
  })

/**
 * Reports which optional features are enabled on the currently selected
 * cluster. The backend inspects its wired `quota.Provider` and returns
 * booleans like `{ quota: true }`.
 *
 * Gate feature UI on this (quota selector) so the same dashboard build can
 * ship against an open-source deployment without breaking when a feature is
 * absent.
 *
 * The result is stable per deployment, so we cache aggressively and skip
 * window-focus refetching.
 */
export const featureGatesQueryOptions = () =>
  currentApiClient().queryOptions("get", "/feature-gates", undefined, {
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
  })
