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

// Generic MCP binding for the sandbox toolset.
//
// The other two bindings target a harness that has file and shell built-ins and
// can be made to give them up. This one targets everything else — a custom agent
// loop, an MCP-capable harness this package has no binding for — and the honest
// framing of what it can do is narrower:
//
//   MCP adds tools. It cannot take a harness's built-ins away.
//
// So exposing the toolset here does not by itself confine anything: an agent that
// still has its own `bash` will keep using it. The confinement has to come from
// the host's own configuration (deny rules, an allow-list, an agent loop that only
// ever offers the tools it was handed). What this binding provides is the other
// half — a sandbox-backed toolset with the same behaviour contract the two
// first-class bindings get, so a third party does not have to reimplement paging,
// offload and the notice convention to get there.
//
// Session identity is the one thing a caller MUST wire up deliberately; see
// resolveSessionKey.
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'

import { toSchemaShape } from '../../core/params.ts'
import { offloadToSandbox, shouldOffload } from '../../core/offload.ts'
import {
  type SandboxCtx,
  renderToolResult,
  sandboxToolset,
} from '../../core/tools.ts'

/** Server name clients see; tools appear under it in the client's namespace. */
export const MCP_SERVER_NAME = 'agentbox-hands'

export const MCP_SERVER_INSTRUCTIONS =
  'File and shell access for this session. Every tool here runs inside a remote ' +
  'sandbox bound to the session, never on the machine hosting the agent. Prefer ' +
  'these over any local file or shell tool you may also have: only these can see ' +
  'the session workspace, and work done with a local tool is invisible here.'

/**
 * Builds an MCP server exposing the sandbox toolset for one session.
 *
 * One server instance per session, because the session key is captured here — the
 * same shape the Claude Code binding uses, and for the same reason: a tool handler
 * receives its arguments and nothing about who is calling.
 */
export function sandboxMcpServer(ctx: SandboxCtx): McpServer {
  const server = new McpServer(
    { name: MCP_SERVER_NAME, version: '0.0.1' },
    { instructions: MCP_SERVER_INSTRUCTIONS }
  )

  for (const spec of sandboxToolset()) {
    server.registerTool(
      spec.name,
      {
        description: spec.description,
        inputSchema: toSchemaShape(
          spec.params,
          z as unknown as Parameters<typeof toSchemaShape<z.ZodTypeAny>>[1]
        ),
        annotations: {
          readOnlyHint: ['read', 'grep', 'glob'].includes(spec.name),
          // Everything here acts on a remote sandbox, not the local machine.
          openWorldHint: true,
        },
      },
      async (args: Record<string, unknown>) => {
        const result = await spec.run(args, ctx)
        const text = renderToolResult(result)
        return { content: [{ type: 'text' as const, text: await offload(spec.name, text, ctx) }] }
      }
    )
  }

  return server
}

/**
 * Push an oversized result into the sandbox and hand back a head plus a pointer.
 *
 * The Claude Code binding does this from a PostToolUse hook, because that harness
 * would otherwise spill the result to a file on its own machine first. MCP has no
 * equivalent interception point, so it happens inline here — which also means a
 * host that applies its own truncation on top can still cut what is left, and a
 * host with a small result limit should raise it.
 */
async function offload(
  toolName: string,
  text: string,
  ctx: SandboxCtx
): Promise<string> {
  if (!shouldOffload(toolName, text)) return text
  try {
    const r = await offloadToSandbox(ctx.sessionKey, `${toolName}-${Date.now()}`, text)
    return r.notice ? `note: ${r.notice}\n${r.text}` : r.text
  } catch (err) {
    // Returning the untruncated text is the lesser evil: the host may cut it, but
    // failing the call loses a result the agent already paid for.
    console.error(`[hands/mcp] offload failed for ${toolName}:`, err)
    return text
  }
}

/**
 * The session key for an incoming request.
 *
 * This is the decision a caller has to make deliberately, because the wrong answer
 * is not an error — it is a sandbox that does not follow the conversation.
 *
 * The key has to be stable for the whole conversation and distinct between
 * conversations. An MCP transport session id satisfies neither on its own: it is
 * per-connection, so a reconnect gets a different one and the conversation silently
 * moves to a fresh sandbox, and a single long-lived connection serving several
 * conversations gives them all the same one and they share a filesystem.
 *
 * So an explicit header wins, and the transport session id is only a fallback for
 * the one case where it happens to be right — one connection, one conversation, no
 * reconnects. `strict` turns that fallback off for deployments where quietly
 * getting it wrong is worse than a rejected request.
 */
export function resolveSessionKey(
  headers: Record<string, string | string[] | undefined>,
  transportSessionId: string | undefined,
  opts: { header?: string; strict?: boolean } = {}
): string | undefined {
  const name = (opts.header ?? 'x-hands-session').toLowerCase()
  const raw = headers[name]
  const fromHeader = (Array.isArray(raw) ? raw[0] : raw)?.trim()
  if (fromHeader) return fromHeader
  if (opts.strict) return undefined
  return transportSessionId
}
