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
import { resolveDashboardVersion } from "@/app/api/version/route"

describe("resolveDashboardVersion", () => {
  it("prefers the runtime value the chart injects", () => {
    // The deployed case: APP_VERSION carries the image tag, which is what the
    // About panel is meant to show.
    expect(resolveDashboardVersion("develop-abc1234", "0.0.2")).toBe("develop-abc1234")
  })

  it("falls back to the build-time constant", () => {
    // `pnpm dev` and any deployment whose chart predates APP_VERSION.
    expect(resolveDashboardVersion(undefined, "0.0.2")).toBe("0.0.2")
  })

  it("treats a blank runtime value as unset", () => {
    // A Helm value left empty renders as an empty variable, not an absent one;
    // reporting "" would blank the panel.
    expect(resolveDashboardVersion("   ", "0.0.2")).toBe("0.0.2")
  })

  it("never returns an empty string", () => {
    expect(resolveDashboardVersion(undefined, undefined)).toBe("0.0.0")
  })
})
