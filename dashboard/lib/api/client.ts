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

// Typed API client for AgentBox backend — uses openapi-fetch with JWT Bearer auth

import createFetchClient, { type Middleware } from "openapi-fetch"
import createClient from "openapi-react-query"
import { toast } from "sonner"
import { errorReportAtom, clearSessionData, store, impersonationAtom } from "@/lib/atoms"
import { getLocaleFromPath } from "@/lib/cluster-path"
import type { paths, components } from "./schema"
import type { AuthState } from "@/lib/atoms"
import type {
  ClusterEntry,
  ClusterListResponse,
  GlobalApiKeyItem,
  GlobalCreateApiKeyResult,
  LoginInput,
  IamLoginInput,
} from "./bff-types"

// Re-export BFF types so consumers can import them from this module directly.
export type {
  ClusterEntry,
  ClusterListResponse,
  GlobalApiKeyItem,
  GlobalCreateApiKeyResult,
  LoginInput,
  IamLoginInput,
}

// ─── Re-export schema components for downstream consumers ──────────────────────
export type { components }

// ─── Schema-derived type aliases (replaces lib/types/index.ts) ─────────────────

// Sandbox
export type AgentSandbox = components["schemas"]["Sandbox"] // backward-compat alias

export type AgentSandboxPool = components["schemas"]["SandboxPool"] // backward-compat alias

// Env
export type AgentSandboxEnv = components["schemas"]["SandboxEnv"]
export type AgentSandboxEnvSpec = components["schemas"]["SandboxEnvSpec"]
export type AgentEnvAutoscalingSpec = components["schemas"]["EnvAutoscalingSpec"]
export type AgentEnvAutoscalingGroup = components["schemas"]["EnvAutoscalingGroup"]
export type AgentEnvClusterMember = components["schemas"]["EnvClusterMember"]
export type AgentEnvObservedMember = components["schemas"]["EnvObservedMember"]
export type AgentEnvOverrides = components["schemas"]["EnvOverrides"]
export type AgentResourceRequirements = components["schemas"]["ResourceRequirements"]
export type AgentCreateSandboxEnvRequest = components["schemas"]["CreateSandboxEnvRequest"]
export type AgentUpdateSandboxEnvRequest = components["schemas"]["UpdateSandboxEnvRequest"]

// Template
export type AgentSandboxTemplate = components["schemas"]["SandboxTemplate"] // backward-compat alias
export type AgentSandboxTemplateSummary = components["schemas"]["SandboxTemplateSummary"]

// API Key
export type AgentboxApiKey = components["schemas"]["APIKeyItem"] // backward-compat alias
export type CreateApiKeyResponse = components["schemas"]["CreateAPIKeyResult"]

// Quota
export type QuotaItem = components["schemas"]["Quota"]

// FeatureGates — which optional providers (quota) are wired into the
// currently selected cluster. Drives feature toggles in the UI so the same
// dashboard build ships against both open-source and proprietary deployments.
export type FeatureGates = components["schemas"]["FeatureGates"]

// Sandbox Logs
export type SandboxLogEntry = components["schemas"]["SandboxLogEntry"]

// Exec Token
export type ExecTokenResponse = components["schemas"]["ExecTokenResponse"]

// ─── Hand-written types (not in OpenAPI spec) ──────────────────────────────────

export interface BackendError {
  error: string
  detail?: unknown
  errorCode?: string
}

// ─── Config ────────────────────────────────────────────────────────────────────

// NEXT_PUBLIC_BASE_PATH is injected at build time from next.config.mjs.
// It is "" for root deployments and e.g. "/agentbox" for sub-path ones.
export const basePath = process.env.NEXT_PUBLIC_BASE_PATH || ""

/**
 * Error codes that should be handled silently (no toast shown).
 * Add codes here when a specific UI flow handles the error itself.
 */
/**
 * "SANDBOX_CREATE_TIMEOUT": the BFF returns 504 when the Ingress gateway
 * (fixed 60s) drops the connection before Go backend finishes creation.
 * The create-dialog handles this case with an in-dialog amber banner, so we
 * suppress the default middleware toast to avoid showing two messages at once.
 */
export const SUPPRESSED_ERROR_CODES: string[] = [
  "SANDBOX_CREATE_TIMEOUT",
  // API_KEY_REQUIRED is handled in-page: create-pool-sheet.tsx and
  // pool-docs-sheet.tsx show a dialog guiding the user to the API Keys page.
  // Suppress the global toast to avoid showing two messages at once.
  "API_KEY_REQUIRED",
]

// ─── Auth helpers ──────────────────────────────────────────────────────────────

export function getToken(): string {
  if (typeof window !== "undefined") {
    try {
      const stored = localStorage.getItem("agentbox-auth")
      if (stored) {
        const parsed = JSON.parse(stored) as Record<string, string>
        return parsed?.token ?? ""
      }
    } catch {
      // ignore
    }
  }
  return ""
}

/**
 * Extracts the current clusterID from the browser URL.
 * Matches /clusters/{clusterID}/ in the pathname, accounting for an optional
 * basePath prefix and optional locale prefix (e.g. /agentbox/zh/clusters/my-cluster/sandboxes → "my-cluster").
 * Falls back to "default" when running server-side or when the path doesn't match.
 */
function getClusterIDFromPath(): string {
  if (typeof window === "undefined") return "default"
  // Support optional locale prefix: /zh/clusters/..., /zh-Hant/clusters/..., or /clusters/...
  const match = window.location.pathname.match(
    /(?:\/[a-z]{2}(?:-[A-Za-z]{2,8})?)?\/clusters\/([^/]+)\//,
  )
  return match?.[1] ?? "default"
}

// ─── Shared error response handler ────────────────────────────────────────────

