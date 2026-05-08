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

import { atom, createStore } from "jotai"
import { atomWithStorage, createJSONStorage } from "jotai/utils"
import type { QuotaItem, ClusterListResponse } from "@/lib/api/client"

export const store = createStore()

// Error report state for the global ErrorReportDialog
export interface ErrorReport {
  status: number
  message: string
  detail?: unknown
  errorCode?: string
  timestamp: string
}

export const errorReportAtom = atom<ErrorReport | null>(null)

// Global search state
export const globalSearchAtom = atom("")

// Sidebar collapsed state
export const sidebarCollapsedAtom = atom(false)

// Current concurrent sandbox count
export const concurrentSandboxesAtom = atom(3)

// Auto-refresh interval in seconds (for sandboxes)
export const autoRefreshIntervalAtom = atom(13)

// System operational status
export const systemStatusAtom = atom<"operational" | "degraded" | "down">("operational")

export const clusterIDAtom = atom<string>("")

// Auth state persisted in localStorage
export interface AuthState {
  token?: string
  role: "admin" | "tenant"
  user?: string
  team?: string
  clusterID?: string
  clusterName?: string
  // IAM-specific fields
  authMethod?: "apikey" | "mock" | "oidc"
  name?: string
  email?: string
}

export const authAtom = atomWithStorage<AuthState | null>("agentbox-auth", null)

/** True when the logged-in user has the admin role, regardless of impersonation state.
 *  Use this to control UI elements that must remain available to admins even while
 *  impersonating (e.g. the ImpersonationSelector itself). */
export const isActualAdminAtom = atom((get) => {
  const auth = get(authAtom)
  return auth?.role === "admin"
})

/** True when the user is acting as an admin (not impersonating anyone).
 *  Returns false while impersonation is active so Admin-only menus/pages
 *  are hidden and the experience mirrors a real tenant user. */
export const isAdminAtom = atom((get) => {
  const auth = get(authAtom)
  const impersonation = get(impersonationAtom)
  if (impersonation?.team && impersonation?.user) {
    return false
  }
  return auth?.role === "admin"
})

export const apiKeyAtom = atom((get) => {
  const auth = get(authAtom)
  if (!auth?.token) return ""
  try {
    const payload = auth.token.split(".")[1]
    if (!payload) return ""
    const decoded = JSON.parse(atob(payload.replace(/-/g, "+").replace(/_/g, "/")))
    return decoded?.apiKey ?? ""
  } catch {
    return ""
  }
})

// Cluster list fetched from BFF /api/clusters (cached in localStorage, rarely changes)
export const clustersAtom = atomWithStorage<ClusterListResponse>("agentbox-clusters", {
  clusters: [],
  multiCluster: false,
})

// Currently selected quota (persisted across sessions)
export const selectedQuotaAtom = atomWithStorage<QuotaItem | null>("agentbox-selected-quota", null)

// ── Impersonation state ───────────────────────────────────────────────────────

/**
 * Admin impersonation state — persisted to sessionStorage so it survives page
 * refreshes within the same tab but is automatically cleared when the tab is
 * closed or the user logs out.
 */
export interface ImpersonationState {
  team: string
  user: string
}

export const impersonationAtom = atomWithStorage<ImpersonationState | null>(
  "agentbox-impersonation",
  null,
  createJSONStorage(() => (typeof window !== "undefined" ? sessionStorage : localStorage)),
)

/** Derived read/write atom for the impersonation team. Writing clears user. */
export const impersonationTeamAtom = atom(
  (get) => get(impersonationAtom)?.team ?? "",
  (_get, set, team: string) => {
    set(impersonationAtom, team ? { team, user: "" } : null)
  },
)

/** Derived read/write atom for the impersonation user. */
export const impersonationUserAtom = atom(
  (get) => get(impersonationAtom)?.user ?? "",
  (get, set, user: string) => {
    const current = get(impersonationAtom)
    if (!current?.team) return
    set(impersonationAtom, { team: current.team, user })
  },
)

/** True when both team and user are filled → impersonation is active. */
export const isImpersonatingAtom = atom((get) => {
  const imp = get(impersonationAtom)
  return !!(imp?.team && imp?.user)
})

// ── Changelog / version tracking ─────────────────────────────────────────────

/**
 * The last changelog version the user explicitly acknowledged ("Got it" or
 * "Full History"). Persisted across sessions; intentionally NOT cleared on
 * logout so returning users don't see the same dialog again.
 */
export const lastSeenVersionAtom = atomWithStorage<string>("agentbox-last-seen-version", "0.0.0")

/**
 * Controls the What's New dialog visibility. Non-persisted — the detection
 * logic in ChangelogDialog re-evaluates on every page load.
 */
export const changelogDialogOpenAtom = atom<boolean>(false)

// ── Session cleanup ───────────────────────────────────────────────────────────

/**
 * Clear all persisted session data from atoms + localStorage.
 * Call this on every logout path (sidebar, general page, 401 auto-redirect).
 *
 * NOTE: lastSeenVersionAtom is intentionally excluded — it should survive
 * logout so users don't get re-prompted after re-login.
 */
export function clearSessionData() {
  store.set(authAtom, null)
  store.set(selectedQuotaAtom, null)
  store.set(clustersAtom, { clusters: [], multiCluster: false })
  store.set(impersonationAtom, null)
  if (typeof window !== "undefined") {
    localStorage.removeItem("agentbox-auth")
    localStorage.removeItem("agentbox-quotas")
    localStorage.removeItem("agentbox-selected-quota")
    localStorage.removeItem("agentbox-clusters")
    sessionStorage.removeItem("agentbox-impersonation")
  }
}
