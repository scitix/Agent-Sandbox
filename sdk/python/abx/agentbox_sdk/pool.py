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

Breaking change in 2026.06: every Pool lives under a SandboxEnv. The API
moved from /v1/sandboxpools to /v1/envs/{env}/sandboxpools, and the server
now derives the PoolName + ScalingGroup from the supplied resources +
quota label. Callers must pass ``env_name`` to every method on PoolsAPI;
the ``create`` method no longer accepts ``name``.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Dict, Optional

from agentbox_sdk._http import raise_for_status
from agentbox_sdk.models import PagedResult, SandboxPoolData

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient


class SandboxPool:
    """A SandboxPool resource object scoped to a SandboxEnv."""

    def __init__(self, data: SandboxPoolData, api: "PoolsAPI", env_name: str) -> None:
        self._data = data
        self._api = api
        self._env_name = env_name

    @property
    def env_name(self) -> str:
        return self._env_name

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
        updated = await self._api.get(self._env_name, self.name)
        self._data = updated._data
        return self

    async def scale(self, replicas: int) -> "SandboxPool":
        """Scale this pool to *replicas* pods (rejected when the pool's
        scalingGroup has autoscaling enabled — only max_replicas is editable
        in that mode)."""
        return await self._api.scale(self._env_name, self.name, replicas)

    async def delete(self) -> None:
        """Delete this pool from its env."""
        await self._api.delete(self._env_name, self.name)

    def __repr__(self) -> str:
        return (
            f"<SandboxPool env={self._env_name!r} name={self.name!r} "
            f"replicas={self.replicas} idle={self.idle_replicas} "
            f"running={self.running_replicas}>"
        )


class PoolsAPI:
    """Env-scoped operations on SandboxPool resources (``client.pools``)."""

    def __init__(self, client: "AuthenticatedClient") -> None:
        self._client = client

    def _make(self, raw: dict, env_name: str) -> SandboxPool:
        # The envelope wraps the pool in {"template": {...}}.
        if "template" in raw and isinstance(raw["template"], dict):
            raw = raw["template"]
        return SandboxPool(SandboxPoolData.model_validate(raw), self, env_name)

    async def create(
        self,
        env_name: str,
        *,
        instance_type: Optional[str] = None,
        multiplier: Optional[int] = None,
        cpu: Optional[str] = None,
        memory: Optional[str] = None,
        replicas: Optional[int] = None,
        max_replicas: Optional[int] = None,
        labels: Optional[Dict[str, str]] = None,
        annotations: Optional[Dict[str, str]] = None,
        quota_url: Optional[str] = None,
    ) -> SandboxPool:
        """Add a member SandboxPool to ``env_name``.

        Pass exactly one of:
          * ``instance_type`` + optional ``multiplier`` (requires the
            InstanceType catalog to be configured server-side), or
          * ``cpu`` + ``memory`` (Kubernetes quantity strings, e.g. "2" /
            "8Gi"); both are written to ``inlineResources`` requests + limits.

        ``quota_url`` is shipped via labels[``quota.scitix.ai/url``] and
        drives the derived pool-name suffix.

        The server derives the PoolName and ScalingGroup — they are NOT
        accepted as inputs.
        """
        from agentbox_sdk._generated.api.pools import create_env_sandbox_pool
        from agentbox_sdk._generated.models.create_env_sandbox_pool_request import (
            CreateEnvSandboxPoolRequest,
        )
        from agentbox_sdk._generated.models.create_env_sandbox_pool_request_annotations import (
            CreateEnvSandboxPoolRequestAnnotations,
        )
        from agentbox_sdk._generated.models.create_env_sandbox_pool_request_labels import (
            CreateEnvSandboxPoolRequestLabels,
        )
        from agentbox_sdk._generated.models.resource_requirements import (
            ResourceRequirements,
        )
        from agentbox_sdk._generated.models.resource_requirements_limits import (
            ResourceRequirementsLimits,
        )
        from agentbox_sdk._generated.models.resource_requirements_requests import (
            ResourceRequirementsRequests,
        )
        from agentbox_sdk._generated.types import UNSET

        merged_labels = dict(labels) if labels else {}
        if quota_url:
            merged_labels["quota.scitix.ai/url"] = quota_url

        inline = UNSET
        if cpu is not None or memory is not None:
            qty: Dict[str, str] = {}
            if cpu is not None:
                qty["cpu"] = cpu
            if memory is not None:
                qty["memory"] = memory
            inline = ResourceRequirements(
                requests=ResourceRequirementsRequests.from_dict(qty),
                limits=ResourceRequirementsLimits.from_dict(qty),
            )

        body = CreateEnvSandboxPoolRequest(
            instance_type=instance_type if instance_type is not None else UNSET,
            multiplier=multiplier if multiplier is not None else UNSET,
            inline_resources=inline,
            replicas=replicas if replicas is not None else UNSET,
            max_replicas=max_replicas if max_replicas is not None else UNSET,
            labels=(
                CreateEnvSandboxPoolRequestLabels.from_dict(merged_labels)
                if merged_labels
                else UNSET
            ),
            annotations=(
                CreateEnvSandboxPoolRequestAnnotations.from_dict(annotations)
                if annotations is not None
                else UNSET
            ),
        )

        resp = await create_env_sandbox_pool.asyncio_detailed(
            env_name, client=self._client, body=body
        )
        raise_for_status(resp, context=f"create pool in env {env_name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict(), env_name)

    async def get(self, env_name: str, name: str) -> SandboxPool:
        """Fetch a pool by name from the given env."""
        from agentbox_sdk._generated.api.pools import get_env_sandbox_pool

        resp = await get_env_sandbox_pool.asyncio_detailed(
            env_name, name, client=self._client
        )
        raise_for_status(resp, context=f"get pool {name!r} in env {env_name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict(), env_name)

    async def list(
        self,
        env_name: str,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> "PagedResult[SandboxPoolData]":
        """List the member pools of ``env_name``."""
        from agentbox_sdk._generated.api.pools import list_env_sandbox_pools

        resp = await list_env_sandbox_pools.asyncio_detailed(env_name, client=self._client)
        raise_for_status(resp, context=f"list pools in env {env_name!r}")
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

    async def scale(
        self,
        env_name: str,
        name: str,
        replicas: Optional[int] = None,
        *,
        max_replicas: Optional[int] = None,
    ) -> SandboxPool:
        """Adjust a pool's replica counts.

        When the pool's scalingGroup has autoscaling enabled, only
        ``max_replicas`` is accepted — passing ``replicas`` returns 400 from
        the server.
        """
        from agentbox_sdk._generated.api.pools import update_env_sandbox_pool
        from agentbox_sdk._generated.models.update_env_sandbox_pool_request import (
            UpdateEnvSandboxPoolRequest,
        )
        from agentbox_sdk._generated.types import UNSET

        body = UpdateEnvSandboxPoolRequest(
            replicas=replicas if replicas is not None else UNSET,
            max_replicas=max_replicas if max_replicas is not None else UNSET,
        )
        resp = await update_env_sandbox_pool.asyncio_detailed(
            env_name, name, client=self._client, body=body
        )
        raise_for_status(resp, context=f"scale pool {name!r} in env {env_name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict(), env_name)

    async def delete(self, env_name: str, name: str) -> None:
        """Remove a pool from its env (Reconciler cascades the SandboxPool CR)."""
        from agentbox_sdk._generated.api.pools import delete_env_sandbox_pool

        resp = await delete_env_sandbox_pool.asyncio_detailed(
            env_name, name, client=self._client
        )
        raise_for_status(resp, context=f"delete pool {name!r} in env {env_name!r}")
