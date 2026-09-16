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

// The console and the CLI read the same rule about what an update may change.
//
// It used to be written down twice: `x-immutable` in the spec, which the
// service enforces and the CLI quotes, and a `disabled` flag in a sheet, which
// nothing enforces. These assertions tie the second to the first — the console
// rule is now whatever the generated table says, so a field the API makes
// editable stops being disabled here without anybody remembering to change it.

import { describe, it, expect } from "vitest"

import { fixedWriteFields, isFixedWriteField, writeFieldRules } from "@/lib/utils/write-rules"
import { envFormDefaults, envFormSchemaFor } from "@/lib/utils/env-form"

describe("write rules come from the API schema", () => {
  it("marks the fields an env update must send back unchanged", () => {
    // The same four the server refuses to see changed and `abx update envs
    // --help` labels "fixed at create".
    expect([...fixedWriteFields("envs")].sort()).toEqual(
      ["annotations", "labels", "mode", "templateRef"].sort(),
    )
  })

  it("marks the shape of a pool, which is also its name", () => {
    expect(isFixedWriteField("pools", "instanceType")).toBe(true)
    expect(isFixedWriteField("pools", "multiplier")).toBe(true)
    expect(isFixedWriteField("pools", "inlineResources")).toBe(true)
    // …and the numbers a person edits are not shape.
    expect(isFixedWriteField("pools", "replicas")).toBe(false)
    expect(isFixedWriteField("pools", "maxReplicas")).toBe(false)
  })

  it("says so when a scaling group has nothing fixed", () => {
    // A group is bounds and policies; every one of them is editable, and a rule
    // that disabled half the form would be worse than no rule.
    expect([...fixedWriteFields("scaling-groups")]).toEqual([])
  })

  it("carries the lever, because a fixed field with no way out is a wall", () => {
    for (const rule of writeFieldRules("envs")) {
      if (!rule.fixed) continue
      expect(rule.lever, `${rule.field} is fixed but names no lever`).toBeTruthy()
    }
  })
})

describe("the env form checks the update case instead of assuming it", () => {
  const blank = envFormDefaults()

  it("lets a create choose any template", () => {
    const schema = envFormSchemaFor(null)
    const parsed = schema.safeParse({ ...blank, name: "env-a", templateName: "other" })
    expect(parsed.success).toBe(true)
  })

  it("refuses an edit that changes the template", () => {
    const original = { ...blank, name: "env-a", templateName: "tmpl" }
    const parsed = envFormSchemaFor(original).safeParse({ ...original, templateName: "other" })
    expect(parsed.success).toBe(false)
    if (parsed.success) return
    // The form shows a translated message; what matters here is which field it
    // is attached to, so the error lands under the input that caused it.
    expect(parsed.error.issues[0]?.path).toEqual(["templateName"])
    expect(parsed.error.issues[0]?.message).toBe("envs.form.errors.templateFixed")
  })

  it("accepts an edit that leaves the template alone", () => {
    const original = { ...blank, name: "env-a", templateName: "tmpl" }
    const parsed = envFormSchemaFor(original).safeParse({ ...original, gatewayEnabled: false })
    expect(parsed.success).toBe(true)
  })
})
