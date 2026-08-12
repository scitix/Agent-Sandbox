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

import { describe, it, expect } from "vitest"

import { breadcrumbsFor } from "@/hooks/use-breadcrumbs"
import { STANDALONE_PAGES, clusterPath, standalonePath } from "@/lib/cluster-path"
import type { TranslationKey } from "@/lib/i18n"

// The label lookup is not what these tests are about; echoing the key keeps the
// assertions readable and independent of the message catalogue.
const t = (key: TranslationKey) => key as string

describe("breadcrumbsFor", () => {
  it("labels a cluster-scoped page", () => {
    expect(breadcrumbsFor("/clusters/foo/sandboxes", "foo", "en", t)).toEqual([
      { label: "nav.sandboxes", href: undefined, isCurrent: true },
    ])
  })

  it("links the page crumb once a cluster-scoped detail route is below it", () => {
    const crumbs = breadcrumbsFor("/clusters/foo/envs/navix", "foo", "en", t)
    expect(crumbs).toEqual([
      { label: "nav.envs", href: "/clusters/foo/envs", isCurrent: false },
      { label: "navix", href: undefined, isCurrent: true },
    ])
  })

  it("labels a standalone page", () => {
    expect(breadcrumbsFor("/overview", "foo", "en", t)).toEqual([
      { label: "nav.overview", href: undefined, isCurrent: true },
    ])
  })

  // The regression this guards: the standalone matcher used to be anchored at
  // the page segment, so a standalone page with sub-routes fell through to the
  // /clusters/{id}/ matcher, missed, and returned no crumbs at all — which also
  // blanks the page title.
  it("keeps crumbs on a standalone page's detail sub-route", () => {
    const crumbs = breadcrumbsFor("/managed-agents/navix/runtime", "foo", "en", t)
    expect(crumbs).toEqual([
      { label: "nav.managedAgents", href: "/managed-agents", isCurrent: false },
      { label: "navix", href: "/managed-agents/navix", isCurrent: false },
      { label: "runtime", href: undefined, isCurrent: true },
    ])
  })

  it("carries the locale prefix into the crumb links", () => {
    const crumbs = breadcrumbsFor("/zh-Hans/managed-agents/navix/hands", "foo", "zh-Hans", t)
    expect(crumbs.map((c) => c.href)).toEqual([
      "/zh-Hans/managed-agents",
      "/zh-Hans/managed-agents/navix",
      undefined,
    ])
  })

  it("decodes a name that needed escaping in the URL", () => {
    const crumbs = breadcrumbsFor("/managed-agents/a%2Fb", "foo", "en", t)
    expect(crumbs.at(-1)?.label).toBe("a/b")
  })

  it("returns nothing for the bare cluster redirect path", () => {
    expect(breadcrumbsFor("/clusters/foo", "foo", "en", t)).toEqual([])
    expect(breadcrumbsFor("/clusters/foo/", "foo", "en", t)).toEqual([])
  })

  // clusterPath and the hook's two matchers each decide independently whether a
  // page carries a cluster in its URL. If they disagree the page crumb links
  // somewhere that 404s, so assert they agree for every standalone page.
  it("agrees with cluster-path about where a standalone page lives", () => {
    for (const page of STANDALONE_PAGES) {
      const href = standalonePath(page, "en")
      expect(clusterPath("foo", page, "en")).toBe(href)
      const crumbs = breadcrumbsFor(`${href}/x`, "foo", "en", t)
      expect(crumbs[0]?.href).toBe(href)
    }
  })
})
