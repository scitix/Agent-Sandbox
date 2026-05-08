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

import { useQueryClient } from "@tanstack/react-query"

const DELAY_MS = 300

export function delayedInvalidate(qc: ReturnType<typeof useQueryClient>, key: unknown[]) {
  setTimeout(() => void qc.invalidateQueries({ queryKey: key }), DELAY_MS)
}

/**
 * Returns headers for admin impersonation.
 * When an admin calls a regular endpoint with these headers, the backend
 * resolves the target user's namespace and executes the request on their behalf.
 * Non-admin callers that provide these headers have them silently ignored.
 */
export const impersonationHeaders = (team: string, user: string): Record<string, string> => ({
  "X-Impersonate-Team": team,
  "X-Impersonate-User": user,
})
