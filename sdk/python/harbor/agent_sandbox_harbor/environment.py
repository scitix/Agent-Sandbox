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
Agent Sandbox Harbor environment plugin.

Subclasses Harbor's stock ``E2BEnvironment`` so that:

  * ``patch_e2b()`` runs at module import (before any AsyncSandbox call),
    redirecting traffic to the Agent Sandbox E2B-compatible endpoint.
  * The "template build" step is skipped (``alias_exists -> True``,
    ``_create_template -> no-op``). Agent Sandbox uses a pre-warmed pool with
    in-place image swap, so no template build is needed.
  * The "template" string is rewritten to Agent Sandbox's pool+image shorthand
    ``"<CLUSTER_ID>::<POOL_NAME>//<rewritten_image>"`` so that PostSandboxes on
    the compat layer creates a sandbox from the pre-warmed pool.

Harbor's stock ``E2BEnvironment.__init__`` runs unchanged (we only subclass
and call ``super().__init__()``), so ``environment/Dockerfile`` is parsed for
``WORKDIR`` and ``self._workdir`` is set automatically. Terminal-Bench 2.0
ships a Dockerfile for every task.

Required environment variables (typically via ``harbor run --env-file ...``):
    E2B_API_KEY            Agent Sandbox API key (``agbx_...``).
    AGBX_POOL_NAME         Pre-warmed pool name (e.g. ``terminal2``).

Optional environment variables:
    E2B_DOMAIN             Data-plane gateway host[:port][/path].
    E2B_API_URL            E2B-compatible control-plane URL (scheme://host).
    AGBX_CLUSTER_ID        Cluster id prefix (e.g. ``bar``); omit for
                           single-cluster setups.
    AGBX_IMAGE_PREFIX      Internal mirror prefix
                           (e.g. ``registry.internal/agent-sandbox``).
                           ``docker.io/`` is stripped before the prefix is
                           applied.
    AGBX_HTTPS             ``true``/``false`` for the data-plane scheme
                           (default ``true``).
    AGBX_STARTUP_TIMEOUT   Sandbox startup timeout, seconds (default ``300``).
    AGBX_READY_TIMEOUT     Cold-image readiness ceiling, seconds
                           (default ``600``).
"""

from __future__ import annotations

import asyncio
import os
import time

from agent_sandbox_e2b import patch_e2b

# CRITICAL: ``patch_e2b()`` must execute before ``AsyncSandbox.create`` is
# invoked. Doing it at module import means all later ``from e2b import ...``
# lines see the already-patched class methods. Harbor's lazy import of the e2b
# backend means this module is imported (and ``patch_e2b()`` runs) before
# Harbor's own ``harbor.environments.e2b`` module is loaded.
patch_e2b(
    https=os.environ.get("AGBX_HTTPS", "true").lower() == "true",
    domain=os.environ.get("E2B_DOMAIN") or None,
    api_url=os.environ.get("E2B_API_URL") or None,
)

from e2b import AsyncSandbox  # noqa: E402
from harbor.environments.e2b import E2BEnvironment  # noqa: E402
from harbor.models.environment_type import EnvironmentType  # noqa: E402


def _rewrite_image(raw_image: str) -> str:
    """Strip ``docker.io/`` and apply the internal mirror prefix."""
    if not raw_image:
        return raw_image
    if raw_image.startswith("docker.io/"):
        raw_image = raw_image[len("docker.io/") :]
    prefix = os.environ.get("AGBX_IMAGE_PREFIX", "").rstrip("/")
    if prefix and not raw_image.startswith(prefix + "/"):
        return f"{prefix}/{raw_image}"
    return raw_image


class AgentSandboxEnvironment(E2BEnvironment):
    """E2B-compatible Harbor environment that targets Agent Sandbox pools."""

    @staticmethod
    def type() -> EnvironmentType:
        return EnvironmentType.E2B

    @classmethod
    def preflight(cls) -> None:
        missing = [
            v for v in ("E2B_API_KEY", "AGBX_POOL_NAME") if not os.environ.get(v)
        ]
        if missing:
            raise SystemExit(
                f"AgentSandboxEnvironment requires env vars: {', '.join(missing)}"
            )

    def __init__(self, *args, **kwargs):
        # Let E2BEnvironment.__init__ parse environment/Dockerfile for WORKDIR
        # and run its own validation. Terminal-Bench 2.0 ships a Dockerfile for
        # every task, so the validation passes.
        super().__init__(*args, **kwargs)

        docker_image = self.task_env_config.docker_image
        if not docker_image:
            raise ValueError(
                f"task '{self.environment_name}' has no environment.docker_image; "
                "AgentSandboxEnvironment requires it to be set."
            )

        cluster = os.environ.get("AGBX_CLUSTER_ID", "").strip()
        pool = os.environ["AGBX_POOL_NAME"]
        image = _rewrite_image(docker_image)
        prefix = f"{cluster}::" if cluster else ""
        # PostSandboxes on the compat layer parses "<cluster>::<pool>//<image>".
        # Replace the hash-based template name that the parent computed.
        self._template_name = f"{prefix}{pool}//{image}"

    # --- skip template build entirely ----------------------------------------

    async def _does_template_exist(self) -> bool:  # type: ignore[override]
        return True

    async def _create_template(self) -> None:  # type: ignore[override]
        return

    # --- sandbox create: Agent Sandbox metadata + secure=False + ready poll ---

    async def _create_sandbox(self) -> None:  # type: ignore[override]
        startup_timeout = os.environ.get("AGBX_STARTUP_TIMEOUT", "300")
        metadata = {
            "environment_name": self.environment_name,
            "session_id": self.session_id,
            "agentbox.scitix.ai/startup-timeout": startup_timeout,
        }
        self._sandbox = await AsyncSandbox.create(
            self._template_name,
            timeout=86_400,
            secure=False,
            metadata=metadata,
            allow_internet_access=self.task_env_config.allow_internet,
        )
        # Cold-image pulls can leave ``AsyncSandbox.create`` returning before
        # the envd daemon is reachable; the first ``commands.run()`` then fails
        # with ``Code.unknown``. Block on a trivial exec until it succeeds.
        ready_deadline = float(os.environ.get("AGBX_READY_TIMEOUT", "600"))
        t0 = time.time()
        last_err: Exception | None = None
        while time.time() - t0 < ready_deadline:
            try:
                h = await self._sandbox.commands.run(
                    "true", background=True, timeout=0, user="root"
                )
                await h.wait()
                return
            except Exception as e:  # noqa: BLE001
                last_err = e
                await asyncio.sleep(3)
        raise TimeoutError(
            f"Sandbox {self._sandbox.sandbox_id} not ready after "
            f"{ready_deadline}s (template={self._template_name}); "
            f"last error: {type(last_err).__name__}: {last_err}"
        )
