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

import { durationToSeconds, SHORT_DURATION_RE } from "@/lib/utils/duration"
import {
  buildE2BCreateBody,
  E2B_META_IMAGE,
  E2B_META_STARTUP_TIMEOUT,
  E2B_META_ALLOW_PRIVATE_NETWORKS,
} from "@/lib/utils/e2b-sandbox"

describe("durationToSeconds", () => {
  it("converts each unit the form accepts", () => {
    expect(durationToSeconds("30s")).toBe(30)
    expect(durationToSeconds("5m")).toBe(300)
    expect(durationToSeconds("1h")).toBe(3600)
  })

  it("returns undefined for empty input so the field can be omitted", () => {
    expect(durationToSeconds(undefined)).toBeUndefined()
    expect(durationToSeconds("")).toBeUndefined()
    expect(durationToSeconds("  ")).toBeUndefined()
  })

  it("rejects what it cannot convert rather than guessing", () => {
    expect(durationToSeconds("5")).toBeUndefined()
    expect(durationToSeconds("1d")).toBeUndefined()
    expect(durationToSeconds("1m30s")).toBeUndefined()
    expect(durationToSeconds("0s")).toBeUndefined()
  })

  it("accepts exactly what the form's own regex accepts", () => {
    // The two must agree: a value that validates but will not convert would be
    // silently dropped from the request instead of failing the form.
    for (const good of ["30s", "5m", "1h", ""]) {
      expect(SHORT_DURATION_RE.test(good)).toBe(true)
      if (good) expect(durationToSeconds(good)).toBeGreaterThan(0)
    }
    for (const bad of ["5", "1d", "1m30s"]) {
      expect(SHORT_DURATION_RE.test(bad)).toBe(false)
    }
  })
})

describe("buildE2BCreateBody", () => {
  it("sends the env name as templateID and nothing else when nothing is set", () => {
    expect(buildE2BCreateBody({ poolName: "slimedev" })).toEqual({ templateID: "slimedev" })
  })

  it("maps the four legacy fields onto their E2B homes", () => {
    const body = buildE2BCreateBody({
      poolName: "slimedev",
      image: "ghcr.io/org/runtime:1.2",
      startupTimeout: "2m",
      idleTimeout: "30m",
    })
    expect(body.templateID).toBe("slimedev")
    expect(body.timeout).toBe(1800)
    expect(body.metadata).toEqual({
      [E2B_META_IMAGE]: "ghcr.io/org/runtime:1.2",
      [E2B_META_STARTUP_TIMEOUT]: "120",
    })
  })

  it("keeps a cluster-qualified env reference intact", () => {
    expect(buildE2BCreateBody({ poolName: "bar::slimedev" }).templateID).toBe("bar::slimedev")
  })

  it("lets the image field win over a hand-typed reserved metadata row", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      image: "from-field:1",
      metadataRows: [{ key: E2B_META_IMAGE, value: "from-row:1" }],
    })
    expect(body.metadata?.[E2B_META_IMAGE]).toBe("from-field:1")
  })

  it("carries user metadata and env vars through", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      metadataRows: [
        { key: "owner", value: "ylli" },
        { key: "", value: "dropped" },
      ],
      envVarRows: [
        { key: "TOKEN", value: "abc" },
        { key: "  ", value: "dropped" },
      ],
    })
    expect(body.metadata).toEqual({ owner: "ylli" })
    expect(body.envVars).toEqual({ TOKEN: "abc" })
  })

  it("only sends the booleans when switched on", () => {
    expect(buildE2BCreateBody({ poolName: "e" }).autoPause).toBeUndefined()
    expect(buildE2BCreateBody({ poolName: "e", autoPause: true, secure: true })).toMatchObject({
      autoPause: true,
      secure: true,
    })
  })
})

