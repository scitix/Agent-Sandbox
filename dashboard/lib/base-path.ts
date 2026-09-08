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
 * The deploy-time sub-path this dashboard is served under.
 *
 * Baked into the bundle at build time (NEXT_BASE_PATH → next.config's
 * basePath), so it is read from the same env var Next itself uses rather than
 * re-derived. Returns "" at the root, and never a trailing slash — callers that
 * want one use basePathPrefix().
 */
export function basePath(): string {
  const raw = process.env.NEXT_PUBLIC_BASE_PATH ?? ""
  return raw === "/" ? "" : raw.replace(/\/+$/, "")
}

/** basePath() with a trailing slash, for prefixing a relative path. */
export function basePathPrefix(): string {
  const base = basePath()
  return base ? `${base}/` : "/"
}
