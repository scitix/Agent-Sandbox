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
 * Zero-dependency semver comparison utilities for changelog version tracking.
 *
 * Only handles simple X.Y.Z version strings (no pre-release suffixes in
 * production builds). Dev builds use "0.0.0" or contain "-dev"/"-local" to
 * suppress the update dialog.
 */

/**
 * Compare two semver strings.
 * @returns positive if a > b, negative if a < b, 0 if equal
 */
export function compareSemver(a: string, b: string): number {
  const parseVersion = (v: string): [number, number, number] => {
    const clean = v.split("-")[0] // strip pre-release suffix
    const parts = clean.split(".").map(Number)
    return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0]
  }

  const [aMaj, aMin, aPatch] = parseVersion(a)
  const [bMaj, bMin, bPatch] = parseVersion(b)

  if (aMaj !== bMaj) return aMaj - bMaj
  if (aMin !== bMin) return aMin - bMin
  return aPatch - bPatch
}

/**
 * Returns true when `appVersion` is newer than `lastSeen` AND is a real
 * production version (not "0.0.0" or a "-dev"/"-local" build).
 *
 * @param appVersion - The version baked into the build (NEXT_PUBLIC_APP_VERSION)
 * @param lastSeen   - The version the user last acknowledged (from localStorage)
 */
export function hasUnseenVersion(appVersion: string, lastSeen: string): boolean {
  // Never show dialog for dev / placeholder builds
  if (!appVersion || appVersion === "0.0.0") return false
  if (appVersion.includes("-dev") || appVersion.includes("-local")) return false

  return compareSemver(appVersion, lastSeen) > 0
}
