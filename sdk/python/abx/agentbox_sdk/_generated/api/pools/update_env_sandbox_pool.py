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

from http import HTTPStatus
from typing import Any, cast
from urllib.parse import quote

import httpx

from ...client import AuthenticatedClient, Client
from ...types import Response, UNSET
from ... import errors

from ...models.error_response import ErrorResponse
from ...models.sandbox_pool_envelope import SandboxPoolEnvelope
from ...models.upsert_sandbox_pool_request import UpsertSandboxPoolRequest
from typing import cast



def _get_kwargs(
    name: str,
    pool_name: str,
    *,
    body: UpsertSandboxPoolRequest,

) -> dict[str, Any]:
    headers: dict[str, Any] = {}


    

    

    _kwargs: dict[str, Any] = {
        "method": "put",
        "url": "/envs/{name}/sandboxpools/{pool_name}".format(name=quote(str(name), safe=""),pool_name=quote(str(pool_name), safe=""),),
    }

    _kwargs["json"] = body.to_dict()

    headers["Content-Type"] = "application/json"

    _kwargs["headers"] = headers
    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | SandboxPoolEnvelope | None:
    if response.status_code == 200:
        response_200 = SandboxPoolEnvelope.from_dict(response.json())



        return response_200

    if response.status_code == 400:
        response_400 = ErrorResponse.from_dict(response.json())



        return response_400

    if response.status_code == 401:
        response_401 = ErrorResponse.from_dict(response.json())



        return response_401

    if response.status_code == 404:
        response_404 = ErrorResponse.from_dict(response.json())



        return response_404

    if response.status_code == 500:
        response_500 = ErrorResponse.from_dict(response.json())



        return response_500

    if response.status_code == 503:
        response_503 = ErrorResponse.from_dict(response.json())



        return response_503

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | SandboxPoolEnvelope]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    name: str,
    pool_name: str,
    *,
    client: AuthenticatedClient | Client,
    body: UpsertSandboxPoolRequest,

) -> Response[ErrorResponse | SandboxPoolEnvelope]:
    """ Update a member SandboxPool

    Args:
        name (str):
        pool_name (str):
        body (UpsertSandboxPoolRequest): Add a member SandboxPool to an Env. The server derives:
              - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
              - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

            where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
            `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
            Members in the same `scalingGroup` share an autoscaling policy.

            WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the
            caller's — read `poolSizing` off the Env this Pool joins (Template
            detail and Env detail both carry it; `abx envs <name>` prints it).

            Billed Env (`poolSizing: billed`) — the Template is billed, so the
            Pool must name what it spends:
              - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
                `instanceType × multiplier` envelope (default `multiplier` = 1).
              - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
                multiplier` is the reservation/billing envelope, while `inlineResources` is the
                actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
                must be ≤ the envelope (round down allowed, round up rejected with 400); the
                reservation still charges quota for the whole instance.
              - `labels` must carry `quota.scitix.ai/url`.

            Free-form Env (`poolSizing: free-form`) — the Template is one the
            deployment does not bill, so the Pool is sized directly:
              - `inlineResources` alone → explicit per-Pool resource requests/limits.
              - `instanceType`, `multiplier` and the quota label are REJECTED (400):
                an instance type buys an instance nobody reserved, and a quota label
                on a Pool that is never submitted for reservation is a claim the
                server cannot honour.

            Unmanaged Env (`poolSizing: either`) — this deployment states no rule,
            so both shapes are accepted and the caller picks. Every deployment
            behaved this way before the rule existed.

            Under the two managed values there is no per-Pool choice: the shape
            follows from the Env, so two Pools of one Env are always sized the same
            way.
            `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
            `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
            real size and Pools downsized differently land in distinct scaling groups.

            This is also what an update takes, and what `GET` returns as `editable`:
            one body for create, update and export, so a client edits what the API
            handed it rather than translating between two subsets that drift.

            The fields marked `x-immutable` describe the Pool's SHAPE and are fixed
            at create. An update must carry them back unchanged — omitting one or
            changing one is a 400 that names the value in force, because a body that
            loses the instance type is a caller bug, not a request for a smaller
            machine.
             Example: {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0,
            'maxReplicas': 4, 'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'},
            'limits': {'cpu': '100m', 'memory': '500Mi'}}, 'labels': {'quota.scitix.ai/url':
            'https://quota.example/q/1'}}.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxPoolEnvelope]
     """


    kwargs = _get_kwargs(
        name=name,
pool_name=pool_name,
body=body,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    name: str,
    pool_name: str,
    *,
    client: AuthenticatedClient | Client,
    body: UpsertSandboxPoolRequest,

) -> ErrorResponse | SandboxPoolEnvelope | None:
    """ Update a member SandboxPool

    Args:
        name (str):
        pool_name (str):
        body (UpsertSandboxPoolRequest): Add a member SandboxPool to an Env. The server derives:
              - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
              - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

            where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
            `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
            Members in the same `scalingGroup` share an autoscaling policy.

            WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the
            caller's — read `poolSizing` off the Env this Pool joins (Template
            detail and Env detail both carry it; `abx envs <name>` prints it).

            Billed Env (`poolSizing: billed`) — the Template is billed, so the
            Pool must name what it spends:
              - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
                `instanceType × multiplier` envelope (default `multiplier` = 1).
              - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
                multiplier` is the reservation/billing envelope, while `inlineResources` is the
                actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
                must be ≤ the envelope (round down allowed, round up rejected with 400); the
                reservation still charges quota for the whole instance.
              - `labels` must carry `quota.scitix.ai/url`.

            Free-form Env (`poolSizing: free-form`) — the Template is one the
            deployment does not bill, so the Pool is sized directly:
              - `inlineResources` alone → explicit per-Pool resource requests/limits.
              - `instanceType`, `multiplier` and the quota label are REJECTED (400):
                an instance type buys an instance nobody reserved, and a quota label
                on a Pool that is never submitted for reservation is a claim the
                server cannot honour.

            Unmanaged Env (`poolSizing: either`) — this deployment states no rule,
            so both shapes are accepted and the caller picks. Every deployment
            behaved this way before the rule existed.

            Under the two managed values there is no per-Pool choice: the shape
            follows from the Env, so two Pools of one Env are always sized the same
            way.
            `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
            `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
            real size and Pools downsized differently land in distinct scaling groups.

            This is also what an update takes, and what `GET` returns as `editable`:
            one body for create, update and export, so a client edits what the API
            handed it rather than translating between two subsets that drift.

            The fields marked `x-immutable` describe the Pool's SHAPE and are fixed
            at create. An update must carry them back unchanged — omitting one or
            changing one is a 400 that names the value in force, because a body that
            loses the instance type is a caller bug, not a request for a smaller
            machine.
             Example: {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0,
            'maxReplicas': 4, 'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'},
            'limits': {'cpu': '100m', 'memory': '500Mi'}}, 'labels': {'quota.scitix.ai/url':
            'https://quota.example/q/1'}}.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxPoolEnvelope
     """


    return sync_detailed(
        name=name,
pool_name=pool_name,
client=client,
body=body,

    ).parsed

