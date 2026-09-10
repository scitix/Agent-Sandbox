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
`-f` — hand the API a request body instead of assembling one out of flags.

A flag per field stops scaling the moment a field has structure. Egress rules,
labels, per-pool resources and update strategies are all objects, and the CLI
was going to grow a flag for each leaf or refuse to express them at all — while
the console, POSTing the same endpoint, could do all of it already.

So the file IS the request body. Nothing is invented and nothing is mapped:
what you write here is what the console sends and what the OpenAPI describes,
which is also why it needs no schema of its own to drift from.

JSON, not YAML. The API speaks JSON, an agent writes it more reliably, and a
second surface would mean a second set of rules about how it converts. A file
that looks like YAML is refused by name rather than by parse error.
"""

from __future__ import annotations

import json
import sys
from typing import Any

import attrs

from agentbox_sdk.cli.parser import UsageError


def load_body(source: str) -> dict[str, Any]:
    """Read a request body from a path, or from stdin when given `-`."""
    raw = _read(source)
    if not raw.strip():
        raise UsageError(f"{_where(source)} is empty")
    try:
        body = json.loads(raw)
    except json.JSONDecodeError as e:
        if _looks_like_yaml(raw):
            raise UsageError(
                f"{_where(source)} looks like YAML; this takes JSON. "
                "The API speaks JSON, so that is the one shape there are no "
                "conversion rules to learn."
            ) from e
        raise UsageError(f"{_where(source)} is not valid JSON: {e}") from e
    if not isinstance(body, dict):
        raise UsageError(
            f"{_where(source)} must hold a JSON object — one request body, "
            f"not a {type(body).__name__}"
        )
    return body


def reject_unknown(body: dict[str, Any], model: Any, source: str) -> None:
    """Refuse top-level fields the API does not have.

    Locally, and by name. The server would answer 400 for these anyway, but it
    cannot say which FILE they came from, and a misspelt field is otherwise
    reported as one that simply had no effect.

    A set difference against the generated model's fields, NOT `from_dict`:
    that raises on a body missing a required field, so it would turn "you
    forgot the name" into a KeyError from inside the SDK. Top level only —
    nested shapes are the server's to judge, and it has the whole schema.
    """
    known = set(wire_fields(model))
    unknown = sorted(k for k in body if k not in known)
    if not unknown:
        return
    raise UsageError(
        f"{_where(source)} has fields the API does not accept: "
        + ", ".join(unknown),
        [f"accepted: {', '.join(sorted(known))}"],
    )


def wire_fields(model: Any) -> list[str]:
    """The body's accepted top-level keys, as the wire spells them.

    Derived from the generated model rather than written out, so a field added
    to the API shows up here without anyone remembering to.
    """
    return [
        _camel(f.name)
        for f in attrs.fields(model)
        if f.name != "additional_properties"
    ]


def put(body: dict[str, Any], path: tuple[str, ...], value: Any) -> None:
    """Set one leaf, creating the objects above it.

    Used to lay a flag over a body from a file. Only the named leaf is touched:
    `--gateway` must not take the rest of `overrides` with it.
    """
    node = body
    for key in path[:-1]:
        nxt = node.get(key)
        if not isinstance(nxt, dict):
            nxt = {}
            node[key] = nxt
        node = nxt
    node[path[-1]] = value


def _read(source: str) -> str:
    if source == "-":
        return sys.stdin.read()
    try:
        with open(source, encoding="utf-8") as fh:
            return fh.read()
    except OSError as e:
        raise UsageError(f"cannot read {source}: {e}") from e


def _where(source: str) -> str:
    return "the body on stdin" if source == "-" else source


def _looks_like_yaml(raw: str) -> bool:
    # A document that never opens a brace but does use `key:` at the start of a
    # line. Deliberately narrow: the point is a better message for the obvious
    # mistake, not a YAML detector.
    stripped = raw.lstrip()
    if stripped.startswith(("{", "[")):
        return False
    return any(
        line.strip() and not line.lstrip().startswith("#") and ":" in line
        for line in raw.splitlines()
    )


def _camel(snake: str) -> str:
    head, *rest = snake.split("_")
    return head + "".join(p[:1].upper() + p[1:] for p in rest)
