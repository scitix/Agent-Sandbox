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

// Typed API client for the Hub (wsproxy) — uses openapi-fetch with JWT Bearer auth.
// Proxied through the BFF /api/hub wildcard proxy, which validates the JWT and
// forwards it to wsproxy. wsproxy enforces RBAC (admin check for write paths).

import createFetchClient from "openapi-fetch"
import createClient from "openapi-react-query"
import type { paths } from "./global-schema"
import { basePath, getToken, handleErrorResponse } from "@/lib/api/client"
import type { Middleware } from "openapi-fetch"

// ─── Type aliases ─────────────────────────────────────────────────────────────

export type GlobalSandboxTemplate =
  import("./global-schema").components["schemas"]["SandboxTemplate"]
export type GlobalSandboxTemplateSummary =
  import("./global-schema").components["schemas"]["SandboxTemplateSummary"]
export type GlobalSandboxTemplateEnvelope =
  import("./global-schema").components["schemas"]["SandboxTemplateEnvelope"]
export type GlobalListSandboxTemplatesResult =
  import("./global-schema").components["schemas"]["ListSandboxTemplatesResult"]
export type GlobalUpsertSandboxTemplateRequest =
  import("./global-schema").components["schemas"]["UpsertSandboxTemplateRequest"]
export type ImageDataset = import("./global-schema").components["schemas"]["ImageDataset"]

// ─── Middleware ───────────────────────────────────────────────────────────────

function buildHubMiddleware(): Middleware {
  return {
    async onRequest({ request }) {
      const token = getToken()
      if (token) request.headers.set("Authorization", `Bearer ${token}`)
      return request
    },
    async onResponse({ response }) {
      if (!response.ok) {
        if (typeof window === "undefined") return response
        return handleErrorResponse(response)
      }
      return response
    },
  }
}

// ─── Client factory ───────────────────────────────────────────────────────────

type HubFetchClient = ReturnType<typeof createFetchClient<paths>>
type HubApiClient = ReturnType<typeof createClient<paths>>

let _hubFetchClient: HubFetchClient | undefined
let _hubApiClient: HubApiClient | undefined

export function getHubFetchClient(): HubFetchClient {
  if (!_hubFetchClient) {
    _hubFetchClient = createFetchClient<paths>({
      baseUrl: `${basePath}/api/hub`,
    })
    _hubFetchClient.use(buildHubMiddleware())
  }
  return _hubFetchClient
}

export function getHubApiClient(): HubApiClient {
  if (!_hubApiClient) {
    _hubApiClient = createClient(getHubFetchClient())
  }
  return _hubApiClient
}

/** Removes cached client instances (useful in tests). */
export function clearHubClientCache(): void {
  _hubFetchClient = undefined
  _hubApiClient = undefined
}
