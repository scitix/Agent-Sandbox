"""Clusters this endpoint knows about."""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _list(ctx) -> list[dict[str, Any]]:
    # `/clusters` answers `clusters`, not `items` — the one list endpoint on
    # this API that does.
    return items_of(ctx.get_json("/clusters"), "clusters")


register_kind(
    ResourceKind(
        kind="cluster",
        plural="clusters",
        label="Clusters",
        describe=(
            "Clusters reachable from this endpoint. `local` marks the one "
            "serving this API."
        ),
        columns=(
            Column("id", lambda r: r.get("id")),
            Column("name", lambda r: r.get("name")),
            Column("local", lambda r: r.get("local")),
        ),
        id_of=lambda r: str(r.get("id")),
        fetch_list=_list,
        filters=(
            FilterSpec("id", "cluster id"),
            FilterSpec("name", "cluster display name"),
        ),
        view_list=lambda ctx, f: {"page": "clusters", "filters": f},
        next_list=lambda ctx, rows: [
            NextAction(
                cmd=f"abx templates{cl(ctx)}",
                reason="what a sandbox can be built from",
            )
        ],
    )
)
