#!/usr/bin/env sh
# Install `abx` and its skills for an agent that is not Claude Code.
#
#   curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
#
# Claude Code users should install the plugin instead — it puts the API key in
# the OS keychain and keeps it out of the conversation, which this script
# cannot do.
#
# POSIX sh on purpose: this runs inside sandbox images whose shell is ash.
set -eu

BASE="${AGBX_CLI_BASE:-https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest}"
BIN_DIR="${AGBX_BIN_DIR:-$HOME/.local/bin}"
SKILL_DIR="${AGBX_SKILL_DIR:-$HOME/.agents/skills}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "abx: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=x64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "abx: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac
name="abx-$os-$arch"

tmp="$(mktemp)"
echo "downloading $name"
curl -fsSL "$BASE/$name" -o "$tmp"

# Verify before installing, not after. A truncated download over a flaky link
# otherwise lands as a file that exists, is executable, and fails in a way
# nobody traces back to the network.
if sha="$(curl -fsSL "$BASE/$name.sha256" 2>/dev/null | awk '{print $1}')" && [ -n "$sha" ]; then
  if command -v sha256sum >/dev/null 2>&1; then
    echo "$sha  $tmp" | sha256sum -c - >/dev/null || { rm -f "$tmp"; echo "abx: checksum mismatch" >&2; exit 1; }
  elif command -v shasum >/dev/null 2>&1; then
    echo "$sha  $tmp" | shasum -a 256 -c - >/dev/null || { rm -f "$tmp"; echo "abx: checksum mismatch" >&2; exit 1; }
  fi
fi

mkdir -p "$BIN_DIR"
chmod +x "$tmp"
mv -f "$tmp" "$BIN_DIR/abx"
echo "installed $BIN_DIR/abx"

# Skills are plain Markdown; every harness that reads a skills directory reads
# these. Fetched individually because the bucket serves objects, not archives.
mkdir -p "$SKILL_DIR"
for s in abx-common abx-reinforcement-learning abx-managed-agent \
         abx-resource-capacity abx-harbor-framework abx-observe \
         abx-sandbox-docker abx-sandbox-network abx-sandbox-secrets; do
  mkdir -p "$SKILL_DIR/$s"
  if curl -fsSL "$BASE/skills/$s/SKILL.md" -o "$SKILL_DIR/$s/SKILL.md" 2>/dev/null; then
    echo "installed $SKILL_DIR/$s/SKILL.md"
  fi
done

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo; echo "add $BIN_DIR to PATH:"; echo "  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

cat <<'EOF'

next:
  export AGENTBOX_ENDPOINT=...    # your console's address; reaches every cluster
  export AGENTBOX_API_KEY=...     # issued in the console under API keys
  abx whoami
  abx agent-context               # the whole tool, as JSON
EOF
