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
import { isDocsOnlyChange, stableStringify } from "@/lib/utils/template-crd"

// Raw YAML as the API server returns it: no apiVersion/kind, server-managed
// metadata present, omitempty-missing fields serialized as null.
const SERVER_YAML = `
metadata:
  name: py-sandbox
  resourceVersion: "180422"
  uid: 4e1f9d18-5c7a-4a9e-9d3f-2b4c6a8e0f11
  creationTimestamp: "2026-07-01T09:12:33Z"
  generation: 7
  annotations:
    agentbox.navix.sh/docs: "old docs"
    kubectl.kubernetes.io/last-applied-configuration: "{}"
spec:
  version: 1.2.3
  idleImage: busybox:1.36
  visibility: null
  template:
    spec:
      containers:
        - name: sandbox
          image: python:3.12
status:
  observedGeneration: 7
`

// What the editor submits: canonical shape from formToCrdObject.
const formYaml = (docs: string, image = "python:3.12") => `
apiVersion: agents.navix.sh/v1alpha1
kind: SandboxTemplate
metadata:
  name: py-sandbox
  resourceVersion: "180422"
  uid: 4e1f9d18-5c7a-4a9e-9d3f-2b4c6a8e0f11
  creationTimestamp: "2026-07-01T09:12:33Z"
  annotations:
    agentbox.navix.sh/docs: "${docs}"
spec:
  version: 1.2.3
  idleImage: busybox:1.36
  template:
    spec:
      containers:
        - name: sandbox
          image: ${image}
`

describe("isDocsOnlyChange", () => {
  it("treats a docs-only rewrite as docs-only across server/form YAML shapes", () => {
    expect(isDocsOnlyChange(SERVER_YAML, formYaml("brand new docs"))).toBe(true)
  })

  it("treats removing the docs entirely as docs-only", () => {
    const noDocs = formYaml("x").replace(/\n  annotations:\n.*\n/, "\n")
    expect(isDocsOnlyChange(SERVER_YAML, noDocs)).toBe(true)
  })

  it("is false when the spec changed alongside the docs", () => {
    expect(isDocsOnlyChange(SERVER_YAML, formYaml("new docs", "python:3.13"))).toBe(false)
  })

  it("is false when a non-docs annotation changed", () => {
    const withLabel = formYaml("old docs").replace(
      "    agentbox.navix.sh/docs:",
      "    team: ops\n    agentbox.navix.sh/docs:",
    )
    expect(isDocsOnlyChange(SERVER_YAML, withLabel)).toBe(false)
  })

  it("is false when either side is empty or unparseable", () => {
    expect(isDocsOnlyChange("", formYaml("d"))).toBe(false)
    expect(isDocsOnlyChange(SERVER_YAML, "")).toBe(false)
    expect(isDocsOnlyChange(SERVER_YAML, "spec: [unclosed")).toBe(false)
  })
})

describe("stableStringify", () => {
  it("is insensitive to object key order but not to array order", () => {
    expect(stableStringify({ b: 1, a: [1, 2] })).toBe(stableStringify({ a: [1, 2], b: 1 }))
    expect(stableStringify([1, 2])).not.toBe(stableStringify([2, 1]))
  })
})
