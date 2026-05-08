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
Unit tests for agentbox_sdk.pool (PoolsAPI + SandboxPool resource).
"""

from __future__ import annotations

import pytest
import respx
import httpx

from agentbox_sdk._generated.client import AuthenticatedClient
from agentbox_sdk.pool import PoolsAPI, SandboxPool
from agentbox_sdk.models import SandboxPoolData
from agentbox_sdk.exceptions import PoolNotFoundError


BASE_URL = "http://agentbox.test/v1"

POOL_FIXTURE: dict = {
    "name": "test-pool",
    "namespace": "default",
    "spec": {"replicas": 3, "templateName": "gpu-v100"},
    "status": {
        "idleReplicas": 2,
        "runningReplicas": 1,
        "failedReplicas": 0,
    },
}

# The API wraps pool responses in {"template": {...}}
POOL_ENVELOPE: dict = {"template": POOL_FIXTURE}


def make_api(base_url: str = BASE_URL) -> PoolsAPI:
    client = AuthenticatedClient(
        base_url=base_url,
        token="agbx_test",
        auth_header_name="AGENTBOX-API-KEY",
        prefix="",
    )
    return PoolsAPI(client)


# ---------------------------------------------------------------------------
# PoolsAPI.get
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_pool_ok():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxpools/test-pool").mock(
            return_value=httpx.Response(200, json=POOL_ENVELOPE)
        )
        api = make_api()
        pool = await api.get("test-pool")

    assert isinstance(pool, SandboxPool)
    assert pool.name == "test-pool"
    assert pool.replicas == 3
    assert pool.idle_replicas == 2
    assert pool.running_replicas == 1
    assert pool.failed_replicas == 0


@pytest.mark.asyncio
async def test_get_pool_not_found():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxpools/missing").mock(
            return_value=httpx.Response(404, json={"error": "pool not found"})
        )
        api = make_api()
        with pytest.raises(PoolNotFoundError):
            await api.get("missing")


# ---------------------------------------------------------------------------
# PoolsAPI.list
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_list_pools():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxpools").mock(
            return_value=httpx.Response(
                200,
                json={
                    "items": [POOL_FIXTURE],
                    "total": 1,
                    "limit": 20,
                    "offset": 0,
                },
            )
        )
        api = make_api()
        result = await api.list()

    assert result.total == 1
    assert len(result.items) == 1
    assert isinstance(result.items[0], SandboxPoolData)
    assert result.items[0].name == "test-pool"


# ---------------------------------------------------------------------------
# PoolsAPI.create
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_create_pool():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.post("/sandboxpools").mock(
            return_value=httpx.Response(201, json=POOL_ENVELOPE)
        )
        api = make_api()
        pool = await api.create(
            "test-pool", template_name="gpu-v100", replicas=3
        )

    assert pool.name == "test-pool"
    assert pool.replicas == 3


# ---------------------------------------------------------------------------
# PoolsAPI.scale — now uses PUT /sandboxpools/{name} (not PATCH /pools/{name})
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_scale_pool():
    scaled_fixture = dict(POOL_FIXTURE)
    scaled_fixture["spec"] = dict(scaled_fixture["spec"], replicas=5)
    scaled_envelope = {"template": scaled_fixture}

    with respx.mock(base_url=BASE_URL) as mock:
        mock.put("/sandboxpools/test-pool").mock(
            return_value=httpx.Response(200, json=scaled_envelope)
        )
        api = make_api()
        pool = await api.scale("test-pool", 5)

    assert pool.replicas == 5


# ---------------------------------------------------------------------------
# PoolsAPI.delete
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_delete_pool():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.delete("/sandboxpools/test-pool").mock(
            return_value=httpx.Response(
                202,
                json={
                    "name": "test-pool",
                    "namespace": "default",
                    "status": "Deleting",
                },
            )
        )
        api = make_api()
        await api.delete("test-pool")  # should not raise


# ---------------------------------------------------------------------------
# SandboxPool resource helpers
# ---------------------------------------------------------------------------


def test_pool_repr():
    data = SandboxPoolData.model_validate(POOL_FIXTURE)
    api = PoolsAPI.__new__(PoolsAPI)
    pool = SandboxPool(data, api)
    assert "test-pool" in repr(pool)
    assert "replicas=3" in repr(pool)


# ---------------------------------------------------------------------------
# Error mapping — generic 4xx/5xx
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_server_error_raises():
    from agentbox_sdk.exceptions import ServerError

    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxpools/bad").mock(
            return_value=httpx.Response(
                500, json={"error": "internal server error"}
            )
        )
        api = make_api()
        with pytest.raises(ServerError):
            await api.get("bad")
