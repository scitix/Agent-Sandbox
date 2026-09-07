"""
Sandboxes. Read-only here — creating one is the E2B SDK's job.

The list is a LEDGER, not an inventory: it carries finished sandboxes as well
as live ones, distinguished only by `status`. Reading a row count as "this many
are running" is how a healthy cluster gets reported as leaking replicas, so
`status` is projected first and filterable.
"""

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


def _id_of(row: dict[str, Any]) -> str:
    # `sandboxId`, not `sandboxID` — the wire spells it with a lowercase d, and
    # a column that misses it renders blank rather than failing.
    return str(row.get("sandboxId") or "")


register_kind(
    ResourceKind(
        kind="sandbox",
        plural="sandboxes",
        label="Sandboxes",
        describe=(
            "Sandbox records for this tenant, live and finished. Filter on "
            "status=Running to see only what currently holds a pool replica. "
            "Sandboxes are created and driven with the E2B SDK, not this CLI."
        ),
        columns=(
            Column("sandboxId", _id_of),
            Column("status", lambda r: r.get("status")),
            Column("envName", lambda r: r.get("envName")),
            Column("poolName", lambda r: r.get("poolName")),
            Column("cpu", lambda r: r.get("cpu")),
            Column("memory", lambda r: r.get("memory")),
            Column("startedAt", lambda r: r.get("startedAt")),
            Column("durationSeconds", lambda r: r.get("durationSeconds")),
        ),
        id_of=_id_of,
        fetch_list=_list,
        filters=(
            FilterSpec(
                "status",
                "lifecycle status; Running is the only one holding a replica",
                ("Running", "Completed", "Failed"),
            ),
            FilterSpec("envName", "owning environment"),
            FilterSpec("poolName", "owning member pool"),
            FilterSpec("user", "owning user"),
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
                cmd=f"abx sandboxes --filter status=Running{cl(ctx)}",
                reason="only the ones still holding a pool replica",
            ),
            NextAction(
                cmd=f"abx envs{cl(ctx)}",
                reason="the environments sandboxes come from",
            ),
        ],
    )
)
