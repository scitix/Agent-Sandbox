// The sandbox toolset: the single definition of "all of the agent's file and
// shell IO happens in the remote sandbox, never on the pod".
//
// Harness-neutral on purpose. Three bindings consume this (OpenCode override
// files, an in-process Claude Agent SDK MCP server, and a plain streamable-HTTP
// MCP server); none of them may re-implement any of the behaviour below, because
// most of it is load-bearing in ways that are invisible from the signatures:
//
//   * `read` pages itself (line cap, per-line cap, byte cap) and its trailing
//     note is a protocol — the agent resumes from `Use offset=N to continue`.
//     Without it the agent re-reads from line 1 or gives up.
//   * `read` is the one tool exempt from oversized-output offload, so its byte
//     cap deliberately sits just above the offload threshold.
//   * `read` turns a miss under the attachment root into an actionable message,
//     because a bare 404 there almost always means the sandbox was recycled.
//   * `grep` / `glob` cap their result count; a repo-wide grep over
//     a large mounted source tree otherwise returns tens of thousands of lines.
//   * every result may carry a one-shot `notice` (the sandbox was transparently
//     rebuilt), which must appear as a LEADING line and never inside the payload.
//   * the exact output shapes (`$ cmd`, `(cwd=…, exit=…)`, `--- stdout ---`)
//     are prompt contract, not cosmetics.
import { type ParamsSpec } from './params.ts'
import { call, withNotice } from './proxy.ts'

export type SandboxToolName =
  | 'bash'
  | 'read'
  | 'write'
  | 'edit'
  | 'grep'
  | 'glob'
  | 'apply_patch'

export interface SandboxCtx {
  /**
   * Session identity. The daemon derives the sandbox from it, so it must be
   * stable for a conversation's whole life. Each harness maps its own session
   * concept onto this: OpenCode passes its session id, the gateway passes its
   * own thread id (so a sandbox survives a backend switch).
   */
  sessionKey: string
}

export interface ToolResult {
  /** The payload the model sees, before the notice is prepended. */
  text: string
  /** One-shot daemon notice, rendered as a leading `note:` line. */
  notice?: string | null
}

export interface SandboxTool {
  name: SandboxToolName
  description: string
  params: ParamsSpec
  /**
   * Built-in tool names this tool is meant to replace, per harness. Bindings
   * derive their own disabling mechanism from this (Claude Code:
   * `disallowedTools` + `toolAliases`; OpenCode: a same-named override file;
   * Codex: `features.shell_tool=false`). Kept on the tool rather than in a
   * separate table so adding a tool cannot silently miss a harness.
   */
  overrides: { claudeCode: string[]; opencode: string[]; codex: string[] }
  run(args: Record<string, unknown>, ctx: SandboxCtx): Promise<ToolResult>
}

/** Apply the notice convention. Every binding must render through this. */
export function renderToolResult(r: ToolResult): string {
  return withNotice(r.notice, r.text)
}

// --- read: paging limits -----------------------------------------------------
// Deliberately identical to opencode's own built-in read tool
// (packages/opencode/src/tool/read.ts). This tool used to pass offset/limit
// straight through with no defaults, so reading a 70 KB file returned all 70 KB —
// which then tripped the oversized-output offload hook, and the agent got a
// pointer to a COPY of a file it was already able to read. Paging here removes
// the reason for that hop entirely (offload skips `read`).
const DEFAULT_READ_LIMIT = 2000
const MAX_LINE_LENGTH = 2000
const MAX_LINE_SUFFIX = `... (line truncated to ${MAX_LINE_LENGTH} chars)`
const MAX_BYTES = 50 * 1024
const MAX_BYTES_LABEL = `${MAX_BYTES / 1024} KB`

// Root-owned, read-only dir user attachments are flushed to (matches the daemon's
// SBX_ATTACH_ROOT). A miss here almost always means the sandbox was recycled, so
// we turn the raw 404 into an actionable message instead of a bare "read failed".
const ATTACH_ROOT = (
  process.env.SBX_ATTACH_ROOT || '/opt/agentbox/attachments'
).replace(/\/+$/, '')

