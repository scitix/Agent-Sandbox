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
SandboxPool resource and PoolsAPI namespace for agentbox_sdk.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, Optional

from agentbox_sdk._http import raise_for_status
from agentbox_sdk.models import PagedResult, SandboxPoolData

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient


class SandboxPool:
    """A SandboxPool resource object."""

    def __init__(self, data: SandboxPoolData, api: "PoolsAPI") -> None:
        self._data = data
        self._api = api

    @property
    def name(self) -> str:
        return self._data.name

    @property
    def replicas(self) -> int:
        return self._data.spec.replicas

    @property
    def idle_replicas(self) -> Optional[int]:
        return self._data.status.idle_replicas

    @property
    def running_replicas(self) -> Optional[int]:
        return self._data.status.running_replicas

    @property
    def failed_replicas(self) -> Optional[int]:
        return self._data.status.failed_replicas

    @property
    def data(self) -> SandboxPoolData:
        return self._data

    async def refresh(self) -> "SandboxPool":
        """Fetch the latest state from the API."""
        updated = await self._api.get(self.name)
        self._data = updated._data
        return self

    async def scale(self, replicas: int) -> "SandboxPool":
        """Scale this pool to *replicas* pods."""
        return await self._api.scale(self.name, replicas)

    async def delete(self) -> None:
        """Delete this pool."""
        await self._api.delete(self.name)

    def __repr__(self) -> str:
        return (
            f"<SandboxPool name={self.name!r} replicas={self.replicas} "
            f"idle={self.idle_replicas} running={self.running_replicas}>"
        )


class PoolsAPI:
    """Operations on SandboxPool resources (accessed via ``client.pools``)."""

    def __init__(self, client: "AuthenticatedClient") -> None:
        self._client = client

    def _make(self, raw: dict) -> SandboxPool:
        # API wraps pool in {"template": {...}}
        if "template" in raw and isinstance(raw["template"], dict):
            raw = raw["template"]
        return SandboxPool(SandboxPoolData.model_validate(raw), self)

    async def create(
        self,
        name: str,
        *,
        template_name: Optional[str] = None,
        replicas: Optional[int] = None,
        min_replicas: Optional[int] = None,
        max_replicas: Optional[int] = None,
        labels: Optional[Dict[str, str]] = None,
        annotations: Optional[Dict[str, str]] = None,
        quota_url: Optional[str] = None,
    ) -> SandboxPool:
        """Create a new SandboxPool.

        The ``quota_url`` parameter is passed via labels
        (``quota.scitix.ai/url``) rather than the deprecated top-level
        ``quotaUrl`` field.
        """
        from agentbox_sdk._generated.api.pools import create_sandbox_pool
        from agentbox_sdk._generated.models.create_sandbox_pool_request import (
            CreateSandboxPoolRequest,
        )
        from agentbox_sdk._generated.models.create_sandbox_pool_request_annotations import (
            CreateSandboxPoolRequestAnnotations,
        )
        from agentbox_sdk._generated.models.create_sandbox_pool_request_labels import (
            CreateSandboxPoolRequestLabels,
        )
        from agentbox_sdk._generated.types import UNSET

        # Merge quota_url into labels (replaces the deprecated top-level quotaUrl field).
        merged_labels = dict(labels) if labels else {}
        if quota_url:
            merged_labels["quota.scitix.ai/url"] = quota_url

        body = CreateSandboxPoolRequest(
            name=name,
            template_name=template_name if template_name is not None else UNSET,
            replicas=replicas if replicas is not None else UNSET,
            min_replicas=min_replicas if min_replicas is not None else UNSET,
            max_replicas=max_replicas if max_replicas is not None else UNSET,
            labels=(
                CreateSandboxPoolRequestLabels.from_dict(merged_labels)
                if merged_labels
                else UNSET
            ),
            annotations=(
                CreateSandboxPoolRequestAnnotations.from_dict(annotations)
                if annotations is not None
                else UNSET
            ),
            # quota_url is now passed via labels; keep the field unset.
        )

        resp = await create_sandbox_pool.asyncio_detailed(
            client=self._client, body=body
        )
        raise_for_status(resp, context=f"create pool {name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict())

    async def get(self, name: str) -> SandboxPool:
        """Fetch a pool by name."""
        from agentbox_sdk._generated.api.pools import get_sandbox_pool

        resp = await get_sandbox_pool.asyncio_detailed(
            name, client=self._client
        )
        raise_for_status(resp, context=f"get pool {name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict())

    async def list(
        self,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> "PagedResult[SandboxPoolData]":
        """List all pools."""
        from agentbox_sdk._generated.api.pools import list_sandbox_pools

        resp = await list_sandbox_pools.asyncio_detailed(client=self._client)
        raise_for_status(resp, context="list pools")
        assert resp.parsed is not None
        items = [
            SandboxPoolData.model_validate(p.to_dict())
            for p in resp.parsed.items
        ]
        return PagedResult[SandboxPoolData](
            items=items,
            total=resp.parsed.total,
            limit=resp.parsed.limit,
            offset=resp.parsed.offset,
        )

    async def scale(self, name: str, replicas: int) -> SandboxPool:
        """Scale a pool to *replicas* pods."""
        from agentbox_sdk._generated.api.pools import update_sandbox_pool
        from agentbox_sdk._generated.models.update_sandbox_pool_request import (
            UpdateSandboxPoolRequest,
        )

        body = UpdateSandboxPoolRequest(replicas=replicas)
        resp = await update_sandbox_pool.asyncio_detailed(
            name, client=self._client, body=body
        )
        raise_for_status(resp, context=f"scale pool {name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict())

    async def delete(self, name: str) -> None:
        """Delete a pool."""
        from agentbox_sdk._generated.api.pools import delete_sandbox_pool

        resp = await delete_sandbox_pool.asyncio_detailed(
            name, client=self._client
        )
        raise_for_status(resp, context=f"delete pool {name!r}")
