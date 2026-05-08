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
Unit tests for agentbox_sdk.sandbox (SandboxesAPI + Sandbox resource).
"""

from __future__ import annotations

import pytest
import respx
import httpx
from datetime import datetime, timezone

from agentbox_sdk._generated.client import AuthenticatedClient
from agentbox_sdk.sandbox import SandboxesAPI, Sandbox
from agentbox_sdk.models import SandboxData, SandboxStatus, EndpointData
from agentbox_sdk.exceptions import (
    SandboxNotFoundError,
    EndpointNotFoundError,
    SandboxTimeoutError,
    SandboxStartupError,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

BASE_URL = "http://agentbox.test/v1"

SANDBOX_FIXTURE: dict = {
    "sandboxId": "sb-abc123",
    "namespace": "default",
    "poolName": "test-pool",
    "podName": "test-pool-pod-0",
    "status": "Running",
    "claimedAt": "2026-01-01T00:00:00Z",
    "containerImages": {"main": "ubuntu:22.04"},
    "metadata": {"user": "alice"},
    "endpoints": {
        "8080": {"url": "http://10.0.0.1:8080"},
        "22": {"url": "10.0.0.1:22"},
    },
}


def make_api(base_url: str = BASE_URL) -> SandboxesAPI:
    client = AuthenticatedClient(
        base_url=base_url,
        token="agbx_test",
        auth_header_name="AGENTBOX-API-KEY",
        prefix="",
    )
    return SandboxesAPI(client)


# ---------------------------------------------------------------------------
# SandboxesAPI.get
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_sandbox_ok():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(200, json={"sandbox": SANDBOX_FIXTURE})
        )
        api = make_api()
        sb = await api.get("sb-abc123")

    assert isinstance(sb, Sandbox)
    assert sb.id == "sb-abc123"
    assert sb.status == SandboxStatus.RUNNING
    assert sb.pool_name == "test-pool"


@pytest.mark.asyncio
async def test_get_sandbox_not_found():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/missing").mock(
            return_value=httpx.Response(
                404, json={"error": "sandbox not found"}
            )
        )
        api = make_api()
        with pytest.raises(SandboxNotFoundError):
            await api.get("missing")


# ---------------------------------------------------------------------------
# SandboxesAPI.list
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_list_sandboxes():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes").mock(
            return_value=httpx.Response(
                200,
                json={
                    "items": [SANDBOX_FIXTURE],
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
    assert isinstance(result.items[0], SandboxData)
    assert result.items[0].sandbox_id == "sb-abc123"


# ---------------------------------------------------------------------------
# SandboxesAPI.delete
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_delete_sandbox():
    delete_response = {
        "sandboxId": "sb-abc123",
        "namespace": "default",
        "poolName": "test-pool",
        "podName": "test-pool-pod-0",
        "status": "Stopping",
    }
    with respx.mock(base_url=BASE_URL) as mock:
        mock.delete("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(202, json=delete_response)
        )
        api = make_api()
        await api.delete("sb-abc123")  # should not raise


# ---------------------------------------------------------------------------
# SandboxesAPI.set_timeout — now uses PUT (not POST)
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_set_timeout():
    with respx.mock(base_url=BASE_URL) as mock:
        mock.put("/sandboxes/sb-abc123/timeout").mock(
            return_value=httpx.Response(204)
        )
        api = make_api()
        await api.set_timeout("sb-abc123", "30m")


# ---------------------------------------------------------------------------
# SandboxesAPI.get_logs
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_logs():
    logs_fixture = {
        "sandboxId": "sb-abc123",
        "namespace": "default",
        "entries": [{"container": "main", "log": "hello world"}],
        "truncated": False,
        "source": "live",
    }
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123/logs").mock(
            return_value=httpx.Response(200, json=logs_fixture)
        )
        api = make_api()
        logs = await api.get_logs("sb-abc123", tail_lines=10)

    assert logs.sandbox_id == "sb-abc123"
    assert len(logs.entries) == 1
    assert logs.entries[0].log == "hello world"


# ---------------------------------------------------------------------------
# Sandbox resource helpers
# ---------------------------------------------------------------------------


def make_sandbox(
    data_overrides: dict | None = None,
) -> tuple[Sandbox, SandboxesAPI]:
    fixture = dict(SANDBOX_FIXTURE)
    if data_overrides:
        fixture.update(data_overrides)
    data = SandboxData.model_validate(fixture)
    # Use a dummy API with no real http client (not used in these tests)
    api = SandboxesAPI.__new__(SandboxesAPI)
    sb = Sandbox(data, api)
    return sb, api


def test_get_endpoint_ok():
    sb, _ = make_sandbox()
    assert sb.get_endpoint(8080) == "http://10.0.0.1:8080"


def test_get_endpoint_missing():
    sb, _ = make_sandbox()
    with pytest.raises(EndpointNotFoundError) as exc_info:
        sb.get_endpoint(9999)
    assert exc_info.value.port == 9999


def test_get_host_strips_scheme():
    sb, _ = make_sandbox()
    assert sb.get_host(8080) == "10.0.0.1:8080"


def test_get_host_no_scheme():
    sb, _ = make_sandbox()
    # Port 22 has no scheme in the fixture
    assert sb.get_host(22) == "10.0.0.1:22"


# ---------------------------------------------------------------------------
# EndpointData model tests — new url+logDir format
# ---------------------------------------------------------------------------


def test_endpoint_data_url_only():
    """Parses endpoint with only url field."""
    data = SandboxData.model_validate(
        {
            "sandboxId": "sb-x",
            "namespace": "default",
            "poolName": "p",
            "podName": "pod",
            "status": "Running",
            "claimedAt": "2026-01-01T00:00:00Z",
            "endpoints": {
                "envd": {"url": "http://gw/sandboxes/sb-x/49983"},
            },
        }
    )
    ep = data.endpoints["envd"]
    assert isinstance(ep, EndpointData)
    assert ep.url == "http://gw/sandboxes/sb-x/49983"
    assert ep.log_dir is None


def test_endpoint_data_with_log_dir():
    """Parses endpoint with url and logDir."""
    data = SandboxData.model_validate(
        {
            "sandboxId": "sb-y",
            "namespace": "default",
            "poolName": "p",
            "podName": "pod",
            "status": "Running",
            "claimedAt": "2026-01-01T00:00:00Z",
            "endpoints": {
                "envd": {
                    "url": "http://gw/sandboxes/sb-y/49983",
                    "logDir": "/tmp/envd.log",
                },
            },
        }
    )
    ep = data.endpoints["envd"]
    assert isinstance(ep, EndpointData)
    assert ep.url == "http://gw/sandboxes/sb-y/49983"
    assert ep.log_dir == "/tmp/envd.log"


def test_endpoint_data_log_dir_alias():
    """log_dir field accepts camelCase alias 'logDir' from JSON."""
    ep = EndpointData.model_validate(
        {"url": "http://x", "logDir": "/var/log/x"}
    )
    assert ep.log_dir == "/var/log/x"


def test_sandbox_endpoints_property():
    """Sandbox.endpoints returns Dict[str, EndpointData]."""
    sb, _ = make_sandbox()
    eps = sb.endpoints
    assert isinstance(eps, dict)
    assert "8080" in eps
    ep = eps["8080"]
    assert isinstance(ep, EndpointData)
    assert ep.url == "http://10.0.0.1:8080"
    assert ep.log_dir is None


def test_sandbox_endpoints_with_log_dir():
    """Sandbox.endpoints exposes log_dir when present."""
    sb, _ = make_sandbox(
        {
            "endpoints": {
                "envd": {
                    "url": "http://gw/sb/49983",
                    "logDir": "/tmp/envd.log",
                },
            }
        }
    )
    ep = sb.endpoints["envd"]
    assert ep.url == "http://gw/sb/49983"
    assert ep.log_dir == "/tmp/envd.log"


def test_get_endpoint_returns_url():
    """get_endpoint(port) returns the url string."""
    sb, _ = make_sandbox(
        {
            "endpoints": {
                "8080": {"url": "http://10.0.0.1:8080"},
            }
        }
    )
    assert sb.get_endpoint(8080) == "http://10.0.0.1:8080"


# ---------------------------------------------------------------------------
# Sandbox.is_ready
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_is_ready_returns_true_on_200():
    """is_ready() returns True when server responds with 200 ready=true."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123/is_ready").mock(
            return_value=httpx.Response(
                200,
                json={
                    "sandboxId": "sb-abc123",
                    "ready": True,
                    "endpoints": {},
                },
            )
        )
        api = make_api()
        sb_data = SandboxData.model_validate(SANDBOX_FIXTURE)
        sb = Sandbox(sb_data, api)
        result = await sb.is_ready()

    assert result is True


