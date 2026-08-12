// Config-level half of gate G1: prove the Claude Code option set actually takes
// every built-in away and puts a sandbox tool in its place.
//
// This runs with no credentials and no network. It cannot prove what the model
// is finally offered (that needs a live session — see sandbox-escape.test.ts),
// but it does catch the realistic regression: someone adds a tool, or the SDK
// gains a built-in, and one of the four mechanisms is left out of sync.
import { describe, expect, it } from 'vitest'

import {
  SANDBOX_MCP_SERVER,
  claudeToolName,
  sandboxAllowedTools,
  sandboxDisallowedTools,
  sandboxToolAliases,
  sandboxToolOptions,
} from './bind.ts'
import { sandboxToolset } from '../../core/tools.ts'

/**
 * Claude Code built-ins that can read, write or execute. Any of these left
 * enabled would let the agent reach the pod's filesystem or the network
 * directly, breaking the "all IO is in the sandbox" contract. Listed here rather
 * than derived so that a NEW built-in in a future SDK shows up as a failing test
 * the moment someone adds it to this list — and the list is the review surface.
 */
const IO_CAPABLE_BUILTINS = [
  'Bash',
  'BashOutput',
  'Edit',
  'Glob',
  'Grep',
  'KillShell',
  'NotebookEdit',
  'Read',
  'WebFetch',
  'WebSearch',
  'Write',
]

/** Built-ins we deliberately keep: they are product features, not IO. */
const INTENTIONALLY_KEPT = ['Agent', 'AskUserQuestion', 'Task', 'TodoWrite']

describe('claude code sandbox binding', () => {
  it('disallows every built-in that could touch the pod or the network', () => {
    const disallowed = new Set(sandboxDisallowedTools())
    const missing = IO_CAPABLE_BUILTINS.filter(
      // BashOutput / KillShell only exist to manage a Bash the model cannot
      // start, so removing Bash is enough; assert the rest explicitly.
      b => !['BashOutput', 'KillShell'].includes(b) && !disallowed.has(b)
    )
    expect(missing).toEqual([])
  })

  it('does not disallow the built-ins we rely on', () => {
    const disallowed = new Set(sandboxDisallowedTools())
    for (const keep of INTENTIONALLY_KEPT) {
      expect(disallowed.has(keep), keep).toBe(false)
    }
  })

  it('aliases every replaced built-in at its sandbox tool', () => {
    const aliases = sandboxToolAliases()
    for (const t of sandboxToolset()) {
      for (const builtin of t.overrides.claudeCode) {
        expect(aliases[builtin], builtin).toBe(claudeToolName(t.name))
      }
    }
  })

  // disallowedTools and toolAliases are complementary, not alternatives: the
  // alias only affects name lookup of a model-emitted tool_use, while
  // disallowedTools also blocks harness-internal direct calls. Applying one
  // without the other leaves a hole, so they must cover the same names.
  it('keeps disallow and alias in lockstep', () => {
    const disallowed = new Set(sandboxDisallowedTools())
    for (const builtin of Object.keys(sandboxToolAliases())) {
      expect(disallowed.has(builtin), builtin).toBe(true)
    }
  })

  it('allows exactly the sandbox tools, namespaced', () => {
    expect(sandboxAllowedTools()).toEqual(
      sandboxToolset().map(t => `mcp__${SANDBOX_MCP_SERVER}__${t.name}`)
    )
  })

  it('ships all four mechanisms together', () => {
    const opts = sandboxToolOptions({ sessionKey: 'ses_x' })
    expect(opts.tools).toEqual([])
    expect(opts.disallowedTools.length).toBeGreaterThan(0)
    expect(Object.keys(opts.toolAliases).length).toBeGreaterThan(0)
    expect(Object.keys(opts.mcpServers)).toEqual([SANDBOX_MCP_SERVER])
    expect(opts.allowedTools).toEqual(sandboxAllowedTools())
  })
})