/**
 * One clause about what else this deployment's sandbox image mounts — a read-only
 * dataset, a checked-out source tree — for deployments that have something to say.
 *
 * Prompt contract, so it is a deployment-level constant rather than a per-call
 * argument: the model needs it identical on every turn. Empty by default, because
 * most images have nothing extra to mount and the base descriptions stand alone.
 *
 * Written as a clause, not a sentence: it is spliced in before `bash`'s closing
 * period, so a deployment migrating off a hand-edited description can reproduce
 * its previous wording exactly rather than approximately. Set it to the text that
 * followed the semicolon, with no leading separator and no trailing period —
 * e.g. `the Foo source is read-only at /opt/foo`.
 */
const WORKSPACE_NOTE = (process.env.SBX_WORKSPACE_NOTE || '')
  .trim()
  .replace(/^[;,\s]+|[.\s]+$/g, '')

/** `; <note>` when the deployment set one, otherwise nothing. */
const WORKSPACE_CLAUSE = WORKSPACE_NOTE ? `; ${WORKSPACE_NOTE}` : ''

/** Split the daemon's slice into lines, dropping the artefact empty element a
 *  trailing newline produces. */
function toLines(content: string): string[] {
  const lines = content.split('\n')
  if (lines.length && lines[lines.length - 1] === '') lines.pop()
  return lines
}

const GREP_MAX_MATCHES = 200
const GLOB_MAX_PATHS = 500

const BASH: SandboxTool = {
  name: 'bash',
  description:
    'Run a shell command. Executes inside the remote sandbox bound to this ' +
    'session (NOT on the local machine). The default working directory is ' +
    `your session working directory (the cwd shown to you)${WORKSPACE_CLAUSE}.`,
  params: {
    command: { type: 'string', description: 'The shell command to run' },
    cwd: {
      type: 'string',
      optional: true,
      description:
        'Working directory (absolute, or relative to your working directory)',
    },
    timeout_seconds: {
      type: 'number',
      optional: true,
      description: 'Max seconds to wait (default 60)',
    },
  },
  overrides: { claudeCode: ['Bash'], opencode: ['bash'], codex: ['shell'] },
  async run(args, ctx) {
    const a = args as {
      command: string
      cwd?: string
      timeout_seconds?: number
    }
    const r = await call<{
      exit_code: number
      stdout: string
      stderr: string
      cwd: string
      notice?: string | null
    }>(ctx.sessionKey, 'bash', a)
    let out = `$ ${a.command}\n(cwd=${r.cwd}, exit=${r.exit_code})`
    if (r.stdout) out += `\n--- stdout ---\n${r.stdout}`
    if (r.stderr) out += `\n--- stderr ---\n${r.stderr}`
    return { text: out, notice: r.notice }
  },
}

