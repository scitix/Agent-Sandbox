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
 * The key goes into code and nowhere else.
 *
 * A rendered document is read by people as well as run by machines: prose
 * carrying the value would put a live credential into a screenshot, a quote in
 * a chat, or a paragraph somebody forwards. These cases hold the substitution
 * to the fenced blocks.
 */

import { describe, expect, it } from "vitest"
import { API_KEY_PLACEHOLDER, fillApiKey, pickDocsApiKey } from "@/lib/utils/docs-api-key"

const KEY = "agbx_deadbeef"

describe("a document is filled in where a key is meant to be", () => {
  it("replaces the placeholder inside a fenced block", () => {
    const md = ["```python", `os.environ["E2B_API_KEY"] = "${API_KEY_PLACEHOLDER}"`, "```"].join(
      "\n",
    )
    expect(fillApiKey(md, KEY)).toContain(`"${KEY}"`)
  })

  it("leaves prose alone, placeholder and all", () => {
    const md = `Replace ${API_KEY_PLACEHOLDER} with your own key.\n\n\`\`\`bash\necho ${API_KEY_PLACEHOLDER}\n\`\`\``
    const filled = fillApiKey(md, KEY)
    expect(filled.startsWith(`Replace ${API_KEY_PLACEHOLDER} with your own key.`)).toBe(true)
    expect(filled).toContain(`echo ${KEY}`)
    // The key appears exactly once — in the code, not in the sentence.
    expect(filled.split(KEY)).toHaveLength(2)
  })

  it("handles several blocks, and tildes as well as backticks", () => {
    const md = [
      "prose",
      "```bash",
      `a=${API_KEY_PLACEHOLDER}`,
      "```",
      "between",
      "~~~python",
      `b="${API_KEY_PLACEHOLDER}"`,
      "~~~",
      "after",
    ].join("\n")
    const filled = fillApiKey(md, KEY)
    expect(filled).toContain(`a=${KEY}`)
    expect(filled).toContain(`b="${KEY}"`)
    expect(filled).toContain("prose")
    expect(filled).toContain("between")
    expect(filled).toContain("after")
  })

  it("does nothing without a key, so the placeholder is the instruction", () => {
    const md = "```bash\necho ${AGBX_API_KEY}\n```"
    expect(fillApiKey(md, undefined)).toBe(md)
    expect(fillApiKey(md, "")).toBe(md)
  })
})

describe("which key a document defaults to", () => {
  const key = (keyId: string, mode: "agent" | "unrestricted", issuedAt: string) => ({
    keyId,
    mode,
    role: "tenant",
    issuedAt,
  })
  const keys = [
    key("agent-new", "agent", "2026-01-03T00:00:00Z"),
    key("unrestricted-old", "unrestricted", "2026-01-01T00:00:00Z"),
    key("unrestricted-new", "unrestricted", "2026-01-02T00:00:00Z"),
  ]

  it("takes the newest unrestricted key for a person's document", () => {
    expect(pickDocsApiKey(keys, "unrestricted")?.keyId).toBe("unrestricted-new")
  })

  it("takes the agent key for a document that is handed to an agent", () => {
    expect(pickDocsApiKey(keys, "agent")?.keyId).toBe("agent-new")
  })

  it("falls back to whatever exists rather than to nothing", () => {
    const agentOnly = [key("agent-only", "agent", "2026-01-01T00:00:00Z")]
    expect(pickDocsApiKey(agentOnly, "unrestricted")?.keyId).toBe("agent-only")
    expect(pickDocsApiKey([], "unrestricted")).toBeUndefined()
    expect(pickDocsApiKey(undefined, "agent")).toBeUndefined()
  })
})
