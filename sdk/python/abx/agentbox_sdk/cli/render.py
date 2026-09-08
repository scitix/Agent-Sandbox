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
Result -> text.

Two rules the rest of the CLI depends on:

  * The header row IS the column id, so the string a caller reads is the string
    they pass to `--filter`.
  * Every trailer has one shape: a `label:` line, then two-space-indented
    content. `view:` / `sections (open: ...)` / `hint:` all follow it, so an
    agent parsing one has parsed them all.
"""

from __future__ import annotations

import csv
import io
import json
from collections.abc import Sequence
from typing import Any

from agentbox_sdk.cli.types import Column, NextAction

INLINE_MAX = 72
COL_CAP = 60


def _cell(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float, str)):
        return str(value)
    compact = json.dumps(value, separators=(",", ":"), ensure_ascii=False)
    return (
        compact
        if len(compact) <= INLINE_MAX
        else compact[: INLINE_MAX - 1] + "…"
    )


def table(
    columns: Sequence[Column], rows: Sequence[dict[str, Any]]
) -> list[str]:
    if not rows:
        return []
    cells = [[_cell(c.get(r)) for c in columns] for r in rows]
    widths = []
    for i, c in enumerate(columns):
        w = (
            max(len(c.id), *(len(row[i]) for row in cells))
            if cells
            else len(c.id)
        )
        widths.append(min(w, COL_CAP))
    out = [
        "  ".join(c.id.ljust(widths[i]) for i, c in enumerate(columns)).rstrip()
    ]
    for row in cells:
        out.append(
            "  ".join(
                row[i].ljust(widths[i]) for i in range(len(columns))
            ).rstrip()
        )
    return out


def render_csv(
    columns: Sequence[Column], rows: Sequence[dict[str, Any]]
) -> str:
    buf = io.StringIO()
    w = csv.writer(buf, lineterminator="\n")
    w.writerow([c.id for c in columns])
    for r in rows:
        w.writerow([_cell(c.get(r)) for c in columns])
    return buf.getvalue().rstrip("\n")


def render_json(data: Any) -> str:
    return json.dumps(data, indent=2, ensure_ascii=False, default=str)


def yaml_lite(value: Any, indent: int = 0) -> list[str]:
    """A readable projection of one object. Not a YAML emitter — a brief."""
    pad = " " * indent
    out: list[str] = []
    if isinstance(value, dict):
        for k, v in value.items():
            if isinstance(v, (dict, list)) and v:
                compact = json.dumps(
                    v, separators=(",", ":"), ensure_ascii=False
                )
                if len(compact) <= INLINE_MAX:
                    out.append(f"{pad}{k}: {compact}")
                else:
                    out.append(f"{pad}{k}:")
                    out.extend(yaml_lite(v, indent + 2))
            else:
                out.append(f"{pad}{k}: {_cell(v)}")
    elif isinstance(value, list):
        for v in value:
            if isinstance(v, (dict, list)):
                out.append(f"{pad}-")
                out.extend(yaml_lite(v, indent + 2))
            else:
                out.append(f"{pad}- {_cell(v)}")
    else:
        out.append(f"{pad}{_cell(value)}")
    return out


def open_page_line(view: dict[str, Any]) -> str:
    parts = [f"page={view['page']}"]
    if view.get("cluster"):
        parts.append(f"cluster={view['cluster']}")
    for key in ("params", "filters"):
        val = view.get(key)
        if val:
            parts.append(
                f"{key}="
                + json.dumps(val, separators=(",", ":"), ensure_ascii=False)
            )
    return "open_page " + " ".join(parts)


def view_url(view: dict[str, Any], web_base: str) -> str | None:
    """A deep link, for a caller that cites it rather than drives a UI."""
    page = view.get("page", "")
    cluster = view.get("cluster")
    if not page:
        return None
    base = web_base.rstrip("/")
    seg = {
        "envs": "envs",
        "env_detail": "envs",
        "pools": "pools",
        "templates": "templates",
        "sandboxes": "sandboxes",
        "quotas": "quota",
        "instancetypes": "templates",
        "clusters": "clusters",
    }.get(page, page)
    path = f"{base}/clusters/{cluster}/{seg}" if cluster else f"{base}/{seg}"
    params = view.get("params") or {}
    ident = params.get("name") or params.get("id")
    if ident:
        path += f"/{ident}"
    filters = view.get("filters") or {}
    if filters:
        from urllib.parse import urlencode

        flat = [(k, v) for k, vs in filters.items() for v in vs]
        path += "?" + urlencode(flat)
    return path


def footer(
    *,
    view: dict[str, Any] | None = None,
    ui_mode: str = "url",
    web_base: str | None = None,
    sections: Sequence[dict[str, str]] = (),
    section_open: str = "",
    hints: Sequence[NextAction] = (),
) -> list[str]:
    out: list[str] = []

    if view:
        if ui_mode == "open_page":
            out += ["view:", "  " + open_page_line(view)]
        elif ui_mode == "url" and web_base:
            url = view_url(view, web_base)
            if url:
                out += ["view:", "  " + url]

    if sections:
        out.append(
            f"sections (open: {section_open}):" if section_open else "sections:"
        )
        w = max(len(s["id"]) for s in sections)
        for s in sections:
            out.append(f"  {s['id'].ljust(w)}  {s['title']}")

    if hints:
        out.append("hint:")
        for h in hints:
            out.append(f"  {h.cmd}  # {h.reason}")

    return out


def approval_block(approval: Any) -> list[str]:
    """The trailer a refused write prints.

    Same shape as every other trailer — a label, then two-space-indented lines —
    so an agent that has parsed one has parsed this one.

    The hint deliberately splits into two turns, and the order is the whole
    point. `approvals wait` BLOCKS, and a tool result only reaches the person
    when the command exits: an agent that runs it in the same turn as the
    refusal shows them nothing until it times out, by which time the link it was
    holding has expired. So the instruction is to surface the link and stop, and
    to wait only once they say they have acted on it.
    """
    lines = ["approval:", "  " + (approval.summary or approval.operation)]
    if approval.url:
        lines.append("  " + approval.url)
    else:
        lines.append("  (no console link configured; approve via the API)")
    scope = "once" if approval.once_only else "once, or for this whole session"
    lines.append(f"  ({scope})")
    lines += [
        "hint:",
        "  # Show the link above and END YOUR TURN. Do NOT run the",
        "  # command below yet: it blocks, and nothing you print reaches",
        "  # the user until it returns. Run it once they have approved:",
        f"  abx approvals wait {approval.id}",
        "  # …then run the original command again.",
    ]
    return lines
