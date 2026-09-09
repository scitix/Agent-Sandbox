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

// The assistant's `open_page` has two halves in two packages: the gateway tool
// tells the model which pages exist, and this catalog turns the answer into a
// path. Neither half fails loudly when they disagree — the model asks for a
// page that builds no route, the tool call lands, and nothing happens.

import { readFileSync } from "node:fs"
import path from "node:path"
import { describe, expect, it } from "vitest"

import {
  OPEN_PAGE_VALUES,
  buildDestination,
} from "@/lib/assistant/navigation-catalog"

const CLUSTER = "prod-foo"

describe("buildDestination", () => {
  it("opens an environment's detail page", () => {
    expect(buildDestination({ page: "env", name: "demo" }, CLUSTER)).toEqual({
      path: "/clusters/prod-foo/envs/demo",
      label: "demo",
    })
  })

  it("opens a pool through its env, which is the only way it is addressable", () => {
    expect(
      buildDestination({ page: "pool", name: "demo", pool: "demo-1c16gi" }, CLUSTER)
    ).toEqual({
      path: "/clusters/prod-foo/envs/demo/pools/demo-1c16gi",
      label: "demo / demo-1c16gi",
    })
  })

  it("falls back to the cluster the person is already viewing", () => {
    const d = buildDestination({ page: "envs" }, CLUSTER)
    expect(d?.path).toBe("/clusters/prod-foo/envs")
  })

  it("prefers a cluster the agent named over the one on screen", () => {
    const d = buildDestination({ page: "envs", cluster: "other" }, CLUSTER)
    expect(d?.path).toBe("/clusters/other/envs")
  })

  it("prefixes a non-default locale, so the jump does not switch language", () => {
    const d = buildDestination({ page: "env", name: "demo" }, CLUSTER, "zh-Hans")
    expect(d?.path).toBe("/zh-Hans/clusters/prod-foo/envs/demo")
  })

  it("escapes names rather than pasting them into a path", () => {
    const d = buildDestination({ page: "env", name: "a/b c" }, CLUSTER)
    expect(d?.path).toBe("/clusters/prod-foo/envs/a%2Fb%20c")
  })

  it.each([
    ["an unknown page", { page: "nowhere" }],
    ["a detail page with no name", { page: "env" }],
    ["a pool with no env", { page: "pool", pool: "p" }],
    ["a pool with no pool", { page: "pool", name: "demo" }],
    ["nothing at all", {}],
  ])("refuses to guess: %s", (_why, args) => {
    // Null, not an approximation. Landing on the wrong page mid-demo is worse
    // than landing on none, and the caller can say so.
    expect(buildDestination(args, CLUSTER)).toBeNull()
  })

  it("needs a cluster from somewhere", () => {
    expect(buildDestination({ page: "envs" }, undefined)).toBeNull()
  })
})

describe("the two halves agree", () => {
  // Read as text rather than imported: the gateway package pulls in the agent
  // SDK, which has no business being loaded by a dashboard test. The enum is a
  // literal array, so lifting it out of the source is exact.
  function toolEnumValues(): string[] {
    const src = readFileSync(
      path.join(__dirname, "../../../brain/gateway/open-page-tool.ts"),
      "utf8"
    )
    const block = /export const OPEN_PAGE_VALUES = \[([\s\S]*?)\] as const/.exec(src)
    expect(block, "OPEN_PAGE_VALUES not found in the gateway tool").toBeTruthy()
    return [...block![1].matchAll(/'([^']+)'/g)].map(m => m[1])
  }

  it("offers exactly the pages this catalog can build", () => {
    expect([...toolEnumValues()].sort()).toEqual([...OPEN_PAGE_VALUES].sort())
  })

  it("builds a real path for every page the tool offers", () => {
    // The detail pages need an object to point at; the rest need nothing.
    const withArgs = (page: string) =>
      page === "pool"
        ? { page, name: "e", pool: "p" }
        : ["env", "template", "sandbox"].includes(page)
          ? { page, name: "e" }
          : { page }
    for (const page of toolEnumValues()) {
      const d = buildDestination(withArgs(page), CLUSTER)
      expect(d, `no route for page "${page}"`).not.toBeNull()
      expect(d!.path.startsWith("/clusters/prod-foo/")).toBe(true)
    }
  })
})
