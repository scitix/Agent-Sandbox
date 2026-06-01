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
    in-place image swap, so no per-task template build is needed.
  * The "template" string is rewritten to Agent Sandbox's pool+image shorthand
    ``"<CLUSTER_ID>::<POOL_NAME>//<image>"`` so that PostSandboxes on the compat
    layer creates a sandbox from the pre-warmed pool.

Design: one capability — bring-your-own pre-built image
=======================================================
This environment assumes the image you point it at is **already fully built**
(everything the agent and verifier need is baked in). It does NOT build images
and does NOT mutate a running sandbox to add tooling. There are two ways to tell
it which image to use, checked in order:

  1. **Image-override map** (``AGBX_IMAGE_MAP``): a text file mapping a task name
     to a fully-qualified image reference, one per line::

         # <task-name>  <image-ref>
         astropy__astropy-7606  registry.internal/agentbox/swebench/sweb.eval.x86_64.astropy_1776_astropy-7606:260328-uv
         django__django-11265   registry.internal/agentbox/swebench/sweb.eval.x86_64.django_1776_django-11265:260328-uv

     ``<task-name>`` is matched against Harbor's ``environment_name`` (the task
     directory name / instance id). ``=`` may be used instead of whitespace.
     The image value is used verbatim. This is how datasets whose ``task.toml``
     has no ``docker_image`` (e.g. SWE-bench, where the upstream task is a
     Dockerfile) are supported: pre-build/mirror the images once, list them
     here, and pass the file in.

  2. **task.toml ``docker_image``** (e.g. Terminal-Bench 2.0): if the task sets
     ``[environment] docker_image`` and there is no map entry, that image is
     used (after optional mirror-prefix / tag rewrite — see ``_rewrite_image``).

If neither applies, the task is rejected (this environment does not build
images). Datasets that need an image built from a Dockerfile must be pre-built
and listed in ``AGBX_IMAGE_MAP``.

Required environment variables (typically via ``harbor run --env-file ...``):
    E2B_API_KEY            Agent Sandbox API key (``agbx_...``).
    AGBX_POOL_NAME         Pre-warmed pool name (e.g. ``terminal2``).

Optional environment variables:
    AGBX_IMAGE_MAP         Path to a ``<task-name> <image>`` map file (see above).
    E2B_DOMAIN             Data-plane gateway host[:port][/path].
    E2B_API_URL            E2B-compatible control-plane URL (scheme://host).
    AGBX_CLUSTER_ID        Cluster id prefix (e.g. ``bar``); omit for
                           single-cluster setups.
    AGBX_IMAGE_PREFIX      Internal mirror prefix applied to the task.toml
                           ``docker_image`` (e.g. ``registry.internal/agentbox``).
                           ``docker.io/`` is stripped first. Not applied to map
                           values (those are used verbatim).
    AGBX_IMAGE_TAG         Override the tag of the task.toml ``docker_image``
                           after rewriting. Not applied to map values.
    AGBX_HTTPS             ``true``/``false`` for the data-plane scheme
                           (default ``true``).
    AGBX_STARTUP_TIMEOUT   Sandbox startup timeout, seconds (default ``300``).
    AGBX_READY_TIMEOUT     Cold-image readiness ceiling, seconds (default
                           ``600``). Large images (e.g. SWE-bench) may need more.
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


def _load_image_map(path: str | None) -> dict[str, str]:
    """Parse an ``AGBX_IMAGE_MAP`` file into ``{task_name: image}``.

    Lines are ``<task-name> <image>`` or ``<task-name>=<image>``. Blank lines
    and ``#`` comments are ignored.
    """
    if not path:
        return {}
    mapping: dict[str, str] = {}
    with open(path, encoding="utf-8") as fh:
        for lineno, raw in enumerate(fh, 1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line and " " not in line.split("=", 1)[0]:
                key, _, value = line.partition("=")
            else:
                parts = line.split()
                if len(parts) < 2:
                    raise ValueError(
                        f"{path}:{lineno}: expected '<task-name> <image>', got: {line!r}"
                    )
                key, value = parts[0], parts[1]
            mapping[key.strip()] = value.strip()
    return mapping


# Loaded once at import. ``--env-file`` is applied to os.environ before this
# module is imported, so AGBX_IMAGE_MAP is visible here.
_IMAGE_MAP: dict[str, str] = _load_image_map(os.environ.get("AGBX_IMAGE_MAP", "").strip() or None)


def _rewrite_image(raw_image: str) -> str:
    """Strip ``docker.io/``, apply the internal mirror prefix, swap the tag.

    Applies only to the task.toml ``docker_image`` path (Terminal-Bench). With
    ``AGBX_IMAGE_TAG`` unset the original tag is kept. OCI repository names are
    lowercase, so the result is lowercased.
    """
    if not raw_image:
        return raw_image
    if raw_image.startswith("docker.io/"):
        raw_image = raw_image[len("docker.io/") :]

    prefix = os.environ.get("AGBX_IMAGE_PREFIX", "").rstrip("/")
    if prefix and not raw_image.startswith(prefix + "/"):
        raw_image = f"{prefix}/{raw_image}"

    tag_override = os.environ.get("AGBX_IMAGE_TAG", "").strip()
    if tag_override:
        head, sep, last = raw_image.rpartition("/")
        name = last.split(":", 1)[0]
        raw_image = f"{head}{sep}{name}:{tag_override}"

    return raw_image.lower()


class AgentSandboxEnvironment(E2BEnvironment):
    """E2B-compatible Harbor environment that targets Agent Sandbox pools.

    Runs pre-built images on a pre-warmed pool. Image selection: an
    ``AGBX_IMAGE_MAP`` entry for the task, else the task's ``docker_image``.
    """

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
        # and run its own validation.
        super().__init__(*args, **kwargs)

        image = self._resolve_image()

        cluster = os.environ.get("AGBX_CLUSTER_ID", "").strip()
        pool = os.environ["AGBX_POOL_NAME"]
        prefix = f"{cluster}::" if cluster else ""
        # PostSandboxes on the compat layer parses "<cluster>::<pool>//<image>".
        # Replace the hash-based template name that the parent computed.
        self._template_name = f"{prefix}{pool}//{image}"

    def _resolve_image(self) -> str:
        """Pick the image: override map first, then task.toml docker_image."""
        override = _IMAGE_MAP.get(self.environment_name)
        if override:
            return override

        docker_image = self.task_env_config.docker_image
        if docker_image:
            return _rewrite_image(docker_image)

        map_path = os.environ.get("AGBX_IMAGE_MAP", "").strip() or "unset"
        raise SystemExit(
            f"AgentSandboxEnvironment: task '{self.environment_name}' has no "
            "environment.docker_image and no entry in the image-override map "
            f"(AGBX_IMAGE_MAP={map_path}). This environment only runs PRE-BUILT "
            "images; add a line '<task-name> <image-ref>' to the map, or use a "
            "dataset whose task.toml sets environment.docker_image."
        )

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
