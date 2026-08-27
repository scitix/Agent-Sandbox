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

// Rendered-output guard for the sandbox toolset.
//
// The exact bytes each tool hands the model are prompt contract: `read`'s paging
// note is a protocol the agent follows to page, `bash`'s header is how it learns
// the cwd and exit code, and a one-shot daemon `notice` must arrive as a LEADING
// line rather than buried in the payload. None of that is covered by types, so it
// is pinned here against a stub daemon.
import { createServer } from 'node:http'
import type { Server } from 'node:http'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { renderToolResult, sandboxToolset } from './tools.ts'

/** Reply for one daemon endpoint, keyed by the last path segment. */
type Handler = (endpoint: string, body: Record<string, unknown>) => unknown

let server: Server
let handler: Handler = () => ({})

beforeAll(async () => {
  server = createServer((req, res) => {
    const chunks: Buffer[] = []
    req.on('data', c => chunks.push(c as Buffer))
    req.on('end', () => {
      const endpoint = (req.url ?? '').split('/').filter(Boolean).pop() ?? ''
      const raw = Buffer.concat(chunks).toString('utf-8')
      const body = raw ? (JSON.parse(raw) as Record<string, unknown>) : {}
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(handler(endpoint, body)))
    })
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const addr = server.address()
  const port = typeof addr === 'object' && addr ? addr.port : 0
  process.env.SBX_PROXY_URL = `http://127.0.0.1:${port}`
})

afterAll(async () => {
  await new Promise<void>(resolve => server.close(() => resolve()))
})

const tool = (name: string) => sandboxToolset().find(t => t.name === name)!
const run = async (name: string, args: Record<string, unknown>) =>
  renderToolResult(await tool(name).run(args, { sessionKey: 'ses_test' }))

describe('rendered tool output', () => {
  it('bash: header, then stdout and stderr sections', async () => {
    handler = () => ({
      exit_code: 2,
      stdout: 'one\ntwo',
      stderr: 'boom',
      cwd: '/home/u/alice',
    })
    expect(await run('bash', { command: 'ls -la' })).toBe(
      '$ ls -la\n' +
        '(cwd=/home/u/alice, exit=2)\n' +
        '--- stdout ---\n' +
        'one\ntwo\n' +
        '--- stderr ---\n' +
        'boom'
    )
  })

  it('bash: omits empty stream sections', async () => {
    handler = () => ({ exit_code: 0, stdout: '', stderr: '', cwd: '/w' })
    expect(await run('bash', { command: 'true' })).toBe(
      '$ true\n(cwd=/w, exit=0)'
    )
  })

  it('a daemon notice becomes a leading note: line, never inline', async () => {
    handler = () => ({
      exit_code: 0,
      stdout: 'hi',
      stderr: '',
      cwd: '/w',
      notice: 'the sandbox was rebuilt',
    })
    const out = await run('bash', { command: 'echo hi' })
    expect(out.startsWith('note: the sandbox was rebuilt\n\n')).toBe(true)
    expect(out.endsWith('--- stdout ---\nhi')).toBe(true)
  })

  it('read: numbers lines from offset and says how to page on', async () => {
    handler = () => ({ content: 'alpha\nbravo\n', path: '/w/f.txt', count: 9 })
    expect(await run('read', { filePath: 'f.txt', offset: 3, limit: 2 })).toBe(
      [
        '<path>/w/f.txt</path>',
        '<type>file</type>',
        '<content>',
        '',
        '3: alpha',
        '4: bravo',
        '',
        '(Showing lines 3-4 of 9. Use offset=5 to continue.)',
        '</content>',
      ].join('\n')
    )
  })

  it('read: reports end-of-file instead of a page hint', async () => {
    handler = () => ({ content: 'only\n', path: '/w/f.txt', count: 1 })
    expect(await run('read', { filePath: 'f.txt' })).toContain(
      '(End of file - total 1 lines)'
    )
  })

  it('read: caps at 50 KB and points at the next line', async () => {
    const line = 'x'.repeat(1024)
    handler = () => ({
      content: Array.from({ length: 60 }, () => line).join('\n') + '\n',
      path: '/w/big.txt',
      count: 60,
    })
    const out = await run('read', { filePath: 'big.txt' })
    expect(out).toContain('(Output capped at 50 KB. Showing lines 1-')
    expect(out).toContain('to continue.)')
  })

  it('read: truncates an over-long single line in place', async () => {
    handler = () => ({
      content: `${'y'.repeat(2500)}\n`,
      path: '/w/l',
      count: 1,
    })
    expect(await run('read', { filePath: 'l' })).toContain(
      '... (line truncated to 2000 chars)'
    )
  })

  it('grep stops at 200 matches, glob at 500 paths', async () => {
    handler = endpoint =>
      endpoint === 'grep'
        ? {
            exit_code: 0,
            matches: Array.from({ length: 500 }, (_, i) => ({
              path: 'f',
              line: i + 1,
              text: 't',
            })),
          }
        : {
            exit_code: 0,
            paths: Array.from({ length: 900 }, (_, i) => `p${i}`),
          }
    expect((await run('grep', { pattern: 'x' })).split('\n')).toHaveLength(200)
    expect((await run('glob', { pattern: '**/*' })).split('\n')).toHaveLength(
      500
    )
  })

  it('empty results render a marker, not an empty string', async () => {
    handler = endpoint =>
      endpoint === 'grep'
        ? { exit_code: 1, matches: [] }
        : { exit_code: 0, paths: [] }
    expect(await run('grep', { pattern: 'zzz' })).toBe('(no matches)')
    expect(await run('glob', { pattern: 'zzz' })).toBe('(no matches)')
  })

  it('write / edit / apply_patch report what they did', async () => {
    handler = endpoint => {
      if (endpoint === 'write') return { bytes_written: 12, path: '/w/a' }
      if (endpoint === 'edit') return { replacements: 1, path: '/w/a' }
      return { exit_code: 0, stdout: 'patching', stderr: '' }
    }
    expect(await run('write', { filePath: 'a', content: 'x' })).toBe(
      'wrote 12 bytes to /w/a'
    )
    expect(
      await run('edit', { filePath: 'a', oldString: 'x', newString: 'y' })
    ).toBe('edited /w/a (1 replacement)')
    expect(await run('apply_patch', { patch: 'diff' })).toBe(
      '(exit=0)\npatching'
    )
  })

  it('edit pluralises the replacement count', async () => {
    handler = () => ({ replacements: 3, path: '/w/a' })
    expect(
      await run('edit', {
        filePath: 'a',
        oldString: 'x',
        newString: 'y',
        replaceAll: true,
      })
    ).toBe('edited /w/a (3 replacements)')
  })
})

describe('attachment miss', () => {
  // A transport failure under the attachment root is rewritten into something
  // the agent can act on; anywhere else the raw error must pass through.
  it('explains a recycled sandbox, and only there', async () => {
    const prev = process.env.SBX_PROXY_URL
    process.env.SBX_PROXY_URL = 'http://127.0.0.1:1'
    try {
      await expect(
        run('read', { filePath: '/opt/agentbox/attachments/notes.md' })
      ).rejects.toThrow(/Ask the user to re-attach/)
      await expect(run('read', { filePath: '/w/other.md' })).rejects.toThrow(
        /Failed to reach/
      )
    } finally {
      process.env.SBX_PROXY_URL = prev
    }
  })
})
