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

// Where the assistant may send the browser, and how each destination is spelled.
//
// Framework-free on purpose: no React, no router. The gateway's `open_page`
// tool describes this same vocabulary to the model, and this module turns what
// the model asked for into a path. Keeping the mapping in one pure function is
// what lets the two be checked against each other — a page the tool offers but
// this cannot build is a jump that silently does nothing.
//
// Adding a destination is: a `page` value here, the route it builds, and the
// same value in the tool's enum (brain/gateway/open-page-tool.ts).

import type { Locale } from "@/lib/i18n/config"
import { localePrefix } from "@/lib/cluster-path"

/**
 * The tool's name AS IT APPEARS IN THE TRANSCRIPT.
 *
 * MCP tools are namespaced by the server that offers them and the gateway
 * forwards the name untouched, so this is the full thing rather than the bare
 * `open_page` the model sees. Matching the short name finds nothing, silently:
 * the card never renders and the jump never happens.
 */
export const OPEN_PAGE_TOOL_NAME = 'mcp__agentbox-navigation__open_page'

/** What the model asks for. Mirrors the tool's argument schema exactly. */
export interface OpenPageArgs {
  page?: string
  /** Defaults to the cluster the browser is already on. */
  cluster?: string
  /** The env for `env` / `pool`, the template for `template`, the id for
   *  `sandbox`. */
  name?: string
  /** Only for `page: "pool"` — pools live inside an env. */
  pool?: string
}

export interface Destination {
  /** Dashboard-relative path, ready for `router.push`. */
  path: string
  /** What was opened, for the card and the accessible announcement. Built from
   *  the args rather than translated: these are object names, not prose. */
  label: string
}

/** Cluster-scoped pages that need nothing but a cluster. */
const LIST_PAGES = new Set([
  "overview",
  "sandboxes",
  "envs",
  "templates",
  "images",
  "datasets",
  "vault",
  "approvals",
  "quota",
  "api-keys",
  "general",
])

/** Every `page` the tool accepts. Exported so a test can hold the tool's enum
 *  and this list to the same content. */
export const OPEN_PAGE_VALUES = [
  ...LIST_PAGES,
  "env",
  "pool",
  "template",
  "sandbox",
] as const

/**
 * Turn what the model asked for into a path, or null if it cannot be built.
 *
 * Null rather than a best guess: a jump to the wrong page in the middle of a
 * demo is worse than no jump, and the caller can say "I could not open that"
 * — which the model can act on, unlike a page that merely looks wrong.
 */
export function buildDestination(
  args: OpenPageArgs | undefined,
  currentCluster: string | undefined,
  locale?: Locale
): Destination | null {
  const page = (args?.page ?? "").trim()
  const cluster = (args?.cluster || currentCluster || "").trim()
  if (!page || !cluster) return null

  const name = (args?.name ?? "").trim()
  const pool = (args?.pool ?? "").trim()
  const base = `${localePrefix(locale)}/clusters/${encodeURIComponent(cluster)}`
  const seg = (s: string) => encodeURIComponent(s)

  if (LIST_PAGES.has(page)) {
    return { path: `${base}/${page}`, label: page }
  }
  switch (page) {
    case "env":
      if (!name) return null
      return { path: `${base}/envs/${seg(name)}`, label: name }
    case "pool":
      // A pool is addressed through its env; without the env there is no route
      // to build, and guessing one would land on somebody else's pool.
      if (!name || !pool) return null
      return {
        path: `${base}/envs/${seg(name)}/pools/${seg(pool)}`,
        label: `${name} / ${pool}`,
      }
    case "template":
      if (!name) return null
      return { path: `${base}/templates/${seg(name)}`, label: name }
    case "sandbox":
      if (!name) return null
      return { path: `${base}/sandboxes/${seg(name)}`, label: name }
    default:
      return null
  }
}
