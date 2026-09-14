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

import { describe, expect, it } from "vitest"
import { PLATFORM_PROJECT, TENANT_PROJECT, projectForNamespace } from "@/lib/cluster-config"

// The same table runs against the Go implementation
// (pkg/utils/logclient.ProjectFor). Both halves query the same gateway, so a
// disagreement here shows up as the console and the API answering differently
// for the same sandbox.
describe("projectForNamespace", () => {
  it("leaves an unsharded cluster's endpoint project alone", () => {
    expect(projectForNamespace(false, "t-team-a")).toBeUndefined()
    expect(projectForNamespace(undefined, "agentbox-system")).toBeUndefined()
  })

  it("sends a tenant namespace to the tenant store", () => {
    expect(projectForNamespace(true, "t-team-a")).toBe(TENANT_PROJECT)
  })

  it("sends everything else to the platform store", () => {
    expect(projectForNamespace(true, "agentbox-system")).toBe(PLATFORM_PROJECT)
    expect(projectForNamespace(true, "default")).toBe(PLATFORM_PROJECT)
  })

  it("requires a prefix, not a substring", () => {
    expect(projectForNamespace(true, "team-t-a")).toBe(PLATFORM_PROJECT)
  })

  it("treats an empty namespace as not a tenant", () => {
    expect(projectForNamespace(true, "")).toBe(PLATFORM_PROJECT)
  })
})
