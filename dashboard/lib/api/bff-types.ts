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

/**
 * Hand-written types for BFF (Backend-For-Frontend) routes.
 *
 * These types describe the contracts of Next.js API routes under /app/api/,
 * which are served by the Dashboard itself and NOT part of the AgentBox backend
 * OpenAPI spec (pkg/openapi/native/openapi.yaml).
 *
 * Keep these in sync with:
 *   - dashboard/lib/cluster-config.ts  (server-side ClusterEntry, source of truth for BFF)
 *   - pkg/utils/cluster/config.go      (Go ClusterEntry — note: Go has an extra `gateway`
 *                                       field for internal cross-cluster forwarding; the
 *                                       Dashboard never needs it and it is stripped by the BFF)
 */

// ─── /api/clusters ─────────────────────────────────────────────────────────────

/**
 * A single cluster entry as returned by GET /api/clusters.
 *
 * Fields omitted vs. the server-side ClusterEntry:
 *   - `visible`  — visibility filtering is applied by the BFF before responding;
 *                  clients always receive only the clusters they are allowed to see.
 *   - `gateway`  — internal cross-cluster forwarding config (Go-only, never exposed).
 */
export interface ClusterEntry {
  id: string
  name: string
  /** Dashboard-facing base URL of the cluster's AgentBox API. */
  url: string
  /** Extra HTTP headers to forward to this cluster (e.g. Host override). */
  headers?: Record<string, string>
  /** Optional display selector / grouping label. */
  selector?: string
}

export interface ClusterListResponse {
  clusters: ClusterEntry[]
  multiCluster: boolean
}

// ─── /api/global-api-keys ──────────────────────────────────────────────────────

/** Item shape returned by GET /api/global-api-keys */
export interface GlobalApiKeyItem {
  keyId: string
  role: string
  user?: string
  team?: string
  quotaURL?: string
  description?: string
  issuedAt?: string
  expiresAt?: string
  /** Full raw token for keys with stored plaintext; absent for legacy keys. */
  rawToken?: string
}

/** Result shape returned by POST /api/global-api-keys */
export interface GlobalCreateApiKeyResult {
  rawToken: string
  keyId: string
  hashPrefix: string
  issuedAt: string
  user?: string
  team?: string
}

// ─── /api/auth/* ───────────────────────────────────────────────────────────────

export interface LoginInput {
  apiKey: string
  clusterID?: string
}

export interface IamLoginInput {
  username: string
  team: string
}
