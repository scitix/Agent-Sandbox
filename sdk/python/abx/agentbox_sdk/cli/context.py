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
import pathlib
import secrets
from dataclasses import dataclass, field
from typing import Any

import httpx

_DEFAULT_TIMEOUT = 30.0

# Sentinel: distinguishes "not looked up yet" from "looked up, and the endpoint
# does not report one".
_UNRESOLVED = object()


class ApiError(RuntimeError):
    """An error the server reported, carrying its own wording."""

    def __init__(self, message: str, status: int = 0) -> None:
        super().__init__(message)
        self.status = status


@dataclass
class Approval:
    """The half of a 428 that says what to do about it."""

    id: str
    operation: str
    summary: str
    once_only: bool
    url: str
    poll_url: str
    expires_at: str


class ApprovalRequired(ApiError):
    """The call is well formed and permitted, and is waiting on a person.

    Its own class because the remedy is different from every other error: not
    "fix the request" or "get access", but "go and click this, then run the same
    command again". Callers that treat it as a plain failure would report a
    refusal the user can resolve in ten seconds as something broken.
    """

    def __init__(self, message: str, approval: Approval) -> None:
        super().__init__(message, 428)
        self.approval = approval


@dataclass
class Context:
    endpoint: str
    api_key: str
    cluster: str | None = None
    impersonate_team: str | None = None
    impersonate_user: str | None = None
    # "api-key" sends AGENTBOX-API-KEY and addresses a cluster's own API
    # directly. "bearer" sends Authorization: Bearer and is what the dashboard
    # BFF takes — that surface routes per cluster, so it is the one endpoint
    # that can answer for every cluster at once.
    auth_scheme: str = "api-key"
    ui_mode: str = "url"
    web_base: str | None = None
    # Explicit Host override, for an endpoint addressed by IP behind a
    # host-routed ingress. The platform's own cluster registry already works
    # this way (`url` is an address, `headers.Host` is the routing name), so a
    # caller reaching a cluster the same way needs the same two pieces.
    host_header: str | None = None
    timeout: float = _DEFAULT_TIMEOUT
    _client: Any = field(default=None, repr=False)
    _local_cluster: Any = field(default_factory=lambda: _UNRESOLVED, repr=False)

    @property
    def routes_by_path(self) -> bool:
        """
        Does the endpoint carry the cluster in its PATH?

        The dashboard BFF is addressed as
        `.../api/clusters/{cluster}/v1/...`, so a `{cluster}` placeholder in
        the endpoint means the target genuinely routes — every registered
        cluster is reachable through this one address, and the
        "endpoint serves one cluster" rule does not apply.
        """
        return "{cluster}" in self.endpoint

    @property
    def base_url(self) -> str:
        endpoint = self.endpoint
        if self.routes_by_path:
            if not self.cluster:
                raise ApiError(
                    "this endpoint routes by cluster and needs one: pass "
                    "--cluster, or set ABX_CLUSTER"
                )
            endpoint = endpoint.replace("{cluster}", self.cluster)
        return endpoint.rstrip("/") + "/v1"

    def headers(self) -> dict[str, str]:
        import agentbox_sdk

        h = {
            "X-AgentBox-Client-Version": agentbox_sdk.__version__,
            "Accept": "application/json",
        }
        # Names the run this call belongs to, so an approval can be granted for
        # the rest of it rather than one call at a time. Sent on every request,
        # not just writes: a session the platform has never seen cannot be
        # granted anything, and a read is the cheapest place to introduce it.
        session = session_id()
        if session:
            h["X-AgentBox-Session-Id"] = session
        if self.auth_scheme == "bearer":
            h["Authorization"] = f"Bearer {self.api_key}"
        else:
            h["AGENTBOX-API-KEY"] = self.api_key
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

    def local_cluster_id(self) -> str | None:
        """
        The cluster this endpoint answers for, or None when it does not say.

        Resolved once per invocation. None is deliberately NOT treated as a
        mismatch: an endpoint that reports no local cluster predates the field,
        and refusing every --cluster against it would be worse than trusting
        the caller.
        """
        if self._local_cluster is _UNRESOLVED:
            self._local_cluster = None
            try:
                payload = self.get_json("/clusters")
            except ApiError:
                return None
            entries = (
                payload.get("clusters") if isinstance(payload, dict) else None
            )
            for c in entries or []:
                if isinstance(c, dict) and c.get("local"):
                    self._local_cluster = str(c.get("id") or "") or None
                    break
        return self._local_cluster

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


