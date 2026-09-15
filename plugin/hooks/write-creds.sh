#!/usr/bin/env bash
# Bridge the plugin's keychain-backed userConfig into the config file `abx` reads.
#
# Hooks — unlike the bin/ command the agent invokes through Bash — receive the
# CLAUDE_PLUGIN_OPTION_* environment, sensitive values included. That is the
# whole reason this file exists: it is the one place the API key can reach the
# CLI without passing through the agent's context, its argv, or its stdin.
#
# Runs on SessionStart and UserPromptSubmit, and is idempotent — it just
# rewrites the file.
#
# Endpoint and key are both required. With either missing nothing is written,
# so a half-configured install leaves whatever the user set up by hand in place
# rather than overwriting it with a half-built file.
set -euo pipefail

dir="${XDG_CONFIG_HOME:-$HOME/.config}/abx"
mkdir -p "$dir"
umask 077

endpoint="${CLAUDE_PLUGIN_OPTION_ENDPOINT:-}"
api_key="${CLAUDE_PLUGIN_OPTION_API_KEY:-}"

[ -n "$endpoint" ] && [ -n "$api_key" ] || exit 0

{
  printf '{\n'
  printf '  "endpoint": "%s",\n' "$endpoint"
  printf '  "apiKey": "%s"\n' "$api_key"
  printf '}\n'
} >"$dir/config.json"

chmod 600 "$dir/config.json"
