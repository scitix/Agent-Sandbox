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
Per-invocation context: where to talk, as whom, and how to render.

Built once in `main`, threaded through dispatch into every kind's fetcher.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from typing import Any

import httpx

_DEFAULT_TIMEOUT = 30.0


class ApiError(RuntimeError):
    """An error the server reported, carrying its own wording."""

    def __init__(self, message: str, status: int = 0) -> None:
        super().__init__(message)
        self.status = status


@dataclass
class Context:
    endpoint: str
    api_key: str
    cluster: str | None = None
    impersonate_team: str | None = None
    impersonate_user: str | None = None
    ui_mode: str = "url"
    web_base: str | None = None
    # Explicit Host override, for an endpoint addressed by IP behind a
    # host-routed ingress. The platform's own cluster registry already works
    # this way (`url` is an address, `headers.Host` is the routing name), so a
    # caller reaching a cluster the same way needs the same two pieces.
    host_header: str | None = None
    timeout: float = _DEFAULT_TIMEOUT
    _client: Any = field(default=None, repr=False)

    @property
    def base_url(self) -> str:
        return self.endpoint.rstrip("/") + "/v1"

    def headers(self) -> dict[str, str]:
        import agentbox_sdk

        h = {
            "AGENTBOX-API-KEY": self.api_key,
            "X-AgentBox-Client-Version": agentbox_sdk.__version__,
            "Accept": "application/json",
        }
        # Admin keys carry no tenant, and several reads (quotas above all)
        # refuse to guess one. Passing the pair through is what lets one admin
        # key answer for a named team without minting a key per tenant.
        if self.impersonate_team:
            h["X-Impersonate-Team"] = self.impersonate_team
        if self.impersonate_user:
            h["X-Impersonate-User"] = self.impersonate_user
        if self.host_header:
            h["Host"] = self.host_header
        return h

    def client(self) -> httpx.Client:
        if self._client is None:
            self._client = httpx.Client(
                base_url=self.base_url,
                headers=self.headers(),
                timeout=self.timeout,
                follow_redirects=False,
            )
        return self._client

    def get_json(self, path: str, params: dict[str, Any] | None = None) -> Any:
        """
        GET one path and return parsed JSON.

        Raises ApiError carrying the server's own message. Relaying it verbatim
        matters more than classifying it: the prompt tells the agent to report a
        failure as stated, and a rewritten message is one it cannot act on.
        """
        try:
            r = self.client().get(path, params=params or {})
        except httpx.HTTPError as e:
            raise ApiError(f"cannot reach {self.base_url}{path}: {e}") from e
        return _parse(r, path)

    def post_json(self, path: str, body: Any) -> Any:
        try:
            r = self.client().post(path, json=body)
        except httpx.HTTPError as e:
            raise ApiError(f"cannot reach {self.base_url}{path}: {e}") from e
        return _parse(r, path)


def _parse(r: httpx.Response, path: str) -> Any:
    text = r.text
    if r.status_code >= 400:
        msg = ""
        try:
            payload = json.loads(text)
            if isinstance(payload, dict):
                msg = str(payload.get("error") or payload.get("message") or "")
        except Exception:
            pass
        raise ApiError(
            msg or f"HTTP {r.status_code} from {path}: {text[:200]}",
            r.status_code,
        )
    if not text.strip():
        return {}
    try:
        return json.loads(text)
    except Exception as e:
        raise ApiError(
            f"unexpected non-JSON response from {path}: {text[:200]}"
        ) from e


def items_of(payload: Any, *keys: str) -> list[dict[str, Any]]:
    """
    Pull the row list out of a list response.

    The native API is not uniform — most list endpoints answer `items`, but
    `/clusters` answers `clusters`. Naming the alternatives per call site
    keeps that asymmetry visible instead of hiding it behind a guess.
    """
    if isinstance(payload, list):
        return payload
    if not isinstance(payload, dict):
        return []
    for k in (*keys, "items"):
        v = payload.get(k)
        if isinstance(v, list):
            return v
    return []


def env_default(*names: str, fallback: str = "") -> str:
    for n in names:
        v = os.environ.get(n, "").strip()
        if v:
            return v
    return fallback
