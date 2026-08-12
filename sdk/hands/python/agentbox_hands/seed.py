"""Sandbox seeding: workspace dir + optional repo clone.

Kubernetes access is NOT seeded here. The assistant reads the cluster through
the read-only kubernetes-mcp-server (per-cluster sidecar, reached via the hub
proxy), so no kubeconfig is ever written into the sandbox. The sandbox exists
only for general bash / file work (e.g. browsing source, running make).
"""
from __future__ import annotations
import os
from e2b import Sandbox

# Same unprivileged identity the daemon runs every agent-facing tool call as
# (see daemon.SBX_USER). Seeded files/dirs must be owned by this user, otherwise
# the agent — running as this user — could not overwrite them later.
SBX_USER = os.environ.get("SBX_USER", "user")


def seed(sbx: Sandbox) -> None:
    """Idempotent seed for a freshly created sandbox.

    Inputs from env:
      SBX_SEED_REPO  - optional git repo to clone into /tmp/workspace/scheduler
      SBX_SKIP_SEED  - "1" = skip workspace/repo seeding (pre-baked image)
    """
    if os.environ.get("SBX_SKIP_SEED") == "1":
        return

    sbx.commands.run("mkdir -p /tmp/workspace", timeout=10, user=SBX_USER)

    repo = os.environ.get("SBX_SEED_REPO", "").strip()
    if repo:
        target = "/tmp/workspace/scheduler"
        r = sbx.commands.run(
            f"[ -d {target}/.git ] || git clone --depth=1 {repo} {target}",
            timeout=180,
            user=SBX_USER,
        )
        if r.exit_code != 0:
            sbx.files.write(
                "/tmp/workspace/SEED_CLONE_FAILED.txt",
                f"repo={repo}\nexit={r.exit_code}\nstderr={r.stderr}\n",
                user=SBX_USER,
            )
