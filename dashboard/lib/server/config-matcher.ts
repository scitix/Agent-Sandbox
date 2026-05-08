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
 * Generic config matcher for semicolon-separated entries.
 * Format: "org1:user1,user2;org2:user3;org3"
 *
 * This is shared by:
 * - lib/server/oidc-admins.ts (for admin role detection)
 * - lib/cluster-config.ts (for cluster visibility filtering)
 */

export interface MatchConfigOptions {
  /** The config string to match against */
  config: string
  /** Organization/team to match */
  team: string
  /** Username to match */
  username: string
  /** When config is empty/undefined, return this default value */
  emptyDefault: boolean
}

/**
 * Checks if a team+username matches a config string.
 * Format: "org:user1,user2;org2:user3;org3"
 * - org only → all users in that org match
 * - org:user1,user2 → only listed users match
 * - Empty config → returns emptyDefault
 * Matching is case-insensitive.
 */
export function matchConfigEntry({
  config,
  team,
  username,
  emptyDefault,
}: MatchConfigOptions): boolean {
  const trimmed = config?.trim()

  // Empty or undefined → return default behavior
  if (!trimmed) return emptyDefault

  const teamLower = team.toLowerCase()
  const userLower = username.toLowerCase()

  for (const entry of trimmed.split(";")) {
    const entryTrimmed = entry.trim()
    if (!entryTrimmed) continue

    const colonIdx = entryTrimmed.indexOf(":")

    if (colonIdx === -1) {
      // No colon — entire org matches
      if (entryTrimmed.toLowerCase() === teamLower) return true
      continue
    }

    const org = entryTrimmed.slice(0, colonIdx).trim().toLowerCase()
    if (org !== teamLower) continue

    const usersRaw = entryTrimmed.slice(colonIdx + 1).trim()

    // Colon present but no users listed → entire org matches
    if (!usersRaw) return true

    const users = usersRaw.split(",").map((u) => u.trim().toLowerCase())
    if (users.includes(userLower)) return true
  }

  return false
}
