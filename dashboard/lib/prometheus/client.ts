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
 * Prometheus BFF Client
 *
 * Thin fetch wrapper for calling the typed Prometheus BFF endpoints.
 * All routes are GET with structured query params — no raw PromQL is sent.
 *
 * Authorization: Bearer token is automatically attached from localStorage.
 */

import { basePath, getToken } from "@/lib/api/client"
import { store, impersonationAtom } from "@/lib/atoms"

/**
 * Call a named Prometheus BFF endpoint with the given query params.
 * Throws an Error on non-2xx responses.
 */
export async function prometheusGet<T>(
  endpoint: string,
  params: Record<string, string | number | boolean | undefined>,
): Promise<T> {
  const token = getToken()
  const impersonation = store.get(impersonationAtom)

  const searchParams = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      searchParams.set(key, String(value))
    }
  }

  const url = `${basePath}/api/prometheus/${endpoint}${searchParams.size > 0 ? `?${searchParams}` : ""}`

  const headers: Record<string, string> = {}
  if (token) headers["Authorization"] = `Bearer ${token}`
  if (impersonation?.team && impersonation?.user) {
    headers["X-Impersonate-Team"] = impersonation.team
    headers["X-Impersonate-User"] = impersonation.user
  }

  const res = await fetch(url, {
    method: "GET",
    headers,
  })

  const data = (await res.json()) as T

  if (!res.ok) {
    const errMsg = (data as { error?: string }).error ?? `Prometheus BFF returned ${res.status}`
    throw new Error(errMsg)
  }

  return data
}
