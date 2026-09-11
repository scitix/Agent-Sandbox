#!/usr/bin/env bash
# Copyright 2026 ScitiX
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Assemble the plugin: build the binaries, bundle them, and lay out the tree a
# release upload copies verbatim.
#
#   hack/build-plugin.sh            -> plugin/dist/ + plugin/release/
#
# The bundled copies in plugin/dist/ are the shim's fallback, so the plugin
# keeps working with no network at all; plugin/release/ is what goes to the
# public bucket for self-update and for `install.sh`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-$(cat "$ROOT/VERSION")}"

echo ">> binaries"
OUT="$ROOT/plugin/dist" VERSION="$VERSION" bash "$ROOT/hack/build-abx.sh" >/dev/null

REL="$ROOT/plugin/release"
rm -rf "$REL"
mkdir -p "$REL/skills"

cp "$ROOT/plugin/dist"/abx-* "$REL/"
cp "$ROOT/plugin/dist/VERSION" "$REL/VERSION"
cp "$ROOT/plugin/install.sh" "$REL/install.sh"

# One .sha256 per binary, next to it. The shim fetches exactly this file, so
# publishing the combined SHA256SUMS instead would leave self-update unverified
# while looking like it was covered.
( cd "$REL" && for f in abx-*; do
    case "$f" in *.sha256) continue ;; esac
    shasum -a 256 "$f" > "$f.sha256"
  done )

# Skills travel with the release so install.sh can fetch them individually —
# the bucket serves objects, not archives.
for d in "$ROOT/plugin/skills"/*/; do
  name="$(basename "$d")"
  mkdir -p "$REL/skills/$name"
  cp "$d/SKILL.md" "$REL/skills/$name/SKILL.md"
done

echo
echo "plugin $VERSION assembled:"
echo "  bundled  plugin/dist/    ($(ls "$ROOT/plugin/dist" | grep -c '^abx-') binaries)"
echo "  release  plugin/release/ -> upload to packages/agentbox/cli/latest/"
