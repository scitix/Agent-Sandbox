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
  impliedResourceMode,
  requiresQuota,
  resolvePoolSizingLayout,
  showsQuotaPicker,
  showsResourceModeToggle,
  templateIsBilled,
} from "@/lib/utils/pool-sizing"
import type { PoolSizingLayout } from "@/lib/utils/pool-sizing"

const gates = { quotaEnabled: true, instanceTypeEnabled: true }

function layout(poolSizing: "billed" | "free-form" | "either" | undefined): PoolSizingLayout {
  return resolvePoolSizingLayout({ poolSizing, ...gates })
}

describe("resolvePoolSizingLayout", () => {
  it("reads a billed env as the billed form", () => {
    expect(layout("billed")).toBe("billed")
  })

  it("reads an unbilled env as free-form", () => {
    expect(layout("free-form")).toBe("free-form")
  })

  it("leaves the choice to the caller when the deployment has no rule", () => {
    expect(layout("either")).toBe("caller-choice")
  })

  // An older worker, or an env whose reconciler has not stamped it yet. The
  // fallback has to be the behaviour that predates the field — in particular
  // never "billed", which would be an env nobody can add a pool to.
  it("falls back to the caller's choice when the env carries no stamp", () => {
    expect(layout(undefined)).toBe("caller-choice")
  })

  it("offers only free-form when this deployment has no instance-type catalog", () => {
    expect(
      resolvePoolSizingLayout({
        poolSizing: "either",
        quotaEnabled: false,
        instanceTypeEnabled: false,
      }),
    ).toBe("free-form")
    expect(
      resolvePoolSizingLayout({
        poolSizing: undefined,
        quotaEnabled: false,
        instanceTypeEnabled: false,
      }),
    ).toBe("free-form")
  })

  // The env's stamp is a fact about newly created pools. An existing pool's
  // shape was fixed at create, so the edit form mirrors the pool.
  it("mirrors an existing pool rather than the env when editing", () => {
    expect(
      resolvePoolSizingLayout({
        poolSizing: "billed",
        ...gates,
        existingInstanceType: "sci.c23-2",
      }),
    ).toBe("billed")
    expect(
      resolvePoolSizingLayout({ poolSizing: "billed", ...gates, existingInstanceType: "" }),
    ).toBe("free-form")
    expect(
      resolvePoolSizingLayout({
        poolSizing: "free-form",
        ...gates,
        existingInstanceType: "sci.c23-2",
      }),
    ).toBe("billed")
  })
})

describe("what each layout shows", () => {
  it("hides the resource-mode toggle wherever the env has decided", () => {
    expect(showsResourceModeToggle("billed")).toBe(false)
    expect(showsResourceModeToggle("free-form")).toBe(false)
    expect(showsResourceModeToggle("caller-choice")).toBe(true)
  })

  it("shows the quota picker only where it means something", () => {
    // A billed env must name its quota, backend or not — hiding the field on a
    // deployment that has no quota backend would leave the form unsubmittable
    // with nothing on screen to explain why.
    expect(showsQuotaPicker("billed", true)).toBe(true)
    expect(showsQuotaPicker("billed", false)).toBe(true)
    // Free-form envs refuse the quota label server-side, so offering the
    // picker would be inviting a 400.
    expect(showsQuotaPicker("free-form", true)).toBe(false)
    // The caller may name one, but only if there are any to name.
    expect(showsQuotaPicker("caller-choice", true)).toBe(true)
    expect(showsQuotaPicker("caller-choice", false)).toBe(false)
  })

  it("makes the quota required exactly where the server does", () => {
    expect(requiresQuota("billed")).toBe(true)
    expect(requiresQuota("free-form")).toBe(false)
    expect(requiresQuota("caller-choice")).toBe(false)
  })

  it("derives the resource mode, and leaves it alone only in caller-choice", () => {
    expect(impliedResourceMode("billed")).toBe("instanceType")
    expect(impliedResourceMode("free-form")).toBe("manual")
    expect(impliedResourceMode("caller-choice")).toBeUndefined()
  })
})

describe("templateIsBilled", () => {
  it("is true only for the templates whose pools spend quota", () => {
    expect(templateIsBilled({ poolSizing: "billed" })).toBe(true)
    expect(templateIsBilled({ poolSizing: "free-form" })).toBe(false)
    expect(templateIsBilled({ poolSizing: "either" })).toBe(false)
    expect(templateIsBilled({})).toBe(false)
  })
})
