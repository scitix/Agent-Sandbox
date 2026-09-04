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

// Credential vault (E2B /secrets), reached through the cluster's E2B proxy.
//
// Not on the generated client: the vault lives on the E2B-compatible surface,
// which has no generated types here and needs the caller's own API key as
// X-API-Key (the E2B auth middleware does not accept the session JWT). Same
// arrangement sandbox creation already uses.
//
// Values are write-only everywhere in this module. There is no read that
// returns one, because the API has none — a secret's value leaves the cluster
// only through the egress proxy, substituted into an outbound request.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { basePath, getToken, handleErrorResponse } from "@/lib/api/client"
import { impersonationHeaders } from "./utils"

/** One vault entry's metadata. Never carries a value. */
export interface SecretInfo {
  secretID: string
  name: string
  currentVersion: number
  metadata: Record<string, string>
  createdAt: string
  updatedAt: string
}

interface VaultRequestOptions {
  clusterID: string
  apiKey: string
  /**
   * Admin impersonation target. The vault is scoped to (namespace, user), so
   * without this an admin using the console's user switcher would be shown
   * their own credentials while believing they were looking at someone else's.
   * The credential on the wire stays the admin's own key; only the effective
   * identity changes, which is what keeps the audit trail honest.
   */
  impersonate?: { team: string; user: string } | null
}

async function vaultFetch<T>(
  opts: VaultRequestOptions,
  path: string,
  init?: RequestInit,
): Promise<T> {
  const impersonation =
    opts.impersonate?.team && opts.impersonate?.user
      ? impersonationHeaders(opts.impersonate.team, opts.impersonate.user)
      : {}
  const res = await fetch(`${basePath}/api/clusters/${opts.clusterID}/e2b${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${getToken()}`,
      "X-API-Key": opts.apiKey,
      ...impersonation,
      ...(init?.headers ?? {}),
    },
  })
  if (!res.ok) {
    // The BFF normalises E2B's {code,message} into the native error shape, so
    // this gets the same handling as every other call.
    return handleErrorResponse(res)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

// The impersonation target is part of the key: two users' vaults are different
// data, and without it switching users would serve the previous one from cache.
export const vaultQueryKey = (clusterID: string, user?: string | null) =>
  ["vault", clusterID, user ?? ""] as const

export function useSecrets(opts: VaultRequestOptions, enabled: boolean) {
  return useQuery({
    queryKey: vaultQueryKey(opts.clusterID, opts.impersonate?.user),
    // An API key is required to read the vault at all; without one the query
    // stays idle rather than firing a request that can only 401.
    enabled: enabled && Boolean(opts.apiKey),
    queryFn: () => vaultFetch<SecretInfo[]>(opts, "/secrets"),
  })
}

export function useCreateSecret(opts: VaultRequestOptions) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { name: string; value: string; metadata?: Record<string, string> }) =>
      vaultFetch<SecretInfo>(opts, "/secrets", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: vaultQueryKey(opts.clusterID, opts.impersonate?.user) }),
  })
}

/**
 * Rotates a secret. The API has no "change the metadata only" call — an update
 * always carries a new value — so the dialog always asks for one.
 */
export function useRotateSecret(opts: VaultRequestOptions) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({
      secretID,
      ...body
    }: {
      secretID: string
      value: string
      metadata?: Record<string, string>
    }) =>
      vaultFetch<SecretInfo>(opts, `/secrets/${encodeURIComponent(secretID)}`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: vaultQueryKey(opts.clusterID, opts.impersonate?.user) }),
  })
}

export function useDeleteSecret(opts: VaultRequestOptions) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (secretID: string) =>
      vaultFetch<void>(opts, `/secrets/${encodeURIComponent(secretID)}`, { method: "DELETE" }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: vaultQueryKey(opts.clusterID, opts.impersonate?.user) }),
  })
}

/** The placeholder that references this secret from a network rule. */
export function secretPlaceholder(name: string): string {
  return `\${e2b.secrets.${name}}`
}

/**
 * Mirrors the server's name rule (E2B's own): letters, digits, `-` and `_`.
 * `.` is excluded upstream, and the check is duplicated here only so the form
 * can say so before a round trip.
 */
export const SECRET_NAME_RE = /^[a-zA-Z0-9_-]+$/
