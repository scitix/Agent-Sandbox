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

// The one place a server contract crosses into a component through an
// `unknown`-typed protocol field.
//
// This is tested because the failure mode has no symptom: `readCard` returning
// null renders nothing, so a gateway that had correctly stopped to ask a
// question looked exactly like one that had carried on without asking. The
// namespace was wrong for as long as it took someone to notice a question card
// that never appeared.

import { describe, expect, it } from "vitest"
import type { AgUiInterrupt } from "@assistant-ui/react-ag-ui"
import { readCard } from "@/components/assistant-ui/assistant-questions"

/** The shape `toInterrupt` in the gateway actually sends. */
function interrupt(metadata: unknown): AgUiInterrupt {
  return {
    id: "int_1",
    reason: "input_required",
    metadata,
  } as unknown as AgUiInterrupt
}

const QUESTIONS = [
  {
    key: "scope",
    question: "Current cluster or all clusters?",
    header: "Scope",
    options: [{ label: "Current", description: "Just this one" }, { label: "All" }],
  },
]

describe("readCard", () => {
  it("reads the namespace the gateway writes", () => {
    // `metadata.agentbox` — see toInterrupt in brain/gateway/server.ts. Reading
    // any other key yields null and the card silently never renders.
    const card = readCard(interrupt({ agentbox: { kind: "question", questions: QUESTIONS } }))
    expect(card?.request.questions?.[0]?.question).toBe("Current cluster or all clusters?")
    expect(card?.request.questions?.[0]?.options).toHaveLength(2)
  })

  it("still accepts a card minted before the namespace was fixed", () => {
    // A rolling deploy leaves one pod on each side for a while; an interrupt
    // already parked by the old one must not become an unanswerable run.
    const card = readCard(interrupt({ navix: { kind: "question", questions: QUESTIONS } }))
    expect(card?.request.questions).toHaveLength(1)
  })

  it("renders nothing rather than throwing on a surprise shape", () => {
    for (const bad of [undefined, {}, { agentbox: null }, { agentbox: 7 }, { agentbox: {} }, { agentbox: { questions: "no" } }]) {
      expect(readCard(interrupt(bad))).toBeNull()
    }
  })

  it("drops a question with no text and an option with no label", () => {
    const card = readCard(
      interrupt({
        agentbox: {
          questions: [
            { question: 42 },
            { question: "ok", options: [{ description: "no label" }, { label: "yes" }] },
          ],
        },
      })
    )
    expect(card?.request.questions).toHaveLength(1)
    expect(card?.request.questions?.[0]?.options).toEqual([{ label: "yes" }])
  })
})
