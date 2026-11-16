import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock

from harbor.environments.base import ExecResult

from agent_sandbox_harbor.compose import AgentBoxCompose


class ComposeTest(unittest.IsolatedAsyncioTestCase):
    async def test_preserves_dependencies_and_rewrites_sidecar_image(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "docker-compose.yaml"
            authored = {
                "services": {
                    "main": {"depends_on": {"db": {"condition": "service_healthy"}}},
                    "db": {
                        "image": "team/database@sha256:abc",
                        "healthcheck": {"test": ["CMD", "true"]},
                    },
                }
            }
            path.write_text(json.dumps(authored))
            env = SimpleNamespace(
                environment_dir=root,
                _compose_path=path,
                _mounts=[],
                extra_docker_compose_paths=[],
                _startup_env=lambda: {"SETTING": "$literal"},
                _effective_cpus=2,
                _effective_memory_mb=4096,
                task_env_config=SimpleNamespace(build_timeout_sec=300),
                _resolve_image=lambda: "registry.example/bench/main@sha256:def",
            )
            ops = AgentBoxCompose(env, lambda image: "registry.example/bench/" + image)
            staged = {}

            async def stage(source, target):
                staged[Path(target).name] = json.loads(Path(source).read_text())

            ops._stage_file_to_host = stage
            ops._stage_dir_to_host = AsyncMock()
            ops._checked = AsyncMock()
            ops._compose_exec = AsyncMock(return_value=ExecResult(return_code=0))
            await ops.start()
            self.assertEqual(
                staged["authored.json"]["services"]["main"], authored["services"]["main"]
            )
            self.assertEqual(
                staged["authored.json"]["services"]["db"]["image"],
                "registry.example/bench/team/database@sha256:abc",
            )
            self.assertEqual(
                ops._compose_exec.call_args.args[0],
                ["up", "--detach", "--wait", "--no-build", "--pull", "never"],
            )


if __name__ == "__main__":
    unittest.main()
