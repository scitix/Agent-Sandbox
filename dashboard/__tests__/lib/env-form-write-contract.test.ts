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

// The Env form writes the same body it reads. `GET /envs/{name}` returns
// `editable` beside the object for exactly that reason, and the assertions here
// are the client half of it: an update built from the form must carry every
// fixed field the server sent, or the PUT is refused — the refusal is the
// feature, so a body that silently drops one is a bug in this file.

import { describe, it, expect } from "vitest"

import {
  envFormDefaults,
  envToFormValues,
  formValuesToUpdateBody,
  type UpsertEnvBody,
} from "@/lib/utils/env-form"
import type { AgentSandboxEnv } from "@/lib/api/client"

function envWith(spec: Record<string, unknown>, extra: Record<string, unknown> = {}) {
  return {
    name: "env-a",
    spec: { mode: "WarmPool", templateRef: { name: "tmpl" }, ...spec },
    ...extra,
  } as unknown as AgentSandboxEnv
}

const editable: UpsertEnvBody = {
  templateRef: { name: "tmpl", version: "1.2.3" },
  mode: "WarmPool",
  overrides: { image: "img:v1" },
  labels: { "quota.example.com/url": "https://quota" },
  annotations: { "note.example.com/owner": "team-a" },
}

describe("env-form write contract", () => {
  it("reads its initial values from the editable projection", () => {
    const v = envToFormValues(envWith({ overrides: { image: "stale:v0" } }), editable)
    expect(v.templateName).toBe("tmpl")
    expect(v.image).toBe("img:v1")
  })

  it("sends every fixed field back, including the annotations the read shape omits", () => {
    const values = { ...envFormDefaults(), name: "env-a", templateName: "tmpl" }
    const body = formValuesToUpdateBody(values, envWith({}), editable)
    expect(body.templateRef).toEqual({ name: "tmpl", version: "1.2.3" })
    expect(body.mode).toBe("WarmPool")
    expect(body.labels).toEqual({ "quota.example.com/url": "https://quota" })
    expect(body.annotations).toEqual({ "note.example.com/owner": "team-a" })
  })

  it("lets the form's own edit win inside overrides", () => {
    const values = { ...envFormDefaults(), name: "env-a", templateName: "tmpl", image: "img:v2" }
    const body = formValuesToUpdateBody(values, envWith({}), editable)
    expect(body.overrides?.image).toBe("img:v2")
  })

  // A server that predates `editable` still has to be form-able, or the console
  // breaks during a rollout that is halfway through.
  it("falls back to the object when no editable projection was sent", () => {
    const env = envWith({ overrides: {} }, { labels: { "env.example.com/keep": "yes" } })
    const values = { ...envFormDefaults(), name: "env-a", templateName: "tmpl" }
    const body = formValuesToUpdateBody(values, env)
    expect(body.templateRef).toEqual({ name: "tmpl" })
    expect(body.mode).toBe("WarmPool")
    expect(body.labels).toEqual({ "env.example.com/keep": "yes" })
    expect(body.annotations).toBeUndefined()
  })
})
