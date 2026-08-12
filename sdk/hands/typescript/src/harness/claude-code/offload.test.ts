// Gate G4: the offload contract.
//
// Replaces hack/verify-offload.sh, which greps the OpenCode binary for a config
// key and is therefore meaningless the moment the harness changes. The property
// that actually matters is testable directly:
//
//   * `read` stays exempt under every binding's naming;
//   * an oversized result is written to the sandbox and replaced by head+pointer;
//   * the daemon's one-shot notice on that write is surfaced, not swallowed;
//   * a sandbox failure degrades to head+reason rather than losing everything;
//   * (live) Claude Code hands PostToolUse the UNTRUNCATED result, and
//     `updatedToolOutput` really does replace what the model sees.
//
// The live half is what proves we are not repeating the opencode 1.17.5 trap
// where truncation ran before the hook could preempt it.
import { createSdkMcpServer, query, tool } from '@anthropic-ai/claude-agent-sdk'
import { mkdtempSync } from 'node:fs'
import { createServer } from 'node:http'
import type { Server } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { z } from 'zod'

import { sandboxPostToolUseHook } from './bind.ts'
import { THRESHOLD, head, offloadToSandbox, shouldOffload } from '../../core/offload.ts'

// Line-oriented, like real tool output. 1 KB per line, past the threshold.
const BIG = Array.from(
  { length: 60 },
  (_, i) => `line${i} ${'x'.repeat(1000)}`
).join('\n')
// A single line longer than the head byte cap — the pathological shape.
const ONE_HUGE_LINE = 'y'.repeat(THRESHOLD + 1024)

describe('offload decision', () => {
  it('exempts read under every binding prefix, offloads the rest', () => {
    expect(shouldOffload('read', BIG)).toBe(false)
    expect(shouldOffload('mcp__sandbox__read', BIG)).toBe(false)
    expect(shouldOffload('bash', BIG)).toBe(true)
    expect(shouldOffload('mcp__sandbox__bash', BIG)).toBe(true)
  })

  it('leaves anything at or below the threshold alone', () => {
    expect(shouldOffload('bash', 'y'.repeat(THRESHOLD))).toBe(false)
    expect(shouldOffload('bash', 'y'.repeat(THRESHOLD + 1))).toBe(true)
  })

  it('keeps the head comfortably under the threshold', () => {
    const h = head(BIG)
    expect(h.bytes).toBeLessThan(THRESHOLD)
    expect(h.lines).toBeGreaterThan(0)
  })

  // Documented wart, carried over unchanged from the OpenCode plugin: the head
  // is built line by line, so a single line larger than the head byte cap keeps
  // NOTHING inline and the agent gets only the pointer. Rare in practice (tool
  // output is line-oriented) but worth recording rather than discovering.
  it('yields an empty head for one line bigger than the cap', () => {
    const h = head(ONE_HUGE_LINE)
    expect(h.lines).toBe(0)
    expect(h.text).toBe('')
  })
})

describe('offload to the sandbox', () => {
  let server: Server
  let mode: 'ok' | 'notice' | 'fail' = 'ok'
  const writes: { path?: string; bytes: number }[] = []

  beforeAll(async () => {
    server = createServer((req, res) => {
      const chunks: Buffer[] = []
      req.on('data', c => chunks.push(c as Buffer))
      req.on('end', () => {
        const body = JSON.parse(Buffer.concat(chunks).toString('utf-8')) as {
          path?: string
          content?: string
        }
        writes.push({
          path: body.path,
          bytes: Buffer.byteLength(body.content ?? '', 'utf8'),
        })
        if (mode === 'fail') {
          res.writeHead(500)
          res.end('nope')
          return
        }
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end(
          JSON.stringify(
            mode === 'notice' ? { notice: 'the sandbox was rebuilt' } : {}
          )
        )
      })
    })
    await new Promise<void>(r => server.listen(0, '127.0.0.1', r))
    const addr = server.address()
    const port = typeof addr === 'object' && addr ? addr.port : 0
    process.env.SBX_PROXY_URL = `http://127.0.0.1:${port}`
  })
  afterAll(async () => {
    await new Promise<void>(r => server.close(() => r()))
  })

  it('writes the full output and returns head plus a pointer', async () => {
    mode = 'ok'
    writes.length = 0
    const r = await offloadToSandbox('ses_a', 'call_1', BIG)
    expect(writes).toHaveLength(1)
    expect(writes[0]?.path).toBe('/tmp/tool-output/call_1.txt')
    // The whole thing went to the sandbox, not a truncated copy.
    expect(writes[0]?.bytes).toBe(Buffer.byteLength(BIG, 'utf8'))
    expect(r.path).toBe('/tmp/tool-output/call_1.txt')
    expect(r.text).toContain('The full output was saved in the sandbox at')
    expect(Buffer.byteLength(r.text, 'utf8')).toBeLessThan(THRESHOLD)
  })

  // The notice rides on the write response and is read-and-clear in the daemon,
  // so dropping the body here is the one place a rebuild notice can be lost for
  // good — the OpenCode plugin used to do exactly that.
  it('surfaces the daemon notice from the write', async () => {
    mode = 'notice'
    const r = await offloadToSandbox('ses_a', 'call_2', BIG)
    expect(r.notice).toBe('the sandbox was rebuilt')
  })

  it('degrades to head plus reason when the sandbox is unavailable', async () => {
    mode = 'fail'
    const r = await offloadToSandbox('ses_a', 'call_3', BIG)
    expect(r.path).toBeUndefined()
    expect(r.text).toContain('sandbox offload failed')
    // The head must survive: losing it too turns a degraded answer into none.
    expect(r.text.startsWith('line0 ')).toBe(true)
  })
})

