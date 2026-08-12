// OpenCode override for the sandbox `apply_patch` tool. OpenCode replaces its built-in
// with the same-named file here; all behaviour lives in ../sandbox/tools.ts so
// the Claude Code and MCP bindings cannot drift from it.
import { openCodeSandboxTool } from '../bind.ts'

export default openCodeSandboxTool('apply_patch')