describe("buildE2BCreateBody network policy", () => {
  it("sends no network config when unrestricted", () => {
    // An empty object would still read as "policy declared" and switch on the
    // anti-SSRF baseline, which is a different thing from unrestricted.
    const body = buildE2BCreateBody({ poolName: "e", networkPolicyMode: "unrestricted" })
    expect(body.network).toBeUndefined()
    expect(body.allow_internet_access).toBeUndefined()
  })

  it("uses allow_internet_access=false for the disable mode", () => {
    const body = buildE2BCreateBody({ poolName: "e", networkPolicyMode: "disable" })
    expect(body.allow_internet_access).toBe(false)
    expect(body.network).toBeUndefined()
  })

  it("merges domains and CIDRs into one allowOut list", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "allowlist",
      allowedDomains: "pypi.org\n*.pythonhosted.org",
      allowedCIDRs: "8.8.8.8/32",
      deniedCIDRs: "1.2.3.4/32",
    })
    // The backend's splitAllowOut() partitions these again by parsing each entry.
    expect(body.network?.allowOut).toEqual(["pypi.org", "*.pythonhosted.org", "8.8.8.8/32"])
    expect(body.network?.denyOut).toEqual(["1.2.3.4/32"])
  })

  it("de-duplicates and ignores blank entries", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "allowlist",
      allowedDomains: "pypi.org, pypi.org,\n\n ,files.pythonhosted.org",
    })
    expect(body.network?.allowOut).toEqual(["pypi.org", "files.pythonhosted.org"])
  })

  it("omits network entirely when allowlist mode has no entries", () => {
    const body = buildE2BCreateBody({ poolName: "e", networkPolicyMode: "allowlist" })
    expect(body.network).toBeUndefined()
  })
})

describe("buildE2BCreateBody credential injection", () => {
  const oneRule = [{ host: "api.example.com", headerName: "Authorization", secretName: "tok" }]

  // The wire must carry a reference, never a value. This is the whole reason the
  // form offers a picker instead of a text field, and the server refuses a
  // literal, so a regression here surfaces as a 400 rather than a leak — but the
  // shape is worth pinning anyway.
  it("emits a vault reference, not a value", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "unrestricted",
      injectionRuleRows: oneRule,
    })
    expect(body.network?.rules).toEqual({
      "api.example.com": [{ transform: { headers: { Authorization: "${e2b.secrets.tok}" } } }],
    })
  })

  it("collapses several headers on one host into a single rule", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "unrestricted",
      injectionRuleRows: [
        { host: "api.example.com", headerName: "Authorization", secretName: "tok" },
        { host: "api.example.com", headerName: "X-Org", secretName: "org" },
      ],
    })
    const rules = body.network?.rules?.["api.example.com"]
    expect(rules).toHaveLength(1)
    expect(rules?.[0]!.transform.headers).toEqual({
      Authorization: "${e2b.secrets.tok}",
      "X-Org": "${e2b.secrets.org}",
    })
  })

  // Rules ride alongside the allowlist, not instead of it: a host that is not
  // allowed out is blocked before the L7 path ever runs.
  it("carries rules alongside an allowlist", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "allowlist",
      allowedDomains: "api.example.com",
      injectionRuleRows: oneRule,
    })
    expect(body.network?.allowOut).toEqual(["api.example.com"])
    expect(Object.keys(body.network?.rules ?? {})).toEqual(["api.example.com"])
  })

  it("ignores incomplete rows rather than sending a half rule", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "unrestricted",
      injectionRuleRows: [
        { host: "api.example.com", headerName: "Authorization" }, // no secret
        { host: "", headerName: "X", secretName: "tok" }, // no host
        { host: "b.example.com", secretName: "tok" }, // no header
      ],
    })
    expect(body.network).toBeUndefined()
  })
})

describe("buildE2BCreateBody allowPrivateNetworks", () => {
  // E2B's network config has no field for it, so it rides as a reserved metadata
  // key the server consumes and strips.
  it("travels as a reserved metadata key", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "allowlist",
      allowedDomains: "op.example.com",
      allowPrivateNetworks: true,
    })
    expect(body.metadata?.[E2B_META_ALLOW_PRIVATE_NETWORKS]).toBe("true")
  })

  // With no network config there is no filter to relax, and sending the key
  // alone would be read as declaring one.
  it("is dropped when the request declares no network config", () => {
    const body = buildE2BCreateBody({
      poolName: "e",
      networkPolicyMode: "unrestricted",
      allowPrivateNetworks: true,
    })
    expect(body.network).toBeUndefined()
    expect(body.metadata?.[E2B_META_ALLOW_PRIVATE_NETWORKS]).toBeUndefined()
  })
})