// --- live half ---------------------------------------------------------------

const BASE_URL = process.env.AGENTBOX_CC_TEST_BASE_URL
const TOKEN = process.env.AGENTBOX_CC_TEST_TOKEN
const MODEL = process.env.AGENTBOX_CC_TEST_MODEL || 'claude-haiku-4-5-20251001'
const LIVE = !!BASE_URL && !!TOKEN

describe.skipIf(!LIVE)('claude code offload wiring (live)', () => {
  it('sees the untruncated result and replaces what the model reads', async () => {
    const server = createServer((req, res) => {
      const chunks: Buffer[] = []
      req.on('data', c => chunks.push(c as Buffer))
      req.on('end', () => {
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end(JSON.stringify({}))
      })
    })
    await new Promise<void>(r => server.listen(0, '127.0.0.1', r))
    const addr = server.address()
    const port = typeof addr === 'object' && addr ? addr.port : 0
    process.env.SBX_PROXY_URL = `http://127.0.0.1:${port}`

    const PAYLOAD = `HEAD-MARK\n${'z'.repeat(400_000)}\nTAIL-MARK`
    const dump = tool(
      'dump',
      'Emit a large report.',
      { n: z.number().optional() },
      async () => ({ content: [{ type: 'text' as const, text: PAYLOAD }] })
    )

    let hookSawBytes = 0
    let hookSawTail = false
    const base = sandboxPostToolUseHook({ sessionKey: 'ses_offload_probe' })

    let modelSaw = ''
    try {
      for await (const m of query({
        prompt: 'Call the dump tool once. Then reply with only the word done.',
        options: {
          cwd: mkdtempSync(join(tmpdir(), 'agentbox-offload-')),
          model: MODEL,
          systemPrompt: { type: 'preset', preset: 'claude_code' },
          settingSources: [],
          strictMcpConfig: true,
          persistSession: false,
          tools: [],
          disallowedTools: ['Bash', 'Read', 'Write', 'Edit', 'Glob', 'Grep'],
          mcpServers: {
            sbx: createSdkMcpServer({ name: 'sbx', tools: [dump] }),
          },
          allowedTools: ['mcp__sbx__dump'],
          maxTurns: 4,
          env: {
            ...process.env,
            ANTHROPIC_BASE_URL: BASE_URL,
            ANTHROPIC_AUTH_TOKEN: TOKEN,
            ANTHROPIC_DEFAULT_HAIKU_MODEL: MODEL,
            CLAUDE_CONFIG_DIR: mkdtempSync(join(tmpdir(), 'agentbox-cc-cfg-')),
          },
          hooks: {
            PostToolUse: [
              {
                hooks: [
                  async (input, toolUseID, options) => {
                    if (input.hook_event_name === 'PostToolUse') {
                      const raw = JSON.stringify(input.tool_response)
                      hookSawBytes = raw.length
                      hookSawTail = raw.includes('TAIL-MARK')
                    }
                    return base(input, toolUseID, options)
                  },
                ],
              },
            ],
          },
        },
      })) {
        if (m.type === 'user') {
          const content = m.message?.content
          if (Array.isArray(content)) {
            for (const b of content) {
              if (typeof b === 'object' && b.type === 'tool_result')
                modelSaw = JSON.stringify(b.content)
            }
          }
        }
      }
    } finally {
      await new Promise<void>(r => server.close(() => r()))
    }

    // 1. No pre-hook truncation: the hook saw the whole 400 KB, tail included.
    expect(hookSawBytes).toBeGreaterThan(400_000)
    expect(hookSawTail).toBe(true)
    // 2. updatedToolOutput really replaced what the model read.
    expect(modelSaw).toContain('The full output was saved in the sandbox at')
    expect(modelSaw).not.toContain('TAIL-MARK')
  }, 240_000)
})
