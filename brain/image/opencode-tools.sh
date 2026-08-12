#!/bin/bash
# Install the sandbox tool overrides into OpenCode's config directory.
#
# Usage: opencode-tools.sh <config-dir>       (e.g. /home/agents/.config/opencode)
#
# Each file is a one-liner re-exporting the binding from @scitix/agentbox-hands.
# The FILENAME is what OpenCode keys on, so the list below is the contract — and a
# file that fails to resolve is silent at runtime, which is why this script
# verifies every one of them at build time instead of just writing them.
#
# Both `tool/` and `tools/` are scanned by OpenCode 1.18.16 (verified against the
# binary). `tool/` is used here because it is the name OpenCode's own source uses.
#
# ---------------------------------------------------------------------------
# KNOWN LIMITATION — read before relying on this for confinement.
#
# The binding is written on the premise that a same-named file REPLACES the
# built-in. Against OpenCode 1.18.16 that is not what happens: with a `bash.ts`
# installed, `/experimental/tool/ids` reports `bash` TWICE and the tool listing
# returns both descriptions — the built-in and the override. Setting
# `"tools": {"bash": false}` in opencode.json did not remove either from that
# endpoint.
#
# What that means for a turn — which of the two a model's `bash` call dispatches
# to — is NOT established here; it needs a live model credential to observe, and
# the endpoints above may be reporting the pre-gate registry rather than the tool
# set a prompt is built with. So this is an open question, not a proven hole.
#
# It is load-bearing either way: if the built-in wins, the agent's shell runs on
# THIS POD. That is why the image's default harness is Claude Code, whose
# confinement rests on a different mechanism (`tools: []` plus disallowedTools,
# mcpServers and toolAliases) that the sandbox-escape test drives a real session
# to verify. OpenCode remains available and is the better-tested path for
# everything else; treat its file/shell confinement as unverified until the
# dispatch question is answered.
# ---------------------------------------------------------------------------
set -euo pipefail

CONFIG_DIR=${1:?usage: opencode-tools.sh <config-dir>}
TOOL_DIR="$CONFIG_DIR/tool"

# The seven tools the sandbox toolset replaces. Keep in step with
# sandboxToolset() in sdk/hands/typescript/src/core/tools.ts — a name here that
# the package does not export fails the resolution check below.
TOOLS=(bash read write edit grep glob apply_patch)

mkdir -p "$TOOL_DIR"
for name in "${TOOLS[@]}"; do
    cat >"$TOOL_DIR/$name.ts" <<EOF
// OpenCode override for the sandbox \`$name\` tool. The filename is what selects
// it; all behaviour lives in the shared toolset so the Claude Code and MCP
// bindings cannot drift from this one.
export { default } from '@scitix/agentbox-hands/opencode/tools/$name'
EOF
done

# Prove each override resolves, from the directory OpenCode will load it in.
#
# Resolution walks up from here to /home/agents/node_modules, so this catches the
# two failures that are otherwise invisible until an agent's first tool call: a
# missing dependency tree, and a tool name the package stopped exporting.
cd "$TOOL_DIR"
for name in "${TOOLS[@]}"; do
    if ! bun --eval "
      const m = await import('./$name.ts')
      if (typeof m.default?.execute !== 'function') {
        throw new Error('$name.ts has no executable default export')
      }
    " >/dev/null; then
        echo "opencode-tools.sh: $TOOL_DIR/$name.ts does not resolve" >&2
        exit 1
    fi
done

echo "opencode-tools.sh: installed and verified ${#TOOLS[@]} tool overrides in $TOOL_DIR"
