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
 * Determines whether an OIDC-authenticated user should be granted the
 * "admin" role based on the DEX_OIDC_ADMINS environment variable.
 *
 * ## Format
 *
 * `DEX_OIDC_ADMINS` is a semicolon-separated list of entries:
 *
 *   org1:user1,user2;org2:user3;org3
 *
 * Each entry is `<org>[:<comma-separated-users>]`.
 *
 * - **org only** (no colon, e.g. `org1`): every user in that org is admin.
 * - **org + colon, no users** (e.g. `org1:`): same — every user in the org.
 * - **org + users** (e.g. `org1:bob,alice`): only the listed users are admin.
 *
 * Matching is **case-insensitive** for both org and username.
 */
import { matchConfigEntry } from "./config-matcher"

export function isOIDCAdmin(team: string, username: string): boolean {
  const config = process.env.DEX_OIDC_ADMINS?.trim()
  // Empty config → no one is admin (fail-safe default)
  if (!config) return false

  return matchConfigEntry({
    config,
    team,
    username,
    emptyDefault: false,
  })
}
