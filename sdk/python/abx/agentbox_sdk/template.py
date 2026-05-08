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
SandboxTemplate resource and TemplatesAPI namespace for agentbox_sdk.
"""

from __future__ import annotations

from typing import TYPE_CHECKING, Any, Dict, Optional

from agentbox_sdk._http import raise_for_status
from agentbox_sdk.models import PagedResult, SandboxTemplateData

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient


class SandboxTemplate:
    """A SandboxTemplate resource object."""

    def __init__(self, data: SandboxTemplateData, api: "TemplatesAPI") -> None:
        self._data = data
        self._api = api

    @property
    def name(self) -> str:
        return self._data.name

    @property
    def version(self) -> Optional[str]:
        return self._data.version

    @property
    def description(self) -> Optional[str]:
        return self._data.description

    @property
    def data(self) -> SandboxTemplateData:
        return self._data

    async def refresh(self) -> "SandboxTemplate":
        updated = await self._api.get(self.name)
        self._data = updated._data
        return self

    async def delete(self) -> None:
        await self._api.delete(self.name)

    def __repr__(self) -> str:
        return f"<SandboxTemplate name={self.name!r} version={self.version!r}>"


class TemplatesAPI:
    """Operations on SandboxTemplate resources (accessed via ``client.templates``)."""

    def __init__(self, client: "AuthenticatedClient") -> None:
        self._client = client

    def _make(self, raw: dict) -> SandboxTemplate:
        if "template" in raw and isinstance(raw["template"], dict):
            raw = raw["template"]
        return SandboxTemplate(SandboxTemplateData.model_validate(raw), self)

    async def get(self, name: str) -> SandboxTemplate:
        """Fetch a template by name."""
        from agentbox_sdk._generated.api.sandbox_templates import (
            get_sandbox_template,
        )

        resp = await get_sandbox_template.asyncio_detailed(
            name, client=self._client
        )
        raise_for_status(resp, context=f"get template {name!r}")
        assert resp.parsed is not None
        return self._make(resp.parsed.to_dict())

    async def list(
        self,
        limit: Optional[int] = None,
        offset: Optional[int] = None,
    ) -> "PagedResult[SandboxTemplateData]":
        """List all templates."""
        from agentbox_sdk._generated.api.sandbox_templates import (
            list_sandbox_templates,
        )

        resp = await list_sandbox_templates.asyncio_detailed(
            client=self._client
        )
        raise_for_status(resp, context="list templates")
        assert resp.parsed is not None
        items = [
            SandboxTemplateData.model_validate(t.to_dict())
            for t in resp.parsed.items
        ]
        return PagedResult[SandboxTemplateData](
            items=items,
            total=resp.parsed.total,
            limit=resp.parsed.limit,
            offset=resp.parsed.offset,
        )

    async def delete(self, name: str) -> None:
        """Delete a template (admin operation)."""
        from agentbox_sdk._generated.api.admin import (
            admin_delete_sandbox_template,
        )

        resp = await admin_delete_sandbox_template.asyncio_detailed(
            name, client=self._client
        )
        raise_for_status(resp, context=f"delete template {name!r}")
