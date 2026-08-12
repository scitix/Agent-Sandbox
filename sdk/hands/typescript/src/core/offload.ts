// Oversized tool-output offload, harness-neutral.
//
// WHY: all of the agent's file/shell tools run in a remote sandbox and the pod
// stays light. A harness's own behaviour when a tool result is huge is to spill
// it to a LOCAL file and tell the agent to grep it — but the agent's read/grep
// run in the sandbox and cannot reach that file, and the spill bloats the pod. So
// the full output is written into the session's sandbox and the inlined result is
// replaced with a head plus a pointer to that path.
//
// Two rules keep this from doing more harm than good:
//   1. `read` is exempt (see OFFLOAD_EXEMPT). It pages itself and its target is
//      already a file in the sandbox — offloading it wrote a second copy of a
//      readable file and handed back a pointer with no content at all.
//   2. Everything else keeps a HEAD inline. A bare pointer costs the agent a
//      whole round trip before it learns anything; a head usually answers the
//      question outright, and the pointer is still there when it does not.
//
// Synchronous by design: the sandbox write completes before the rewritten result
// is returned, so the agent's next read/grep finds the file. That means it can
// also trigger the lazy sandbox cold start (SBX_READY_TIMEOUT, 300s by default) —
// any timeout between the caller and here must be larger than that.
import {
  OFFLOAD_EXEMPT,
  type SandboxToolName,
  logicalToolName,
} from './tools.ts'

/** Offload above this many bytes. Kept below the 50 KB spill threshold a harness
 *  applies to its own tool output so this always preempts the local write. */
export const THRESHOLD = 48 * 1024

/**
 * Offload above this many lines, whatever the byte size.
 *
 * A harness spills on lines as well as bytes — OpenCode's is 2000 lines OR 50 KB,
 * and it applies to plugin-provided tools, not just its own built-ins. A byte-only
 * check therefore leaves a hole: several thousand short lines can sit well under
 * 48 KB and still be spilled to a pod-side file, which is the one outcome this
 * module exists to prevent, and it fails silently — the agent is handed a path
 * that its sandbox-confined `read` cannot open.
 *
 * Raising the harness's own limits out of the way is still required (the deployment
 * notes say so), because these caps preempt rather than replace them. This is the
 * belt to that suspenders: a deployment that forgets the config still degrades to
 * a readable sandbox file instead of an unreachable pod one.
 */
export const LINE_THRESHOLD = 1500

function num(raw: string | undefined, fallback: number): number {
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : fallback
}

/** How much stays inline ahead of the pointer. Both caps apply (whichever hits
 *  first) and the head must stay comfortably under THRESHOLD, or the rewritten
 *  result would be oversized in turn. */
const HEAD_LINES = () => num(process.env.OFFLOAD_HEAD_LINES, 2000)
const HEAD_BYTES = () => num(process.env.OFFLOAD_HEAD_BYTES, 40 * 1024)

function proxyUrl(): string {
  return process.env.SBX_PROXY_URL || 'http://127.0.0.1:8765'
}

/** First HEAD_LINES lines, truncated at HEAD_BYTES, plus how much that covered. */
export function head(text: string): {
  text: string
  lines: number
  bytes: number
} {
  const all = text.split('\n')
  const kept: string[] = []
  let bytes = 0
  const maxBytes = HEAD_BYTES()
  for (const line of all.slice(0, HEAD_LINES())) {
    const size = Buffer.byteLength(line, 'utf8') + (kept.length ? 1 : 0)
    if (bytes + size > maxBytes) break
    bytes += size
    kept.push(line)
  }
  return { text: kept.join('\n'), lines: kept.length, bytes }
}

/** Is this tool result big enough, and this tool eligible, to offload? */
export function shouldOffload(toolName: string, text: string): boolean {
  if (OFFLOAD_EXEMPT.has(logicalToolName(toolName) as SandboxToolName))
    return false
  if (Buffer.byteLength(text, 'utf8') > THRESHOLD) return true
  // countLines rather than split: this runs on every tool result, including the
  // large ones, and split allocates the whole array to answer a question about
  // its length.
  return countLines(text) > LINE_THRESHOLD
}

/** Number of newline-separated lines, without materialising them. */
function countLines(text: string): number {
  if (text === '') return 0
  let lines = 1
  for (let i = text.indexOf('\n'); i !== -1; i = text.indexOf('\n', i + 1)) {
    lines++
  }
  return lines
}

export interface OffloadOutcome {
  /** What the model should see instead of the original output. */
  text: string
  /** Set when the write succeeded; the sandbox path holding the full output. */
  path?: string
  /** A one-shot daemon notice returned by the write, to be surfaced upstream. */
  notice?: string | null
  /** Total bytes of the original output. */
  bytes: number
}

/**
 * Write `text` into the session's sandbox and return the replacement result.
 *
 * Never throws: if the sandbox is unavailable the same head is returned with an
 * explanation instead of a pointer, because losing the head as well would turn a
 * degraded answer into no answer.
 */
export async function offloadToSandbox(
  sessionKey: string,
  callId: string,
  text: string
): Promise<OffloadOutcome> {
  const bytes = Buffer.byteLength(text, 'utf8')
  const path = `/tmp/tool-output/${callId}.txt`
  const h = head(text)
  const totalLines = text.split('\n').length
  try {
    const res = await fetch(
      `${proxyUrl()}/sessions/${encodeURIComponent(sessionKey)}/write`,
      {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ path, content: text }),
      }
    )
    if (!res.ok) throw new Error(`sandbox write HTTP ${res.status}`)
    // The daemon's one-shot notice (e.g. the sandbox was rebuilt) rides on THIS
    // response. It is read-and-clear, so dropping the body here would silently
    // eat a rebuild notice that no later tool call can re-deliver.
    let notice: string | null = null
    try {
      const body = (await res.json()) as { notice?: string | null }
      notice = body?.notice ?? null
    } catch {
      // A daemon that answers 200 with a non-JSON body is still a success.
    }
    return {
      text:
        h.text +
        `\n\n[showing the first ${h.lines} of ${totalLines} lines (${h.bytes} of ` +
        `${bytes} bytes). The full output was saved in the sandbox at ${path}. Use ` +
        `the read/grep/bash tools (which run in the sandbox) to inspect the rest: ` +
        `grep for what you need, or read it with offset/limit.]`,
      path,
      notice,
      bytes,
    }
  } catch (e) {
    return {
      text:
        h.text +
        `\n\n[showing the first ${h.lines} of ${totalLines} lines; the remaining ` +
        `${bytes - h.bytes} bytes are unavailable — sandbox offload failed: ${String(e)}]`,
      bytes,
    }
  }
}
