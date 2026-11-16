import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, patch

from harbor.models.task.config import EnvironmentConfig
from harbor.models.trial.paths import TrialPaths

from agent_sandbox_harbor import AgentSandboxEnvironment


class EnvironmentTest(unittest.IsolatedAsyncioTestCase):
    async def test_start_uses_pool_without_template_build(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "Dockerfile").write_text("FROM ubuntu:22.04\n")
            with patch.dict(os.environ, {"AGBX_POOL_NAME": "cpu"}):
                env = AgentSandboxEnvironment(
                    environment_dir=root,
                    environment_name="task",
                    session_id="trial",
                    trial_paths=TrialPaths(root / "trial"),
                    task_env_config=EnvironmentConfig(docker_image="team/task:v1"),
                )
            if not hasattr(getattr(env, "options", None), "prebuilt_template_id"):
                self.skipTest("Public prebuilt startup requires Harbor 0.26")
            self.assertEqual(env.template_ref, "cpu//team/task:v1")
            env._renew_interval_sec = 0
            env._sandbox = AsyncMock()
            with (
                patch.object(env, "_create_sandbox", new_callable=AsyncMock) as create,
                patch.object(env, "_launch_from_built_template", new_callable=AsyncMock) as build,
                patch.object(env, "_prepare_sandbox", new_callable=AsyncMock) as prepare,
                patch.object(env, "ensure_dirs", new_callable=AsyncMock),
                patch.object(env, "_upload_environment_dir_after_start", new_callable=AsyncMock),
            ):
                await env.start(force_build=False)
                create.assert_awaited_once()
                build.assert_not_awaited()
                prepare.assert_awaited_once()

    async def test_explicit_thread_budget_preserves_task_settings(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "Dockerfile").write_text("FROM ubuntu:22.04\n")
            with patch.dict(os.environ, {"AGBX_POOL_NAME": "cpu", "AGBX_CPU_THREADS": "2"}):
                env = AgentSandboxEnvironment(
                    environment_dir=root,
                    environment_name="task",
                    session_id="trial",
                    trial_paths=TrialPaths(root / "trial"),
                    task_env_config=EnvironmentConfig(
                        cpus=2,
                        memory_mb=4096,
                        docker_image="team/task:v1",
                        env={"OMP_NUM_THREADS": "1"},
                    ),
                )
                startup = env._startup_env()
                self.assertEqual(startup["OMP_NUM_THREADS"], "1")
                self.assertEqual(startup["MKL_NUM_THREADS"], "2")

    async def test_compose_artifact_type_checks_use_container(self):
        env = object.__new__(AgentSandboxEnvironment)
        env._compose = AsyncMock()
        env._compose.is_file.return_value = True
        env._compose.is_dir.return_value = False
        self.assertTrue(await env.is_file("/workspace/dispatch"))
        self.assertFalse(await env.is_dir("/workspace/dispatch"))
        env._compose.is_file.assert_awaited_once_with("/workspace/dispatch", user=None)
        env._compose.is_dir.assert_awaited_once_with("/workspace/dispatch", user=None)

    async def test_gpu_create_preserves_environment(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "Dockerfile").write_text("FROM ubuntu:22.04\nWORKDIR /app\n")
            profiles = root / "profiles.json"
            profiles.write_text(
                json.dumps(
                    [
                        dict(
                            pool="gpu-pool",
                            cpus=8,
                            memory_mb=16384,
                            storage_mb=30720,
                            gpus=1,
                            gpu_type="H100",
                        )
                    ]
                )
            )
            with patch.dict(
                os.environ,
                {
                    "AGBX_POOL_PROFILES": str(profiles),
                    "AGBX_IMAGE_PREFIX": "registry.example/bench",
                },
            ):
                env = AgentSandboxEnvironment(
                    environment_dir=root,
                    environment_name="task",
                    session_id="trial",
                    trial_paths=TrialPaths(root / "trial"),
                    task_env_config=EnvironmentConfig(
                        cpus=8,
                        memory_mb=16384,
                        gpus=1,
                        gpu_types=["H100"],
                        storage_mb=30720,
                        docker_image="docker.io/team/task:v1",
                        env={"TEST_SETTING": "kept"},
                    ),
                )
                sandbox = AsyncMock()
                with patch(
                    "agent_sandbox_harbor.environment.AsyncSandbox.create",
                    new=AsyncMock(return_value=sandbox),
                ) as create:
                    await env._create_sandbox()
                    self.assertEqual(
                        create.call_args.args[0], "gpu-pool//registry.example/bench/team/task:v1"
                    )
                    self.assertEqual(create.call_args.kwargs["envs"]["TEST_SETTING"], "kept")
                    self.assertGreaterEqual(create.call_args.kwargs["request_timeout"], 330)
                    self.assertTrue(env.capabilities.gpus)


