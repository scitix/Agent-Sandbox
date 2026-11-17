import json
import os
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import yaml
from harbor.environments.base import BaseEnvironment
from harbor.models.task.config import EnvironmentConfig
from harbor.models.trial.config import ResourceMode
from harbor.models.trial.paths import TrialPaths

from agent_sandbox_harbor import AgentSandboxEnvironment
from agent_sandbox_harbor.compose import AgentBoxCompose


class PolicyEnvironment(SimpleNamespace):
    _resource_mode = BaseEnvironment._resource_mode
    _resource_value = BaseEnvironment._resource_value
    _resource_policy_value = BaseEnvironment._resource_policy_value
    _resource_request_value = BaseEnvironment._resource_request_value
    _resource_limit_value = BaseEnvironment._resource_limit_value
    _effective_cpus = BaseEnvironment._effective_cpus
    _effective_memory_mb = BaseEnvironment._effective_memory_mb


class ComposeResourceTests(unittest.IsolatedAsyncioTestCase):
    async def render(
        self,
        cpus,
        memory_mb,
        *,
        override=False,
        cpu_mode=ResourceMode.AUTO,
        memory_mode=ResourceMode.AUTO,
    ):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            authored = {
                "services": {
                    "main": {"cap_add": ["SYS_PTRACE"], "healthcheck": {"test": ["CMD", "true"]}},
                    "loadgen": {
                        "image": "example/loadgen:v1",
                        "cpus": 1.0,
                        "mem_limit": "512m",
                        "depends_on": {"main": {"condition": "service_healthy"}},
                        "healthcheck": {
                            "test": ["CMD", "/app/loadgen", "--healthcheck"],
                            "interval": "2s",
                            "retries": 60,
                        },
                    },
                }
            }
            compose_path = root / "docker-compose.yaml"
            compose_path.write_text(yaml.safe_dump(authored))
            if override:
                with patch.dict(os.environ, {"AGBX_DIND_ENV_NAME": "dind"}, clear=True):
                    env = AgentSandboxEnvironment(
                        environment_dir=root,
                        environment_name="resource-test",
                        session_id="test",
                        trial_paths=TrialPaths(root / "trial"),
                        task_env_config=EnvironmentConfig(
                            cpus=2, memory_mb=4096, storage_mb=10240, docker_image="example/main:v1"
                        ),
                        override_cpus=cpus,
                        override_memory_mb=memory_mb,
                        cpu_enforcement_policy=cpu_mode,
                        memory_enforcement_policy=memory_mode,
                    )
            else:
                env = PolicyEnvironment(
                    extra_docker_compose_paths=[],
                    environment_dir=root,
                    _compose_path=compose_path,
                    _cpu_resource_mode=cpu_mode,
                    _memory_resource_mode=memory_mode,
                    _resolve_image=lambda: "example/main:v1",
                    _mounts=[],
                    _startup_env=lambda: {},
                    task_env_config=SimpleNamespace(
                        build_timeout_sec=60, cpus=cpus, memory_mb=memory_mb
                    ),
                )
            ops = AgentBoxCompose(env, lambda image: image)
            sent = {}
            order = []

            async def capture(local, remote):
                order.append(Path(remote).name)
                sent[Path(remote).name] = json.loads(Path(local).read_text())

            ops._checked = AsyncMock()
            ops._stage_dir_to_host = AsyncMock()
            ops._stage_file_to_host = AsyncMock(side_effect=capture)
            ops._compose_exec = AsyncMock(
                return_value=SimpleNamespace(return_code=0, stdout="", stderr="")
            )
            await ops.start()
            self.assertEqual(
                order, ["base.json", "resources.json", "authored.json", "env.json", "mounts.json"]
            )
            self.assertEqual(sent["authored.json"], authored)
            self.assertEqual(list(sent["resources.json"]["services"]), ["main"])
            return sent["resources.json"]["services"]["main"]

    async def test_task_two_cpu_four_gib_gets_real_limits(self):
        self.assertEqual(await self.render(2, 4096), {"cpus": 2.0, "mem_limit": "4096m"})

    async def test_actual_environment_override_values_are_used(self):
        self.assertEqual(
            await self.render(4, 8192, override=True), {"cpus": 4.0, "mem_limit": "8192m"}
        )

    async def test_fractional_effective_cpu_is_not_truncated(self):
        self.assertEqual(await self.render(0.5, 1024), {"cpus": 0.5, "mem_limit": "1024m"})

    async def test_none_does_not_invent_limits_or_override_authored_sidecar(self):
        self.assertEqual(await self.render(None, None), {})

    async def test_independent_unspecified_dimensions(self):
        self.assertEqual(await self.render(2, None), {"cpus": 2.0})
        self.assertEqual(await self.render(None, 4096), {"mem_limit": "4096m"})

    async def test_request_means_reservation_without_hard_limits(self):
        self.assertEqual(
            await self.render(
                2, 4096, cpu_mode=ResourceMode.REQUEST, memory_mode=ResourceMode.REQUEST
            ),
            {"deploy": {"resources": {"reservations": {"cpus": "2", "memory": "4096M"}}}},
        )

    async def test_limit_matches_auto(self):
        self.assertEqual(
            await self.render(2, 4096, cpu_mode=ResourceMode.LIMIT, memory_mode=ResourceMode.LIMIT),
            {"cpus": 2.0, "mem_limit": "4096m"},
        )

    async def test_guarantee_sets_request_and_limit(self):
        self.assertEqual(
            await self.render(
                2, 4096, cpu_mode=ResourceMode.GUARANTEE, memory_mode=ResourceMode.GUARANTEE
            ),
            {
                "cpus": 2.0,
                "mem_limit": "4096m",
                "deploy": {"resources": {"reservations": {"cpus": "2", "memory": "4096M"}}},
            },
        )

    async def test_ignore_omits_limits_even_when_task_has_values(self):
        self.assertEqual(
            await self.render(
                2, 4096, cpu_mode=ResourceMode.IGNORE, memory_mode=ResourceMode.IGNORE
            ),
            {},
        )

    async def test_independent_cpu_and_memory_policy(self):
        self.assertEqual(
            await self.render(
                2, 4096, cpu_mode=ResourceMode.REQUEST, memory_mode=ResourceMode.LIMIT
            ),
            {"mem_limit": "4096m", "deploy": {"resources": {"reservations": {"cpus": "2"}}}},
        )
        self.assertEqual(
            await self.render(
                2, 4096, cpu_mode=ResourceMode.IGNORE, memory_mode=ResourceMode.REQUEST
            ),
            {"deploy": {"resources": {"reservations": {"memory": "4096M"}}}},
        )

    async def test_real_environment_supported_request_policy(self):
        self.assertEqual(
            await self.render(
                4,
                8192,
                override=True,
                cpu_mode=ResourceMode.REQUEST,
                memory_mode=ResourceMode.REQUEST,
            ),
            {"deploy": {"resources": {"reservations": {"cpus": "4", "memory": "8192M"}}}},
        )

    async def test_real_environment_ignore_policy_omits_resources(self):
        self.assertEqual(
            await self.render(
                4,
                8192,
                override=True,
                cpu_mode=ResourceMode.IGNORE,
                memory_mode=ResourceMode.IGNORE,
            ),
            {},
        )

    async def test_existing_backend_limit_capability_rejection_is_preserved(self):
        for mode in (ResourceMode.LIMIT, ResourceMode.GUARANTEE):
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                await self.render(4, 8192, override=True, cpu_mode=mode, memory_mode=mode)


if __name__ == "__main__":
    unittest.main()
