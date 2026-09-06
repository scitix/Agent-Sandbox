"""Live sandboxes. Read-only here — creating one is the E2B SDK's job."""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json("/sandboxes"))


def _get(ctx, sid: str) -> dict[str, Any]:
    payload = ctx.get_json(f"/sandboxes/{sid}")
    body = (
        payload.get("sandbox")
        if isinstance(payload, dict) and "sandbox" in payload
        else payload
    )
    return dict(body or {})


register_kind(
    ResourceKind(
        kind="sandbox",
        plural="sandboxes",
        label="Sandboxes",
        describe=(
            "Running sandboxes. Listing and inspection only: sandboxes are "
            "created and driven with the E2B SDK, not with this CLI."
        ),
        columns=(
            Column(
                "sandboxID",
                lambda r: (
                    r.get("sandboxID") or r.get("sandboxId") or r.get("id")
                ),
            ),
            Column("envName", lambda r: r.get("envName") or r.get("poolName")),
            Column("state", lambda r: r.get("state") or r.get("phase")),
            Column(
                "startedAt", lambda r: r.get("startedAt") or r.get("createdAt")
            ),
        ),
        id_of=lambda r: str(
            r.get("sandboxID") or r.get("sandboxId") or r.get("id") or ""
        ),
        fetch_list=_list,
        filters=(
            FilterSpec("envName", "owning environment"),
            FilterSpec("state", "lifecycle state"),
        ),
        fetch_get=_get,
        id_hint="sandbox id",
        view_list=lambda ctx, f: {
            "page": "sandboxes",
            "cluster": ctx.cluster,
            "filters": f,
        },
        next_list=lambda ctx, rows: [
            NextAction(
                cmd=f"abx envs{cl(ctx)}",
                reason="the environments sandboxes come from",
            )
        ],
    )
)
