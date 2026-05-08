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
patch_e2b — AgentBox patch for e2b==2.19.0

This module monkey-patches the E2B SDK to redirect API calls and sandbox
connections to an AgentBox deployment.

Architecture Overview:
  - Control Plane (E2B-compatible REST API): AgentBox exposes standard E2B routes 
    on port :8090. Paths are direct (e.g., /sandboxes, /templates) with no extra prefix.
    E2B_API_URL should be set to http(s)://<e2b-api-host> (no path suffix).
  - Data Plane (envd gRPC / connect-proto): Traffic goes through the Envoy ExtProc gateway,
    using the standard URL path format: <gateway>/sandboxes/<sandboxID>/<port>/...
    E2B_DOMAIN should be set to the gateway address, and the SDK's `get_host` will 
    automatically append the path.

Default In-Cluster Configuration (calling patch_e2b() directly without arguments):
  - Data Plane Gateway: agentbox-data-plane.agentbox-system.svc.cluster.local
  - Control Plane API: http://agentbox-e2b-api.agentbox-system.svc.cluster.local
  - HTTPS: False

Usage (In-cluster, zero arguments):
    from agentbox.patch_e2b import patch_e2b
    patch_e2b()

    from e2b import Sandbox
    sandbox = Sandbox.create(template_id="my-gpu-pool")

Usage (Custom addresses):
    from agentbox.patch_e2b import patch_e2b
    patch_e2b(
        https=False,
        domain="agentbox-data-plane.agentbox-system.svc.cluster.local",
        api_url="http://agentbox-e2b-api.agentbox-system.svc.cluster.local",
    )

    from e2b import Sandbox
    sandbox = Sandbox.create(template_id="my-gpu-pool")
"""

import os
from typing import Optional
from urllib.parse import urlparse

#: e2b SDK version this patch was written and tested against.
#: If your installed e2b version differs, the patch may still work but is not guaranteed.
COMPATIBLE_E2B_VERSION = "2.19.0"

# Default in-cluster service addresses
_DEFAULT_DOMAIN = "agentbox-data-plane.agentbox-system.svc.cluster.local"
_DEFAULT_API_URL = "http://agentbox-e2b-api.agentbox-system.svc.cluster.local"


def _strip_scheme(url: str) -> str:
    """Remove http:// or https:// scheme prefix from a URL, keeping only host[:port]."""
    parsed = urlparse(url)
    if parsed.scheme in ("http", "https"):
        return parsed.netloc or parsed.path
    return url.rstrip("/")


def _get_api_url(domain: str, https: bool) -> str:
    """Build the E2B-compatible API base URL for AgentBox.

    AgentBox E2B-compatible API routes are registered directly under the root path
    (/sandboxes, /templates, etc.) with no extra path prefix. Therefore, we only
    need to return scheme://host.
    """
    scheme = "https" if https else "http"
    host = _strip_scheme(domain)
    return f"{scheme}://{host}"


def _make_sandbox_get_host(resolved_domain: Optional[str]):
    """Return a get_host method for SandboxBase that uses the standard URL path format.

    Always uses the domain that was passed to patch_e2b(), completely ignoring
    sandbox_domain from the API response (which may contain a scheme-prefixed
    internal cluster URL like http://e2b-api.svc.cluster.local).
    """

    def _get_host(self, port: int) -> str:
        dom = resolved_domain or os.environ.get("E2B_DOMAIN", "localhost")
        dom = _strip_scheme(dom)
        sid = getattr(self, "sandbox_id", None) or ""
        return f"{dom}/sandboxes/{sid}/{port}"

    return _get_host


def patch_e2b(
    https: bool = False,
    domain: Optional[str] = None,
    api_url: Optional[str] = None,
) -> None:
    """
    Patch the E2B SDK to route API calls and sandbox connections to AgentBox.

    This function must be called BEFORE importing/using any E2B Sandbox classes.

    Args:
        https:   If True, use HTTPS for the API URL. Defaults to False (in-cluster HTTP).
        domain:  Data plane Envoy gateway address.
                 Priority: Argument > E2B_DOMAIN environment variable > Default in-cluster value
                 (agentbox-data-plane.agentbox-system.svc.cluster.local)
        api_url: Control plane E2B-compatible API full URL (including scheme).
                 Priority: Argument > E2B_API_URL environment variable > Default in-cluster value
                 (http://agentbox-e2b-api.agentbox-system.svc.cluster.local)
                 This argument can be omitted if the E2B_API_URL environment variable is already set.

    Raises:
        ImportError: If the e2b package is not installed.

    Example:
        # Zero-argument call within the cluster (using default ClusterIP addresses):
        from agentbox.patch_e2b import patch_e2b
        patch_e2b()

        # Custom addresses (e.g., local debugging via port-forward):
        patch_e2b(https=False, domain="localhost:9081", api_url="http://localhost:9082")

        # Controlled via environment variables (CI/CD scenarios):
        # export E2B_DOMAIN=agentbox-data-plane.agentbox-system.svc.cluster.local
        # export E2B_API_URL=http://agentbox-e2b-api.agentbox-system.svc.cluster.local
        # export E2B_API_KEY=agbx_your_key
        patch_e2b()
    """
    try:
        import e2b  # noqa: F401
    except ImportError as exc:
        raise ImportError(
            "The 'e2b' package is required. Install it with: pip install e2b"
        ) from exc

    # Resolve domain (data plane gateway): Argument > Environment variable > Default in-cluster value
    resolved_domain = (
        domain or os.environ.get("E2B_DOMAIN", "") or _DEFAULT_DOMAIN
    )

    # Resolve api_url (control plane API): Argument > Environment variable > Default in-cluster value
    # If E2B_API_URL is already set externally and no argument is passed,
    # keep the environment variable; otherwise, use the default value.
    resolved_api_url = (
        api_url or os.environ.get("E2B_API_URL", "") or _DEFAULT_API_URL
    )
    os.environ["E2B_API_URL"] = resolved_api_url

    # Patch the SandboxBase.get_host method to use standard URL path format
    try:
        from e2b.sandbox.main import SandboxBase  # type: ignore[import]

        SandboxBase.get_host = _make_sandbox_get_host(resolved_domain)
    except ImportError:
        # Older versions of e2b may have a different module structure
        pass

    # Patch ConnectionConfig.get_host and get_sandbox_url if available
    try:
        from e2b.connection_config import ConnectionConfig  # type: ignore[import]

        def _connection_config_get_host(
            _, sandbox_id: str, sandbox_domain: str, port: int
        ) -> str:
            # Always use the configured gateway domain — ignore sandbox_domain from the
            # API response since it may be an internal cluster URL with a scheme prefix.
            dom = _strip_scheme(
                resolved_domain or sandbox_domain or "localhost"
            )
            return f"{dom}/sandboxes/{sandbox_id}/{port}"

        ConnectionConfig.get_host = _connection_config_get_host

        # Patch get_sandbox_url to respect the https flag passed to patch_e2b().
        # The default SDK implementation always uses https:// when debug=False,
        # which breaks local development against HTTP-only gateways.
        def _connection_config_get_sandbox_url(
            self, sandbox_id: str, sandbox_domain: str
        ) -> str:
            if self._sandbox_url:
                return self._sandbox_url
            scheme = "https" if https else "http"
            return f"{scheme}://{self.get_host(sandbox_id, sandbox_domain, self.envd_port)}"

        ConnectionConfig.get_sandbox_url = _connection_config_get_sandbox_url
    except (ImportError, AttributeError):
        pass