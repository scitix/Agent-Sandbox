// A best-effort structure extractor for one log line, used by the log detail
// panel. Framework-agnostic and side-effect free, so it is unit-testable and can
// move to @scitix/navix-headless if the CLI/assistant ever needs the same view.
//
// The engine degrades in layers and NEVER throws: a klog line yields a header +
// message + key/value fields (with embedded JSON pretty-printed), a JSON line
// yields its top-level keys, anything else comes back as plain text. Callers
// always render the raw line alongside the parse, so a mis-parse can't hide the
// truth.
//
// The shape it is built around (klog InfoS):
//   I0729 04:13:08.156526       7 server.go:49] "Submit-request" clientIP="1.2.3.4" request="{\"a\":1}" trace="3c7d…" span="9efd…"

/** klog severity letter, expanded. */
export type LogLevel = "info" | "warning" | "error" | "fatal"

export interface ParsedField {
  key: string
  /** Unescaped scalar value, as written (already un-quoted / un-escaped). */
  value: string
  /** Pretty-printed JSON, set only when `value` itself parsed as object/array. */
  json?: string
}

/** A trace correlation extracted from the line (span optional). */
export interface TraceRef {
  traceId: string
  spanId?: string
}

export interface ParsedLogLine {
  /** Severity, when the line carried a klog header or a JSON `level` key. */
  level?: LogLevel
  /**
   * The process's own clock as written in the line (`07-29 04:13:08.156526`).
   * Deliberately NOT converted to an absolute instant: klog prints no year and
   * no timezone, so any conversion would be a guess. The absolute time shown
   * next to it is the ingest timestamp, which the log service does carry.
   */
  inlineTime?: string
  /** klog's thread/goroutine id column. */
  threadId?: string
  /** klog's `file.go:line` source location. */
  source?: string
  /** The InfoS quoted message, a JSON `msg`, or the free-text remainder. */
  message?: string
  fields: ParsedField[]
  trace?: TraceRef
  /** True when no structure at all could be derived (render the raw line only). */
  plain: boolean
}

// klog header: <I|W|E|F>MMDD HH:MM:SS.uuuuuu <threadid> <file>:<line>]
const KLOG_HEADER =
  /^([IWEF])(\d{2})(\d{2})\s(\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\d+)\s+([^\s\]]+:\d+)\]\s*/

const LEVEL_BY_LETTER: Record<string, LogLevel> = {
  I: "info",
  W: "warning",
  E: "error",
  F: "fatal",
}

// Key names the observability platform uses for the correlation ids. klog's own
// convention here is `trace=`/`span=`, but JSON loggers emit the longer forms.
const TRACE_KEYS = new Set(["trace", "traceid", "trace_id"])
const SPAN_KEYS = new Set(["span", "spanid", "span_id"])

const HEX32 = /^[0-9a-f]{32}$/i
const HEX16 = /^[0-9a-f]{16}$/i

// SGR / CSI escape sequences. The log service returns plain text today, but a
// component that colorizes its own output would otherwise leak raw escapes into
// the DOM (xterm used to swallow them).

const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g

/** Remove ANSI escape sequences so a line is safe to render as DOM text. */
export function stripAnsi(s: string): string {
  return s.replace(ANSI, "")
}

/** Parse one log line. Never throws: the worst case is `{ plain: true }`. */
export function parseLogLine(log: string): ParsedLogLine {
  try {
    return parseUnsafe(stripAnsi(log).replace(/\r?\n$/, ""))
  } catch {
    return { fields: [], plain: true }
  }
}

function parseUnsafe(line: string): ParsedLogLine {
  const header = KLOG_HEADER.exec(line)
  if (header) {
    const [, letter, month, day, clock, threadId, source] = header
    const rest = line.slice(header[0].length)
    const { message, fields } = splitMessageAndFields(rest)
    return finalize({
      level: LEVEL_BY_LETTER[letter],
      inlineTime: `${month}-${day} ${clock}`,
      threadId,
      source,
      message,
      fields,
      plain: false,
    })
  }

  const asJson = parseWholeLineJson(line)
  if (asJson) return finalize(asJson)

  // No klog header and not JSON: still try the kv scanner, so a logfmt-ish line
  // (`level=info msg="…" trace=…`) gets fields and a trace link.
  const { message, fields } = splitMessageAndFields(line)
  return finalize({
    message,
    fields,
    plain: fields.length === 0,
  })
}