const READ: SandboxTool = {
  name: 'read',
  description:
    'Read the contents of a text file from the remote sandbox. Paths are ' +
    'absolute or resolved against your working directory. Returns at most ' +
    `${DEFAULT_READ_LIMIT} lines (or ${MAX_BYTES_LABEL}) per call, numbered from ` +
    '`offset`; the trailing note says how to page through the rest.',
  params: {
    filePath: {
      type: 'string',
      description:
        'Path to the file (absolute or relative to your working directory)',
    },
    offset: {
      type: 'number',
      optional: true,
      description: '1-based line offset to start reading',
    },
    limit: {
      type: 'number',
      optional: true,
      description: `Number of lines to read (default ${DEFAULT_READ_LIMIT})`,
    },
  },
  overrides: { claudeCode: ['Read'], opencode: ['read'], codex: [] },
  async run(args, ctx) {
    const a = args as { filePath: string; offset?: number; limit?: number }
    const offset = Math.max(1, a.offset ?? 1)
    const limit = Math.max(1, a.limit ?? DEFAULT_READ_LIMIT)
    let r: {
      content: string
      path: string
      count?: number
      notice?: string | null
    }
    try {
      r = await call(ctx.sessionKey, 'read', {
        path: a.filePath,
        offset,
        limit,
      })
    } catch (e) {
      if (a.filePath.startsWith(`${ATTACH_ROOT}/`)) {
        throw new Error(
          `Attachment not found at ${a.filePath}. The sandbox may have been ` +
            `recycled (its files are cleared on rebuild). Ask the user to re-attach ` +
            `the file, then read it again.`
        )
      }
      throw e
    }

    const raw = toLines(r.content)
    const total = r.count ?? offset - 1 + raw.length
    const kept: string[] = []
    let bytes = 0
    let cut = false
    for (const line of raw) {
      const text =
        line.length > MAX_LINE_LENGTH
          ? line.slice(0, MAX_LINE_LENGTH) + MAX_LINE_SUFFIX
          : line
      const size = Buffer.byteLength(text, 'utf-8') + (kept.length ? 1 : 0)
      if (bytes + size > MAX_BYTES) {
        cut = true
        break
      }
      bytes += size
      kept.push(text)
    }
    // `more` means the LINE limit stopped us short of the end of the file; `cut`
    // means the byte cap did. They are reported separately because the offset to
    // resume from differs in neither case but the reason matters to the reader.
    const more = !cut && offset - 1 + kept.length < total
    const last = offset + kept.length - 1
    const next = last + 1
    const note = cut
      ? `(Output capped at ${MAX_BYTES_LABEL}. Showing lines ${offset}-${last}. Use offset=${next} to continue.)`
      : more
        ? `(Showing lines ${offset}-${last} of ${total}. Use offset=${next} to continue.)`
        : `(End of file - total ${total} lines)`
    const numbered = kept.map((line, i) => `${i + offset}: ${line}`)
    const body = [
      `<path>${r.path}</path>`,
      '<type>file</type>',
      '<content>',
      '',
      ...numbered,
      '',
      note,
      '</content>',
    ].join('\n')
    return { text: body, notice: r.notice }
  },
}

const WRITE: SandboxTool = {
  name: 'write',
  description:
    'Create a new file or overwrite an existing one inside the remote sandbox.',
  params: {
    filePath: {
      type: 'string',
      description:
        'Path to the file (absolute or relative to your working directory)',
    },
    content: { type: 'string', description: 'Full file content to write' },
  },
  overrides: { claudeCode: ['Write'], opencode: ['write'], codex: [] },
  async run(args, ctx) {
    const a = args as { filePath: string; content: string }
    const r = await call<{
      bytes_written: number
      path: string
      notice?: string | null
    }>(ctx.sessionKey, 'write', { path: a.filePath, content: a.content })
    return {
      text: `wrote ${r.bytes_written} bytes to ${r.path}`,
      notice: r.notice,
    }
  },
}

const EDIT: SandboxTool = {
  name: 'edit',
  description:
    'Modify an existing file inside the remote sandbox via exact string ' +
    'replacement. Fails if old_string is not unique unless replace_all=true.',
  params: {
    filePath: {
      type: 'string',
      description:
        'Path to the file (absolute or relative to your working directory)',
    },
    oldString: { type: 'string', description: 'Exact string to replace' },
    newString: { type: 'string', description: 'Replacement string' },
    replaceAll: {
      type: 'boolean',
      optional: true,
      description: 'If true, replace every occurrence (default false)',
    },
  },
  overrides: { claudeCode: ['Edit'], opencode: ['edit'], codex: [] },
  async run(args, ctx) {
    const a = args as {
      filePath: string
      oldString: string
      newString: string
      replaceAll?: boolean
    }
    const r = await call<{
      replacements: number
      path: string
      notice?: string | null
    }>(ctx.sessionKey, 'edit', {
      path: a.filePath,
      old_string: a.oldString,
      new_string: a.newString,
      replace_all: !!a.replaceAll,
    })
    return {
      text: `edited ${r.path} (${r.replacements} replacement${r.replacements === 1 ? '' : 's'})`,
      notice: r.notice,
    }
  },
}

