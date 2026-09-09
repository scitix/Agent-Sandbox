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
import {
  readCard,
  tokenForApprovalStatus,
} from "@/components/assistant-ui/assistant-questions"
import { transcriptToMessages } from "@/components/assistant-ui/gw/transcript"

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

// ── approval cards ───────────────────────────────────────────────────────────
//
// An approval card is a question card carrying the ids the browser needs to
// record a decision. The failure mode is the same shape as the one above and
// just as quiet: drop the block anywhere along the way and the card still
// renders, still resolves the interrupt, and the agent still retries — into a
// second refusal, because nobody ever told the platform anything.

const APPROVAL = {
  approvalId: "apr_931d62ab185ac98d",
  cluster: "foo",
  operation: "env.create",
  summary: "Create an environment (t1)",
  onceOnly: false,
  command: "abx envs create --name t1",
}

const DECISION = [
  {
    key: "decision",
    question: "Create an environment (t1)",
    header: "env.create",
    options: [
      { label: "approve_once" },
      { label: "approve_session" },
      { label: "deny" },
    ],
  },
]

describe("readCard: approvals", () => {
  it("carries the approval through", () => {
    const card = readCard(
      interrupt({
        agentbox: { kind: "permission", questions: DECISION, approval: APPROVAL },
      })
    )
    expect(card?.approval).toEqual(APPROVAL)
  })

  it("renders as a plain question when there is no approval block", () => {
    const card = readCard(
      interrupt({ agentbox: { kind: "question", questions: QUESTIONS } })
    )
    expect(card?.approval).toBeUndefined()
  })

  it.each([
    ["no id", { ...APPROVAL, approvalId: "" }],
    ["no cluster", { ...APPROVAL, cluster: undefined }],
    ["not an object", "apr_1"],
  ])(
    "ignores an approval with %s rather than offering a button that records nothing",
    (_why, approval) => {
      const card = readCard(
        interrupt({
          agentbox: { kind: "permission", questions: DECISION, approval },
        })
      )
      // Still a card — the interrupt must remain answerable either way.
      expect(card).not.toBeNull()
      expect(card?.approval).toBeUndefined()
    }
  )

  it("keeps onceOnly, which is what hides the wider scope", () => {
    const card = readCard(
      interrupt({
        agentbox: {
          kind: "permission",
          questions: DECISION,
          approval: { ...APPROVAL, onceOnly: true },
        },
      })
    )
    expect(card?.approval?.onceOnly).toBe(true)
  })
})

describe("a reloaded page keeps the approval buttons", () => {
  it("round-trips the approval from GET /interactions back into a card", () => {
    // What hydration does: a parked request is mapped by `asInterrupt` rather
    // than the gateway's `toInterrupt`, and those are two separate functions.
    // A block that survives the live stream but not this path loses its buttons
    // exactly when the person comes back to press them.
    const messages = transcriptToMessages(
      [
        {
          uuid: "m1",
          role: "assistant",
          timestamp: new Date().toISOString(),
          parts: [{ type: "text", text: "I need permission for that." }],
        } as never,
      ],
      [
        {
          requestId: "int_1",
          kind: "permission",
          questions: DECISION,
          approval: APPROVAL,
        },
      ]
    )
    const interrupts = (
      messages.at(-1)?.metadata?.custom as {
        agui?: { interrupts?: AgUiInterrupt[] }
      }
    )?.agui?.interrupts
    expect(interrupts).toHaveLength(1)
    expect(readCard(interrupts![0])?.approval).toEqual(APPROVAL)
  })
})

// ── a decision made somewhere else ───────────────────────────────────────────
//
// The card's buttons are not the only way to answer: the same request is on the
// approvals page, and in `abx`. Whichever route is taken, the conversation is
// still parked on an interrupt only the browser can settle — so the card
// follows the request rather than only pushing to it. Without that, approving
// on the page leaves the conversation stopped with nothing on screen to explain
// it, and pressing the card afterwards fails too, because the platform refuses
// to decide the same request twice.

describe("tokenForApprovalStatus", () => {
  it("keeps waiting while the request is open", () => {
    expect(tokenForApprovalStatus("pending")).toBeNull()
    expect(tokenForApprovalStatus(undefined)).toBeNull()
  })

  it("carries the conversation forward when it was approved elsewhere", () => {
    expect(tokenForApprovalStatus("approved")).toBe("approve_once")
  })

  it.each(["denied", "expired", "something-new"])(
    "treats %s as: do not run it",
    (status) => {
      // A denial, an expiry and a status this build has never heard of all mean
      // the same thing to the agent. Mapping an unknown one to "approved" would
      // be the one mistake that matters.
      expect(tokenForApprovalStatus(status)).toBe("deny")
    }
  )
})
