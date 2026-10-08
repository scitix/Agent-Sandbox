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
Check that every control-plane route the installed e2b SDK calls is served.

The patch redirects the SDK to an Agent Sandbox server, which serves the E2B
API generated from a vendored copy of the upstream spec. When the SDK starts
calling an operation that copy does not declare, the server answers with a
bare router 404 ("404 page not found") before any handler runs. That is how
e2b 2.51 broke: create moved to POST /v2/sandboxes. Checking only the patch's
own hook points cannot see this, so this script compares the routes instead.

A declared route can still answer 501 (pause, snapshots, ...). That is a
documented feature gap with a message, not a broken SDK, so it passes.

Usage:
    python check_sdk_routes.py <path to pkg/openapi/e2b/openapi.yaml>

Exit status is 1 when a route is missing.
"""

import importlib.metadata
import importlib.util
import re
import sys
from pathlib import Path

# The modules behind Sandbox.create / connect / kill / list / set_timeout / ...
# Template building and volumes are not served by Agent Sandbox at all.
_SANDBOX_MODULES = ("e2b.sandbox_sync", "e2b.sandbox_async")

_OP_CALL = re.compile(
    r"\b((?:get|post|put|patch|delete)_[a-z0-9_]+)\.(?:sync_detailed|asyncio_detailed|sync|asyncio)\b"
)
_METHOD = re.compile(r'"method":\s*"([a-z]+)"')
_URL = re.compile(r'"url":\s*f?"([^"]+)"')
_SPEC_PATH = re.compile(r"^  (/[^:\s]*):\s*$")
_SPEC_METHOD = re.compile(r"^    (get|post|put|patch|delete):\s*$")


def _package_dir(name: str) -> Path:
    spec = importlib.util.find_spec(name)
    if spec is None or not spec.submodule_search_locations:
        raise SystemExit(f"cannot locate package {name}")
    return Path(next(iter(spec.submodule_search_locations)))


def _normalize(path: str) -> str:
    # {sandbox_id} in the SDK, {sandboxID} in the spec: the name is irrelevant.
    return re.sub(r"\{[^}]+\}", "{}", path)


def sdk_routes() -> set[tuple[str, str]]:
    ops: set[str] = set()
    for mod in _SANDBOX_MODULES:
        for f in _package_dir(mod).rglob("*.py"):
            ops.update(_OP_CALL.findall(f.read_text()))

    api_dir = _package_dir("e2b.api.client.api")
    routes: set[tuple[str, str]] = set()
    for op in sorted(ops):
        files = list(api_dir.rglob(f"{op}.py"))
        if not files:
            raise SystemExit(f"SDK calls {op}, no client module defines it")
        src = files[0].read_text()
        method, url = _METHOD.search(src), _URL.search(src)
        if not (method and url):
            raise SystemExit(f"cannot read method/url from {files[0]}")
        routes.add((method.group(1).upper(), _normalize(url.group(1))))
    return routes


def spec_routes(spec: Path) -> set[tuple[str, str]]:
    routes: set[tuple[str, str]] = set()
    in_paths, current = False, None
    for line in spec.read_text().splitlines():
        if line.startswith("paths:"):
            in_paths = True
            continue
        if in_paths and line and not line.startswith(" "):
            break
        if not in_paths:
            continue
        if m := _SPEC_PATH.match(line):
            current = _normalize(m.group(1))
        elif current and (m := _SPEC_METHOD.match(line)):
            routes.add((m.group(1).upper(), current))
    return routes


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__.strip().split("Usage:")[1], file=sys.stderr)
        return 2
    spec = Path(sys.argv[1])
    served = spec_routes(spec)
    called = sdk_routes()
    missing = sorted(called - served, key=lambda r: (r[1], r[0]))

    print(f"e2b={importlib.metadata.version('e2b')}  spec={spec}")
    for method, path in sorted(called, key=lambda r: (r[1], r[0])):
        mark = "MISSING" if (method, path) in missing else "ok"
        print(f"  {mark:7} {method:6} {path}")
    if missing:
        print(
            f"\n{len(missing)} route(s) called by this SDK are not in the "
            "server's E2B spec, so the server answers them with a 404. Bump "
            "E2B_SPEC_VERSION, run `make sync-e2b-spec generate-api`, and "
            "implement or stub the new operations.",
            file=sys.stderr,
        )
        return 1
    print("all SDK routes are served")
    return 0


if __name__ == "__main__":
    sys.exit(main())