// ── key/value scanning ──────────────────────────────────────────────────────

// A key token: starts at a word boundary, ends at `=`. Restricted to the
// characters real loggers use, so `a=1` inside prose or a URL query does not
// shatter the message into bogus fields.
const KEY_AT = /(^|\s)([A-Za-z_][A-Za-z0-9_.\-/]*)=/g

/**
 * Split a klog message tail into the leading message and the trailing
 * `key=value` sequence. Quoted values are read with full escape awareness —
 * indispensable here, since klog serializes a nested JSON payload as
 * `request="{\"a\":1}"`, and any regex that stops at the first `"` cuts it in
 * half.
 */
function splitMessageAndFields(rest: string): {
  message?: string
  fields: ParsedField[]
} {
  const { pairs, endsClean } = scanKvRanges(rest)
  // Structured loggers put the whole kv sequence at the END of the line. If text
  // survives past the last value we mis-read prose as a field (`quota url a=b is
  // odd`), so drop the fields rather than silently swallowing that trailing text.
  if (pairs.length === 0 || !endsClean) {
    return { message: unquote(rest.trim()) || undefined, fields: [] }
  }
  const fields = pairs.map((p) => toField(rest.slice(p.keyStart, p.keyEnd), valueText(rest, p)))
  const head = rest.slice(0, pairs[0].keyStart).trim()
  return { message: unquote(head) || undefined, fields }
}

/** One `key=value` occurrence, as offsets into the scanned string. */
interface KvRange {
  keyStart: number
  keyEnd: number
  valueStart: number
  valueEnd: number
  quoted: boolean
}

/**
 * Locate the `key=value` sequence in a message tail. Range-based (rather than
 * returning strings) so both the structured parse and the syntax highlighter read
 * the SAME scan — one scanner, no chance of the colors disagreeing with the parsed
 * fields. `endsClean` reports whether the last value ends the line, which is what
 * distinguishes a real kv tail from prose containing an `=`.
 */
function scanKvRanges(s: string): { pairs: KvRange[]; endsClean: boolean } {
  const pairs: KvRange[] = []
  let cursor = 0
  while (cursor < s.length) {
    KEY_AT.lastIndex = cursor
    const m = KEY_AT.exec(s)
    if (!m) break
    const keyStart = m.index + m[1].length
    const keyEnd = keyStart + m[2].length
    const valueStart = keyEnd + 1
    const valueEnd = readValueEnd(s, valueStart)
    pairs.push({
      keyStart,
      keyEnd,
      valueStart,
      valueEnd,
      quoted: s[valueStart] === '"',
    })
    cursor = valueEnd
  }
  return { pairs, endsClean: pairs.length > 0 && s.slice(cursor).trim() === "" }
}

/**
 * End offset of the value starting at `at`: a quoted run read with full escape
 * awareness — indispensable here, since klog serializes a nested JSON payload as
 * `request="{\"a\":1}"`, and any scan that stops at the first `"` cuts it in half
 * — or a bare token up to the next space.
 */
function readValueEnd(s: string, at: number): number {
  if (s[at] === '"') {
    let i = at + 1
    while (i < s.length) {
      if (s[i] === "\\") {
        i += 2
        continue
      }
      if (s[i] === '"') break
      i += 1
    }
    return Math.min(i + 1, s.length)
  }
  let i = at
  while (i < s.length && !/\s/.test(s[i])) i += 1
  return i
}

/** The value of one scanned pair, unquoted/unescaped when it was quoted. */
function valueText(s: string, p: KvRange): string {
  const raw = s.slice(p.valueStart, p.valueEnd)
  return p.quoted ? unescapeQuoted(raw) : raw
}

