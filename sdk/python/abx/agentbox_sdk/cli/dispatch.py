"""
Kind-agnostic list / get, plus the filter-validation rule that makes a typo
loud.
"""

from __future__ import annotations

from collections.abc import Sequence

from agentbox_sdk.cli.parser import UsageError
from agentbox_sdk.cli.types import Column, ResourceKind, Row


def validate_filters(
    kind: ResourceKind,
    raw: dict[str, list[str]],
    *,
    columns: Sequence[Column] | None = None,
    where: str = "",
) -> dict[str, list[str]]:
    """
    Canonicalise filter keys, and REJECT any the resource does not declare.

    An unknown key is a hard failure, not a warning. Dropping it and applying
    the rest returns the UNFILTERED set, which is indistinguishable from "these
    are the rows matching your filter" — so one typo answers
    `--filter pool=p1` with the whole cluster, and whoever asked (a person or an
    agent) reports that as p1's.
    """
    declared: dict[str, str] = {}
    if columns is not None:
        for c in columns:
            declared[c.id.lower()] = c.id
    else:
        for f in kind.filters:
            declared[f.key.lower()] = f.key
        for c in kind.columns:
            declared.setdefault(c.id.lower(), c.id)

    out: dict[str, list[str]] = {}
    unknown: list[str] = []
    for key, vals in raw.items():
        canon = declared.get(key.lower())
        if canon:
            out.setdefault(canon, []).extend(vals)
        elif key not in unknown:
            unknown.append(key)

    if unknown:
        raise UsageError(
            _unknown_filter_message(kind, unknown, declared, where)
        )
    return out


def _unknown_filter_message(
    kind: ResourceKind,
    unknown: Sequence[str],
    declared: dict[str, str],
    where: str,
) -> str:
    accepted = []
    for f in kind.filters:
        accepted.append(f"{f.key}={'|'.join(f.values)}" if f.values else f.key)
    if not accepted:
        accepted = sorted(declared.values())

    # Near-miss guessing, but only for a fragment long enough to mean
    # something: a one-character typo that "suggests" half the vocabulary is
    # noise rather than help.
    near: list[str] = []
    for u in unknown:
        lu = u.lower()
        if len(lu) < 3:
            continue
        for key in declared.values():
            lk = key.lower()
            if (lk in lu or lu in lk) and key not in near:
                near.append(key)

    msg = f"unknown --filter key(s): {', '.join(unknown)}"
    if near:
        msg += f" (did you mean {' / '.join(near)}?)"
    target = where or f"`abx {kind.plural}`"
    listed = ", ".join(accepted) if accepted else "(no filters)"
    msg += f". {target} accepts: {listed}."
    msg += f" Run `abx {kind.plural} --help` for the live values."
    return msg


def apply_filters(
    rows: Sequence[Row],
    columns: Sequence[Column],
    filters: dict[str, list[str]],
) -> list[Row]:
    """Substring match, case-insensitive. Keys AND together, values OR."""
    if not filters:
        return list(rows)
    by_id = {c.id: c for c in columns}
    out = []
    for r in rows:
        keep = True
        for key, wanted in filters.items():
            col = by_id.get(key)
            if col is None:
                # Declared but not projected: refuse rather than pass every
                # row, which would look like a filter that matched everything.
                raise UsageError(
                    f'filter "{key}" is declared but has no column to match '
                    f"against (columns: {', '.join(by_id)})"
                )
            got = str(col.get(r) or "").lower()
            if not any(w.lower() in got for w in wanted):
                keep = False
                break
        if keep:
            out.append(r)
    return out


def search_rows(
    rows: Sequence[Row], columns: Sequence[Column], needle: str
) -> list[Row]:
    n = needle.strip().lower()
    if not n:
        return list(rows)
    return [
        r
        for r in rows
        if any(n in str(c.get(r) or "").lower() for c in columns)
    ]


def page(
    rows: Sequence[Row], limit: int | None, offset: int | None
) -> tuple[list[Row], int, int]:
    start = max(0, offset or 0)
    if limit and limit > 0:
        return list(rows[start : start + limit]), start, len(rows)
    return list(rows[start:]), start, len(rows)


def facets(
    rows: Sequence[Row], columns: Sequence[Column], keys: Sequence[str]
) -> list[str]:
    """
    What values ARE present, when a filter matched nothing.

    An empty result plus a non-empty fetch is the moment a caller most needs to
    know the vocabulary; printing it here is cheaper than a second round trip.
    """
    by_id = {c.id: c for c in columns}
    out: list[str] = []
    for key in keys:
        col = by_id.get(key)
        if col is None:
            continue
        counts: dict[str, int] = {}
        for r in rows:
            v = str(col.get(r) or "").strip()
            if v:
                counts[v] = counts.get(v, 0) + 1
        if not counts:
            continue
        top = sorted(counts.items(), key=lambda kv: (-kv[1], kv[0]))[:8]
        shown = ", ".join(f"{v} ({n})" for v, n in top)
        more = "" if len(counts) <= 8 else f", … +{len(counts) - 8} more"
        out.append(f"  {key}: {shown}{more}")
    return out
