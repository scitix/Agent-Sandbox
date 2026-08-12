// Claude Code binding for the sandbox toolset.
//
// Claude Code has a first-class way to replace its built-ins, and it takes all
// four of these together — each covers a hole the others leave:
//
//   tools: []          the base set of built-ins that exist at all
//   disallowedTools    removes them from the model's context, AND blocks
//                      harness-internal direct calls that hold the tool object
//                      without a name lookup
//   mcpServers         our replacements, in-process, talking to the daemon
//   toolAliases        safety net: if the model still emits `Bash` (training
//                      habit, or a skill document told it to) the call is routed
//                      to the MCP tool instead of failing as unknown
//
// The SDK's MCP handler signature is `(args, extra: unknown)` — there is no
// session on it — so a server instance is built PER SESSION and the sandbox key
// is captured in the closure. That is also why this is a factory, not a const.
//
// Subagents inherit the parent's tools/disallowedTools/mcpServers, so the
// sandbox guarantee holds for them too; sandbox-escape.test.ts asserts it.
import {
  type HookCallback,
  type HookJSONOutput,
  createSdkMcpServer,
  tool,
} from '@anthropic-ai/claude-agent-sdk'
import { z } from 'zod'

import { offloadToSandbox, shouldOffload } from '../../core/offload.ts'
import { toSchemaShape } from '../../core/params.ts'
import { withNotice } from '../../core/proxy.ts'
import { type SandboxCtx, renderToolResult, sandboxToolset } from '../../core/tools.ts'

/** MCP server name; the model sees tools as `mcp__<SERVER>__<tool>`. */
export const SANDBOX_MCP_SERVER = 'sandbox'

/** The model-visible name of a sandbox tool under this binding. */
export function claudeToolName(name: string): string {
  return `mcp__${SANDBOX_MCP_SERVER}__${name}`
}

/**
 * Built-ins to take away. Everything the sandbox toolset replaces, plus the
 * built-ins that would let the agent reach the network or the pod's filesystem
 * by another route. Anything NOT listed here stays available on purpose —
 * `Task`/`Agent` (delegation) and `AskUserQuestion` (the question cards) are
 * load-bearing product features.
 */
export function sandboxDisallowedTools(): string[] {
  const replaced = sandboxToolset().flatMap(t => t.overrides.claudeCode)
  // Built-ins with no sandbox replacement that must still be gone: they would
  // execute on the pod (NotebookEdit) or reach the internet directly (WebFetch,
  // WebSearch), neither of which the sandbox contract allows.
  const unreplaced = ['NotebookEdit', 'WebFetch', 'WebSearch']
  return [...new Set([...replaced, ...unreplaced])].sort()
}

/**
 * Route model-emitted built-in names at our replacements. Single-hop by design
 * in the SDK, so there is no chain to worry about.
 */
export function sandboxToolAliases(): Record<string, string> {
  const aliases: Record<string, string> = {}
  for (const t of sandboxToolset()) {
    for (const builtin of t.overrides.claudeCode) {
      aliases[builtin] = claudeToolName(t.name)
    }
  }
  return aliases
}

/** Every sandbox tool's model-visible name, for `allowedTools`. */
export function sandboxAllowedTools(): string[] {
  return sandboxToolset().map(t => claudeToolName(t.name))
}

/**
 * An in-process MCP server exposing the sandbox toolset for one session.
 *
 * Build one per session: the sandbox key is closed over, because the SDK gives
 * MCP handlers no session context.
 */
export function sandboxMcpServer(ctx: SandboxCtx) {
  const tools = sandboxToolset().map(spec =>
    tool(
      spec.name,
      spec.description,
      // The SDK takes a zod raw shape. Built with OUR zod here (the SDK accepts
      // zod 3 and 4), which is safe because nothing else inspects these objects.
      toSchemaShape(
        spec.params,
        z as unknown as Parameters<typeof toSchemaShape<z.ZodTypeAny>>[1]
      ),
      async args => {
        const result = await spec.run(args as Record<string, unknown>, ctx)
        return {
          content: [{ type: 'text' as const, text: renderToolResult(result) }],
        }
      },
      {
        annotations: {
          // read/grep/glob do not change the sandbox; bash can do anything.
          readOnlyHint: ['read', 'grep', 'glob'].includes(spec.name),
          // Everything here acts on a remote sandbox, not the local machine.
          openWorldHint: true,
        },
      }
    )
  )
  return createSdkMcpServer({
    name: SANDBOX_MCP_SERVER,
    version: '1.0.0',
    instructions:
      'File and shell access for this session. Every tool here runs inside the ' +
      'remote sandbox bound to the session, never on the machine hosting the ' +
      'agent. There is no other filesystem or shell available.',
    tools,
    // These are the agent's only way to touch a filesystem, so they must never
    // be deferred behind tool search.
    alwaysLoad: true,
  })
}

