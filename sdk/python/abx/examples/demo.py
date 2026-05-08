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
Full demo of the agentbox_sdk.

Run with:
    export AGENTBOX_ENDPOINT=http://agentbox.example.com
    export AGENTBOX_API_KEY=agbx_xxx
    python examples/demo.py
"""

from __future__ import annotations

import asyncio
import agentbox_sdk as abx


async def main() -> None:
    async with abx.AgentBoxClient() as client:

        # ------------------------------------------------------------------
        # 1. Confirm identity
        # ------------------------------------------------------------------
        me = await client.auth.whoami()
        print(f"[whoami] role={me.role!r} user={me.user!r} team={me.team!r}")

        # ------------------------------------------------------------------
        # 2. List available templates
        # ------------------------------------------------------------------
        templates = await client.templates.list(limit=5)
        print(f"[templates] total={templates.total}")
        for t in templates.items:
            print(f"  - {t.name!r}  version={t.version!r}")

        # ------------------------------------------------------------------
        # 3. Create a Pool
        # ------------------------------------------------------------------
        pool_name = "demo-pool"
        pool = await client.pools.create(
            pool_name,
            template_name=templates.items[0].name if templates.items else "default",
            replicas=2,
        )
        print(f"[pool.create] {pool}")

        # ------------------------------------------------------------------
        # 4. Scale the Pool
        # ------------------------------------------------------------------
        pool = await client.pools.scale(pool_name, replicas=3)
        print(f"[pool.scale]  {pool}")

        # ------------------------------------------------------------------
        # 5. Create a Sandbox (context manager — auto-released on exit)
        # ------------------------------------------------------------------
        print("[sandbox] creating …")
        async with client.sandboxes.create(
            pool=pool_name,
            metadata={"user": "demo", "session": "abc"},
            idle_timeout="10m",
        ) as sb:
            print(f"[sandbox.create] {sb}")

            # Endpoint helper
            try:
                endpoint = sb.get_endpoint(8080)
                print(f"[sandbox] port 8080 → {endpoint}")
            except abx.EndpointNotFoundError as e:
                print(f"[sandbox] {e}")

            # Fetch logs
            logs = await sb.get_logs(tail_lines=20)
            print(f"[sandbox.logs] {len(logs.entries)} entries  truncated={logs.truncated}")

            # Extend timeout
            await sb.set_timeout("20m")
            print("[sandbox] timeout extended to 20m")

        # sb.close() was called automatically by the context manager
        print("[sandbox] released")

        # ------------------------------------------------------------------
        # 6. List sandboxes in the pool
        # ------------------------------------------------------------------
        sandboxes = await client.sandboxes.list(pool=pool_name, limit=10)
        print(f"[sandboxes.list] total={sandboxes.total}")

        # ------------------------------------------------------------------
        # 7. Delete the Pool
        # ------------------------------------------------------------------
        await client.pools.delete(pool_name)
        print(f"[pool.delete] {pool_name!r} deleted")

        # ------------------------------------------------------------------
        # 8. Error handling example
        # ------------------------------------------------------------------
        print("[errors] testing not-found …")
        try:
            await client.sandboxes.get("nonexistent-sandbox-id")
        except abx.SandboxNotFoundError as e:
            print(f"  SandboxNotFoundError: {e}")

        try:
            await client.pools.get("nonexistent-pool")
        except abx.PoolNotFoundError as e:
            print(f"  PoolNotFoundError: {e}")


if __name__ == "__main__":
    asyncio.run(main())
