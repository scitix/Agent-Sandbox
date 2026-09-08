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
