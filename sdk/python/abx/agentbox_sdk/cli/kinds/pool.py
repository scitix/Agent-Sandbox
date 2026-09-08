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
Member SandboxPool: the warm pool that actually holds Pods.

Always addressed through its owning Env, which is why every command here takes
`--env`. There is no bare `/v1/sandboxpools` on this API.
"""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl, spec_of, status_of
from agentbox_sdk.cli.parser import UsageError
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _env_of(ctx) -> str:
    env = getattr(ctx, "env", None)
    if not env:
        raise UsageError(
            "pools live inside an environment: pass --env <name>",
            [f"abx envs{cl(ctx)}"],
        )
    return str(env)


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json(f"/envs/{_env_of(ctx)}/sandboxpools"))


def _get(ctx, name: str) -> dict[str, Any]:
    payload = ctx.get_json(f"/envs/{_env_of(ctx)}/sandboxpools/{name}")
    body = (
        payload.get("pool")
        if isinstance(payload, dict) and "pool" in payload
        else payload
    )
    return dict(body or {})


def _next_list(ctx, rows) -> list[NextAction]:
    env = getattr(ctx, "env", "<env>")
    out = []
    if rows:
        out.append(
            NextAction(
                cmd=f"abx pools {rows[0].get('name')} --env {env}{cl(ctx)}",
                reason="one pool's detail",
            )
        )
    out.append(
        NextAction(
            cmd=f"abx envs {env}{cl(ctx)}", reason="the owning environment"
        )
    )
    return out


register_kind(
    ResourceKind(
        kind="pool",
        plural="pools",
        label="Member pools",
        describe=(
            "Pre-warmed Pod pools belonging to one environment. Sized from an "
            "instance type (times an optional multiplier), optionally charged "
            "against a quota. Requires --env."
        ),
        columns=(
            Column("name", lambda r: r.get("name")),
            Column("scalingGroup", lambda r: r.get("scalingGroup")),
            Column("phase", lambda r: status_of(r, "phase")),
            Column("replicas", lambda r: spec_of(r, "replicas")),
            Column("idleReplicas", lambda r: status_of(r, "idleReplicas")),
            Column(
                "runningReplicas", lambda r: status_of(r, "runningReplicas")
            ),
            Column("cpu", lambda r: r.get("cpu")),
            Column("memory", lambda r: r.get("memory")),
        ),
        id_of=lambda r: str(r.get("name")),
        fetch_list=_list,
        filters=(
            FilterSpec("name", "substring of the pool name"),
            FilterSpec(
                "scalingGroup",
                "autoscaling group (derived from the resource key)",
            ),
            FilterSpec("phase", "pool phase", ("Ready", "Pending", "Failed")),
        ),
        fetch_get=_get,
        id_hint="pool name",
        view_list=lambda ctx, f: {
            "page": "env_detail",
            "cluster": ctx.cluster,
            "params": {"name": getattr(ctx, "env", "") or ""},
            "filters": f,
        },
        view_get=lambda ctx, name: {
            "page": "env_detail",
            "cluster": ctx.cluster,
            "params": {"name": getattr(ctx, "env", "") or "", "pool": name},
        },
        next_list=_next_list,
        next_get=lambda ctx, name, row: [
            NextAction(
                cmd=f"abx envs {getattr(ctx, 'env', '<env>')}{cl(ctx)}",
                reason="the owning environment",
            )
        ],
    )
)
