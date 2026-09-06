"""Helpers shared by the kinds."""

from __future__ import annotations

from typing import Any


def cl(ctx) -> str:
    """The `--cluster` tail, so every emitted hint is copy-pasteable as-is."""
    return f" --cluster {ctx.cluster}" if ctx.cluster else ""


def res_summary(res: Any) -> str:
    """cpu/memory out of a ResourceRequirements-shaped blob, for one column."""
    if not isinstance(res, dict):
        return ""
    req = res.get("requests") or res.get("limits") or {}
    if not isinstance(req, dict):
        return ""
    cpu = req.get("cpu")
    mem = req.get("memory")
    bits = [str(cpu) if cpu else "", str(mem) if mem else ""]
    return "/".join(b for b in bits if b)


def ready_of(row: dict[str, Any]) -> Any:
    """`ready` is a bool on some shapes and a condition list on others."""
    v = row.get("ready")
    if isinstance(v, bool):
        return v
    status = row.get("status")
    if isinstance(status, dict) and isinstance(status.get("ready"), bool):
        return status["ready"]
    return v


def spec_of(row: dict[str, Any], key: str) -> Any:
    """A pool's declared values sit under `spec`, its observed ones under
    `status`."""
    spec = row.get("spec")
    return spec.get(key) if isinstance(spec, dict) else None


def status_of(row: dict[str, Any], key: str) -> Any:
    status = row.get("status")
    return status.get(key) if isinstance(status, dict) else None