@pytest.mark.asyncio
async def test_is_ready_returns_false_on_503():
    """is_ready() returns False when server responds with 503 (probe failed)."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123/is_ready").mock(
            return_value=httpx.Response(
                503,
                json={
                    "sandboxId": "sb-abc123",
                    "ready": False,
                    "endpoints": {
                        "envd": {
                            "ready": False,
                            "message": "connection refused",
                        }
                    },
                },
            )
        )
        api = make_api()
        sb_data = SandboxData.model_validate(SANDBOX_FIXTURE)
        sb = Sandbox(sb_data, api)
        result = await sb.is_ready()

    assert result is False


@pytest.mark.asyncio
async def test_is_ready_returns_false_on_502():
    """is_ready() returns False when server responds with 502 (gateway not ready)."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123/is_ready").mock(
            return_value=httpx.Response(502)
        )
        api = make_api()
        sb_data = SandboxData.model_validate(SANDBOX_FIXTURE)
        sb = Sandbox(sb_data, api)
        result = await sb.is_ready()

    assert result is False


@pytest.mark.asyncio
async def test_is_ready_raises_on_404():
    """is_ready() raises an error when sandbox is not found (404)."""
    from agentbox_sdk.exceptions import SandboxNotFoundError

    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123/is_ready").mock(
            return_value=httpx.Response(404, json={"error": "not found"})
        )
        api = make_api()
        sb_data = SandboxData.model_validate(SANDBOX_FIXTURE)
        sb = Sandbox(sb_data, api)
        with pytest.raises(Exception):  # SandboxNotFoundError or APIError
            await sb.is_ready()


