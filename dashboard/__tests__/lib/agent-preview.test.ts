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

// The SSE reader, against real AG-UI frame shapes.
//
// This is the part of the conversation path that cannot be checked by looking at
// it: every failure mode here produces a plausible-looking transcript rather than
// an error. A frame boundary read wrongly drops a sentence; a heartbeat treated as
// data throws mid-turn; an unknown event type treated as fatal breaks a
// conversation the next gateway release would otherwise have kept working.

import { describe, expect, it } from "vitest"

import { runTurn, type StreamEvent } from "@/lib/agent-preview"

/** Serve a body as one or more network chunks, so frame splitting is exercised
 *  the way a real socket exercises it rather than as one tidy string. */
function streamOf(chunks: string[]): Response {
  const encoder = new TextEncoder()
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
        controller.close()
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  )
}

function frame(event: unknown): string {
  return `data: ${JSON.stringify(event)}\n\n`
}

async function collect(chunks: string[]): Promise<StreamEvent[]> {
  const original = globalThis.fetch
  globalThis.fetch = (async () => streamOf(chunks)) as typeof fetch
  try {
    const out: StreamEvent[] = []
    for await (const event of runTurn("agent", "token", { threadId: "th_1", text: "hi" })) {
      out.push(event)
    }
    return out
  } finally {
    globalThis.fetch = original
  }
}

describe("the agent run stream", () => {
  it("assembles text deltas in order", async () => {
    const events = await collect([
      frame({ type: "TEXT_MESSAGE_START", messageId: "m1" }),
      frame({ type: "TEXT_MESSAGE_CONTENT", messageId: "m1", delta: "Hel" }),
      frame({ type: "TEXT_MESSAGE_CONTENT", messageId: "m1", delta: "lo" }),
      frame({ type: "RUN_FINISHED" }),
    ])
    const text = events
      .filter((e): e is Extract<StreamEvent, { kind: "text" }> => e.kind === "text")
      .map((e) => e.delta)
      .join("")
    expect(text).toBe("Hello")
  })

  // The single most likely bug in an SSE reader: a frame split across two network
  // chunks. Splitting on newline, or parsing per chunk, loses the tail of one frame
  // and the head of the next — and the result is a transcript missing a sentence,
  // not an error anyone would notice in review.
  it("survives a frame split across chunk boundaries", async () => {
    const whole =
      frame({ type: "TEXT_MESSAGE_CONTENT", delta: "abc" }) +
      frame({ type: "TEXT_MESSAGE_CONTENT", delta: "def" })
    const cut = Math.floor(whole.length / 2)
    const events = await collect([whole.slice(0, cut), whole.slice(cut)])
    const text = events
      .filter((e): e is Extract<StreamEvent, { kind: "text" }> => e.kind === "text")
      .map((e) => e.delta)
      .join("")
    expect(text).toBe("abcdef")
  })

  // The gateway sends comment frames so an intermediary with a read timeout does not
  // decide an idle stream is dead. They carry no data field; treating one as data
  // would throw in the middle of a turn that was working.
  it("ignores heartbeat comment frames", async () => {
    const events = await collect([
      ":heartbeat\n\n",
      frame({ type: "TEXT_MESSAGE_CONTENT", delta: "x" }),
      ":heartbeat\n\n",
    ])
    expect(events.filter((e) => e.kind === "text")).toHaveLength(1)
  })

  it("reports tool calls and their results", async () => {
    const events = await collect([
      frame({ type: "TOOL_CALL_START", toolCallId: "c1", toolCallName: "bash" }),
      frame({ type: "TOOL_CALL_RESULT", toolCallId: "c1", content: "ok" }),
    ])
    expect(events).toEqual(
      expect.arrayContaining([
        { kind: "tool", id: "c1", name: "bash" },
        { kind: "tool-result", id: "c1", result: "ok" },
      ]),
    )
  })

  it("surfaces a run error as an error rather than silence", async () => {
    const events = await collect([frame({ type: "RUN_ERROR", message: "model refused" })])
    expect(events).toEqual(
      expect.arrayContaining([{ kind: "error", message: "model refused" }]),
    )
  })

  // The wire is allowed to grow. A reader that rejects an event it does not know
  // turns every gateway release into a broken conversation, so unknown types are
  // dropped and the turn continues.
  it("ignores unknown event types without breaking the turn", async () => {
    const events = await collect([
      frame({ type: "SOMETHING_NEW", payload: { whatever: true } }),
      frame({ type: "TEXT_MESSAGE_CONTENT", delta: "still here" }),
    ])
    expect(events.filter((e) => e.kind === "text")).toEqual([
      { kind: "text", delta: "still here" },
    ])
  })

  // Malformed JSON is dropped for the same reason, and must not abort the stream:
  // one bad frame costs a fragment, an exception costs the rest of the answer.
  it("drops a malformed frame and keeps reading", async () => {
    const events = await collect([
      "data: {not json\n\n",
      frame({ type: "TEXT_MESSAGE_CONTENT", delta: "after" }),
    ])
    expect(events.filter((e) => e.kind === "text")).toEqual([{ kind: "text", delta: "after" }])
  })

  // Always exactly one terminator, so the caller's `finally` runs and the composer
  // is re-enabled whether the gateway sent RUN_FINISHED or the socket just ended.
  it("ends with done even when the stream stops without RUN_FINISHED", async () => {
    const events = await collect([frame({ type: "TEXT_MESSAGE_CONTENT", delta: "x" })])
    expect(events.at(-1)).toEqual({ kind: "done" })
  })

  it("reports a failed run instead of yielding nothing", async () => {
    const original = globalThis.fetch
    globalThis.fetch = (async () =>
      new Response("no harness can serve", { status: 503 })) as typeof fetch
    try {
      const out: StreamEvent[] = []
      for await (const e of runTurn("agent", "token", { threadId: "th_1", text: "hi" })) {
        out.push(e)
      }
      expect(out[0]?.kind).toBe("error")
      expect((out[0] as { message: string }).message).toContain("no harness can serve")
    } finally {
      globalThis.fetch = original
    }
  })
})
