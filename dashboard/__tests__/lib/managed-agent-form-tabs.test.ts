// Copyright 2026 ScitiX
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import { describe, expect, it } from "vitest"

import {
  MANAGED_AGENT_FORM_TABS,
  MANAGED_AGENT_TAB_FIELDS,
  firstErrorPath,
  firstTabWithErrors,
  tabOfField,
  tabsWithErrors,
} from "@/lib/utils/managed-agent-form-tabs"
import { managedAgentFormDefaults } from "@/lib/utils/managed-agent-form"
import type { FormValues } from "@/lib/utils/managed-agent-form"

const claude = { defaultRuntime: "claude-code" as const }
const opencode = { defaultRuntime: "opencode" as const }

/**
 * The one that earns its keep: a field added to the form without a tab would be
 * rendered nowhere and, if it were required, would fail every save with no
 * visible reason.
 */
it("every form field is claimed by a tab", () => {
  const fields = Object.keys(managedAgentFormDefaults()) as (keyof FormValues)[]
  const unclaimed = fields.filter((f) => !MANAGED_AGENT_TAB_FIELDS.includes(f))
  expect(unclaimed).toEqual([])
})

describe("tabOfField", () => {
  it("keeps the essentials on the first tab", () => {
    for (const field of [
      "name",
      "imageRepository",
      "basePrompt",
      "handsMode",
      "envName",
    ] as const) {
      expect(tabOfField(field, claude)).toBe("basics")
    }
  })

  it("puts the DEFAULT harness's connection on basics and the other one on runtime", () => {
    expect(tabOfField("claudeApiKey", claude)).toBe("basics")
    expect(tabOfField("opencodeApiKey", claude)).toBe("runtime")
    // Flip the default and the two swap: the required one must stay visible.
    expect(tabOfField("opencodeApiKey", opencode)).toBe("basics")
    expect(tabOfField("claudeApiKey", opencode)).toBe("runtime")
  })

  it("routes the rest", () => {
    expect(tabOfField("scenarios", claude)).toBe("scenarios")
    expect(tabOfField("classifierModel", claude)).toBe("classifier")
  })
})

describe("error routing", () => {
  it("finds the tabs holding errors", () => {
    const errors = { envName: { message: "x" }, classifierModel: { message: "y" } }
    expect([...tabsWithErrors(errors as never, claude)].sort()).toEqual(["basics", "classifier"])
  })

  it("jumps to the leftmost offending tab, so fixing runs front to back", () => {
    const errors = { classifierModel: { message: "y" }, envName: { message: "x" } }
    expect(firstTabWithErrors(errors as never, claude)).toBe("basics")
    expect(MANAGED_AGENT_FORM_TABS.indexOf("basics")).toBe(0)
  })

  it("reports no tab when there are no errors", () => {
    expect(firstTabWithErrors({} as never, claude)).toBeNull()
  })

  it("sees a deep array error through its top-level key", () => {
    const errors = { scenarios: [{ name: { message: "required" } }] }
    expect(firstTabWithErrors(errors as never, claude)).toBe("scenarios")
  })
})

describe("firstErrorPath", () => {
  it("returns a dotted leaf path for scrolling", () => {
    expect(firstErrorPath({ envName: { message: "required", ref: {} } })).toBe("envName")
  })

  it("descends into arrays", () => {
    expect(firstErrorPath({ scenarios: [undefined, { name: { message: "x" } }] })).toBe(
      "scenarios.1.name",
    )
  })

  it("returns null for an empty error tree", () => {
    expect(firstErrorPath({})).toBeNull()
  })
})
