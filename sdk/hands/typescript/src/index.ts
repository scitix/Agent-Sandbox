// Hands: confine an agent harness's file and shell tools to an AgentBox sandbox.
//
// The package is three layers, and only the middle one is meant to be read by
// someone integrating it:
//
//   core/      the seven tools and their behaviour contract, harness-neutral.
//              Nothing here knows what a harness is.
//   harness/*  one thin binding per harness. A binding's whole job is to express
//              "replace these built-ins with these tools" in that harness's own
//              vocabulary; it must not reimplement any behaviour from core.
//   (daemon)   the session-binding layer, in the Python package next door. It
//              owns session -> sandbox: lazy create, idle reclaim, transparent
//              rebuild, and the one-shot notice that says a rebuild happened.
//
// Importing this module gives you the neutral pieces. Import the binding you
// need directly — `@scitix/agentbox-hands/claude-code` or `/opencode` — so a
// deployment that uses one harness does not need the other's peer dependency
// installed.
export {
  type SandboxCtx,
  type SandboxTool,
  type SandboxToolName,
  type ToolResult,
  OFFLOAD_EXEMPT,
  logicalToolName,
  renderToolResult,
  sandboxToolset,
} from './core/tools.ts'

export {
  LINE_THRESHOLD,
  THRESHOLD,
  head,
  offloadToSandbox,
  shouldOffload,
} from './core/offload.ts'

export { proxyUrl, withNotice } from './core/proxy.ts'

export { type ParamsSpec, toSchemaShape } from './core/params.ts'
