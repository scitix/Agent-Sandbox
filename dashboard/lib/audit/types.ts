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
 * Audit log event types.
 *
 * AuditEvent is the canonical record written for every mutating API call
 * that passes through the BFF (non-GET/HEAD cluster proxy, global API-key
 * management).  Read-only requests are intentionally excluded.
 */

export type AuditAction =
  | "api.create" //  POST  → 2xx  (cluster proxy)
  | "api.update" //  PUT / PATCH → 2xx  (cluster proxy)
  | "api.delete" //  DELETE → 2xx  (cluster proxy)
  | "api.error" //  any method → 4xx / 5xx  (cluster proxy)
  | "apikey.create" //  POST /api/global-api-keys
  | "apikey.delete" //  DELETE /api/global-api-keys/[name]
  | "template.create" //  POST /api/global-templates
  | "template.update" //  PUT /api/global-templates/[name]
  | "template.delete" //  DELETE /api/global-templates/[name]
  | "images.create" //  POST /api/images-catalog
  | "images.update" //  PUT /api/images-catalog/[id]
  | "images.delete" //  DELETE /api/images-catalog/[id]

export interface AuditActor {
  /** Username stored in the JWT (e.g. "alice") */
  user?: string
  /** Team stored in the JWT (e.g. "team-ops") */
  team?: string
  /** Role of the actual authenticated user — always reflects the real role, never the impersonated one */
  role: "admin" | "tenant"
  /** How the user authenticated: "apikey" | "oidc" | "mock" */
  authMethod?: string
  /** Display name from OIDC claims */
  name?: string
  /** Email from OIDC claims */
  email?: string
}

export interface AuditImpersonation {
  /** The user being impersonated (from X-Impersonate-User header) */
  asUser: string
  /** The team being impersonated (from X-Impersonate-Team header) */
  asTeam: string
}

export interface AuditEvent {
  /** ISO 8601 timestamp, e.g. "2026-04-02T08:15:33.421Z" */
  timestamp: string
  action: AuditAction
  /** HTTP method as sent by the client */
  method: string
  /** Request path on the backend, e.g. "/v1/sandboxpools/my-pool" */
  path: string
  /** Cluster ID from the URL (only present for cluster-proxy requests) */
  clusterID?: string
  /** HTTP status code returned to the client */
  statusCode: number
  /** The real authenticated caller */
  actor: AuditActor
  /**
   * Present only when an admin is impersonating another user via
   * X-Impersonate-Team / X-Impersonate-User headers.
   */
  impersonation?: AuditImpersonation
}