def _approval_of(payload: Any) -> Approval | None:
    """Read the approval out of an error body, or return None.

    Keyed on the errorCode rather than on the status, because the status is a
    number a proxy or a gateway can also produce; the code is only ever written
    by the gate itself. A body that claims the code but carries no id is treated
    as not-an-approval — there would be nothing to poll or point at.
    """
    if not isinstance(payload, dict):
        return None
    if payload.get("errorCode") != "APPROVAL_REQUIRED":
        return None
    detail = payload.get("detail")
    if not isinstance(detail, dict) or not detail.get("approvalId"):
        return None
    return Approval(
        id=str(detail.get("approvalId") or ""),
        operation=str(detail.get("operation") or ""),
        summary=str(detail.get("summary") or ""),
        once_only=bool(detail.get("onceOnly")),
        url=str(detail.get("url") or ""),
        poll_url=str(detail.get("pollUrl") or ""),
        expires_at=str(detail.get("expiresAt") or ""),
    )


def _parse(r: httpx.Response, path: str) -> Any:
    text = r.text
    if r.status_code >= 400:
        msg = ""
        payload: Any = None
        try:
            payload = json.loads(text)
            if isinstance(payload, dict):
                msg = str(payload.get("error") or payload.get("message") or "")
        except Exception:
            pass
        approval = _approval_of(payload)
        if approval is not None:
            raise ApprovalRequired(msg or "approval required", approval)
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


class ClusterMismatch(ApiError):
    """`--cluster` named a cluster this endpoint does not serve."""


def assert_cluster_served(ctx: Context) -> None:
    """
    Refuse a --cluster that this endpoint cannot answer for.

    The management surface (envs, pools, templates, quotas, instance types) is
    PER CLUSTER: there is no cluster parameter on those routes and no
    forwarding behind them, so an endpoint answers for its own cluster and
    nothing else. Without this check the flag is decoration — the rows come
    from the local cluster and the header, the `view:` target and every hint
    carry the name of a different one. That is worse than a refusal: it is
    confidently mislabelled data, and whoever reads it (a person or an agent)
    reports the wrong cluster's environments as the one they asked for.

    Sandbox operations are the exception and do NOT come through here: the E2B
    surface forwards `cluster::env` to the owning cluster, so creating and
    driving a sandbox elsewhere works from a single endpoint.
    """
    if not ctx.cluster:
        return
    if ctx.routes_by_path:
        # The address itself names the cluster, so the request goes where it
        # says. Nothing to reconcile.
        return
    served = ctx.local_cluster_id()
    if served is None or served == ctx.cluster:
        return
    raise ClusterMismatch(
        f'this endpoint serves cluster "{served}", not "{ctx.cluster}". '
        "Environment, pool, template and quota calls are per cluster — point "
        f"--endpoint at {ctx.cluster}'s own API, or drop --cluster to act on "
        f'"{served}". (Sandboxes are different: the E2B SDK reaches another '
        "cluster with `cluster::env`.)"
    )


def env_default(*names: str, fallback: str = "") -> str:
    for n in names:
        v = os.environ.get(n, "").strip()
        if v:
            return v
    return fallback


# ── session identity ─────────────────────────────────────────────────────────

# Where a generated session id is remembered.
#
# Per user, not per directory: a person driving an agent from two terminals is
# in one working session as far as an approval is concerned, and asking them to
# approve the same operation once per shell would train them to click yes
# without reading.
_SESSION_FILE = "session-id"


def session_id() -> str:
    """The id this invocation belongs to.

    Order matters. An explicitly supplied id wins, because the caller that set
    it knows more than we do — the platform's own sandboxes set it to the
    conversation they were opened for, so an approval granted in a conversation
    covers the rest of that conversation and nothing else.

    Failing that we generate one and remember it. Generating is worth doing
    rather than sending nothing: without an id the platform can only ever grant
    one call at a time, which is correct but means a person clicking approve for
    every step of a long task.
    """
    explicit = os.environ.get("AGENTBOX_SESSION_ID", "").strip()
    if explicit:
        return explicit

    path = _session_path()
    if path is None:
        return ""
    try:
        existing = path.read_text(encoding="utf-8").strip()
        if existing:
            return existing
    except OSError:
        pass

    generated = "cli_" + secrets.token_hex(8)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(generated, encoding="utf-8")
    except OSError:
        # An unwritable home is not a reason to fail a command. Returning the id
        # anyway keeps THIS invocation coherent; the next one gets a new one and
        # simply has to approve again.
        return generated
    return generated


def _session_path() -> pathlib.Path | None:
    base = os.environ.get("XDG_CONFIG_HOME", "").strip()
    try:
        root = pathlib.Path(base) if base else pathlib.Path.home() / ".config"
    except (OSError, RuntimeError):
        # `Path.home()` raises where there is no home at all, which happens in
        # some container images.
        return None
    return root / "abx" / _SESSION_FILE