# ---------------------------------------------------------------------------
# Sandbox.wait_for_ready
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_wait_for_ready_already_running():
    """If sandbox is already Running, wait_for_ready returns immediately."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(200, json={"sandbox": SANDBOX_FIXTURE})
        )
        api = make_api()
        sb_data = SandboxData.model_validate(SANDBOX_FIXTURE)
        sb = Sandbox(sb_data, api)
        result = await sb.wait_for_ready(timeout=5.0, poll_interval=0.01)

    assert result.status == SandboxStatus.RUNNING


@pytest.mark.asyncio
async def test_wait_for_ready_terminal_raises():
    """If sandbox enters Failed state, SandboxStartupError is raised."""
    failed_fixture = dict(SANDBOX_FIXTURE, status="Failed")
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(200, json={"sandbox": failed_fixture})
        )
        api = make_api()
        starting_fixture = dict(SANDBOX_FIXTURE, status="Starting")
        sb_data = SandboxData.model_validate(starting_fixture)
        sb = Sandbox(sb_data, api)
        with pytest.raises(SandboxStartupError) as exc_info:
            await sb.wait_for_ready(timeout=5.0, poll_interval=0.01)
        assert exc_info.value.status == "Failed"


@pytest.mark.asyncio
async def test_wait_for_ready_timeout():
    """If timeout expires before Running, SandboxTimeoutError is raised."""
    starting_fixture = dict(SANDBOX_FIXTURE, status="Starting")
    with respx.mock(base_url=BASE_URL) as mock:
        mock.get("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(200, json={"sandbox": starting_fixture})
        )
        api = make_api()
        sb_data = SandboxData.model_validate(starting_fixture)
        sb = Sandbox(sb_data, api)
        with pytest.raises(SandboxTimeoutError):
            await sb.wait_for_ready(timeout=0.05, poll_interval=0.01)


# ---------------------------------------------------------------------------
# _SandboxCreateContext
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_create_context_await():
    """Direct await of create() returns a Sandbox."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.post("/sandboxes").mock(
            return_value=httpx.Response(201, json={"sandbox": SANDBOX_FIXTURE})
        )
        api = make_api()
        sb = await api.create("test-pool", wait=False)

    assert sb.id == "sb-abc123"


@pytest.mark.asyncio
async def test_create_context_manager():
    """``async with create()`` calls close() on exit."""
    with respx.mock(base_url=BASE_URL) as mock:
        mock.post("/sandboxes").mock(
            return_value=httpx.Response(201, json={"sandbox": SANDBOX_FIXTURE})
        )
        delete_route = mock.delete("/sandboxes/sb-abc123").mock(
            return_value=httpx.Response(
                202,
                json={
                    "sandboxId": "sb-abc123",
                    "namespace": "default",
                    "poolName": "test-pool",
                    "podName": "test-pool-pod-0",
                    "status": "Stopping",
                },
            )
        )
        api = make_api()
        async with api.create("test-pool", wait=False) as sb:
            assert sb.id == "sb-abc123"
        # delete should have been called
        assert delete_route.called
