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
Sandbox resource and SandboxesAPI namespace for agentbox_sdk.
"""

from __future__ import annotations

import asyncio
from typing import TYPE_CHECKING, Any, Dict, Optional

from agentbox_sdk._http import raise_for_status
from agentbox_sdk.exceptions import (
    EndpointNotFoundError,
    SandboxStartupError,
    SandboxTimeoutError,
)
from agentbox_sdk.models import (
    PagedResult,
    SandboxData,
    EndpointData,
    SandboxLogsData,
    SandboxStatus,
)

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient

_TERMINAL = {
    SandboxStatus.FAILED,
    SandboxStatus.CANCELED,
    SandboxStatus.COMPLETED,
}


class Sandbox:
    """A live Sandbox instance returned by SandboxesAPI methods."""

    def __init__(self, data: SandboxData, api: "SandboxesAPI") -> None:
        self._data = data
        self._api = api

    @property
    def id(self) -> str:
        return self._data.sandbox_id

    @property
    def status(self) -> SandboxStatus:
        return self._data.status

    @property
    def pool_name(self) -> str:
        return self._data.pool_name

    @property
    def endpoints(self) -> Dict[str, EndpointData]:
        return self._data.endpoints

    @property
    def metadata(self) -> Dict[str, str]:
        return self._data.metadata

    @property
    def container_images(self) -> Dict[str, str]:
        return self._data.container_images

    @property
    def data(self) -> SandboxData:
        """Return the underlying SandboxData snapshot."""
        return self._data

    @property
    def is_terminal(self) -> bool:
        """True if the sandbox is in a terminal state (Failed, Completed, Canceled)."""
        return self.status in _TERMINAL

    def get_endpoint(self, port: int) -> str:
        """Return the full URL/address for *port* from the endpoints map.

        :raises EndpointNotFoundError: if port is not present.
        """
        key = str(port)
        if key not in self._data.endpoints:
            raise EndpointNotFoundError(self.id, port)
        return self._data.endpoints[key].url

    def get_host(self, port: int) -> str:
        """Return the ``host:port`` portion of the endpoint URL (strips scheme)."""
        url = self.get_endpoint(port)
        if "://" in url:
            url = url.split("://", 1)[1]
        return url

    async def refresh(self) -> "Sandbox":
        """Fetch the latest state from the API and update this object."""
        updated = await self._api.get(self.id)
        self._data = updated._data
        return self

    async def wait_for_ready(
        self,
        timeout: float = 120.0,
        poll_interval: float = 2.0,
    ) -> "Sandbox":
        """Poll until the sandbox reaches RUNNING state.

        :raises SandboxTimeoutError: if not ready within *timeout* seconds.
        :raises SandboxStartupError: if sandbox enters a terminal failure state.
        """
        elapsed = 0.0
        while elapsed < timeout:
            await self.refresh()
            if self.status == SandboxStatus.RUNNING:
                return self
            if self.status in _TERMINAL:
                msg = ""
                if self._data.status_detail:
                    msg = self._data.status_detail.message or ""
                raise SandboxStartupError(self.id, self.status.value, msg)
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval
        raise SandboxTimeoutError(self.id, timeout)

    async def set_timeout(self, timeout: str) -> None:
        """Set the sandbox idle timeout (e.g. ``'30m'``, ``'2h'``, ``'0'`` to disable)."""
        await self._api.set_timeout(self.id, timeout)

    async def is_ready(self) -> bool:
        """Check if all configured runtime endpoints of this sandbox are ready.

        Calls ``GET /v1/sandboxes/{id}/is_ready`` and returns True only when all
        configured ReadinessProbes pass.

        - Returns ``False`` when the API responds with HTTP 502 (gateway not yet routing
          to the sandbox) or 503 (one or more probes failed).
        - Raises on other error status codes (404 = sandbox not found, 500 = server error).
        """
        from agentbox_sdk._generated.api.sandboxes import is_sandbox_ready

        resp = await is_sandbox_ready.asyncio_detailed(
            self.id, client=self._api._client
        )
        # 502: gateway not ready yet; 503: probes failed → treat both as "not ready"
        if resp.status_code in (502, 503):
            return False
        raise_for_status(resp, context=f"is_ready for sandbox {self.id!r}")
        assert resp.parsed is not None
        return resp.parsed.ready

    async def get_logs(
        self,
        container: Optional[str] = None,
        tail_lines: Optional[int] = None,
        since_seconds: Optional[int] = None,
    ) -> SandboxLogsData:
        """Fetch logs from the sandbox."""
        return await self._api.get_logs(
            self.id,
            container=container,
            tail_lines=tail_lines,
            since_seconds=since_seconds,
        )

    async def close(self) -> None:
        """Terminate and release this sandbox."""
        await self._api.delete(self.id)

    async def wait_for_deleted(
        self,
        timeout: float = 60.0,
        poll_interval: float = 2.0,
    ) -> None:
        """Poll until the sandbox reaches a terminal state or disappears (404).

        Call after :meth:`close` to confirm the pool slot has been freed.
        Useful in batch mode where workers compete for limited pool slots.

        :raises SandboxTimeoutError: if not terminal within *timeout* seconds.
        """
        elapsed = 0.0
        while elapsed < timeout:
            try:
                await self.refresh()
            except Exception:
                # 404 or connection error → sandbox is gone
                return
            if self.is_terminal:
                return
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval
        raise SandboxTimeoutError(self.id, timeout)

    async def __aenter__(self) -> "Sandbox":
        return self

    async def __aexit__(self, *_: Any) -> None:
        await self.close()

    def __repr__(self) -> str:
        return f"<Sandbox id={self.id!r} status={self.status.value!r} pool={self.pool_name!r}>"


class _SandboxCreateContext:
    """Returned by SandboxesAPI.create — supports both ``await`` and ``async with``."""

    def __init__(self, coro: Any) -> None:
        self._coro = coro
        self._sb: Optional[Sandbox] = None

    def __await__(self):
        return self._coro.__await__()

    async def __aenter__(self) -> Sandbox:
        self._sb = await self._coro
        return self._sb

    async def __aexit__(self, *_: Any) -> None:
        if self._sb is not None:
            await self._sb.close()


class SandboxesAPI:
    """Operations on Sandbox resources (accessed via ``client.sandboxes``)."""

    def __init__(self, client: "AuthenticatedClient") -> None:
        self._client = client

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _make(self, raw: dict) -> Sandbox:
        return Sandbox(SandboxData.model_validate(raw), self)

    async def _create_impl(
        self,
        pool: str,
        *,
        image: Optional[str] = None,
        container_images: Optional[Dict[str, str]] = None,
        labels: Optional[Dict[str, str]] = None,
        annotations: Optional[Dict[str, str]] = None,
        metadata: Optional[Dict[str, str]] = None,
        startup_timeout: Optional[str] = None,
        idle_timeout: Optional[str] = None,
        wait: bool = True,
        wait_timeout: float = 120.0,
    ) -> Sandbox:
        from agentbox_sdk._generated.api.sandboxes import create_sandbox
        from agentbox_sdk._generated.models.create_sandbox_request import (
            CreateSandboxRequest,
        )
        from agentbox_sdk._generated.models.create_sandbox_request_annotations import (
            CreateSandboxRequestAnnotations,
        )
        from agentbox_sdk._generated.models.create_sandbox_request_container_images import (
            CreateSandboxRequestContainerImages,
        )
        from agentbox_sdk._generated.models.create_sandbox_request_labels import (
            CreateSandboxRequestLabels,
        )
        from agentbox_sdk._generated.models.create_sandbox_request_metadata import (
            CreateSandboxRequestMetadata,
        )
        from agentbox_sdk._generated.types import UNSET

        body = CreateSandboxRequest(
            pool_name=pool,
            image=image if image is not None else UNSET,
            container_images=(
                CreateSandboxRequestContainerImages.from_dict(container_images)
                if container_images is not None
                else UNSET
            ),
            labels=(
                CreateSandboxRequestLabels.from_dict(labels)
                if labels is not None
                else UNSET
            ),
            annotations=(
                CreateSandboxRequestAnnotations.from_dict(annotations)
                if annotations is not None
                else UNSET
            ),
            metadata=(
                CreateSandboxRequestMetadata.from_dict(metadata)
                if metadata is not None
                else UNSET
            ),
            startup_timeout=startup_timeout
            if startup_timeout is not None
            else UNSET,
            idle_timeout=idle_timeout if idle_timeout is not None else UNSET,
        )

        resp = await create_sandbox.asyncio_detailed(
            client=self._client, body=body
        )
        raise_for_status(resp, context="create sandbox")
        assert resp.parsed is not None
        raw = resp.parsed.sandbox.to_dict()
        sb = self._make(raw)
        if wait:
            await sb.wait_for_ready(timeout=wait_timeout)
        return sb

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def create(
        self,
        pool: str,
        *,
        image: Optional[str] = None,
        container_images: Optional[Dict[str, str]] = None,
        labels: Optional[Dict[str, str]] = None,
        annotations: Optional[Dict[str, str]] = None,
        metadata: Optional[Dict[str, str]] = None,
        startup_timeout: Optional[str] = None,
        idle_timeout: Optional[str] = None,
        wait: bool = True,
        wait_timeout: float = 120.0,
    ) -> _SandboxCreateContext:
        """Create a new Sandbox from *pool*.

        Supports two usage patterns::

            sb = await client.sandboxes.create(pool="my-pool")
            async with client.sandboxes.create(pool="my-pool") as sb:
                ...
        """
        return _SandboxCreateContext(
            self._create_impl(
                pool,
                image=image,
                container_images=container_images,
                labels=labels,
                annotations=annotations,
                metadata=metadata,
                startup_timeout=startup_timeout,
                idle_timeout=idle_timeout,
                wait=wait,
                wait_timeout=wait_timeout,
            )
        )

    async def get(self, sandbox_id: str) -> Sandbox:
        """Fetch a sandbox by ID."""
        from agentbox_sdk._generated.api.sandboxes import get_sandbox

        resp = await get_sandbox.asyncio_detailed(
            sandbox_id, client=self._client
        )
        raise_for_status(resp, context=f"get sandbox {sandbox_id!r}")
        assert resp.parsed is not None
        raw = resp.parsed.sandbox.to_dict()
        return self._make(raw)

    async def list(
        self,
        pool: Optional[str] = None,
        status: Optional[str] = None,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> "PagedResult[SandboxData]":
        """List sandboxes with optional filters."""
        from agentbox_sdk._generated.api.sandboxes import list_sandboxes
        from agentbox_sdk._generated.types import UNSET

        resp = await list_sandboxes.asyncio_detailed(
            client=self._client,
            pool_name=pool if pool is not None else UNSET,
            status=status if status is not None else UNSET,
            limit=limit if limit is not None else UNSET,
            offset=offset if offset is not None else UNSET,
        )
        raise_for_status(resp, context="list sandboxes")
        assert resp.parsed is not None
        items = [
            SandboxData.model_validate(s.to_dict()) for s in resp.parsed.items
        ]
        return PagedResult[SandboxData](
            items=items,
            total=resp.parsed.total,
            limit=resp.parsed.limit,
            offset=resp.parsed.offset,
        )

    async def delete(self, sandbox_id: str) -> None:
        """Terminate and release a sandbox."""
        from agentbox_sdk._generated.api.sandboxes import delete_sandbox

        resp = await delete_sandbox.asyncio_detailed(
            sandbox_id, client=self._client
        )
        raise_for_status(resp, context=f"delete sandbox {sandbox_id!r}")

    async def set_timeout(self, sandbox_id: str, timeout: str) -> None:
        """Update the idle timeout. Use ``'0'`` to disable."""
        from agentbox_sdk._generated.api.sandboxes import set_sandbox_timeout
        from agentbox_sdk._generated.models.set_sandbox_timeout_request import (
            SetSandboxTimeoutRequest,
        )

        body = SetSandboxTimeoutRequest(timeout=timeout)
        resp = await set_sandbox_timeout.asyncio_detailed(
            sandbox_id, client=self._client, body=body
        )
        raise_for_status(
            resp, context=f"set timeout for sandbox {sandbox_id!r}"
        )

    async def get_logs(
        self,
        sandbox_id: str,
        *,
        container: Optional[str] = None,
        tail_lines: Optional[int] = None,
        since_seconds: Optional[int] = None,
    ) -> SandboxLogsData:
        """Retrieve logs from a sandbox."""
        from agentbox_sdk._generated.api.sandboxes import get_sandbox_logs
        from agentbox_sdk._generated.types import UNSET

        resp = await get_sandbox_logs.asyncio_detailed(
            sandbox_id,
            client=self._client,
            container=container if container is not None else UNSET,
            lines=tail_lines if tail_lines is not None else UNSET,
        )
        raise_for_status(resp, context=f"get logs for sandbox {sandbox_id!r}")
        assert resp.parsed is not None
        return SandboxLogsData.model_validate(resp.parsed.to_dict())
