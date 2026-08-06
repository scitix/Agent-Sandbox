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

// buildClusterMatcher turns a cluster scope into the one PromQL matcher every
// metrics route interpolates. It is the reason a multi-cluster selection costs
// no extra requests, so its edge cases are worth pinning down.

import { describe, it, expect, vi, beforeEach } from "vitest"
import type { ClusterEntry } from "@/lib/cluster-config"

const CLUSTERS: ClusterEntry[] = [
  { id: "foo", name: "Foo", url: "http://a", selector: 'cluster="prod-foo"' },
  { id: "bar", name: "Bar", url: "http://c", selector: 'cluster="prod-bar"' },
  // Multi-label selector: only the cluster label may survive, because a
  // multi-cluster scope has to collapse into a single matcher.
  { id: "baz", name: "Baz", url: "http://v", selector: 'cluster="prod-baz",region="us-west"' },
  // No selector at all — falls back to the cluster ID.
  { id: "plain", name: "Plain", url: "http://p" },
  // Selector that pins some other label; there is no cluster value to read.
  { id: "manager", name: "MA", url: "http://m", selector: 'manager="hub"' },
  // Regex metacharacters in the value must not become regex syntax.
  { id: "dotted", name: "Dotted", url: "http://d", selector: 'cluster="a.b+c"' },
]

vi.mock("@/lib/cluster-config", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/cluster-config")>()
  return {
    ...actual,
    listClusters: () => CLUSTERS,
    getClusterConfig: (id: string) => CLUSTERS.find((c) => c.id === id),
  }
})

import { buildClusterMatcher } from "@/app/api/prometheus/_shared"

describe("buildClusterMatcher", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("reads the cluster label value out of a single cluster's selector", () => {
    expect(buildClusterMatcher("bar")).toBe('cluster="prod-bar"')
  })

  it("keeps only the cluster label from a multi-label selector", () => {
    expect(buildClusterMatcher("baz")).toBe('cluster="prod-baz"')
  })

  it("falls back to the cluster ID when there is no selector", () => {
    expect(buildClusterMatcher("plain")).toBe('cluster="plain"')
  })

  it("falls back to the cluster ID when the selector pins some other label", () => {
    expect(buildClusterMatcher("manager")).toBe('cluster="manager"')
  })

  it("collapses a comma-separated scope into one regex matcher", () => {
    expect(buildClusterMatcher("foo,bar")).toBe('cluster=~"prod-foo|prod-bar"')
  })

  it("tolerates whitespace and empty entries in the list", () => {
    expect(buildClusterMatcher(" foo , , bar ")).toBe(
      'cluster=~"prod-foo|prod-bar"',
    )
  })

  it("de-duplicates repeated clusters rather than repeating alternatives", () => {
    expect(buildClusterMatcher("foo,foo")).toBe('cluster="prod-foo"')
  })

  it("expands 'all' to every configured cluster, not to an unfiltered query", () => {
    // Dropping the label instead would sweep in any other AgentBox cluster
    // reporting into the same Prometheus.
    expect(buildClusterMatcher("all")).toBe(
      'cluster=~"prod-foo|prod-bar|prod-baz|plain|manager|a\\.b\\+c"',
    )
  })

  it("escapes regex metacharacters so a value cannot act as a pattern", () => {
    expect(buildClusterMatcher("plain,dotted")).toBe('cluster=~"plain|a\\.b\\+c"')
  })
})
