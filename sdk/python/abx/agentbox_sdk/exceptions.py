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
Exception hierarchy for agentbox_sdk.

AgentBoxError
  └── AgentBoxAPIError
        ├── NotFoundError (404)
        │     ├── SandboxNotFoundError
        │     ├── PoolNotFoundError
        │     └── TemplateNotFoundError
        ├── AuthenticationError (401)
        ├── PermissionError (403)
        ├── ConflictError (409)
        ├── ValidationError (422)
        ├── RateLimitError (429)
        └── ServerError (5xx)
  ├── SandboxTimeoutError   (polling timeout)
  ├── SandboxStartupError   (sandbox entered Failed/Canceled state)
  └── EndpointNotFoundError (port not in endpoints map)
"""

from __future__ import annotations


class AgentBoxError(Exception):
    """Base exception for all agentbox_sdk errors."""


class AgentBoxAPIError(AgentBoxError):
    """Raised when the AgentBox API returns an error response."""

    def __init__(
        self,
        message: str,
        *,
        status_code: int,
        context: str = "",
        raw_body: str = "",
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.context = context
        self.raw_body = raw_body

    def __str__(self) -> str:
        ctx = f" [{self.context}]" if self.context else ""
        return f"HTTP {self.status_code}{ctx}: {self.args[0]}"


class NotFoundError(AgentBoxAPIError):
    """HTTP 404 — resource not found."""


class SandboxNotFoundError(NotFoundError):
    """Sandbox with the given ID was not found."""


class PoolNotFoundError(NotFoundError):
    """SandboxPool with the given name was not found."""


class TemplateNotFoundError(NotFoundError):
    """SandboxTemplate with the given name was not found."""


class AuthenticationError(AgentBoxAPIError):
    """HTTP 401 — invalid or missing API key."""


class PermissionError(AgentBoxAPIError):  # noqa: A001
    """HTTP 403 — insufficient permissions."""


class ConflictError(AgentBoxAPIError):
    """HTTP 409 — resource conflict."""


class ValidationError(AgentBoxAPIError):
    """HTTP 422 — request validation failed."""


class RateLimitError(AgentBoxAPIError):
    """HTTP 429 — rate limit exceeded."""


class ServerError(AgentBoxAPIError):
    """HTTP 5xx — server-side error."""


class SandboxTimeoutError(AgentBoxError):
    """Raised when waiting for a sandbox to become ready times out."""

    def __init__(self, sandbox_id: str, timeout: float) -> None:
        super().__init__(
            f"Sandbox {sandbox_id!r} did not become ready within {timeout:.0f}s"
        )
        self.sandbox_id = sandbox_id
        self.timeout = timeout


class SandboxStartupError(AgentBoxError):
    """Raised when a sandbox enters a terminal failure state during startup."""

    def __init__(self, sandbox_id: str, status: str, message: str = "") -> None:
        detail = f": {message}" if message else ""
        super().__init__(
            f"Sandbox {sandbox_id!r} entered terminal state {status!r}{detail}"
        )
        self.sandbox_id = sandbox_id
        self.status = status
        self.message = message


class EndpointNotFoundError(AgentBoxError):
    """Raised when the requested port is not in the sandbox's endpoints map."""

    def __init__(self, sandbox_id: str, port: int) -> None:
        super().__init__(
            f"Sandbox {sandbox_id!r} has no endpoint for port {port}"
        )
        self.sandbox_id = sandbox_id
        self.port = port
