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

// The generic binding's own risk is not tool behaviour — that is core's, and core's
// tests cover it. It is session identity: a wrong session key is not an error, it is
// a sandbox that does not follow the conversation, and the agent finds out when its
// second tool call cannot see what its first one wrote.
import { describe, expect, it } from 'vitest'

import { sandboxToolset } from '../../core/tools.ts'
import {
  MCP_SERVER_INSTRUCTIONS,
  resolveSessionKey,
  sandboxMcpServer,
} from './server.ts'

describe('resolveSessionKey', () => {
  it('prefers the explicit header', () => {
    expect(resolveSessionKey({ 'x-hands-session': 'th_42' }, 'mcp-abc')).toBe('th_42')
  })

  it('accepts a custom header name', () => {
    expect(
      resolveSessionKey({ 'x-thread': 'th_42' }, undefined, { header: 'X-Thread' })
    ).toBe('th_42')
  })

  it('takes the first value when a header repeats', () => {
    expect(resolveSessionKey({ 'x-hands-session': ['th_42', 'th_7'] }, undefined)).toBe(
      'th_42'
    )
  })

  it('ignores a blank header', () => {
    expect(resolveSessionKey({ 'x-hands-session': '   ' }, 'mcp-abc')).toBe('mcp-abc')
  })

  // The fallback exists for one narrow case — a single connection carrying a single
  // conversation — and is wrong outside it: a transport id changes on reconnect, and
  // is shared when one connection serves several conversations.
  it('falls back to the transport session id when no header is sent', () => {
    expect(resolveSessionKey({}, 'mcp-abc')).toBe('mcp-abc')
  })

  it('refuses to guess under strict', () => {
    expect(resolveSessionKey({}, 'mcp-abc', { strict: true })).toBeUndefined()
  })

  it('is undefined when there is nothing at all to use', () => {
    expect(resolveSessionKey({}, undefined)).toBeUndefined()
  })
})

describe('sandboxMcpServer', () => {
  it('registers every tool in the toolset', async () => {
    const server = sandboxMcpServer({ sessionKey: 'th_42' })
    // Reach through to the low-level server's registered tools rather than standing
    // up a transport: what matters here is that nothing in the toolset was missed,
    // and a client round-trip would not check that any better.
    const registered = Object.keys(
      (server as unknown as { _registeredTools: Record<string, unknown> })
        ._registeredTools
    )
    expect(registered.sort()).toEqual(sandboxToolset().map(t => t.name).sort())
    await server.close()
  })

  it('marks the read-only tools read-only and all of them remote', async () => {
    const server = sandboxMcpServer({ sessionKey: 'th_42' })
    const tools = (
      server as unknown as {
        _registeredTools: Record<string, { annotations?: Record<string, unknown> }>
      }
    )._registeredTools
    for (const [name, tool] of Object.entries(tools)) {
      expect(tool.annotations?.openWorldHint, name).toBe(true)
      expect(tool.annotations?.readOnlyHint, name).toBe(
        ['read', 'grep', 'glob'].includes(name)
      )
    }
    await server.close()
  })

  // Unlike the other two bindings, this one cannot remove the host's own built-ins,
  // so the only lever it has over which tool the model reaches for is what it says
  // about itself. Losing that sentence would not fail anything — the agent would
  // just keep using a local shell and wonder where its files went.
  it('tells the model these tools are the only ones that see the workspace', () => {
    expect(MCP_SERVER_INSTRUCTIONS).toMatch(/remote\s+sandbox/i)
    expect(MCP_SERVER_INSTRUCTIONS).toMatch(/prefer these/i)
  })
})
