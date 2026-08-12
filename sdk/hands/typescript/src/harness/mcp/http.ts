// Streamable-HTTP front end for the generic MCP binding.
//
// Runs stateless: a server and transport are built per request and discarded.
// That is not a simplification, it is the correct shape here. Session state — which
// sandbox a conversation owns, whether it was rebuilt — lives in the daemon, keyed
// by the session key the caller sends. Keeping a second, transport-level session
// concept alongside it would add a way for the two to disagree without adding
// anything: there is nothing to remember between requests.
//
// The cost is that server-initiated messages are unavailable, which a toolset does
// not use.
import { randomUUID } from 'node:crypto'
import type { IncomingMessage, ServerResponse } from 'node:http'

import type { AuthInfo } from '@modelcontextprotocol/sdk/server/auth/types.js'
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js'

import { resolveSessionKey, sandboxMcpServer } from './server.ts'

export interface HandsMcpHttpOptions {
  /**
   * Header carrying the caller's conversation id. Defaults to `x-hands-session`.
   *
   * Required on every request: stateless mode has no transport session id to fall
   * back on, and inventing one per request would give each tool call its own
   * sandbox — which looks like it works until the second call cannot see the first
   * one's files.
   */
  sessionHeader?: string
  /** Called for each request once the session key is known; for logging. */
  onRequest?: (sessionKey: string, method: string) => void
}

/**
 * A `node:http` request handler serving the sandbox toolset over MCP.
 *
 * Mount it behind whatever authentication the deployment already has. This handler
 * performs none: the session key it trusts is a plain header, so anything that can
 * reach it can act on any session whose key it can guess. That is the same posture
 * as the loopback daemon it front-ends — appropriate on a private network, not on
 * an exposed port.
 */
export function handsMcpHttpHandler(opts: HandsMcpHttpOptions = {}) {
  return async function handle(
    // `auth` is the SDK's slot for a value an upstream authentication middleware
    // attached; typed to match so a caller that populates it is not forced to cast.
    req: IncomingMessage & { auth?: AuthInfo },
    res: ServerResponse,
    body?: unknown
  ): Promise<void> {
    const sessionKey = resolveSessionKey(req.headers, undefined, {
      header: opts.sessionHeader,
      strict: true,
    })
    if (!sessionKey) {
      const header = opts.sessionHeader ?? 'x-hands-session'
      respondJson(res, 400, {
        jsonrpc: '2.0',
        error: {
          code: -32600,
          message:
            `Missing ${header}. Send the conversation's own stable id: the ` +
            `sandbox is bound to it, so it must be identical across every ` +
            `request of a conversation and different between conversations.`,
        },
        id: null,
      })
      return
    }

    opts.onRequest?.(sessionKey, req.method ?? 'POST')

    const server = sandboxMcpServer({ sessionKey })
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: undefined,
    })
    // Close on response end rather than after handleRequest resolves: the response
    // may still be streaming, and tearing the transport down early truncates it.
    res.on('close', () => {
      void transport.close()
      void server.close()
    })

    try {
      await server.connect(transport)
      await transport.handleRequest(req, res, body)
    } catch (err) {
      console.error(`[hands/mcp] request failed for session ${sessionKey}:`, err)
      if (!res.headersSent) {
        respondJson(res, 500, {
          jsonrpc: '2.0',
          error: { code: -32603, message: 'Internal error' },
          id: null,
        })
      }
    }
  }
}

function respondJson(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'content-type': 'application/json' })
  res.end(JSON.stringify(body))
}

/** A session id for callers that genuinely have no conversation id of their own. */
export function newSessionKey(): string {
  return `hands_${randomUUID()}`
}
