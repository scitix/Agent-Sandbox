import { describe, expect, it } from "vitest"

import {
  ansiSpans,
  parseLogLine,
  parsedLineToMarkdown,
  prettyJson,
  stripAnsi,
  tokenizeLogLine,
} from "./log-parse"

// The real si-scheduler line this engine was built for: a klog InfoS header, a
// quoted message, plain kv pairs, a fully escape-encoded JSON payload, and the
// trace correlation. Kept verbatim as the primary fixture.
const SUBMIT_REQUEST =
  'I0729 04:13:08.156526       7 server.go:49] "Submit-request" clientIP="172.16.219.213" request="{\\"requestID\\":\\"c69665e9-3ea4-4150-98c7-8a9fd98100d4-800779fa\\",\\"metadata\\":{\\"entryID\\":\\"c69665e9-3ea4-4150-98c7-8a9fd98100d4\\",\\"user\\":\\"rhjiang\\",\\"team\\":\\"ai4s\\"},\\"namespace\\":\\"t-ai4s-rhjiang\\",\\"quotaUrl\\":\\"rhjiang.13.ai4s.shared\\",\\"taskPolicy\\":{\\"preemptable\\":false,\\"preemptMode\\":\\"\\"},\\"ttl\\":\\"5m\\"}" trace="3c7de68c963211c1c42e3595931a5a76" span="9efde3fb19fe29b1"'

describe("parseLogLine — klog", () => {
  it("parses the header, message, fields, embedded JSON and trace", () => {
    const p = parseLogLine(SUBMIT_REQUEST)

    expect(p.plain).toBe(false)
    expect(p.level).toBe("info")
    expect(p.inlineTime).toBe("07-29 04:13:08.156526")
    expect(p.threadId).toBe("7")
    expect(p.source).toBe("server.go:49")
    expect(p.message).toBe("Submit-request")

    expect(p.fields.map((f) => f.key)).toEqual(["clientIP", "request", "trace", "span"])
    expect(p.fields[0].value).toBe("172.16.219.213")

    // The escaped payload must survive the scanner whole and pretty-print.
    const request = p.fields[1]
    expect(request.json).toBeDefined()
    expect(JSON.parse(request.json as string)).toMatchObject({
      requestID: "c69665e9-3ea4-4150-98c7-8a9fd98100d4-800779fa",
      metadata: { user: "rhjiang", team: "ai4s" },
      namespace: "t-ai4s-rhjiang",
      taskPolicy: { preemptable: false },
    })
    expect(request.json).toContain("\n  ")

    expect(p.trace).toEqual({
      traceId: "3c7de68c963211c1c42e3595931a5a76",
      spanId: "9efde3fb19fe29b1",
    })
  })

  it("handles the other severities and a message with no fields", () => {
    const p = parseLogLine(
      "E0729 04:13:08.156526       1 cache.go:120] failed to bind pod: node gone",
    )
    expect(p.level).toBe("error")
    expect(p.source).toBe("cache.go:120")
    expect(p.message).toBe("failed to bind pod: node gone")
    expect(p.fields).toEqual([])
    // A klog header alone is structure, so the line is not "plain".
    expect(p.plain).toBe(false)
  })

  it("keeps prose whole rather than reading an inline = as a field", () => {
    const p = parseLogLine(
      "W0729 04:13:08.100000       7 quota.go:88] quota url rhjiang.13=shared is odd",
    )
    // Text survives past the candidate value, so this is prose, not a kv tail:
    // the message must stay intact instead of losing " is odd".
    expect(p.message).toBe("quota url rhjiang.13=shared is odd")
    expect(p.fields).toEqual([])
  })

  it("keeps a quoted value that never closes", () => {
    const p = parseLogLine('I0729 04:13:08.1 7 a.go:1] "msg" payload="{unclosed')
    expect(p.fields[0].key).toBe("payload")
    expect(p.fields[0].value).toBe("{unclosed")
  })

  it("reads an escaped newline inside a quoted value", () => {
    const p = parseLogLine('I0729 04:13:08.1 7 a.go:1] "m" err="line1\\nline2"')
    expect(p.fields[0].value).toBe("line1\nline2")
  })
})

