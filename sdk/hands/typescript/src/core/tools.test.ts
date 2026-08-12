// Drift guard for the sandbox toolset (gate G3).
//
// The tool descriptions, argument names and argument descriptions are PROMPT
// CONTRACT: they are the only thing telling the model that its file and shell IO
// happens in a remote sandbox, and how to page a
// large read. Editing them changes model behaviour with no test failure and no
// runtime error, so they are snapshotted here. A diff means "confirm you meant
// this", not "the code is broken" — update the snapshot deliberately.
import { describe, expect, it } from 'vitest'

import { toJsonSchema } from './params.ts'
import { OFFLOAD_EXEMPT, logicalToolName, sandboxToolset } from './tools.ts'

describe('sandbox toolset', () => {
  it('exposes a stable set of tools in a stable order', () => {
    expect(sandboxToolset().map(t => t.name)).toEqual([
      'bash',
      'read',
      'write',
      'edit',
      'grep',
      'glob',
      'apply_patch',
    ])
  })

  it('matches the recorded prompt contract', () => {
    const shape = sandboxToolset().map(t => ({
      name: t.name,
      description: t.description,
      params: t.params,
      overrides: t.overrides,
    }))
    expect(shape).toMatchSnapshot()
  })

  it('derives JSON Schema with the right required set', () => {
    const read = sandboxToolset().find(t => t.name === 'read')!
    const schema = toJsonSchema(read.params)
    expect(schema.required).toEqual(['filePath'])
    expect(Object.keys(schema.properties)).toEqual([
      'filePath',
      'offset',
      'limit',
    ])
    expect(schema.additionalProperties).toBe(false)
  })

  it('every tool documents itself and names what it overrides', () => {
    for (const t of sandboxToolset()) {
      expect(t.description.length, t.name).toBeGreaterThan(40)
      for (const [, spec] of Object.entries(t.params)) {
        expect(spec.description.length, t.name).toBeGreaterThan(0)
      }
      // Every tool must be reachable from at least one harness's built-in name,
      // otherwise disabling that built-in leaves the model with no replacement.
      const all = [
        ...t.overrides.claudeCode,
        ...t.overrides.opencode,
        ...t.overrides.codex,
      ]
      expect(all.length, t.name).toBeGreaterThan(0)
    }
  })

  // The offload hook used to match the literal name 'read'. Under an MCP binding
  // the model-visible name becomes mcp__sandbox__read, which silently stopped
  // matching — and the agent then got a pointer to a copy of a file it could
  // already read. Exemption is by LOGICAL name, and this proves the mapping.
  it('keeps read exempt from offload under every binding prefix', () => {
    expect(OFFLOAD_EXEMPT.has('read')).toBe(true)
    for (const name of [
      'read',
      'mcp__sandbox__read',
      'mcp__sandbox__read',
    ]) {
      expect(OFFLOAD_EXEMPT.has(logicalToolName(name) as 'read'), name).toBe(
        true
      )
    }
    expect(
      OFFLOAD_EXEMPT.has(logicalToolName('mcp__sandbox__bash') as 'read')
    ).toBe(false)
  })
})
