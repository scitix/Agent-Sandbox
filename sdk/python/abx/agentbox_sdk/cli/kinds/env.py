"""
SandboxEnv: the unit users actually manage.

An Env binds one Template and fans out to member SandboxPools. Pools are never
created directly — `POST /envs/{name}/sandboxpools` is the only supported path,
because a bare SandboxPool skips the Env's rendering and freezing and comes up
without the metadata, revision hash and plugin admission the platform needs.
"""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl, ready_of, spec_of, status_of
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import (
    Column,
    FilterSpec,
    NextAction,
    ResourceKind,
    Section,
)

POOL_COLUMNS = (
    Column("name", lambda r: r.get("name")),
    Column("scalingGroup", lambda r: r.get("scalingGroup")),
    Column("phase", lambda r: status_of(r, "phase")),
    Column("replicas", lambda r: spec_of(r, "replicas")),
    Column("idleReplicas", lambda r: status_of(r, "idleReplicas")),
    Column("runningReplicas", lambda r: status_of(r, "runningReplicas")),
    Column("cpu", lambda r: r.get("cpu")),
    Column("memory", lambda r: r.get("memory")),
    Column("templateVersion", lambda r: r.get("templateVersion")),
)


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json("/envs"))


def _get(ctx, name: str) -> dict[str, Any]:
    payload = ctx.get_json(f"/envs/{name}")
    body = dict(
        (
            payload.get("env")
            if isinstance(payload, dict) and "env" in payload
            else payload
        )
        or {}
    )
    # The full per-cluster status is pages of member bookkeeping and is never
    # what someone choosing or checking an env reads. Summarise it; the member
    # pools are a section of their own.
    status = body.get("status")
    if isinstance(status, dict):
        body["status"] = {
            k: v
            for k, v in status.items()
            if k not in ("clusters",) and not isinstance(v, (list, dict))
        }
        clusters = status.get("clusters")
        if isinstance(clusters, list):
            body["status"]["clusterCount"] = len(clusters)
    return body


def _pools(ctx, name: str) -> list[dict[str, Any]]:
    return items_of(ctx.get_json(f"/envs/{name}/sandboxpools"))


def _events(ctx, name: str) -> list[dict[str, Any]]:
    return items_of(ctx.get_json(f"/envs/{name}/events"))


def _next_list(ctx, rows) -> list[NextAction]:
    out: list[NextAction] = []
    if rows:
        out.append(
            NextAction(
                cmd=f"abx envs {rows[0].get('name')}{cl(ctx)}",
                reason="one environment's detail plus its member pools",
            )
        )
    out.append(
        NextAction(
            cmd=f"abx envs create --name <env> --template <template>{cl(ctx)}",
            reason="create an environment (see `abx templates` first)",
        )
    )
    return out


def _next_get(ctx, name: str, row) -> list[NextAction]:
    return [
        NextAction(
            cmd=f"abx envs {name} pools{cl(ctx)}",
            reason="this env's member pools",
        ),
        NextAction(
            cmd=(
                f"abx pools create --env {name} "
                f"--instance-type <it> --replicas 1{cl(ctx)}"
            ),
            reason="add a warm pool (see `abx instancetypes` and `abx quotas`)",
        ),
        NextAction(
            cmd=f"abx templates "
            f"{row.get('templateName') or '<template>'}{cl(ctx)}",
            reason="the template this env is bound to",
        ),
    ]


register_kind(
    ResourceKind(
        kind="env",
        plural="envs",
        label="Sandbox environments",
        describe=(
            "A SandboxEnv binds one template and fans out to member warm "
            "pools. "
            "This is the object to create first; pools are added to it."
        ),
        columns=(
            Column("name", lambda r: r.get("name")),
            Column("templateName", lambda r: r.get("templateName")),
            Column("mode", lambda r: r.get("mode")),
            Column("memberCount", lambda r: r.get("memberCount")),
            Column("desiredReplicas", lambda r: r.get("desiredReplicas")),
            Column("idleReplicas", lambda r: r.get("idleReplicas")),
            Column("ready", ready_of),
            Column("team", lambda r: r.get("team")),
        ),
        id_of=lambda r: str(r.get("name")),
        fetch_list=_list,
        filters=(
            FilterSpec("name", "substring of the env name"),
            FilterSpec("templateName", "the bound template"),
            FilterSpec(
                "mode", "provisioning mode", ("WarmPool", "OnDemandJob")
            ),
            FilterSpec("ready", "readiness", ("true", "false")),
            FilterSpec("team", "owning team"),
        ),
        fetch_get=_get,
        id_hint="env name",
        sections=(
            Section("pools", "Member pools", _pools, POOL_COLUMNS),
            Section(
                "events",
                "Events",
                _events,
                (
                    Column("type", lambda r: r.get("type")),
                    Column("reason", lambda r: r.get("reason")),
                    Column("message", lambda r: r.get("message")),
                    Column("lastTimestamp", lambda r: r.get("lastTimestamp")),
                ),
            ),
        ),
        view_list=lambda ctx, f: {
            "page": "envs",
            "cluster": ctx.cluster,
            "filters": f,
        },
        view_get=lambda ctx, name: {
            "page": "env_detail",
            "cluster": ctx.cluster,
            "params": {"name": name},
        },
        next_list=_next_list,
        next_get=_next_get,
    )
)
