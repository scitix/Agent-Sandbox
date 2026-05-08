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
AgentBoxClient — main entry point for agentbox_sdk.

Usage::

    import agentbox_sdk as abx

    async with abx.AgentBoxClient() as client:
        me = await client.auth.whoami()
        sb = await client.sandboxes.create(pool="my-pool")
        pool = await client.pools.get("my-pool")
        tmpl = await client.templates.get("gpu-v100")

Environment variables (same as abx CLI):
    AGENTBOX_ENDPOINT   — API server base URL
    AGENTBOX_API_KEY    — API key (agbx_xxx...)
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Optional

from agentbox_sdk._http import build_gen_client
from agentbox_sdk.auth import AuthAPI
from agentbox_sdk.pool import PoolsAPI
from agentbox_sdk.sandbox import SandboxesAPI
from agentbox_sdk.template import TemplatesAPI

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient


class AgentBoxClient:
    """
    Async client for the AgentBox API.

    Can be used as an async context manager (recommended)::

        async with AgentBoxClient(endpoint=..., api_key=...) as client:
            sb = await client.sandboxes.create(pool="demo")

    Or created/closed manually::

        client = AgentBoxClient()
        await client.__aenter__()
        try:
            ...
        finally:
            await client.__aexit__(None, None, None)
    """

    def __init__(
        self,
        endpoint: Optional[str] = None,
        api_key: Optional[str] = None,
        *,
        timeout: float = 30.0,
        verify_ssl: bool = True,
    ) -> None:
        """
        Create the client.

        :param endpoint: Base URL of the AgentBox API server
            (falls back to ``AGENTBOX_ENDPOINT`` env var).
        :param api_key: API key starting with ``agbx_``
            (falls back to ``AGENTBOX_API_KEY`` env var).
        :param timeout: HTTP request timeout in seconds.
        :param verify_ssl: Whether to verify TLS certificates.
        """
        self._gen_client: Optional[AuthenticatedClient] = None
        self._endpoint = endpoint
        self._api_key = api_key
        self._timeout = timeout
        self._verify_ssl = verify_ssl

        # Namespace sub-APIs (populated in __aenter__ / _ensure_open)
        self._sandboxes: Optional[SandboxesAPI] = None
        self._pools: Optional[PoolsAPI] = None
        self._templates: Optional[TemplatesAPI] = None
        self._auth: Optional[AuthAPI] = None

    # ------------------------------------------------------------------
    # Lazy initializer
    # ------------------------------------------------------------------

    def _ensure_open(self) -> "AuthenticatedClient":
        if self._gen_client is None:
            self._gen_client = build_gen_client(
                self._endpoint,
                self._api_key,
                timeout=self._timeout,
                verify_ssl=self._verify_ssl,
            )
            self._sandboxes = SandboxesAPI(self._gen_client)
            self._pools = PoolsAPI(self._gen_client)
            self._templates = TemplatesAPI(self._gen_client)
            self._auth = AuthAPI(self._gen_client)
        return self._gen_client

    # ------------------------------------------------------------------
    # Sub-API namespaces
    # ------------------------------------------------------------------

    @property
    def sandboxes(self) -> SandboxesAPI:
        """Sandbox operations."""
        self._ensure_open()
        assert self._sandboxes is not None
        return self._sandboxes

    @property
    def pools(self) -> PoolsAPI:
        """SandboxPool operations."""
        self._ensure_open()
        assert self._pools is not None
        return self._pools

    @property
    def templates(self) -> TemplatesAPI:
        """SandboxTemplate operations."""
        self._ensure_open()
        assert self._templates is not None
        return self._templates

    @property
    def auth(self) -> AuthAPI:
        """Authentication / identity operations."""
        self._ensure_open()
        assert self._auth is not None
        return self._auth

    # ------------------------------------------------------------------
    # Async context manager
    # ------------------------------------------------------------------

    async def __aenter__(self) -> "AgentBoxClient":
        self._ensure_open()
        return self

    async def __aexit__(self, *_) -> None:
        await self.close()

    async def close(self) -> None:
        """Close the underlying HTTP client.

        This method is safe to call even when the event loop that created the
        client has already been closed (e.g. during ``__del__`` or ``atexit``
        cleanup in multi-threaded scenarios).
        """
        if self._gen_client is not None:
            try:
                await self._gen_client.get_async_httpx_client().aclose()
            except Exception:
                # Swallow errors during close — the most common case is
                # RuntimeError("Event loop is closed") when the loop that
                # created the httpx connection pool has already shut down.
                pass
            self._gen_client = None

    def __repr__(self) -> str:
        endpoint = self._endpoint or "<from env>"
        return f"<AgentBoxClient endpoint={endpoint!r}>"
