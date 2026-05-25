#!/usr/bin/env python3
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

"""
check-openapi-fumadocs.py — validate openapi.yaml against the FumaDocs
constraints used by docs/website.

The docs site loads the spec with `groupBy: 'tag'`, which means FumaDocs
expects:
  1. Every operation declares at least one entry in `tags:`.
  2. Every tag referenced by an operation also appears in the top-level
     `tags:` array (so the auto-preset can read its display name).

When (2) is violated, the production build dies with the unhelpful error
`Cannot destructure property 'displayName' of 'a.fromTagName(...)' as it
is undefined.` This script surfaces the same condition early.

Exit codes:
    0  All checks passed
    1  One or more violations found
"""

from __future__ import annotations

import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

OPENAPI_PATH = Path("pkg/openapi/native/openapi.yaml")
HTTP_METHODS = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}


def main() -> int:
    if not OPENAPI_PATH.exists():
        print(f"ERROR: {OPENAPI_PATH} not found (run from repo root)", file=sys.stderr)
        return 1

    with OPENAPI_PATH.open() as f:
        spec = yaml.safe_load(f)

    declared = {t["name"] for t in (spec.get("tags") or []) if isinstance(t, dict) and "name" in t}

    untagged: list[str] = []
    undeclared: dict[str, list[str]] = {}

    for path, item in (spec.get("paths") or {}).items():
        if not isinstance(item, dict):
            continue
        for method, op in item.items():
            if method.lower() not in HTTP_METHODS or not isinstance(op, dict):
                continue
            op_id = op.get("operationId") or f"{method.upper()} {path}"
            tags = op.get("tags") or []
            if not tags:
                untagged.append(f"{method.upper()} {path} ({op_id})")
                continue
            for tag in tags:
                if tag not in declared:
                    undeclared.setdefault(tag, []).append(f"{method.upper()} {path} ({op_id})")

    errors = 0

    if untagged:
        errors += len(untagged)
        print("ERROR: operations without any `tags:` entry (FumaDocs groupBy='tag' requires at least one):", file=sys.stderr)
        for line in untagged:
            print(f"  - {line}", file=sys.stderr)

    if undeclared:
        errors += sum(len(v) for v in undeclared.values())
        print("ERROR: operations reference tags that are not declared in top-level `tags:`:", file=sys.stderr)
        for tag, ops in sorted(undeclared.items()):
            print(f"  tag '{tag}' (used by {len(ops)} operation(s)):", file=sys.stderr)
            for op in ops:
                print(f"    - {op}", file=sys.stderr)
        print(
            "\nFix: add a `- name: <tag>` entry under top-level `tags:` in "
            f"{OPENAPI_PATH} for each missing tag.",
            file=sys.stderr,
        )

    if errors:
        return 1

    print(f"OK: {OPENAPI_PATH} satisfies FumaDocs tag constraints.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
