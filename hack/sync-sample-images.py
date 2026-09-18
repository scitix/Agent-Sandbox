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
"""Keep config/samples' image tags on the versions the platform is built for.

    hack/sync-sample-images.py            rewrite the samples in place
    hack/sync-sample-images.py --check    report what is out of date; exit 1

The samples are what a reader applies first, so a stale tag is a first-run
failure: the repository shipped `agent-sandbox-idle:0.0.1` for months, a tag
that was never published, and every fresh install following the sample sat in
ImagePullBackOff. This exists so that cannot happen again by hand.

"Newest published" is NOT the rule, because the images are not the same kind of
thing. Each has an authority, and the script goes to that authority:

  agent-sandbox-envd   the newest published tag, in two parts. Its base version
                       must equal DefaultEnvdVersion (pkg/e2bcompat/domain/
                       convert.go), because that constant is what the API
                       reports to every E2B client that asks and what the SDK
                       trusts — a template running a different envd is one the
                       platform disagrees with. Within one version the image
                       can still be rebuilt, which the registry spells with a
                       `-N` suffix: `0.9.0-2` is the third image of envd 0.9.0,
                       and it is what the platform runs.
  agent-sandbox-idle   newest release tag on GHCR. It carries the idle
                       entrypoint and the egress proxy, its tags come from the
                       repository's own release tags, and no constant names it
                       — the registry is the record.
  agent-sandbox-tini   newest published `vX.Y.Z-static`.
  docker:<N>-dind      newest stable major on Docker Hub; upstream's image, so
                       upstream is the authority.

What this proves, and what it does not: a tag that exists is not a sandbox that
boots. This keeps the pins on the versions the platform expects, so the runtime
inside a sample agrees with what the API reports to the SDK. The end-to-end
proof is a cluster running test/e2e, which this script has no cluster for.
"""

import argparse
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SAMPLES = ROOT / "config" / "samples"
ENVD_CONSTANT = ROOT / "pkg" / "e2bcompat" / "domain" / "convert.go"


def get_json(url: str, headers: dict | None = None):
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=20) as r:
        return json.load(r)


def newest(tags: list[str], pattern: str) -> str | None:
    """The highest tag matching `pattern`, comparing the numbers it contains.

    Not `sort -V` on the strings: 0.0.10 is newer than 0.0.9 and older than
    0.0.9 in a plain sort, which is exactly the mistake this script exists to
    avoid making by hand.
    """
    rx = re.compile(pattern)
    matching = [t for t in tags if rx.match(t)]
    if not matching:
        return None
    return max(matching, key=lambda t: [int(p) for p in re.findall(r"\d+", t)])


def ghcr_latest(repo: str, pattern: str) -> str | None:
    token = get_json(f"https://ghcr.io/token?scope=repository:{repo}:pull&service=ghcr.io").get("token", "")
    if not token:
        return None
    tags = get_json(f"https://ghcr.io/v2/{repo}/tags/list", {"Authorization": f"Bearer {token}"}).get("tags") or []
    return newest(tags, pattern)


def dockerhub_latest(repo: str, pattern: str) -> str | None:
    page = get_json(f"https://hub.docker.com/v2/repositories/library/{repo}/tags?page_size=100")
    return newest([t.get("name", "") for t in page.get("results", [])], pattern)


def envd_declared() -> str | None:
    """The version the API answers with, read from the code that answers."""
    m = re.search(r'^\s*DefaultEnvdVersion\s*=\s*"([^"]+)"', ENVD_CONSTANT.read_text(), re.M)
    return m.group(1) if m else None


def envd_tag() -> str | None:
    """The newest published envd image whose version the API agrees with.

    Two authorities, and both have to hold: the registry says which images
    exist (`0.9.0-2` is a rebuild of 0.9.0), and the constant says which version
    the platform reports. If the registry has moved on without the constant,
    that is a platform change in progress rather than something to paper over
    in a sample — say so instead of silently picking one.
    """
    declared = envd_declared()
    published = ghcr_latest("scitix/agent-sandbox-envd", r"^[0-9]+\.[0-9]+\.[0-9]+(-[0-9]+)?$")
    if not published:
        return None
    base = published.split("-")[0]
    if declared and base != declared:
        print(
            f"  ! envd: GHCR publishes {published}, but DefaultEnvdVersion is {declared}.\n"
            f"    The API would report {declared} to the SDK while the sandbox runs {base}.\n"
            f"    Bump the constant (and the runbook — hack/check-envd-version.sh) first.",
            file=sys.stderr,
        )
        return None
    return published


def resolve() -> list[tuple[str, str | None]]:
    """(image reference without its tag, the tag it should carry)."""
    return [
        ("ghcr.io/scitix/agent-sandbox-envd", envd_tag()),
        ("ghcr.io/scitix/agent-sandbox-idle", ghcr_latest("scitix/agent-sandbox-idle", r"^[0-9]+\.[0-9]+\.[0-9]+$")),
        ("ghcr.io/scitix/agent-sandbox-tini", ghcr_latest("scitix/agent-sandbox-tini", r"^v[0-9.]+-static$")),
        ("docker", dockerhub_latest("docker", r"^[0-9]+-dind$")),
    ]


def references(text: str, image: str) -> set[str]:
    """Every tag `image` is used with in a file.

    Anchored so `docker:` does not match inside a name that merely ends in
    `-docker`, and so a tag is read to its end rather than to the next dot.
    """
    return set(re.findall(rf"(?<![\w.-]){re.escape(image)}:([0-9A-Za-z][0-9A-Za-z._-]*)", text))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true", help="report drift instead of rewriting")
    args = ap.parse_args()

    files = sorted(SAMPLES.glob("*.yaml"))
    if not files:
        print(f"sync-sample-images: no samples under {SAMPLES}", file=sys.stderr)
        return 2

    unresolved, stale, rewrites = [], [], []
    for image, tag in resolve():
        if not tag:
            unresolved.append(image)
            continue
        for path in files:
            text = path.read_text()
            for current in sorted(references(text, image)):
                if current == tag:
                    continue
                rel = path.relative_to(ROOT)
                stale.append(f"  {rel}: {image}:{current} → {image}:{tag}")
                if not args.check:
                    text = re.sub(
                        rf"(?<![\w.-]){re.escape(image)}:{re.escape(current)}(?![0-9A-Za-z._-])",
                        f"{image}:{tag}",
                        text,
                    )
            if not args.check and text != path.read_text():
                if path not in rewrites:
                    rewrites.append(path)
                path.write_text(text)

    for line in stale:
        print(line)

    if unresolved:
        # Offline or a registry having a bad day. Under --check that is a
        # warning, because an offline commit is not a broken commit; when
        # rewriting it is fatal, because there is nothing to write.
        print(f"  ! could not resolve: {', '.join(unresolved)}" + (" (no network?)" if args.check else ""), file=sys.stderr)
        if not args.check:
            return 1

    if args.check:
        if stale:
            print("\nsync-sample-images: FAILED — config/samples is behind the versions above.", file=sys.stderr)
            print("  run: hack/sync-sample-images.py", file=sys.stderr)
            return 1
        print("sync-sample-images: OK — samples match the published versions")
        return 0

    print("\nsync-sample-images: rewrote " + (", ".join(str(p.relative_to(ROOT)) for p in rewrites) if rewrites else "nothing"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