/** Unescape a `"…"` token. Falls back to the literal inner text. */
function unescapeQuoted(token: string): string {
  try {
    const parsed: unknown = JSON.parse(token)
    if (typeof parsed === "string") return parsed
  } catch {
    // Not valid JSON (a lone trailing backslash, a control char) — hand-unescape.
  }
  const inner = token.replace(/^"/, "").replace(/"$/, "")
  return inner
    .replace(/\\"/g, '"')
    .replace(/\\n/g, "\n")
    .replace(/\\t/g, "\t")
    .replace(/\\\\/g, "\\")
}

/** Strip the surrounding quotes of an InfoS message, if it is one. */
function unquote(s: string): string {
  if (s.length >= 2 && s.startsWith('"') && s.endsWith('"')) {
    return unescapeQuoted(s)
  }
  return s
}

// ── value typing ────────────────────────────────────────────────────────────

function toField(key: string, value: string): ParsedField {
  const json = prettyJson(value)
  return json ? { key, value, json } : { key, value }
}

/**
 * Pretty-print a value that is itself JSON. Handles the double-encoded case
 * (a JSON string whose content is again JSON), which is exactly what klog
 * produces for a struct serialized into a quoted field.
 */
export function prettyJson(value: string): string | undefined {
  const trimmed = value.trim()
  // A leading quote is allowed so a doubly-encoded payload (a JSON string whose
  // content is again JSON) unwraps through the recursion below; an ordinary
  // quoted scalar decodes to a non-JSON string and drops out.
  if (!/^[{["]/.test(trimmed)) return undefined
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (typeof parsed === "string") return prettyJson(parsed)
    if (parsed === null || typeof parsed !== "object") return undefined
    return JSON.stringify(parsed, null, 2)
  } catch {
    return undefined
  }
}

// ── whole-line JSON ─────────────────────────────────────────────────────────

const JSON_MESSAGE_KEYS = ["msg", "message", "Message"]
const JSON_LEVEL_KEYS = ["level", "severity", "lvl"]

const LEVEL_BY_NAME: Record<string, LogLevel> = {
  info: "info",
  information: "info",
  warn: "warning",
  warning: "warning",
  error: "error",
  err: "error",
  fatal: "fatal",
  panic: "fatal",
}

function parseWholeLineJson(line: string): Omit<ParsedLogLine, "trace"> | null {
  const trimmed = line.trim()
  if (!trimmed.startsWith("{")) return null
  let obj: unknown
  try {
    obj = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (obj === null || typeof obj !== "object" || Array.isArray(obj)) return null
  const record = obj as Record<string, unknown>

  let message: string | undefined
  let level: LogLevel | undefined
  const fields: ParsedField[] = []
  for (const [key, raw] of Object.entries(record)) {
    if (message === undefined && JSON_MESSAGE_KEYS.includes(key)) {
      if (typeof raw === "string") {
        message = raw
        continue
      }
    }
    if (level === undefined && JSON_LEVEL_KEYS.includes(key)) {
      const name = String(raw).toLowerCase()
      if (LEVEL_BY_NAME[name]) {
        level = LEVEL_BY_NAME[name]
        continue
      }
    }
    fields.push(jsonField(key, raw))
  }
  return { level, message, fields, plain: false }
}

function jsonField(key: string, raw: unknown): ParsedField {
  if (typeof raw === "string") return toField(key, raw)
  if (raw === null) return { key, value: "null" }
  if (typeof raw === "object") {
    return {
      key,
      value: JSON.stringify(raw),
      json: JSON.stringify(raw, null, 2),
    }
  }
  return { key, value: String(raw) }
}

// ── trace correlation ───────────────────────────────────────────────────────

function finalize(parsed: Omit<ParsedLogLine, "trace">): ParsedLogLine {
  const trace = extractTrace(parsed.fields)
  return { ...parsed, trace }
}

/**
 * Pick the trace/span ids out of the parsed fields. Both are validated as hex of
 * the OTel width (32 / 16) so a `trace="none"` or a truncated id never becomes a
 * dead link in the panel.
 */
function extractTrace(fields: ParsedField[]): TraceRef | undefined {
  let traceId: string | undefined
  let spanId: string | undefined
  for (const f of fields) {
    const key = f.key.toLowerCase()
    if (!traceId && TRACE_KEYS.has(key) && HEX32.test(f.value.trim())) {
      traceId = f.value.trim()
    }
    if (!spanId && SPAN_KEYS.has(key) && HEX16.test(f.value.trim())) {
      spanId = f.value.trim()
    }
  }
  return traceId ? { traceId, spanId } : undefined
}

// ── syntax highlighting ─────────────────────────────────────────────────────

/** What a highlighted span of a line is. */
export type TokenKind =
  | "level"
  | "time"
  | "thread"
  | "source"
  | "message"
  | "key"
  | "value"
  | "errorValue"

/** A highlighted span, as offsets into the line. Gaps between tokens are plain. */
export interface LogToken {
  start: number
  end: number
  kind: TokenKind
  /** Severity of the line, on the `level` token only — drives its color. */
  level?: LogLevel
}

// Keys whose value is worth reading as a failure rather than as data.
const ERROR_KEYS = new Set(["err", "error", "reason", "cause"])

/**
 * Tokenize a line for syntax highlighting: the klog header parts, the quoted
 * message, and each `key=value` pair. Shares `scanKvRanges` with the structured
 * parse, so the colors can never disagree with the detail panel's fields.
 *
 * Returns sorted, non-overlapping ranges (possibly empty for an unstructured
 * line); the caller renders the gaps unstyled.
 */
export function tokenizeLogLine(line: string): LogToken[] {
  try {
    const tokens: LogToken[] = []
    let rest = line
    let base = 0

    const header = KLOG_HEADER.exec(line)
    if (header) {
      const head = header[0]
      // Offsets are WALKED, not searched. The header's parts are `<letter><MMDD>
      // <clock> <thread> <file:line>]` separated by runs of spaces, so stepping
      // through in that order gives each part's exact position. Searching for a
      // part's text instead would mislocate the thread id whenever its digits also
      // occur in the timestamp ("1" in `04:13:01`), leaving the real thread number
      // unstyled and a digit of the clock styled in its place.
      tokens.push({
        start: 0,
        end: 5,
        kind: "level",
        level: LEVEL_BY_LETTER[header[1]],
      })
      let at = 5
      for (const [group, kind] of [
        [header[4], "time"],
        [header[5], "thread"],
        [header[6], "source"],
      ] as [string, TokenKind][]) {
        while (at < head.length && /\s/.test(head[at])) at += 1
        let end = at + group.length
        // The `]` closing the source location belongs to the header, not to the
        // message that follows it — include it so the whole prefix is one color.
        if (kind === "source" && head[end] === "]") end += 1
        tokens.push({ start: at, end, kind })
        at = end
      }
      base = head.length
      rest = line.slice(base)
    }

    // A whole-line JSON object: colour its keys and values the same way the
    // key/value scanner colours a logfmt line, so the two formats read alike.
    // Walked rather than searched — a key's text often reappears inside a
    // value ("message" in a message), and searching would style the wrong run.
    const jsonTokens = tokenizeJsonLine(rest, base)
    if (jsonTokens) return jsonTokens

    const { pairs, endsClean } = scanKvRanges(rest)
    if (pairs.length > 0 && endsClean) {
      const messageEnd = pairs[0].keyStart
      if (rest.slice(0, messageEnd).trim()) {
        tokens.push({
          start: base,
          end: base + trimmedEnd(rest, messageEnd),
          kind: "message",
        })
      }
      for (const p of pairs) {
        const key = rest.slice(p.keyStart, p.keyEnd)
        tokens.push({
          start: base + p.keyStart,
          end: base + p.keyEnd,
          kind: "key",
        })
        tokens.push({
          start: base + p.valueStart,
          end: base + p.valueEnd,
          kind: ERROR_KEYS.has(key.toLowerCase()) ? "errorValue" : "value",
        })
      }
    } else if (header && rest.trim()) {
      tokens.push({ start: base, end: line.length, kind: "message" })
    }
    return tokens
  } catch {
    return []
  }
}

/**
 * Tokens for a line that is one JSON object, or null when it is not.
 *
 * Only the top level is coloured: a nested object stays one `value` run. The
 * point is to make the line's shape scannable — which key is which — not to
 * reimplement a JSON viewer, and the detail panel already pretty-prints nested
 * payloads for the line under the cursor.
 */
function tokenizeJsonLine(rest: string, base: number): LogToken[] | null {
  const lead = rest.length - rest.trimStart().length
  const body = rest.slice(lead)
  if (!body.startsWith("{") || !body.trimEnd().endsWith("}")) return null
  let obj: unknown
  try {
    obj = JSON.parse(body)
  } catch {
    return null
  }
  if (obj === null || typeof obj !== "object" || Array.isArray(obj)) return null

  const tokens: LogToken[] = []
  let at = 0
  for (const [key, raw] of Object.entries(obj as Record<string, unknown>)) {
    const quoted = JSON.stringify(key)
    const keyStart = body.indexOf(quoted, at)
    if (keyStart < 0) continue
    const keyEnd = keyStart + quoted.length
    const colon = body.indexOf(":", keyEnd)
    if (colon < 0) continue
    const valueStart =
      colon + 1 + (body.slice(colon + 1).length - body.slice(colon + 1).trimStart().length)
    const valueEnd = valueStart + JSON.stringify(raw).length
    // A re-serialized value is not always byte-identical to the source (spacing,
    // number formatting), so only trust the span when it matches what is there.
    if (body.slice(valueStart, valueEnd) !== JSON.stringify(raw)) {
      at = colon + 1
      continue
    }
    const isLevel = JSON_LEVEL_KEYS.includes(key)
    const level = isLevel ? LEVEL_BY_NAME[String(raw).toLowerCase()] : undefined
    tokens.push({ start: base + lead + keyStart, end: base + lead + keyEnd, kind: "key" })
    tokens.push({
      start: base + lead + valueStart,
      end: base + lead + valueEnd,
      kind: level === "error" || level === "fatal" ? "errorValue" : "value",
      ...(level ? { level } : {}),
    })
    at = valueEnd
  }
  return tokens.length > 0 ? tokens : null
}

/** End offset of `s.slice(0, end)` with trailing whitespace excluded. */
function trimmedEnd(s: string, end: number): number {
  let i = end
  while (i > 0 && /\s/.test(s[i - 1])) i -= 1
  return i
}

// ── ANSI colors ─────────────────────────────────────────────────────────────

/** The subset of SGR styling worth carrying into the DOM. */
export interface AnsiStyle {
  /** Base 8 color name of the foreground, when set. */
  color?: "black" | "red" | "green" | "yellow" | "blue" | "magenta" | "cyan" | "white"
  /** True for the bright (90–97) variant of `color`. */
  bright?: boolean
  bold?: boolean
  dim?: boolean
  italic?: boolean
  underline?: boolean
}

/** A styled span, as offsets into the ESCAPE-FREE text. */
export interface AnsiSpan extends AnsiStyle {
  start: number
  end: number
}

const ANSI_COLORS: AnsiStyle["color"][] = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
]

/**
 * Split a line into escape-free text plus the SGR-styled spans over it, so a
 * component that colorizes its own output renders in color instead of losing it
 * (what stripping did) or leaking raw escapes (what no handling would do).
 *
 * Only foreground color + bold/dim/italic/underline are carried: background
 * colors would fight the selected-row highlight, and 256/truecolor sequences are
 * consumed and dropped rather than approximated.
 */
export function ansiSpans(line: string): { text: string; spans: AnsiSpan[] } {
  if (!line.includes("\u001b")) return { text: line, spans: [] }
  const spans: AnsiSpan[] = []
  let text = ""
  let style: AnsiStyle = {}
  let cursor = 0
  // A private matcher: ANSI carries the `g` flag, so sharing it across calls
  // would leak lastIndex between this scan and stripAnsi's.
  const re = new RegExp(ANSI.source, "g")
  for (;;) {
    const m = re.exec(line)
    const chunkEnd = m ? m.index : line.length
    if (chunkEnd > cursor) {
      const chunk = line.slice(cursor, chunkEnd)
      if (hasStyle(style)) {
        spans.push({
          ...style,
          start: text.length,
          end: text.length + chunk.length,
        })
      }
      text += chunk
    }
    if (!m) break
    style = applySgr(style, m[0])
    cursor = m.index + m[0].length
  }
  return { text, spans }
}

function hasStyle(s: AnsiStyle): boolean {
  return !!(s.color || s.bold || s.dim || s.italic || s.underline)
}

/** Fold one escape sequence into the running style. Non-SGR sequences reset nothing. */
function applySgr(style: AnsiStyle, seq: string): AnsiStyle {
  if (!seq.endsWith("m")) return style
  const body = seq.slice(2, -1)
  const codes = body === "" ? [0] : body.split(";").map((n) => Number(n) || 0)
  let next: AnsiStyle = { ...style }
  for (let i = 0; i < codes.length; i += 1) {
    const code = codes[i]
    if (code === 0) next = {}
    else if (code === 1) next.bold = true
    else if (code === 2) next.dim = true
    else if (code === 3) next.italic = true
    else if (code === 4) next.underline = true
    else if (code === 22) next = { ...next, bold: false, dim: false }
    else if (code === 23) next.italic = false
    else if (code === 24) next.underline = false
    else if (code >= 30 && code <= 37) {
      next = { ...next, color: ANSI_COLORS[code - 30], bright: false }
    } else if (code >= 90 && code <= 97) {
      next = { ...next, color: ANSI_COLORS[code - 90], bright: true }
    } else if (code === 39) next = { ...next, color: undefined, bright: false }
    else if (code === 38 || code === 48) {
      // 256-color / truecolor: consume the parameters, keep no color.
      i += codes[i + 1] === 5 ? 2 : codes[i + 1] === 2 ? 4 : 0
    }
    // Background (40–47/100–107) and everything else: ignored on purpose.
  }
  return next
}

/**
 * Render a parsed line as Markdown — the Ask AI attachment body. The raw line is
 * kept last and verbatim, so the agent can always fall back to it.
 */
export function parsedLineToMarkdown(
  raw: string,
  parsed: ParsedLogLine,
  meta: { ingestTime?: string; component?: string; pod?: string },
): string {
  const out: string[] = []
  const head: string[] = []
  if (meta.component) head.push(`- component: ${meta.component}`)
  if (meta.pod) head.push(`- pod: ${meta.pod}`)
  if (meta.ingestTime) head.push(`- ingested at: ${meta.ingestTime}`)
  if (parsed.inlineTime) head.push(`- process clock: ${parsed.inlineTime}`)
  if (parsed.level) head.push(`- level: ${parsed.level}`)
  if (parsed.source) head.push(`- source: ${parsed.source}`)
  if (parsed.trace) {
    head.push(
      `- trace: ${parsed.trace.traceId}${
        parsed.trace.spanId ? ` (span ${parsed.trace.spanId})` : ""
      }`,
    )
  }
  if (head.length > 0) out.push(head.join("\n"))
  if (parsed.message) out.push(`## Message\n\n${parsed.message}`)
  if (parsed.fields.length > 0) {
    const body = parsed.fields
      .map((f) =>
        f.json ? `### ${f.key}\n\n\`\`\`json\n${f.json}\n\`\`\`` : `### ${f.key}\n\n${f.value}`,
      )
      .join("\n\n")
    out.push(`## Fields\n\n${body}`)
  }
  out.push(`## Raw\n\n\`\`\`\n${raw}\n\`\`\``)
  return out.join("\n\n")
}
