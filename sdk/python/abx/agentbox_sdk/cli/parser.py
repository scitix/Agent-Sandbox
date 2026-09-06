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
The flag parser.

Hand-written rather than argparse/click, for one reason: **a flag a command does
not declare must fail, and it must fail before its value can be mistaken for a
positional argument.** A permissive parser turns `--nosuch v` into a boolean
plus a stray `v`, and `v` is then read as the resource id — so the command
runs, against the wrong object, and reports success.
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass


class UsageError(RuntimeError):
    """A malformed invocation. Carries hints the caller can actually run."""

    def __init__(
        self, message: str, hints: Sequence[str] | None = None
    ) -> None:
        super().__init__(message)
        self.hints = list(hints or [])


@dataclass(frozen=True)
class Param:
    name: str  # canonical, camelCase
    kind: str  # "string" | "string[]" | "int" | "bool"
    describe: str
    value_hint: str = ""
    aliases: tuple[str, ...] = ()


def flag_of(name: str) -> str:
    """camelCase -> --kebab-case; one letter -> -x. Inverse of name_of."""
    if len(name) == 1:
        return "-" + name
    out = []
    for ch in name:
        if ch.isupper():
            out.append("-")
            out.append(ch.lower())
        else:
            out.append(ch)
    return "--" + "".join(out)


def name_of(flag: str) -> str:
    body = flag.lstrip("-")
    parts = body.split("-")
    return parts[0] + "".join(p[:1].upper() + p[1:] for p in parts[1:])


@dataclass
class Parsed:
    positionals: list[str]
    values: dict[str, object]
    supplied: list[str]


def _spellings(p: Param) -> list[str]:
    return [p.name, *p.aliases]


def parse(argv: Sequence[str], params: Sequence[Param]) -> Parsed:
    by_spelling: dict[str, Param] = {}
    for p in params:
        for s in _spellings(p):
            by_spelling[s] = p

    positionals: list[str] = []
    values: dict[str, object] = {}
    supplied: list[str] = []

    i = 0
    argv = list(argv)
    while i < len(argv):
        tok = argv[i]
        if not tok.startswith("-") or tok == "-":
            positionals.append(tok)
            i += 1
            continue

        raw, eq, inline = tok.partition("=")
        canonical = name_of(raw)
        p = by_spelling.get(canonical)
        if p is None:
            raise UsageError(f"unknown flag {raw}")

        if p.name not in supplied:
            supplied.append(p.name)

        if p.kind == "bool":
            values[p.name] = (inline != "false") if eq else True
            i += 1
            continue

        if eq:
            val = inline
            i += 1
        else:
            if i + 1 >= len(argv):
                raise UsageError(f"{raw} needs a value")
            val = argv[i + 1]
            i += 2

        if p.kind == "string[]":
            cur = values.setdefault(p.name, [])
            assert isinstance(cur, list)
            cur.append(val)
        elif p.kind == "int":
            try:
                values[p.name] = int(val)
            except ValueError:
                # Coercion failures are fatal rather than ignored: a dropped
                # --limit silently returns the default page, which reads as
                # "that is all there is".
                raise UsageError(
                    f"{raw} expects an integer, got {val!r}"
                ) from None
        else:
            values[p.name] = val

    return Parsed(positionals=positionals, values=values, supplied=supplied)


def parse_filters(raw: Sequence[str]) -> dict[str, list[str]]:
    """`--filter k=v` (repeatable, and `v` may be comma-joined)."""
    out: dict[str, list[str]] = {}
    for item in raw:
        key, eq, val = str(item).partition("=")
        if not eq:
            raise UsageError(
                f"--filter expects key=value, got {item!r}",
                ["--filter phase=Ready"],
            )
        out.setdefault(key.strip(), []).extend(
            v.strip() for v in val.split(",") if v.strip()
        )
    return out
