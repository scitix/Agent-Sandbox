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

// Shared ky client for all BFF (Next.js API route) requests.
//
// Provides the same error-handling behaviour as the openapi-fetch middleware:
//   - Injects Bearer token + impersonation headers on every request
//   - On 401: clears session and redirects to login
//   - On non-2xx: parses { error, detail, errorCode } body and shows a toast,
//     then re-throws so react-query receives an error state
//
// Usage:
//   import { bff } from "@/lib/api/bff-client"
//   const data = await bff.get("api/global-templates").json<{ items: Foo[] }>()
//   await bff.post("api/global-templates", { json: body }).json<{ name: string }>()

import ky from "ky"
import type { BeforeRequestHook, AfterResponseHook } from "ky"
import { toast } from "sonner"
import { errorReportAtom, clearSessionData, store, impersonationAtom } from "@/lib/atoms"
import { getLocaleFromPath } from "@/lib/cluster-path"
import { basePath, getToken } from "@/lib/api/client"

interface BackendError {
  error: string
  detail?: unknown
  errorCode?: string
}

const beforeRequest: BeforeRequestHook = ({ request }) => {
  const token = getToken()
  if (token) request.headers.set("Authorization", `Bearer ${token}`)

  const impersonation = store.get(impersonationAtom)
  if (impersonation?.team && impersonation?.user) {
    request.headers.set("X-Impersonate-Team", impersonation.team)
    request.headers.set("X-Impersonate-User", impersonation.user)
  }
}

const afterResponse: AfterResponseHook = async ({ response }) => {
  if (typeof window === "undefined") return

  if (response.status === 401) {
    clearSessionData()
    const locale = getLocaleFromPath(window.location.pathname)
    const localeLoginPath = locale === "en" ? `${basePath}/login` : `${basePath}/${locale}/login`
    if (!window.location.pathname.includes("/login")) {
      const fullPath = window.location.pathname + window.location.search
      const appPath =
        basePath && fullPath.startsWith(basePath) ? fullPath.slice(basePath.length) : fullPath
      window.location.replace(`${localeLoginPath}?redirect=${encodeURIComponent(appPath)}`)
    }
    return
  }

  if (!response.ok) {
    const status = response.status
    let message = `HTTP ${status}: ${response.statusText || "Request failed"}`
    let detail: unknown = undefined
    let errorCode: string | undefined = undefined

    try {
      const text = await response.clone().text()
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

    // ky will also throw an HTTPError; we attach our parsed fields so callers
    // can inspect them via error.message / error.errorCode if needed.
    throw Object.assign(new Error(message), { errorCode })
  }
}

// basePath may be "" in dev — ky requires a non-empty prefix, so fall back to "/"
const resolvedPrefix = basePath || "/"

export const bff = ky.create({
  prefix: resolvedPrefix,
  retry: 0,
  hooks: {
    beforeRequest: [beforeRequest],
    afterResponse: [afterResponse],
  },
})
