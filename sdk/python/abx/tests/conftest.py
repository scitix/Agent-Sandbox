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
Pytest configuration and shared fixtures for agentbox_sdk tests.
"""

from __future__ import annotations

import pytest
import respx
import httpx


@pytest.fixture
def base_url() -> str:
    return "http://agentbox.test/v1"


@pytest.fixture
def mock_http(base_url: str):
    """Return a mocked httpx.AsyncClient bound to base_url."""
    with respx.mock(base_url=base_url, assert_all_called=False) as mock:
        yield mock


@pytest.fixture
def http_client(base_url: str, mock_http):
    """Return an httpx.AsyncClient wired to the respx mock."""
    client = httpx.AsyncClient(
        base_url=base_url,
        headers={"AGENTBOX-API-KEY": "agbx_test"},
    )
    yield client
    # Sync teardown is fine here; the client is never actually opened
