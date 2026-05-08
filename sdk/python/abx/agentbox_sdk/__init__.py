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
agentbox_sdk — Python SDK for AgentBox Sandbox lifecycle management.

Usage:
    import agentbox_sdk as abx

    async with abx.AgentBoxClient() as client:
        sb = await client.sandboxes.create(pool="my-pool")
        print(sb.get_endpoint(8080))
        await sb.close()

Environment variables (same as abx CLI):
    AGENTBOX_ENDPOINT   — API server URL, e.g. http://agentbox.example.com
    AGENTBOX_API_KEY    — API key (agbx_xxx...)
"""

from agentbox_sdk.client import AgentBoxClient
from agentbox_sdk.sandbox import Sandbox
from agentbox_sdk.exceptions import (
    AgentBoxAPIError,
    AgentBoxError,
    AuthenticationError,
    ConflictError,
    EndpointNotFoundError,
    NotFoundError,
    PermissionError,
    PoolNotFoundError,
    RateLimitError,
    SandboxNotFoundError,
    SandboxStartupError,
    SandboxTimeoutError,
    ServerError,
    TemplateNotFoundError,
    ValidationError,
)
from agentbox_sdk.models import (
    PagedResult,
    SandboxData,
    SandboxLogsData,
    SandboxLogEntry,
    SandboxPoolData,
    SandboxPoolSpec,
    SandboxPoolStatus,
    SandboxStatus,
    SandboxStatusDetail,
    SandboxTemplateData,
    WhoAmIData,
)

__version__ = "0.0.0"

__all__ = [
    # Client
    "AgentBoxClient",
    # Resources
    "Sandbox",
    # Exceptions
    "AgentBoxError",
    "AgentBoxAPIError",
    "NotFoundError",
    "SandboxNotFoundError",
    "PoolNotFoundError",
    "TemplateNotFoundError",
    "AuthenticationError",
    "PermissionError",
    "ConflictError",
    "ValidationError",
    "RateLimitError",
    "ServerError",
    "SandboxTimeoutError",
    "SandboxStartupError",
    "EndpointNotFoundError",
    # Models
    "SandboxData",
    "SandboxStatus",
    "SandboxStatusDetail",
    "SandboxLogsData",
    "SandboxLogEntry",
    "SandboxPoolData",
    "SandboxPoolSpec",
    "SandboxPoolStatus",
    "SandboxTemplateData",
    "WhoAmIData",
    "PagedResult",
]
