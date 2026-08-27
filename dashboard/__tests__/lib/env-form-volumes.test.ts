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

import {
  envFormDefaults,
  envToFormValues,
  formValuesToCreateBody,
  formValuesToUpdateBody,
} from "@/lib/utils/env-form"
import type { FormValues } from "@/lib/utils/env-form"
import type { AgentSandboxEnv } from "@/lib/api/client"

function envWith(overrides: Record<string, unknown>): AgentSandboxEnv {
  return {
    name: "env-a",
    spec: {
      mode: "WarmPool",
      templateRef: { name: "tmpl" },
      overrides,
    },
  } as unknown as AgentSandboxEnv
}

function valuesWith(volumeRows: FormValues["volumeRows"]): FormValues {
  return { ...envFormDefaults(), name: "env-a", templateName: "tmpl", volumeRows }
}

describe("env-form volumes", () => {
  it("reads volumes back off the API response", () => {
    const v = envToFormValues(
      envWith({
        volumes: [
          { claimName: "ds", mountPath: "/volume/ds", readOnly: true },
          { claimName: "scratch", mountPath: "/volume/s", subPath: "a/b", readOnly: false },
        ],
      }),
    )
    expect(v.volumeRows).toEqual([
      { claimName: "ds", mountPath: "/volume/ds", subPath: undefined, readOnly: true },
      { claimName: "scratch", mountPath: "/volume/s", subPath: "a/b", readOnly: false },
    ])
  })

  // A response from an older server could omit readOnly. Defaulting to true
  // keeps the UI from presenting a dataset as writable when it is not.
  it("defaults a missing readOnly to true on read-back", () => {
    const v = envToFormValues(envWith({ volumes: [{ claimName: "ds", mountPath: "/volume/ds" }] }))
    expect(v.volumeRows[0].readOnly).toBe(true)
  })

  it("has no volume rows for a blank form or an env without volumes", () => {
    expect(envToFormValues(null).volumeRows).toEqual([])
    expect(envToFormValues(envWith({ image: "x:v1" })).volumeRows).toEqual([])
  })

  it("sends volumes on create, omitting an empty subPath", () => {
    const body = formValuesToCreateBody(
      valuesWith([
        { claimName: "ds", mountPath: "/volume/ds", subPath: undefined, readOnly: true },
      ]),
    )
    expect(body.overrides.volumes).toEqual([
      { claimName: "ds", mountPath: "/volume/ds", readOnly: true },
    ])
  })

  it("carries readOnly: false through so a writable mount is explicit on the wire", () => {
    const body = formValuesToUpdateBody(
      valuesWith([
        { claimName: "ds", mountPath: "/volume/ds", subPath: undefined, readOnly: false },
      ]),
    )
    expect(body.overrides.volumes).toEqual([
      { claimName: "ds", mountPath: "/volume/ds", readOnly: false },
    ])
  })

  // PATCH replaces the whole overrides block, so an omitted volumes key would
  // clear the mounts. Emitting [] explicitly is how the user removes the last
  // one, and emitting it always is what makes that unambiguous.
  it("always emits a volumes array, empty when nothing is mounted", () => {
    const body = formValuesToUpdateBody(valuesWith([]))
    expect(body.overrides.volumes).toEqual([])
  })

  // buildOverrides used to return undefined when every override was blank,
  // which made formValuesToUpdateBody send {} — read by the server as
  // "overrides not supplied", so clearing everything was a silent no-op.
  it("returns an overrides object even when every field is blank", () => {
    const body = formValuesToUpdateBody({ ...envFormDefaults(), name: "e", templateName: "t" })
    expect(body.overrides).toBeDefined()
    expect(typeof body.overrides).toBe("object")
  })

  it("round-trips volumes through read-back and payload build", () => {
    const original = [
      { claimName: "ds", mountPath: "/volume/ds", readOnly: true },
      { claimName: "scratch", mountPath: "/volume/s", subPath: "a", readOnly: false },
    ]
    const values = envToFormValues(envWith({ volumes: original }))
    const body = formValuesToCreateBody({ ...values, name: "env-a", templateName: "tmpl" })
    expect(body.overrides.volumes).toEqual(original)
  })
})
