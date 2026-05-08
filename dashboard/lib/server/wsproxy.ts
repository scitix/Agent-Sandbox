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
 * wsproxy internal API client — server-only.
 *
 * Centralises the WSPROXY_INTERNAL_URL / AGENTBOX_MANAGER_TOKEN env vars and
 * the callWsproxy helper that were previously duplicated across every BFF route
 * that delegates to ws-proxy.
 */

const WSPROXY_INTERNAL_URL = process.env.WSPROXY_INTERNAL_URL ?? "http://localhost:9004"
const AGENTBOX_MANAGER_TOKEN = process.env.AGENTBOX_MANAGER_TOKEN ?? ""

/** Make an authenticated request to the wsproxy internal API. */
export async function callWsproxy(path: string, method: string, body?: unknown): Promise<Response> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  }
  if (AGENTBOX_MANAGER_TOKEN) {
    headers["AGENTBOX-MANAGER-TOKEN"] = AGENTBOX_MANAGER_TOKEN
  }
  return fetch(`${WSPROXY_INTERNAL_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
}
