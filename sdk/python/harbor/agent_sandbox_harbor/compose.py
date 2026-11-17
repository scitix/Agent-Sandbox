"""Harbor Compose operations inside an AgentBox Docker-in-Docker sandbox."""

from __future__ import annotations

import json
import os
import re
import shlex
import tarfile
import tempfile
from pathlib import Path
from uuid import uuid4

import yaml
from harbor.environments.dind_compose import DinDComposeOps
from harbor.environments.docker import (
    self_bind_mount,
    write_env_compose_file,
    write_mounts_compose_file,
    write_resources_compose_file,
)
from harbor.environments.docker.compose_env import ComposeInfraEnvVars, legacy_log_mount_env_vars
from harbor.environments.e2b import E2BEnvironment
from harbor.models.trial.config import ResourceMode


class AgentBoxCompose(DinDComposeOps):
    ROOT = "/harbor"
    _MAIN_LOGIN_SHELL = False

    def __init__(self, environment, rewrite_image):
        self._env = environment
        self._rewrite_image = rewrite_image
        self._files = []
        self._variables = {}

    async def _host_exec(self, command, timeout_sec=None):
        return await E2BEnvironment.exec(
            self._env, command, cwd="/", user="root", env=self._variables, timeout_sec=timeout_sec
        )

    async def _checked(self, command, timeout_sec=300):
        result = await self._host_exec(command, timeout_sec)
        if result.return_code:
            raise RuntimeError(f"DinD setup failed: {result.stdout} {result.stderr}")
        return result

    async def _compose_exec(self, subcommand, timeout_sec=None):
        return await self._host_exec(
            shlex.join(
                [
                    "docker",
                    "compose",
                    "--project-name",
                    "harbor",
                    "--project-directory",
                    f"{self.ROOT}/environment",
                    *(arg for path in self._files for arg in ("-f", path)),
                    *subcommand,
                ]
            ),
            timeout_sec,
        )

    async def _stage_file_to_host(self, source_path, host_path):
        await E2BEnvironment.upload_file(self._env, source_path, host_path)

    async def _stage_dir_to_host(self, source_dir, host_dir):
        remote = f"/tmp/harbor-{uuid4().hex}.tar"
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "files.tar"
            with tarfile.open(archive, "w") as tar:
                tar.add(source_dir, arcname=".")
            await self._stage_file_to_host(archive, remote)
            await self._checked(
                f"mkdir -p {shlex.quote(host_dir)} && "
                f"tar -xf {remote} -C {shlex.quote(host_dir)} && rm -f {remote}"
            )

    async def _fetch_file_from_host(self, host_path, target_path):
        Path(target_path).parent.mkdir(parents=True, exist_ok=True)
        await E2BEnvironment.download_file(self._env, host_path, target_path)

    async def _fetch_dir_from_host(self, host_dir, target_dir):
        remote = f"/tmp/harbor-{uuid4().hex}.tar"
        await self._checked(f"tar -cf {remote} -C {shlex.quote(host_dir)} .")
        try:
            with tempfile.TemporaryDirectory() as tmp:
                archive = Path(tmp) / "files.tar"
                await self._fetch_file_from_host(remote, archive)
                Path(target_dir).mkdir(parents=True, exist_ok=True)
                with tarfile.open(archive) as tar:
                    tar.extractall(target_dir, filter="data")
        finally:
            await self._host_exec(f"rm -f {remote}")

    async def start(self, force_build=False):
        if self._env.extra_docker_compose_paths:
            raise ValueError("Additional Compose overlays are not supported by this adapter")
        if force_build:
            raise ValueError(
                "AgentBox Compose requires prebuilt images; force_build is unsupported"
            )
        await self._checked("docker info >/dev/null && docker compose version")
        await self._stage_dir_to_host(self._env.environment_dir, f"{self.ROOT}/environment")
        authored_path = self._env._compose_path
        authored = yaml.safe_load(authored_path.read_text()) or {}
        for service in authored.get("services", {}).values():
            if "build" in service:
                raise ValueError(
                    "Use the official prebuilt Compose bundle, not source build definitions"
                )
            image = service.get("image")
            if image and "$" not in image:
                service["image"] = self._rewrite_image(image)
        main = dict(image=self._env._resolve_image(), command=["sh", "-c", "sleep infinity"])
        mounts = [self_bind_mount(m) if m["type"] == "bind" else m for m in self._env._mounts]
        referenced = re.findall(
            r"\$\$|\$(?:\{)?([A-Za-z_][A-Za-z0-9_]*)", authored_path.read_text()
        )
        self._variables = {k: os.environ[k] for k in referenced if k in os.environ}
        self._variables.update(self._env._startup_env())
        self._variables.update(
            ComposeInfraEnvVars(
                main_image_name="harbor-main",
                context_dir=f"{self.ROOT}/environment",
                prebuilt_image_name=main["image"],
                cpus=self._env._effective_cpus,
                memory=f"{self._env._effective_memory_mb}M"
                if self._env._effective_memory_mb
                else None,
            ).to_env_dict()
        )
        self._variables.update(legacy_log_mount_env_vars(mounts, host_value="target"))
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "base.json").write_text(json.dumps({"services": {"main": main}}))
            (root / "authored.json").write_text(json.dumps(authored))
            write_resources_compose_file(
                root / "resources.json",
                cpu_request=self._env._resource_request_value("cpu", auto_mode=ResourceMode.LIMIT),
                cpu_limit=self._env._resource_limit_value("cpu", auto_mode=ResourceMode.LIMIT),
                memory_request_mb=self._env._resource_request_value(
                    "memory", auto_mode=ResourceMode.LIMIT
                ),
                memory_limit_mb=self._env._resource_limit_value(
                    "memory", auto_mode=ResourceMode.LIMIT
                ),
            )
            write_mounts_compose_file(root / "mounts.json", mounts)
            write_env_compose_file(
                root / "env.json",
                {k: v.replace("$", "$$") for k, v in self._env._startup_env().items()},
            )
            for name in ["base.json", "resources.json", "authored.json", "env.json", "mounts.json"]:
                remote = f"{self.ROOT}/environment/{name}"
                await self._stage_file_to_host(root / name, remote)
                self._files.append(remote)
        sources = [m["source"] for m in mounts if m["type"] == "bind"]
        if sources:
            await self._checked(
                "mkdir -p " + shlex.join(sources) + " && chmod 777 " + shlex.join(sources)
            )
        for command in [["pull"], ["up", "--detach", "--wait", "--no-build", "--pull", "never"]]:
            result = await self._compose_exec(
                command, round(self._env.task_env_config.build_timeout_sec)
            )
            if result.return_code:
                raise RuntimeError(f"Compose {command[0]} failed: {result.stdout} {result.stderr}")

    async def stop(self):
        await self._compose_exec(["down", "--volumes", "--remove-orphans"], 120)
