"""SandboxTemplate: the starting point of the onboarding flow."""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json("/sandbox-templates"))


def _get(ctx, name: str) -> dict[str, Any]:
    payload = ctx.get_json(f"/sandbox-templates/{name}")
    tpl = payload.get("template") if isinstance(payload, dict) else None
    body = dict(tpl or payload or {})
    # The full CRD is a screenful of Pod spec and is never what a chooser
    # needs; it stays reachable as the `yaml` section.
    body.pop("crdYaml", None)
    body.pop("docs", None)
    return body


def _next_list(ctx, rows) -> list[NextAction]:
    out = [
        NextAction(
            cmd=f"abx templates <name>{_cl(ctx)}",
            reason="one template's detail (cpu/memory, runtimes, docs)",
        )
    ]
    if rows:
        out.append(
            NextAction(
                cmd=f"abx envs create --name <env> "
                f"--template {rows[0].get('name')}{_cl(ctx)}",
                reason="bind a template into a new environment",
            )
        )
    return out


def _next_get(ctx, name: str, row) -> list[NextAction]:
    return [
        NextAction(
            cmd=f"abx envs create --name <env> --template {name}{_cl(ctx)}",
            reason="create an environment from this template",
        ),
        NextAction(
            cmd=f"abx templates {name} yaml{_cl(ctx)}",
            reason="the full SandboxTemplate CRD",
        ),
    ]


def _cl(ctx) -> str:
    return f" --cluster {ctx.cluster}" if ctx.cluster else ""


def _yaml_section(ctx, name: str) -> list[dict[str, Any]]:
    payload = ctx.get_json(f"/sandbox-templates/{name}")
    tpl = (payload or {}).get("template") or {}
    return [{"yaml": tpl.get("crdYaml", "")}]


register_kind(
    ResourceKind(
        kind="template",
        plural="templates",
        label="Sandbox templates",
        describe=(
            "Cluster-scoped Pod blueprints. A template fixes the idle "
            "image, the "
            "runtimes (e.g. envd) and the default cpu/memory; an "
            "environment binds "
            "one. Start here when choosing what a sandbox should be."
        ),
        columns=(
            Column("name", lambda r: r.get("name")),
            Column("cpu", lambda r: r.get("cpu")),
            Column("memory", lambda r: r.get("memory")),
            Column("version", lambda r: r.get("version")),
            Column("hasDocs", lambda r: r.get("hasDocs")),
            Column("description", lambda r: r.get("description")),
        ),
        id_of=lambda r: str(r.get("name")),
        fetch_list=_list,
        filters=(
            FilterSpec("name", "substring of the template name"),
            FilterSpec("description", "substring of the description"),
            FilterSpec("version", "template version"),
        ),
        fetch_get=_get,
        id_hint="template name",
        sections=(),
        view_list=lambda ctx, f: {
            "page": "templates",
            "cluster": ctx.cluster,
            "filters": f,
        },
        view_get=lambda ctx, name: {
            "page": "templates",
            "cluster": ctx.cluster,
            "params": {"name": name},
        },
        next_list=_next_list,
        next_get=_next_get,
    )
)
