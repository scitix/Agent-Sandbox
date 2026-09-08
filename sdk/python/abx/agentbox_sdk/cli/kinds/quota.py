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
Quota: what a team may charge a reservation against.

The read is tenant-scoped and the server refuses to guess: an admin key must
name a team and a user (`--as-team` / `--as-user`, sent as the impersonation
headers) or the call fails with that instruction rather than an empty list.
"""

from __future__ import annotations

from typing import Any

from agentbox_sdk.cli.context import items_of
from agentbox_sdk.cli.kinds._common import cl
from agentbox_sdk.cli.registry import register_kind
from agentbox_sdk.cli.types import Column, FilterSpec, NextAction, ResourceKind


def _list(ctx) -> list[dict[str, Any]]:
    return items_of(ctx.get_json("/quotas"))


def _next_list(ctx, rows) -> list[NextAction]:
    if rows:
        url = rows[0].get("url") or rows[0].get("id") or "<quota-url>"
        return [
            NextAction(
                cmd=(
                    f"abx pools create --env <env> --instance-type <it> "
                    f"--replicas 1 --quota {url}{cl(ctx)}"
                ),
                reason="charge a new pool against this quota",
            )
        ]
    # An empty quota list is a legitimate state, and saying so beats a bare
    # "0 rows": a pool can still be created, it just reserves nothing.
    return [
        NextAction(
            cmd=f"abx pools create --env <env> "
            f"--instance-type <it> --replicas 1{cl(ctx)}",
            reason=(
                "no quota is configured for this team; "
                "create a pool without one"
            ),
        )
    ]


register_kind(
    ResourceKind(
        kind="quota",
        plural="quotas",
        label="Quotas",
        describe=(
            "Reservation budgets visible to the calling team. A pool "
            "selects one "
            "by URL and the reservation plugin charges the pool's envelope "
            "against it. Reads are tenant-scoped."
        ),
        columns=(
            Column("name", lambda r: r.get("name") or r.get("id")),
            Column("url", lambda r: r.get("url")),
            Column("type", lambda r: r.get("type")),
            Column("total", lambda r: r.get("total")),
            Column("used", lambda r: r.get("used")),
        ),
        id_of=lambda r: str(r.get("name") or r.get("id") or ""),
        fetch_list=_list,
        filters=(
            FilterSpec("name", "substring of the quota name"),
            FilterSpec("type", "quota type"),
        ),
        view_list=lambda ctx, f: {
            "page": "quotas",
            "cluster": ctx.cluster,
            "filters": f,
        },
        next_list=_next_list,
    )
)
