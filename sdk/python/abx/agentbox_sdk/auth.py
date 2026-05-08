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
Auth API namespace for agentbox_sdk.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from agentbox_sdk._http import raise_for_status
from agentbox_sdk.models import WhoAmIData

if TYPE_CHECKING:
    from agentbox_sdk._generated.client import AuthenticatedClient


class AuthAPI:
    """Provides authentication-related operations (whoami)."""

    def __init__(self, client: "AuthenticatedClient") -> None:
        self._client = client

    async def whoami(self) -> WhoAmIData:
        """Return identity information for the current API key."""
        from agentbox_sdk._generated.api.auth import get_who_am_i

        resp = await get_who_am_i.asyncio_detailed(client=self._client)
        raise_for_status(resp, context="whoami")
        assert resp.parsed is not None
        return WhoAmIData.model_validate(resp.parsed.to_dict())
