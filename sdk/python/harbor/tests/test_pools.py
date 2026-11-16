import json
import tempfile
import unittest
from pathlib import Path

from agent_sandbox_harbor.pools import select_pool


class PoolSelectionTest(unittest.TestCase):
    def test_separate_verifier_and_gpu_routes(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "pools.json"
            path.write_text(
                json.dumps(
                    [
                        dict(pool="cpu", cpus=8, memory_mb=16384, storage_mb=30720, gpus=0),
                        dict(
                            pool="gpu",
                            cpus=8,
                            memory_mb=16384,
                            storage_mb=30720,
                            gpus=1,
                            gpu_type="H100",
                        ),
                    ]
                )
            )
            req = dict(cpus=8, memory_mb=16384, storage_mb=30720, gpu_types=[])
            self.assertEqual(select_pool(str(path), gpus=0, **req), "cpu")
            self.assertEqual(select_pool(str(path), gpus=1, **req), "gpu")
            with self.assertRaises(ValueError):
                select_pool(str(path), gpus=2, **req)
            req["gpu_types"] = ["A100"]
            with self.assertRaises(ValueError):
                select_pool(str(path), gpus=1, **req)


class ScalingGroupSelectionTest(unittest.TestCase):
    def select(self, profiles, **kwargs):
        from agent_sandbox_harbor.pools import select_route

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "profiles.json"
            path.write_text(json.dumps(profiles))
            needs = dict(cpus=2, memory_mb=4096, storage_mb=10240, gpus=0, gpu_types=[])
            needs.update(kwargs)
            return select_route(str(path), **needs)

    def test_auto_group_uses_real_name_and_sufficient_storage(self):
        profiles = [
            dict(scaling_group="small-disk", cpus=2, memory_mb=4096, storage_mb=8192),
            dict(scaling_group="custom-balanced", cpus=2, memory_mb=4096, storage_mb=16384),
            dict(scaling_group="large", cpus=4, memory_mb=8192, storage_mb=16384),
        ]
        route = self.select(profiles, default_env="benchmark-env")
        self.assertEqual(route.env, "benchmark-env")
        self.assertEqual(route.scaling_group, "custom-balanced")

    def test_fixed_group_never_falls_back_or_ignores_capacity(self):
        profiles = [
            dict(env="bench", scaling_group="small", cpus=1, memory_mb=2048, storage_mb=10240),
            dict(env="bench", scaling_group="large", cpus=4, memory_mb=8192, storage_mb=10240),
        ]
        route = self.select(profiles, scaling_group="large")
        self.assertEqual(route.scaling_group, "large")
        for group in ("small", "unknown", "LARGE"):
            with self.subTest(group=group), self.assertRaisesRegex(ValueError, "No pool profile"):
                self.select(profiles, scaling_group=group)

    def test_group_gpu_count_and_model_are_exact_constraints(self):
        profiles = [
            dict(env="bench", scaling_group="cpu", cpus=8, memory_mb=16384, storage_mb=30720),
            dict(
                env="bench",
                scaling_group="gpu-a",
                cpus=8,
                memory_mb=16384,
                storage_mb=30720,
                gpus=1,
                gpu_type="A100",
            ),
            dict(
                env="bench",
                scaling_group="gpu-h",
                cpus=8,
                memory_mb=16384,
                storage_mb=30720,
                gpus=1,
                gpu_type="H100",
            ),
        ]
        route = self.select(profiles, gpus=1, gpu_types=["H100"])
        self.assertEqual(route.scaling_group, "gpu-h")
        self.assertEqual(self.select(profiles).scaling_group, "cpu")
        with self.assertRaises(ValueError):
            self.select(profiles, gpus=2)
        with self.assertRaises(ValueError):
            self.select(profiles, gpus=1, gpu_types=["H100"], scaling_group="cpu")

    def test_legacy_target_and_group_profile_can_coexist(self):
        route = self.select([dict(pool="legacy-env", cpus=2, memory_mb=4096, storage_mb=10240)])
        self.assertEqual(route.env, "legacy-env")
        self.assertIsNone(route.scaling_group)
        with self.assertRaises(ValueError):
            self.select(
                [dict(pool="legacy-env", cpus=2, memory_mb=4096, storage_mb=10240)],
                scaling_group="not-declared",
            )

    def test_invalid_profiles_fail_closed(self):
        valid = dict(env="bench", scaling_group="actual", cpus=2, memory_mb=4096, storage_mb=10240)
        invalid = [
            None,
            {},
            [None],
            [dict(valid, cpus=float("nan"))],
            [dict(valid, gpus=1.5)],
            [dict(valid, scaling_group="")],
            [dict(valid, pool="other")],
            [dict(valid, cpus=-1)],
            [dict(env="bench", cpus=2, memory_mb=4096, storage_mb=10240)],
        ]
        for profiles in invalid:
            with self.subTest(profiles=profiles), self.assertRaises(ValueError):
                self.select(profiles)


if __name__ == "__main__":
    unittest.main()