/**
 * PostToolUse hook that offloads an oversized tool result into the sandbox.
 *
 * Replaces the OpenCode plugin of the same purpose. Measured on CLI 2.1.220:
 * the hook receives the result UNTRUNCATED (400 KB in, 400 KB seen) and
 * `updatedToolOutput` really does replace what the model sees — so unlike
 * OpenCode this needs no `tool_output` config to preempt a built-in spill.
 * sandbox/offload.test.ts pins both halves of that.
 */
export function sandboxPostToolUseHook(ctx: SandboxCtx): HookCallback {
  return async (input): Promise<HookJSONOutput> => {
    // One callback type serves every hook event, so narrow before reading
    // PostToolUse-only fields.
    if (input.hook_event_name !== 'PostToolUse') return {}
    const text = toolResponseText(input.tool_response)
    if (!shouldOffload(input.tool_name, text)) return {}
    const r = await offloadToSandbox(ctx.sessionKey, input.tool_use_id, text)
    return {
      hookSpecificOutput: {
        hookEventName: 'PostToolUse',
        updatedToolOutput: withNotice(r.notice, r.text),
      },
    }
  }
}

/**
 * Flatten whatever shape a tool result arrives in into the text the model saw.
 *
 * `tool_response` is not one shape. An MCP tool's result arrives as the bare
 * content ARRAY (`[{type:'text',text}]`); a built-in's as `{output}`; some
 * wrappers as `{content:[...]}`; a trivial one as a plain string. Getting this
 * wrong is silent: the hook returns no replacement, and the harness then applies
 * its OWN large-result handling — which persists the output to a file on the pod
 * that the sandbox-confined agent cannot read. Measured on CLI 2.1.220, that
 * fallback looks like `<persisted-output>Output too large (390.7KB)…`, which is
 * exactly the failure the sandbox offload exists to prevent.
 */
export function toolResponseText(response: unknown): string {
  if (typeof response === 'string') return response
  if (Array.isArray(response)) return joinTextBlocks(response)
  if (response && typeof response === 'object') {
    const content = (response as { content?: unknown }).content
    if (Array.isArray(content)) return joinTextBlocks(content)
    if (typeof content === 'string') return content
    const out = (response as { output?: unknown }).output
    if (typeof out === 'string') return out
  }
  return ''
}

function joinTextBlocks(blocks: unknown[]): string {
  return blocks
    .map(c => {
      if (typeof c === 'string') return c
      if (c && typeof c === 'object') {
        const text = (c as { text?: unknown }).text
        if (typeof text === 'string') return text
      }
      return ''
    })
    .join('')
}

/**
 * The complete tool-related option set for a Claude Code session whose IO is
 * confined to the sandbox. Spread into `query({ options })`.
 *
 * Returned as one object rather than five exports so a caller cannot apply four
 * of the five and quietly lose the guarantee.
 */
export function sandboxToolOptions(ctx: SandboxCtx) {
  return {
    tools: [] as string[],
    disallowedTools: sandboxDisallowedTools(),
    toolAliases: sandboxToolAliases(),
    allowedTools: sandboxAllowedTools(),
    // A Record, not a single-key literal: the gateway adds the navigation and
    // publishing servers on top, and a narrowed type would reject them.
    mcpServers: { [SANDBOX_MCP_SERVER]: sandboxMcpServer(ctx) } as Record<
      string,
      ReturnType<typeof sandboxMcpServer>
    >,
    hooks: {
      PostToolUse: [{ hooks: [sandboxPostToolUseHook(ctx)] }],
    },
  }
}
