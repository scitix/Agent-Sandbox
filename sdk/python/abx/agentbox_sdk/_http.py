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
Internal HTTP utilities for agentbox_sdk.

build_gen_client()  — construct an AuthenticatedClient for the generated layer
raise_for_status()  — map HTTP error responses to domain exceptions
"""

from __future__ import annotations

import json
import os
from typing import Optional, Union

import httpx

from agentbox_sdk.exceptions import (
    AgentBoxAPIError,
    AuthenticationError,
    ConflictError,
    NotFoundError,
    PermissionError,
    PoolNotFoundError,
    RateLimitError,
    SandboxNotFoundError,
    ServerError,
    TemplateNotFoundError,
    ValidationError,
)


# ---------------------------------------------------------------------------
# Client factory
# ---------------------------------------------------------------------------

_DEFAULT_TIMEOUT = 30.0


def build_gen_client(
    endpoint: Optional[str],
    api_key: Optional[str],
    timeout: float = _DEFAULT_TIMEOUT,
    verify_ssl: bool = True,
) -> "AuthenticatedClient":  # type: ignore[name-defined]
    """
    Build an AuthenticatedClient for the generated layer.

    Falls back to environment variables AGENTBOX_ENDPOINT / AGENTBOX_API_KEY
    (same vars as the abx CLI).
    """
    from agentbox_sdk._generated.client import AuthenticatedClient
    import agentbox_sdk

    resolved_endpoint = endpoint or os.environ.get("AGENTBOX_ENDPOINT", "")
    resolved_api_key = api_key or os.environ.get("AGENTBOX_API_KEY", "")

    if not resolved_endpoint:
        raise ValueError(
            "No endpoint provided. Pass endpoint= or set AGENTBOX_ENDPOINT."
        )
    if not resolved_api_key:
        raise ValueError(
            "No API key provided. Pass api_key= or set AGENTBOX_API_KEY."
        )

    base_url = resolved_endpoint.rstrip("/") + "/v1"

    return AuthenticatedClient(
        base_url=base_url,
        token=resolved_api_key,
        # The AgentBox API uses the custom header "AGENTBOX-API-KEY" (not Bearer)
        auth_header_name="AGENTBOX-API-KEY",
        prefix="",
        timeout=httpx.Timeout(timeout),
        verify_ssl=verify_ssl,
        headers={
            "X-AgentBox-Client-Version": agentbox_sdk.__version__,
        },
    )


# ---------------------------------------------------------------------------
# Error mapping
# ---------------------------------------------------------------------------

_NOT_FOUND_CONTEXT_MAP = {
    "sandbox": SandboxNotFoundError,
    "pool": PoolNotFoundError,
    "template": TemplateNotFoundError,
}


def raise_for_status(response: object, *, context: str = "") -> None:
    """
    Raise a domain exception if ``response`` indicates an error.

    Accepts either an ``httpx.Response`` or the generated ``Response`` dataclass
    (which has ``status_code: HTTPStatus``, ``content: bytes``, ``headers``).

    The API returns errors as JSON ``{"error": "<message>"}``; falls back to
    raw text if the body is not valid JSON.
    """
    if isinstance(response, httpx.Response):
        _raise_from_status(
            status_code=response.status_code,
            text=response.text,
            context=context,
        )
    else:
        # Generated Response dataclass: attrs class with status_code (HTTPStatus),
        # content (bytes), headers, parsed
        status_code = int(response.status_code)  # type: ignore[union-attr]
        content: bytes = response.content  # type: ignore[union-attr]
        try:
            text = content.decode("utf-8", errors="replace")
        except Exception:
            text = f"HTTP {status_code}"
        _raise_from_status(status_code=status_code, text=text, context=context)


def _raise_from_status(status_code: int, text: str, context: str) -> None:
    # 2xx / 3xx are successes
    if status_code < 400:
        return

    message = _extract_error_message_from_text(text, status_code)
    kwargs = dict(status_code=status_code, context=context, raw_body=text)

    if status_code == 401:
        raise AuthenticationError(message, **kwargs)
    if status_code == 403:
        raise PermissionError(message, **kwargs)
    if status_code == 404:
        ctx_lower = context.lower()
        for hint, exc_cls in _NOT_FOUND_CONTEXT_MAP.items():
            if hint in ctx_lower:
                raise exc_cls(message, **kwargs)
        raise NotFoundError(message, **kwargs)
    if status_code == 409:
        raise ConflictError(message, **kwargs)
    if status_code == 422:
        raise ValidationError(message, **kwargs)
    if status_code == 429:
        raise RateLimitError(message, **kwargs)
    if status_code >= 500:
        raise ServerError(message, **kwargs)

    raise AgentBoxAPIError(message, **kwargs)


def _extract_error_message_from_text(text: str, status_code: int) -> str:
    """Extract a human-readable error message from response text."""
    try:
        body = json.loads(text)
        return body.get("error") or body.get("message") or str(body)
    except Exception:
        return text or f"HTTP {status_code}"