const GREP: SandboxTool = {
  name: 'grep',
  description:
    'Search file contents inside the remote sandbox using a regular ' +
    'expression. Returns matching path:line:text lines.',
  params: {
    pattern: {
      type: 'string',
      description: 'Regex pattern (or literal if fixedStrings=true)',
    },
    path: {
      type: 'string',
      optional: true,
      description: 'Directory to search (default: your working directory)',
    },
    include: {
      type: 'string',
      optional: true,
      description: 'Filename glob filter, e.g. "*.py"',
    },
    fixedStrings: {
      type: 'boolean',
      optional: true,
      description: 'Treat pattern as a literal string',
    },
  },
  overrides: { claudeCode: ['Grep'], opencode: ['grep'], codex: [] },
  async run(args, ctx) {
    const a = args as {
      pattern: string
      path?: string
      include?: string
      fixedStrings?: boolean
    }
    const r = await call<{
      matches: { path: string; line: number; text: string }[]
      exit_code: number
      notice?: string | null
    }>(ctx.sessionKey, 'grep', {
      pattern: a.pattern,
      path: a.path,
      include: a.include,
      fixed_strings: !!a.fixedStrings,
    })
    if (!r.matches.length) return { text: '(no matches)', notice: r.notice }
    return {
      text: r.matches
        .slice(0, GREP_MAX_MATCHES)
        .map(m => (m.line ? `${m.path}:${m.line}:${m.text}` : m.text))
        .join('\n'),
      notice: r.notice,
    }
  },
}

const GLOB: SandboxTool = {
  name: 'glob',
  description:
    'Find files by glob pattern inside the remote sandbox. Supports ** for ' +
    'recursive matching.',
  params: {
    pattern: { type: 'string', description: 'Glob pattern, e.g. "**/*.py"' },
    path: {
      type: 'string',
      optional: true,
      description: 'Directory to search from (default: your working directory)',
    },
  },
  overrides: { claudeCode: ['Glob'], opencode: ['glob'], codex: [] },
  async run(args, ctx) {
    const a = args as { pattern: string; path?: string }
    const r = await call<{
      paths: string[]
      exit_code: number
      notice?: string | null
    }>(ctx.sessionKey, 'glob', a)
    if (!r.paths.length) return { text: '(no matches)', notice: r.notice }
    return {
      text: r.paths.slice(0, GLOB_MAX_PATHS).join('\n'),
      notice: r.notice,
    }
  },
}

const APPLY_PATCH: SandboxTool = {
  name: 'apply_patch',
  description:
    "Apply a unified-diff patch inside the remote sandbox via 'patch -p1'. " +
    'Pass the entire patch as a single string.',
  params: {
    patch: { type: 'string', description: 'Unified diff content' },
    cwd: {
      type: 'string',
      optional: true,
      description: 'Working directory (default: your working directory)',
    },
  },
  overrides: { claudeCode: [], opencode: ['patch'], codex: ['apply_patch'] },
  async run(args, ctx) {
    const a = args as { patch: string; cwd?: string }
    const r = await call<{
      exit_code: number
      stdout: string
      stderr: string
      notice?: string | null
    }>(ctx.sessionKey, 'apply_patch', a)
    let out = `(exit=${r.exit_code})`
    if (r.stdout) out += `\n${r.stdout}`
    if (r.stderr) out += `\n--- stderr ---\n${r.stderr}`
    return { text: out, notice: r.notice }
  },
}

/**
 * Every sandbox tool, in a stable order. The order is part of the CI gate that
 * asserts the harness advertises exactly this set and no built-ins.
 */
export function sandboxToolset(): SandboxTool[] {
  return [BASH, READ, WRITE, EDIT, GREP, GLOB, APPLY_PATCH]
}

/** Tools that must never be offloaded: `read` pages itself and its target is
 *  already a file in the sandbox, so replacing its output with a pointer hands
 *  back a reference to something the agent could already read. Matched by
 *  logical name so a binding's prefix (`mcp__sandbox__read`) cannot break it. */
export const OFFLOAD_EXEMPT: ReadonlySet<SandboxToolName> = new Set(['read'])

/** Strip a harness's namespacing to get back to a logical tool name. */
export function logicalToolName(name: string): string {
  const m = /^mcp__[^_]+(?:_[^_]+)*?__(.+)$/.exec(name)
  return m?.[1] ?? name
}