describe("parseLogLine — non-klog", () => {
  it("parses a whole-line JSON log", () => {
    const p = parseLogLine(
      '{"level":"warn","msg":"leader lost","attempt":3,"detail":{"id":"x"},"trace":"3c7de68c963211c1c42e3595931a5a76"}',
    )
    expect(p.level).toBe("warning")
    expect(p.message).toBe("leader lost")
    expect(p.fields.map((f) => f.key)).toEqual(["attempt", "detail", "trace"])
    expect(p.fields[0].value).toBe("3")
    expect(p.fields[1].json).toBe('{\n  "id": "x"\n}')
    expect(p.trace?.traceId).toBe("3c7de68c963211c1c42e3595931a5a76")
    expect(p.trace?.spanId).toBeUndefined()
  })

  it("parses a logfmt-ish line", () => {
    const p = parseLogLine("starting up level=info port=8080")
    expect(p.message).toBe("starting up")
    expect(p.fields).toEqual([
      { key: "level", value: "info" },
      { key: "port", value: "8080" },
    ])
    expect(p.plain).toBe(false)
  })

  it("falls back to plain text", () => {
    const p = parseLogLine("a wholly unstructured line")
    expect(p.plain).toBe(true)
    expect(p.fields).toEqual([])
    expect(p.message).toBe("a wholly unstructured line")
  })

  it("never throws on hostile input", () => {
    for (const line of ["", "{", '{"a":', '"', "\\", 'k="\\']) {
      expect(() => parseLogLine(line)).not.toThrow()
    }
  })
})

describe("trace correlation", () => {
  it("ignores ids that are not OTel-width hex", () => {
    const p = parseLogLine('I0729 04:13:08.1 7 a.go:1] "m" trace="none" span="x"')
    expect(p.trace).toBeUndefined()
  })

  it("accepts a trace without a usable span", () => {
    const p = parseLogLine(
      'I0729 04:13:08.1 7 a.go:1] "m" trace="3c7de68c963211c1c42e3595931a5a76" span="zz"',
    )
    expect(p.trace).toEqual({
      traceId: "3c7de68c963211c1c42e3595931a5a76",
      spanId: undefined,
    })
  })

  it("reads the long-form keys a JSON logger emits", () => {
    const p = parseLogLine(
      '{"msg":"x","trace_id":"3c7de68c963211c1c42e3595931a5a76","span_id":"9efde3fb19fe29b1"}',
    )
    expect(p.trace).toEqual({
      traceId: "3c7de68c963211c1c42e3595931a5a76",
      spanId: "9efde3fb19fe29b1",
    })
  })
})

describe("helpers", () => {
  it("pretty-prints JSON, including the double-encoded case", () => {
    expect(prettyJson('{"a":1}')).toBe('{\n  "a": 1\n}')
    expect(prettyJson('"{\\"a\\":1}"')).toBe('{\n  "a": 1\n}')
    expect(prettyJson("plain")).toBeUndefined()
    expect(prettyJson("42")).toBeUndefined()
  })

  it("strips ANSI escapes but keeps bracketed text", () => {
    expect(stripAnsi("\u001b[31mred\u001b[0m")).toBe("red")
    expect(stripAnsi("a [bracketed] b")).toBe("a [bracketed] b")
  })

  it("renders the Ask AI attachment with the raw line last", () => {
    const md = parsedLineToMarkdown(SUBMIT_REQUEST, parseLogLine(SUBMIT_REQUEST), {
      ingestTime: "2026-07-29 12:13:08.200",
      component: "scheduler",
    })
    expect(md).toContain("- component: scheduler")
    expect(md).toContain("- ingested at: 2026-07-29 12:13:08.200")
    expect(md).toContain("- process clock: 07-29 04:13:08.156526")
    expect(md).toContain("- trace: 3c7de68c963211c1c42e3595931a5a76 (span 9efde3fb19fe29b1)")
    expect(md).toContain("## Message\n\nSubmit-request")
    expect(md).toContain("### request\n\n```json")
    expect(md.indexOf("## Raw")).toBeGreaterThan(md.indexOf("## Fields"))
    expect(md).toContain(SUBMIT_REQUEST)
  })
})

// A real SiQuota rejection: the shape whose coloring matters most in the list —
// severity, source, quoted message, then a kv tail ending in a long err= value.
const QUOTA_FAILED =
  'I0729 06:22:48.229046       7 siquota.go:161] "quota check failed, quota is not enough" task="t-skyinfer-ysheng/rsv-worker-0" quotaURL="ysheng.20.skyinfer.exclusive" err="not enough quota. url: ysheng.20.skyinfer.exclusive, resource: sci.g22-3.2xlarge. request: 4, allocated: 8, accumulated: 0, total: 8"'

