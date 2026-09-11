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
# Build `abx` for every platform we ship, into dist/.
#
# Bun compiles the CLI and its runtime into one file, which is the property
# that matters here: the thing people install has no Node, no npm, and no
# virtualenv behind it. A sandbox image adds it with one curl, and a laptop
# gets the same binary the sandbox has.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/cli"

VERSION="${VERSION:-$(cat "$ROOT/VERSION")}"
OUT="${OUT:-$ROOT/cli/dist}"

# One name per platform, in the spelling `uname -s`/`uname -m` produce, so the
# installer can build the filename from what it already knows rather than
# carrying a translation table that drifts from this list.
TARGETS="
linux-x64:bun-linux-x64
linux-arm64:bun-linux-arm64
darwin-x64:bun-darwin-x64
darwin-arm64:bun-darwin-arm64
"

rm -rf "$OUT"
mkdir -p "$OUT"

for entry in $TARGETS; do
  name="${entry%%:*}"
  target="${entry##*:}"
  echo ">> abx-$name"
  bun build src/index.ts \
    --compile \
    --target "$target" \
    --define "AGBX_CLI_VERSION=\"$VERSION\"" \
    --outfile "$OUT/abx-$name" >/dev/null
done

printf '%s\n' "$VERSION" > "$OUT/VERSION"

# Checksums travel with the binaries so an install can verify what it just
# downloaded. A truncated download over a flaky link otherwise lands as a file
# that exists, is executable, and fails in a way nobody traces back to the
# network.
( cd "$OUT" && shasum -a 256 abx-* > SHA256SUMS )

echo
echo "built $VERSION into $OUT:"
ls -la "$OUT"
