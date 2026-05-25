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
Unit tests for agentbox_sdk.pool (env-scoped PoolsAPI + SandboxPool).
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
ENV_NAME = "env-a"
POOL_NAME = "env-a-2c8Gi"  # derived: envName + "-" + resourceKey

POOL_FIXTURE: dict = {
    "name": POOL_NAME,
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
        mock.get(f"/envs/{ENV_NAME}/sandboxpools/{POOL_NAME}").mock(
            return_value=httpx.Response(200, json=POOL_ENVELOPE)
        )
        api = make_api()
        pool = await api.get(ENV_NAME, POOL_NAME)

    assert isinstance(pool, SandboxPool)
    assert pool.env_name == ENV_NAME
    assert pool.name == POOL_NAME
    assert pool.replicas == 3
    assert pool.idle_replicas == 2
    assert pool.running_replicas == 1
    assert pool.failed_replicas == 0


@pytest.mark.asyncio
async def test_get_pool_not_found():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get(f"/envs/{ENV_NAME}/sandboxpools/missing").mock(
            return_value=httpx.Response(404, json={"error": "pool not found"})
        )
        api = make_api()
        with pytest.raises(PoolNotFoundError):
            await api.get(ENV_NAME, "missing")


# ---------------------------------------------------------------------------
# PoolsAPI.list
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_list_pools():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get(f"/envs/{ENV_NAME}/sandboxpools").mock(
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
        result = await api.list(ENV_NAME)

    assert result.total == 1
    assert len(result.items) == 1
    assert isinstance(result.items[0], SandboxPoolData)
    assert result.items[0].name == POOL_NAME


# ---------------------------------------------------------------------------
# PoolsAPI.create
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_create_pool_with_inline_resources():
    with respx.mock(base_url=BASE_URL) as mock:
        route = mock.post(f"/envs/{ENV_NAME}/sandboxpools").mock(
            return_value=httpx.Response(202, json=POOL_ENVELOPE)
        )
        api = make_api()
        pool = await api.create(
            ENV_NAME,
            cpu="2",
            memory="8Gi",
            replicas=3,
        )

    assert pool.name == POOL_NAME
    assert pool.replicas == 3
    # Verify inlineResources made it into the body.
    sent = route.calls.last.request
    import json as _json

    body = _json.loads(sent.content)
    assert body["replicas"] == 3
    assert body["inlineResources"]["requests"] == {"cpu": "2", "memory": "8Gi"}
    assert body["inlineResources"]["limits"] == {"cpu": "2", "memory": "8Gi"}


@pytest.mark.asyncio
async def test_create_pool_with_instance_type_and_quota():
    with respx.mock(base_url=BASE_URL) as mock:
        route = mock.post(f"/envs/{ENV_NAME}/sandboxpools").mock(
            return_value=httpx.Response(202, json=POOL_ENVELOPE)
        )
        api = make_api()
        await api.create(
            ENV_NAME,
            instance_type="sci.c22-2",
            multiplier=2,
            quota_url="alice.bob.exclusive",
        )

    sent = route.calls.last.request
    import json as _json

    body = _json.loads(sent.content)
    assert body["instanceType"] == "sci.c22-2"
    assert body["multiplier"] == 2
    assert body["labels"] == {"quota.scitix.ai/url": "alice.bob.exclusive"}


# ---------------------------------------------------------------------------
# PoolsAPI.scale — uses PUT /envs/{env}/sandboxpools/{pool}
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_scale_pool():
    scaled_fixture = dict(POOL_FIXTURE)
    scaled_fixture["spec"] = dict(scaled_fixture["spec"], replicas=5)
    scaled_envelope = {"template": scaled_fixture}

    with respx.mock(base_url=BASE_URL) as mock:
        mock.put(f"/envs/{ENV_NAME}/sandboxpools/{POOL_NAME}").mock(
            return_value=httpx.Response(200, json=scaled_envelope)
        )
        api = make_api()
        pool = await api.scale(ENV_NAME, POOL_NAME, 5)

    assert pool.replicas == 5


@pytest.mark.asyncio
async def test_scale_pool_max_replicas_only():
    with respx.mock(base_url=BASE_URL) as mock:
        route = mock.put(f"/envs/{ENV_NAME}/sandboxpools/{POOL_NAME}").mock(
            return_value=httpx.Response(200, json=POOL_ENVELOPE)
        )
        api = make_api()
        await api.scale(ENV_NAME, POOL_NAME, max_replicas=20)

    sent = route.calls.last.request
    import json as _json

    body = _json.loads(sent.content)
    assert body == {"maxReplicas": 20}


# ---------------------------------------------------------------------------
# PoolsAPI.delete
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_delete_pool():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.delete(f"/envs/{ENV_NAME}/sandboxpools/{POOL_NAME}").mock(
            return_value=httpx.Response(
                202,
                json={
                    "name": POOL_NAME,
                    "namespace": "default",
                    "status": "Terminating",
                },
            )
        )
        api = make_api()
        await api.delete(ENV_NAME, POOL_NAME)  # should not raise


# ---------------------------------------------------------------------------
# SandboxPool resource helpers
# ---------------------------------------------------------------------------


def test_pool_repr():
    data = SandboxPoolData.model_validate(POOL_FIXTURE)
    api = PoolsAPI.__new__(PoolsAPI)
    pool = SandboxPool(data, api, ENV_NAME)
    assert POOL_NAME in repr(pool)
    assert ENV_NAME in repr(pool)
    assert "replicas=3" in repr(pool)


# ---------------------------------------------------------------------------
# Error mapping — generic 4xx/5xx
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_server_error_raises():
    from agentbox_sdk.exceptions import ServerError

    with respx.mock(base_url=BASE_URL) as mock:
        mock.get(f"/envs/{ENV_NAME}/sandboxpools/bad").mock(
            return_value=httpx.Response(
                500, json={"error": "internal server error"}
            )
        )
        api = make_api()
        with pytest.raises(ServerError):
            await api.get(ENV_NAME, "bad")
