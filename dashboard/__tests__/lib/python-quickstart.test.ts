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
 * The empty-state snippet is generated for the same reason the setup guide is:
 * this repository is public, one organisation runs several deployments from it,
 * and a copied snippet either reaches THIS platform or it reaches nothing.
 */

import { describe, it, expect } from "vitest"
import { readFileSync } from "node:fs"
import { join } from "node:path"
import { buildPythonQuickstart } from "@/lib/utils/python-quickstart"

const LIVE = {
  e2bURL: "https://gw.example.test/agent-sandbox/api/e2b",
  dataURL: "https://gw.example.test/agent-sandbox/api/data",
  envName: "my-env",
}

describe("the quickstart snippet carries this deployment and no other", () => {
  it("hard-codes no host of its own", () => {
    const src = readFileSync(
      join(__dirname, "..", "..", "lib", "utils", "python-quickstart.ts"),
      "utf8",
    )
    const hosts = [...src.matchAll(/https?:\/\/([a-z0-9.-]+\.[a-z]{2,})/gi)].map((m) => m[1])
    for (const h of hosts) {
      expect(h, `python-quickstart.ts names the host ${h}`).toMatch(/^(www\.apache\.org|e2b\.dev)$/)
    }
  })

  it("points the SDK at this deployment before importing it", () => {
    const { code } = buildPythonQuickstart(LIVE)
    // patch_e2b has to run before the import or the SDK resolves e2b.dev at
    // import time and the api_url above never takes effect.
    expect(code.indexOf("patch_e2b(")).toBeLessThan(code.indexOf("from e2b import Sandbox"))
    expect(code).toContain(LIVE.e2bURL)
    // E2B_DOMAIN wants a bare host, not a URL.
    expect(code).toContain('domain="gw.example.test/agent-sandbox/api/data"')
    expect(code).not.toContain('domain="https://')
  })

  it("names an Env the caller has, not a template from e2b.dev", () => {
    const { code } = buildPythonQuickstart(LIVE)
    expect(code).toContain('Sandbox.create("my-env"')
    expect(code).not.toContain('Sandbox.create("base"')
  })

  it("declares plain http explicitly, and stays quiet when the data plane is https", () => {
    expect(
      buildPythonQuickstart({ ...LIVE, dataURL: "http://gw.example.test/data" }).code,
    ).toContain("https=False")
    expect(buildPythonQuickstart(LIVE).code).not.toContain("https=False")
  })

  it("degrades to placeholders rather than to somebody else's address", () => {
    const { code, missingGateway, missingEnv } = buildPythonQuickstart({})
    expect(missingGateway).toBe(true)
    expect(missingEnv).toBe(true)
    expect(code).toContain("https://<e2b-api>")
    expect(code).toContain("<data-plane>")
    expect(code).toContain("YOUR_ENV")
  })

  it("separates a missing Env from a missing gateway", () => {
    // The two have different fixes, so a deployment that publishes a gateway
    // must not be told it does not.
    const noEnv = buildPythonQuickstart({ e2bURL: LIVE.e2bURL, dataURL: LIVE.dataURL })
    expect(noEnv.missingGateway).toBe(false)
    expect(noEnv.missingEnv).toBe(true)
  })

  it("reports a complete snippet as complete", () => {
    const full = buildPythonQuickstart(LIVE)
    expect(full.missingGateway).toBe(false)
    expect(full.missingEnv).toBe(false)
  })
})