async def asyncio_detailed(
    name: str,
    pool_name: str,
    *,
    client: AuthenticatedClient | Client,
    body: UpsertSandboxPoolRequest,

) -> Response[ErrorResponse | SandboxPoolEnvelope]:
    """ Update a member SandboxPool

    Args:
        name (str):
        pool_name (str):
        body (UpsertSandboxPoolRequest): Add a member SandboxPool to an Env. The server derives:
              - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
              - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

            where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
            `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
            Members in the same `scalingGroup` share an autoscaling policy.

            WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the
            caller's — read `poolSizing` off the Env this Pool joins (Template
            detail and Env detail both carry it; `abx envs <name>` prints it).

            Billed Env (`poolSizing: billed`) — the Template is billed, so the
            Pool must name what it spends:
              - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
                `instanceType × multiplier` envelope (default `multiplier` = 1).
              - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
                multiplier` is the reservation/billing envelope, while `inlineResources` is the
                actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
                must be ≤ the envelope (round down allowed, round up rejected with 400); the
                reservation still charges quota for the whole instance.
              - `labels` must carry `quota.scitix.ai/url`.

            Free-form Env (`poolSizing: free-form`) — the Template is one the
            deployment does not bill, so the Pool is sized directly:
              - `inlineResources` alone → explicit per-Pool resource requests/limits.
              - `instanceType`, `multiplier` and the quota label are REJECTED (400):
                an instance type buys an instance nobody reserved, and a quota label
                on a Pool that is never submitted for reservation is a claim the
                server cannot honour.

            Unmanaged Env (`poolSizing: either`) — this deployment states no rule,
            so both shapes are accepted and the caller picks. Every deployment
            behaved this way before the rule existed.

            Under the two managed values there is no per-Pool choice: the shape
            follows from the Env, so two Pools of one Env are always sized the same
            way.
            `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
            `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
            real size and Pools downsized differently land in distinct scaling groups.

            This is also what an update takes, and what `GET` returns as `editable`:
            one body for create, update and export, so a client edits what the API
            handed it rather than translating between two subsets that drift.

            The fields marked `x-immutable` describe the Pool's SHAPE and are fixed
            at create. An update must carry them back unchanged — omitting one or
            changing one is a 400 that names the value in force, because a body that
            loses the instance type is a caller bug, not a request for a smaller
            machine.
             Example: {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0,
            'maxReplicas': 4, 'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'},
            'limits': {'cpu': '100m', 'memory': '500Mi'}}, 'labels': {'quota.scitix.ai/url':
            'https://quota.example/q/1'}}.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxPoolEnvelope]
     """


    kwargs = _get_kwargs(
        name=name,
pool_name=pool_name,
body=body,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    name: str,
    pool_name: str,
    *,
    client: AuthenticatedClient | Client,
    body: UpsertSandboxPoolRequest,

) -> ErrorResponse | SandboxPoolEnvelope | None:
    """ Update a member SandboxPool

    Args:
        name (str):
        pool_name (str):
        body (UpsertSandboxPoolRequest): Add a member SandboxPool to an Env. The server derives:
              - `name`         = "{envName}-{resourceKey}[-{quotaShort}]"
              - `scalingGroup` = `resourceKey` (e.g. "2c8Gi")

            where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and
            `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`.
            Members in the same `scalingGroup` share an autoscaling policy.

            WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the
            caller's — read `poolSizing` off the Env this Pool joins (Template
            detail and Env detail both carry it; `abx envs <name>` prints it).

            Billed Env (`poolSizing: billed`) — the Template is billed, so the
            Pool must name what it spends:
              - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full
                `instanceType × multiplier` envelope (default `multiplier` = 1).
              - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType ×
                multiplier` is the reservation/billing envelope, while `inlineResources` is the
                actual (possibly rounded-down) Pod request. Every dimension of `inlineResources`
                must be ≤ the envelope (round down allowed, round up rejected with 400); the
                reservation still charges quota for the whole instance.
              - `labels` must carry `quota.scitix.ai/url`.

            Free-form Env (`poolSizing: free-form`) — the Template is one the
            deployment does not bill, so the Pool is sized directly:
              - `inlineResources` alone → explicit per-Pool resource requests/limits.
              - `instanceType`, `multiplier` and the quota label are REJECTED (400):
                an instance type buys an instance nobody reserved, and a quota label
                on a Pool that is never submitted for reservation is a claim the
                server cannot honour.

            Unmanaged Env (`poolSizing: either`) — this deployment states no rule,
            so both shapes are accepted and the caller picks. Every deployment
            behaved this way before the rule existed.

            Under the two managed values there is no per-Pool choice: the shape
            follows from the Env, so two Pools of one Env are always sized the same
            way.
            `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down
            `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's
            real size and Pools downsized differently land in distinct scaling groups.

            This is also what an update takes, and what `GET` returns as `editable`:
            one body for create, update and export, so a client edits what the API
            handed it rather than translating between two subsets that drift.

            The fields marked `x-immutable` describe the Pool's SHAPE and are fixed
            at create. An update must carry them back unchanged — omitting one or
            changing one is a 400 that names the value in force, because a body that
            loses the instance type is a caller bug, not a request for a smaller
            machine.
             Example: {'instanceType': 'sci.c23-2', 'multiplier': 1, 'replicas': 1, 'minReplicas': 0,
            'maxReplicas': 4, 'inlineResources': {'requests': {'cpu': '100m', 'memory': '500Mi'},
            'limits': {'cpu': '100m', 'memory': '500Mi'}}, 'labels': {'quota.scitix.ai/url':
            'https://quota.example/q/1'}}.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxPoolEnvelope
     """


    return (await asyncio_detailed(
        name=name,
pool_name=pool_name,
client=client,
body=body,

    )).parsed
