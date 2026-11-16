"""Route to pre-provisioned environments/groups without dropping task resources."""

from __future__ import annotations

import json
import math
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class PoolRoute:
    """E2B template target and optional server-side member-group constraint."""

    env: str
    scaling_group: str | None = None


def _name(value: object, field: str) -> str:
    if not isinstance(value, str) or not value.strip() or value != value.strip():
        raise ValueError(f"Pool profiles require a nonempty, trimmed {field}")
    return value


def _capacity(profile: dict, field: str) -> float:
    value = profile.get(field, 0)
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"Pool profile {field} must be a nonnegative finite number") from exc
    if isinstance(value, bool) or not math.isfinite(number) or number < 0:
        raise ValueError(f"Pool profile {field} must be a nonnegative finite number")
    if field == "gpus" and not number.is_integer():
        raise ValueError("Pool profile gpus must be an integer")
    return number


def select_route(
    path: str,
    *,
    cpus: float,
    memory_mb: int,
    storage_mb: int,
    gpus: int,
    gpu_types: list[str],
    default_env: str | None = None,
    scaling_group: str | None = None,
) -> PoolRoute:
    """Select the smallest declared fit, optionally restricted to an exact group.

    ``env`` names an existing SandboxEnv and ``scaling_group`` is its real group
    name. Legacy ``pool`` entries retain their template-target meaning. Capacity
    declarations are caller supplied, not discovered or provisioned by this code.
    """
    profiles = json.loads(Path(path).read_text())
    if not isinstance(profiles, list):
        raise ValueError("Pool profiles must be a JSON array")
    if default_env is not None:
        default_env = _name(default_env, "default env name")
    if scaling_group is not None:
        scaling_group = _name(scaling_group, "scaling_group name")
    candidates = []
    for profile in profiles:
        if not isinstance(profile, dict):
            raise ValueError("Each pool profile must be a JSON object")
        env = profile.get("env", profile.get("pool", default_env))
        env = _name(env, "env (or legacy pool) name")
        if "env" in profile and "pool" in profile and profile["env"] != profile["pool"]:
            raise ValueError("Pool profile env and legacy pool must not disagree")
        group = profile.get("scaling_group")
        if ("env" in profile or "pool" not in profile) and "scaling_group" not in profile:
            raise ValueError("Environment profiles require an explicit scaling_group")
        if "scaling_group" in profile:
            group = _name(group, "scaling_group name")
        capacity = {
            key: _capacity(profile, key) for key in ("cpus", "memory_mb", "storage_mb", "gpus")
        }
        model = profile.get("gpu_type")
        if model is not None:
            model = _name(model, "gpu_type")
        if scaling_group is not None and group != scaling_group:
            continue
        if capacity["gpus"] != gpus:
            continue
        if any(
            capacity[key] < need
            for key, need in (("cpus", cpus), ("memory_mb", memory_mb), ("storage_mb", storage_mb))
        ):
            continue
        if gpus and gpu_types and model not in gpu_types:
            continue
        candidates.append((capacity, PoolRoute(env=env, scaling_group=group)))
    if not candidates:
        restriction = f", scaling_group={scaling_group!r}" if scaling_group is not None else ""
        raise ValueError(
            f"No pool profile fits cpu={cpus}, memory_mb={memory_mb}, "
            f"storage_mb={storage_mb}, gpus={gpus}, gpu_types={gpu_types}{restriction}"
        )
    candidates.sort(
        key=lambda item: (
            item[0]["cpus"],
            item[0]["memory_mb"],
            item[0]["storage_mb"],
            item[1].env,
            item[1].scaling_group or "",
        )
    )
    return candidates[0][1]


def select_pool(
    path: str, *, cpus: float, memory_mb: int, storage_mb: int, gpus: int, gpu_types: list[str]
) -> str:
    """Compatibility helper returning the template target of a legacy profile."""
    return select_route(
        path,
        cpus=cpus,
        memory_mb=memory_mb,
        storage_mb=storage_mb,
        gpus=gpus,
        gpu_types=gpu_types,
    ).env