/**
 * Parses a non-2xx response, shows a toast, and throws so react-query sees an
 * error state. Pass suppressedCodes to silence toasts for specific error codes.
 */
export async function handleErrorResponse(
  response: Response,
  suppressedCodes: string[] = [],
): Promise<never> {
  const status = response.status
  let message = `HTTP ${status}: ${response.statusText || "Request failed"}`
  let detail: unknown = undefined
  let errorCode: string | undefined = undefined

  try {
    const cloned = response.clone()
    const text = await cloned.text()
    if (text) {
      const body = JSON.parse(text) as Partial<BackendError>
      if (body.error) {
        message = body.error
        detail = body.detail
        errorCode = body.errorCode
      }
    }
  } catch {
    // keep default message
  }

  if (!(errorCode && suppressedCodes.includes(errorCode))) {
    const timestamp = new Date().toISOString()
    if (detail !== undefined) {
      toast.error(message, {
        duration: 8000,
        action: {
          label: "Details",
          onClick: () => {
            store.set(errorReportAtom, { status, message, detail, errorCode, timestamp })
          },
        },
      })
    } else {
      toast.error(message, { duration: 6000 })
    }
  }

  throw Object.assign(new Error(message), { errorCode })
}

// ─── Middleware ────────────────────────────────────────────────────────────────

function buildMiddleware(): Middleware {
  return {
    async onRequest({ request }) {
      const token = getToken()
      if (token) {
        request.headers.set("Authorization", `Bearer ${token}`)
      }
      const impersonation = store.get(impersonationAtom)
      if (impersonation?.team && impersonation?.user) {
        request.headers.set("X-Impersonate-Team", impersonation.team)
        request.headers.set("X-Impersonate-User", impersonation.user)
      }
      return request
    },
    async onResponse({ response }) {
      // On 401, clear auth state and hard-redirect to login.
      if (response.status === 401 && typeof window !== "undefined") {
        clearSessionData()
        const locale = getLocaleFromPath(window.location.pathname)
        const localeLoginPath =
          locale === "en" ? `${basePath}/login` : `${basePath}/${locale}/login`
        if (!window.location.pathname.includes("/login")) {
          const fullPath = window.location.pathname + window.location.search
          const appPath =
            basePath && fullPath.startsWith(basePath) ? fullPath.slice(basePath.length) : fullPath
          window.location.replace(`${localeLoginPath}?redirect=${encodeURIComponent(appPath)}`)
        }
        return response
      }

      if (!response.ok) {
        if (typeof window === "undefined") return response
        return handleErrorResponse(response, SUPPRESSED_ERROR_CODES)
      }

      return response
    },
  }
}

// ─── Per-cluster client caches ─────────────────────────────────────────────────

type FetchClient = ReturnType<typeof createFetchClient<paths>>
type ApiClient = ReturnType<typeof createClient<paths>>

const _fetchClientCache = new Map<string, FetchClient>()
const _apiClientCache = new Map<string, ApiClient>()

/**
 * Returns a cached openapi-fetch client scoped to the given cluster.
 * Suitable for imperative (non-hook) calls such as batch deletes.
 */
export function getFetchClient(clusterID: string): FetchClient {
  let client = _fetchClientCache.get(clusterID)
  if (!client) {
    client = createFetchClient<paths>({
      baseUrl: `${basePath}/api/clusters/${clusterID}/v1`,
    })
    client.use(buildMiddleware())
    _fetchClientCache.set(clusterID, client)
  }
  return client
}

/**
 * Returns a cached openapi-react-query client scoped to the given cluster.
 * Provides .queryOptions(), .useQuery(), .useMutation() etc.
 */
export function getApiClient(clusterID: string): ApiClient {
  let client = _apiClientCache.get(clusterID)
  if (!client) {
    client = createClient(getFetchClient(clusterID))
    _apiClientCache.set(clusterID, client)
  }
  return client
}

/** Returns the fetch client for the cluster in the current browser URL. */
export function currentFetchClient(): FetchClient {
  return getFetchClient(getClusterIDFromPath())
}

/** Returns the react-query api client for the cluster in the current browser URL. */
export function currentApiClient(): ApiClient {
  return getApiClient(getClusterIDFromPath())
}

/** Removes all cached client instances (useful in tests or after config changes). */
export function clearClientCache(): void {
  _fetchClientCache.clear()
  _apiClientCache.clear()
}

// ─── BFF functions (unauthenticated, native fetch) ─────────────────────────────

export async function getClusters(): Promise<ClusterListResponse> {
  const res = await fetch(`${basePath}/api/clusters`)
  if (!res.ok) throw new Error(`Failed to fetch clusters (${res.status})`)
  return res.json() as Promise<ClusterListResponse>
}

export async function login(input: LoginInput): Promise<AuthState> {
  const res = await fetch(`${basePath}/api/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  })
  const json = (await res.json()) as Record<string, string>
  if (!res.ok) {
    throw new Error((json.error as string) || `Login failed (${res.status})`)
  }
  const { token, role, user, team, clusterID, clusterName } = json
  return { token, role: role as AuthState["role"], user, team, clusterID, clusterName }
}

export async function iamLogin(input: IamLoginInput): Promise<AuthState> {
  const res = await fetch(`${basePath}/api/auth/mock/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  })
  const json = (await res.json()) as Record<string, string>
  if (!res.ok) {
    throw new Error((json.error as string) || `Mock login failed (${res.status})`)
  }
  const { token, role, user, team, clusterID, clusterName, authMethod, name, email } = json
  return {
    token,
    role: role as AuthState["role"],
    user,
    team,
    clusterID,
    clusterName,
    authMethod: authMethod as AuthState["authMethod"],
    name,
    email,
  }
}
