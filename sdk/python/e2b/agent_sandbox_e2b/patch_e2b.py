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
Patch the E2B Python SDK to target an Agent Sandbox deployment.

Default in-cluster configuration:
  - Data plane gateway:
    agentbox-data-plane.agentbox-system.svc.cluster.local
  - E2B-compatible API:
    http://agentbox-e2b-api.agentbox-system.svc.cluster.local

Usage:
    from agent_sandbox_e2b import patch_e2b

    patch_e2b()

    from e2b import Sandbox
    sandbox = Sandbox.create("my-pool", timeout=3600, secure=False)
"""

import os
import warnings
from urllib.parse import urlparse

_DEFAULT_DOMAIN = "agentbox-data-plane.agentbox-system.svc.cluster.local"
_DEFAULT_API_URL = "http://agentbox-e2b-api.agentbox-system.svc.cluster.local"


def _domain_host_and_path(url: str) -> str:
    """Return host[:port][/path] without a scheme or trailing slash."""
    parsed = urlparse(url)
    if parsed.scheme in ("http", "https"):
        value = f"{parsed.netloc}{parsed.path}"
    else:
        value = url
    return value.rstrip("/")


def _make_sandbox_get_host(resolved_domain: str | None):
    """Return a get_host method for SandboxBase using Agent Sandbox paths."""

    def _get_host(self, port: int) -> str:
        dom = _domain_host_and_path(
            resolved_domain or os.environ.get("E2B_DOMAIN", "localhost")
        )
        sid = getattr(self, "sandbox_id", None) or ""
        return f"{dom}/sandboxes/{sid}/{port}"

    return _get_host


def _warn_if_unchecked_e2b() -> None:
    """Warn when the installed e2b is newer than this release was checked with.

    The dependency is capped at release time, so this only fires when the
    resolver was bypassed (``--no-deps``, a prebuilt image, a source checkout).
    A newer SDK can call API routes the server does not serve yet, which
    surfaces as a bare ``404: 404 page not found`` from ``Sandbox.create``.
    """
    from . import SUPPORTED_E2B_MAX_VERSION

    if not SUPPORTED_E2B_MAX_VERSION:
        return
    try:
        import importlib.metadata

        from packaging.version import Version

        installed = importlib.metadata.version("e2b")
        if Version(installed) > Version(SUPPORTED_E2B_MAX_VERSION):
            warnings.warn(
                f"e2b {installed} is newer than {SUPPORTED_E2B_MAX_VERSION}, "
                "the newest version this agent-sandbox-e2b release was checked "
                "against. If Sandbox.create fails with '404 page not found', "
                f"install e2b<={SUPPORTED_E2B_MAX_VERSION} or upgrade "
                "agent-sandbox-e2b. See the compatibility matrix in the "
                "agent-sandbox-e2b README.",
                stacklevel=3,
            )
    except Exception:  # noqa: BLE001 - a version check must never break patching
        pass


def _truthy(value: str) -> bool:
    """Interpret a string env var as a boolean."""
    return value.strip().lower() in ("1", "true", "yes", "on")


def patch_e2b(
    https: bool | None = None,
    domain: str | None = None,
    api_url: str | None = None,
) -> None:
    """
    Patch the E2B SDK to target Agent Sandbox.

    Call this function before importing or using E2B Sandbox classes.

    Args:
        https: Use HTTPS for data-plane sandbox URLs.
            Priority: argument > E2B_HTTPS > default (False).
            E2B_HTTPS is truthy for "1"/"true"/"yes"/"on" (case-insensitive).
        domain: Data-plane gateway host, optionally with an ingress path.
            Priority: argument > E2B_DOMAIN > default in-cluster service.
        api_url: E2B-compatible API URL, including scheme.
            Priority: argument > E2B_API_URL > default in-cluster service.

    Raises:
        ImportError: If the e2b package is not installed.
    """
    try:
        import e2b  # noqa: F401
    except ImportError as exc:
        raise ImportError(
            "The 'e2b' package is required. Install it with: pip install e2b"
        ) from exc

    _warn_if_unchecked_e2b()

    if https is None:
        https = _truthy(os.environ.get("E2B_HTTPS", ""))

    resolved_domain = (
        domain or os.environ.get("E2B_DOMAIN", "") or _DEFAULT_DOMAIN
    )
    resolved_api_url = (
        api_url or os.environ.get("E2B_API_URL", "") or _DEFAULT_API_URL
    )
    os.environ["E2B_API_URL"] = resolved_api_url

    try:
        from e2b.sandbox.main import SandboxBase  # type: ignore[import]

        SandboxBase.get_host = _make_sandbox_get_host(resolved_domain)
    except ImportError:
        pass

    try:
        from e2b.connection_config import (
            ConnectionConfig,  # type: ignore[import]
        )

        def _connection_config_get_host(
            _, sandbox_id: str, sandbox_domain: str, port: int
        ) -> str:
            dom = _domain_host_and_path(
                resolved_domain or sandbox_domain or "localhost"
            )
            return f"{dom}/sandboxes/{sandbox_id}/{port}"

        ConnectionConfig.get_host = _connection_config_get_host

        def _connection_config_get_sandbox_url(
            self, sandbox_id: str, sandbox_domain: str
        ) -> str:
            if self._sandbox_url:
                return self._sandbox_url
            scheme = "https" if https else "http"
            host = self.get_host(sandbox_id, sandbox_domain, self.envd_port)
            return f"{scheme}://{host}"

        ConnectionConfig.get_sandbox_url = _connection_config_get_sandbox_url
    except (ImportError, AttributeError):
        pass

    # Newer E2B SDKs (>= ~2.24.0) added a client-side API-key format check
    # (e2b.api.validate_api_key / _API_KEY_PATTERN = r"\Ae2b_[0-9a-f]+\Z").
    # Agent Sandbox uses "agbx_" keys, so this check rejects them with an
    # AuthenticationException before any request is sent. The real auth happens
    # at the Agent Sandbox gateway, so neutralize the local format check.
    # validate_api_key is referenced only within e2b.api (both ApiClient and
    # AsyncApiClient route through ApiClient.__init__), and the call resolves
    # the name from the module globals at call time, so replacing the module
    # attribute is sufficient for sync and async clients alike.
    try:
        import re as _re

        import e2b.api as _e2b_api  # type: ignore[import]

        if hasattr(_e2b_api, "validate_api_key"):
            _e2b_api.validate_api_key = lambda api_key: None  # type: ignore[assignment]
        if hasattr(_e2b_api, "_API_KEY_PATTERN"):
            _e2b_api._API_KEY_PATTERN = _re.compile(r"\A.+\Z")  # type: ignore[assignment]
    except (ImportError, AttributeError):
        pass

    # The E2B SDK attaches telemetry headers (lang_version, package_version,
    # sdk_runtime) whose names contain underscores. Gateways in the nginx family
    # reject or silently drop headers with underscores (underscores_in_headers),
    # which breaks requests routed through such an ingress. Rewrite those header
    # names to their hyphenated equivalents.
    #
    # default_headers is imported by-reference into e2b.api and the volume
    # clients and is splatted (**default_headers) at client-init time, so
    # mutating the shared dict object in place reaches every consumer;
    # reassigning the module attribute would leave the other imports pointing at
    # the original dict.
    try:
        from e2b.api import metadata as _e2b_metadata  # type: ignore[import]

        _hdrs = _e2b_metadata.default_headers
        for _key in [k for k in _hdrs if "_" in k]:
            _hdrs[_key.replace("_", "-")] = _hdrs.pop(_key)
    except (ImportError, AttributeError):
        pass
