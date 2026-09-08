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

"""InstanceType: the sizing catalog a pool is charged against."""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl, res_summary
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json("/instancetypes"))


def _next_list(ctx, rows) -> list[NextAction]:
    sample = rows[0].get("name") if rows else "<instance-type>"
    return [
        NextAction(
            cmd=f"abx pools create --env <env> "
            f"--instance-type {sample} --replicas 1{cl(ctx)}",
            reason="size a warm pool from the catalog",
        ),
        NextAction(
            cmd=f"abx quotas{cl(ctx)}",
            reason="what this team may charge against",
        ),
    ]


register_kind(
    ResourceKind(
        kind="instancetype",
        plural="instancetypes",
        label="Instance types",
        describe=(
            "The sizing catalog. A pool names one entry (optionally times a "
            "multiplier) and that product is the reservation envelope quota is "
            "charged for — even when the Pod itself requests less."
        ),
        columns=(
            Column("name", lambda r: r.get("name")),
            Column("showName", lambda r: r.get("showName")),
            Column(
                "baseResources", lambda r: res_summary(r.get("baseResources"))
            ),
            Column("cost", lambda r: r.get("cost")),
            Column("description", lambda r: r.get("description")),
        ),
        id_of=lambda r: str(r.get("name")),
        fetch_list=_list,
        filters=(
            FilterSpec("name", "substring of the instance-type name"),
            FilterSpec("showName", "substring of the display name"),
        ),
        view_list=lambda ctx, f: {
            "page": "instancetypes",
            "cluster": ctx.cluster,
            "filters": f,
        },
        next_list=_next_list,
    )
)