class ScalingGroupEnvironmentTest(unittest.IsolatedAsyncioTestCase):
    def make_environment(self, root, **kwargs):
        (root / "Dockerfile").write_text("FROM ubuntu:22.04\n")
        config = kwargs.pop(
            "task_env_config",
            EnvironmentConfig(
                cpus=1,
                memory_mb=2048,
                storage_mb=8192,
                docker_image="team/task:v1",
            ),
        )
        return AgentSandboxEnvironment(
            environment_dir=root,
            environment_name="task",
            session_id="trial",
            trial_paths=TrialPaths(root / "trial"),
            task_env_config=config,
            **kwargs,
        )

    async def assert_create_route(self, env, template, group):
        sandbox = AsyncMock()
        with patch(
            "agent_sandbox_harbor.environment.AsyncSandbox.create",
            new=AsyncMock(return_value=sandbox),
        ) as create:
            await env._create_sandbox()
        self.assertEqual(create.call_args.args[0], template)
        metadata = create.call_args.kwargs["metadata"]
        if group is None:
            self.assertNotIn("agentbox.scitix.ai/scaling-group", metadata)
        else:
            self.assertEqual(metadata["agentbox.scitix.ai/scaling-group"], group)

    async def test_fixed_group_separate_from_environment_template(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                {
                    "AGBX_ENV_NAME": "bench",
                    "AGBX_SCALING_GROUP": "actual-group",
                    "AGBX_CLUSTER_ID": "foo",
                },
                clear=True,
            ),
        ):
            env = self.make_environment(Path(tmp))
            await self.assert_create_route(env, "foo::bench//team/task:v1", "actual-group")

    async def test_legacy_target_omits_group_metadata(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                {"AGBX_POOL_NAME": "legacy"},
                clear=True,
            ),
        ):
            env = self.make_environment(Path(tmp))
            await self.assert_create_route(env, "legacy//team/task:v1", None)

    async def test_profile_selection_respects_harbor_resource_overrides(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "profiles.json"
            path.write_text(
                json.dumps(
                    [
                        dict(scaling_group="small", cpus=1, memory_mb=2048, storage_mb=32768),
                        dict(scaling_group="small-disk", cpus=4, memory_mb=8192, storage_mb=8192),
                        dict(
                            scaling_group="custom-big-disk",
                            cpus=4,
                            memory_mb=8192,
                            storage_mb=32768,
                        ),
                    ]
                )
            )
            with patch.dict(
                os.environ, {"AGBX_ENV_NAME": "bench", "AGBX_POOL_PROFILES": str(path)}, clear=True
            ):
                env = self.make_environment(
                    root, override_cpus=4, override_memory_mb=8192, override_storage_mb=32768
                )
                await self.assert_create_route(env, "bench//team/task:v1", "custom-big-disk")

    async def test_dind_has_independent_env_and_group(self):
        with (
            tempfile.TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                {
                    "AGBX_ENV_NAME": "regular",
                    "AGBX_SCALING_GROUP": "cpu",
                    "AGBX_DIND_ENV_NAME": "compose-env",
                    "AGBX_DIND_SCALING_GROUP": "compose-group",
                },
                clear=True,
            ),
        ):
            root = Path(tmp)
            (root / "docker-compose.yaml").write_text(
                "services:\n  main:\n    image: team/task:v1\n"
            )
            env = self.make_environment(root)
            await self.assert_create_route(env, "compose-env", "compose-group")

    async def test_ambiguous_alias_and_gpu_without_profiles_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.dict(os.environ, {"AGBX_ENV_NAME": "a", "AGBX_POOL_NAME": "b"}, clear=True):
                with self.assertRaisesRegex(ValueError, "disagree"):
                    self.make_environment(root)
            with patch.dict(
                os.environ, {"AGBX_ENV_NAME": "bench", "AGBX_SCALING_GROUP": "gpu"}, clear=True
            ):
                with self.assertRaisesRegex(ValueError, "GPU tasks require"):
                    self.make_environment(
                        root,
                        task_env_config=EnvironmentConfig(
                            docker_image="team/task:v1",
                            gpus=1,
                        ),
                    )

    async def test_preflight_accepts_env_name_and_profile_only(self):
        for settings in (
            {"AGBX_ENV_NAME": "bench"},
            {"AGBX_POOL_PROFILES": "profiles.json"},
            {"AGBX_DIND_ENV_NAME": "compose"},
        ):
            with patch.dict(os.environ, {"E2B_API_KEY": "test", **settings}, clear=True):
                AgentSandboxEnvironment.preflight()

    async def test_unmatched_profiles_do_not_fall_back_to_default_env(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "profiles.json"
            path.write_text(
                json.dumps(
                    [
                        dict(
                            env="bench",
                            scaling_group="small",
                            cpus=1,
                            memory_mb=2048,
                            storage_mb=4096,
                        ),
                    ]
                )
            )
            with (
                patch.dict(
                    os.environ,
                    {"AGBX_ENV_NAME": "default-fallback", "AGBX_POOL_PROFILES": str(path)},
                    clear=True,
                ),
                patch(
                    "agent_sandbox_harbor.environment.AsyncSandbox.create", new_callable=AsyncMock
                ) as create,
            ):
                with self.assertRaisesRegex(ValueError, "No pool profile"):
                    self.make_environment(root)
                create.assert_not_called()