describe("tokenizeLogLine", () => {
  // Read each token back as the text it covers, so these assertions are about
  // what gets colored — not about offsets nobody can check by eye.
  function tokenText(line: string) {
    return tokenizeLogLine(line).map((tk) => [tk.kind, line.slice(tk.start, tk.end)])
  }

  it("covers the klog header, message and every key/value", () => {
    expect(tokenText(QUOTA_FAILED)).toEqual([
      ["level", "I0729"],
      ["time", "06:22:48.229046"],
      ["thread", "7"],
      ["source", "siquota.go:161]"],
      ["message", '"quota check failed, quota is not enough"'],
      ["key", "task"],
      ["value", '"t-skyinfer-ysheng/rsv-worker-0"'],
      ["key", "quotaURL"],
      ["value", '"ysheng.20.skyinfer.exclusive"'],
      ["key", "err"],
      [
        "errorValue",
        '"not enough quota. url: ysheng.20.skyinfer.exclusive, resource: sci.g22-3.2xlarge. request: 4, allocated: 8, accumulated: 0, total: 8"',
      ],
    ])
  })

  it("locates the thread id even when its digits recur in the timestamp", () => {
    // Regression: the header parts used to be located by searching for their
    // text, so a thread id of "1" matched a "1" inside `04:13:01` — the clock got
    // styled as the thread and the real thread id was left unstyled.
    const line = 'I0729 04:13:01.100000       1 server.go:49] "msg"'
    expect(tokenText(line)).toEqual([
      ["level", "I0729"],
      ["time", "04:13:01.100000"],
      ["thread", "1"],
      ["source", "server.go:49]"],
      ["message", '"msg"'],
    ])
    // …and it is the standalone "1" before the source, not one inside the clock.
    const thread = tokenizeLogLine(line).find((tk) => tk.kind === "thread")
    expect(thread?.start).toBe(line.indexOf("       1") + 7)
  })

  it("carries the severity on the level token", () => {
    expect(tokenizeLogLine(QUOTA_FAILED)[0].level).toBe("info")
    expect(tokenizeLogLine('E0729 06:22:48.2 7 a.go:1] "boom"')[0].level).toBe("error")
  })

  it("ranges are sorted, non-overlapping and inside the line", () => {
    let prevEnd = 0
    for (const tk of tokenizeLogLine(QUOTA_FAILED)) {
      expect(tk.start).toBeGreaterThanOrEqual(prevEnd)
      expect(tk.end).toBeGreaterThan(tk.start)
      expect(tk.end).toBeLessThanOrEqual(QUOTA_FAILED.length)
      prevEnd = tk.end
    }
  })

  it("agrees with the structured parse about the fields", () => {
    const keys = tokenizeLogLine(QUOTA_FAILED)
      .filter((tk) => tk.kind === "key")
      .map((tk) => QUOTA_FAILED.slice(tk.start, tk.end))
    expect(keys).toEqual(parseLogLine(QUOTA_FAILED).fields.map((f) => f.key))
  })

  it("colors a header-only message and adds nothing to plain text", () => {
    expect(tokenText("E0729 04:13:08.1 7 cache.go:1] bind failed: node gone")).toEqual([
      ["level", "E0729"],
      ["time", "04:13:08.1"],
      ["thread", "7"],
      ["source", "cache.go:1]"],
      ["message", "bind failed: node gone"],
    ])
    expect(tokenizeLogLine("a wholly unstructured line")).toEqual([])
  })

  it("never throws on hostile input", () => {
    for (const line of ["", "{", 'k="\\', "I0729 ]]]"]) {
      expect(() => tokenizeLogLine(line)).not.toThrow()
    }
  })
})

describe("ansiSpans", () => {
  const ESC = "\u001b"

  it("returns the text unchanged when there are no escapes", () => {
    expect(ansiSpans("plain line")).toEqual({ text: "plain line", spans: [] })
  })

  it("separates escape-free text from the styled ranges", () => {
    const { text, spans } = ansiSpans(`before ${ESC}[31mRED${ESC}[0m after`)
    expect(text).toBe("before RED after")
    expect(spans).toEqual([{ start: 7, end: 10, color: "red", bright: false }])
    // The span covers the colored word in the escape-free coordinates.
    expect(text.slice(spans[0].start, spans[0].end)).toBe("RED")
  })

  it("accumulates and resets attributes", () => {
    const { text, spans } = ansiSpans(`${ESC}[1m${ESC}[33mwarn${ESC}[22m still${ESC}[0m done`)
    expect(text).toBe("warn still done")
    expect(spans[0]).toMatchObject({ color: "yellow", bold: true })
    expect(spans[1]).toMatchObject({ color: "yellow", bold: false })
    expect(spans).toHaveLength(2)
  })

  it("reads bright colors and drops 256-color parameters", () => {
    expect(ansiSpans(`${ESC}[91mx`).spans[0]).toMatchObject({
      color: "red",
      bright: true,
    })
    expect(ansiSpans(`${ESC}[38;5;208mx`).spans).toEqual([])
  })

  it("drops the escapes even when nothing is styled", () => {
    expect(ansiSpans(`${ESC}[0mclean`).text).toBe("clean")
    expect(ansiSpans(`${ESC}[2J${ESC}[Hcleared`).text).toBe("cleared")
  })
})
