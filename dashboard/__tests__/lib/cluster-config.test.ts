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
import { filterClustersByVisibility, type ClusterEntry } from "@/lib/cluster-config"

describe("filterClustersByVisibility", () => {
  const clusters: ClusterEntry[] = [
    { id: "c1", name: "Cluster 1", url: "http://c1" }, // no visible → visible to all
    { id: "c2", name: "Cluster 2", url: "http://c2", visible: "" }, // empty visible → visible to all
    { id: "c3", name: "Cluster 3", url: "http://c3", visible: "org1" }, // org only → all users in org visible
    { id: "c4", name: "Cluster 4", url: "http://c4", visible: "org1:bob,alice" }, // org + users
    { id: "c5", name: "Cluster 5", url: "http://c5", visible: "org2" }, // different org
    { id: "c6", name: "Cluster 6", url: "http://c6", visible: "org2:carol" }, // different user
  ]

  it("returns all clusters when no user context provided", () => {
    expect(filterClustersByVisibility(clusters)).toHaveLength(2) // only c1, c2 (no visible or empty)
    expect(filterClustersByVisibility(clusters, undefined, "bob")).toHaveLength(2)
    expect(filterClustersByVisibility(clusters, "org1")).toHaveLength(2)
  })

  it("returns all visible clusters for matching user in org1 org", () => {
    const result = filterClustersByVisibility(clusters, "org1", "bob")
    // c1 (no visible), c2 (empty), c3 (org=org1), c4 (org=org1 + user bob)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3", "c4"])
  })

  it("returns only org-wide visible clusters for non-listed user in org1", () => {
    const result = filterClustersByVisibility(clusters, "org1", "stranger")
    // c1, c2, c3 (org=org1, no user restriction), but NOT c4 (bob,alice only)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3"])
  })

  it("returns correct clusters for org2 org with matching user", () => {
    const result = filterClustersByVisibility(clusters, "org2", "carol")
    // c1, c2, c5 (org=org2), c6 (user carol)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c5", "c6"])
  })

  it("returns only org-wide visible clusters for non-matching user in org2", () => {
    const result = filterClustersByVisibility(clusters, "org2", "other")
    // c1, c2, c5 (org=org2), but NOT c6 (only carol)
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c5"])
  })

  it("handles case-insensitive matching", () => {
    const result = filterClustersByVisibility(clusters, "org1", "bob")
    expect(result.map((c) => c.id)).toEqual(["c1", "c2", "c3", "c4"])
  })

  it("returns empty array when no clusters match", () => {
    const result = filterClustersByVisibility(clusters, "other-org", "anyone")
    // only c1, c2 have no visible restriction
    expect(result.map((c) => c.id)).toEqual(["c1", "c2"])
  })
})
