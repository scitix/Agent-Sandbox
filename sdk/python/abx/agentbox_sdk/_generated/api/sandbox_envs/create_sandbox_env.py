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

from ...models.create_sandbox_env_request import CreateSandboxEnvRequest
from ...models.error_response import ErrorResponse
from ...models.sandbox_env_envelope import SandboxEnvEnvelope
from typing import cast



def _get_kwargs(
    *,
    body: CreateSandboxEnvRequest,

) -> dict[str, Any]:
    headers: dict[str, Any] = {}


    

    

    _kwargs: dict[str, Any] = {
        "method": "post",
        "url": "/envs",
    }

    _kwargs["json"] = body.to_dict()


    headers["Content-Type"] = "application/json"

    _kwargs["headers"] = headers
    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | SandboxEnvEnvelope | None:
    if response.status_code == 201:
        response_201 = SandboxEnvEnvelope.from_dict(response.json())



        return response_201

    if response.status_code == 400:
        response_400 = ErrorResponse.from_dict(response.json())



        return response_400

    if response.status_code == 401:
        response_401 = ErrorResponse.from_dict(response.json())



        return response_401

    if response.status_code == 409:
        response_409 = ErrorResponse.from_dict(response.json())



        return response_409

    if response.status_code == 500:
        response_500 = ErrorResponse.from_dict(response.json())



        return response_500

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | SandboxEnvEnvelope]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient | Client,
    body: CreateSandboxEnvRequest,

) -> Response[ErrorResponse | SandboxEnvEnvelope]:
    """ Create a new SandboxEnv. The Env Reconciler materialises one member SandboxPool per entry in
    `members` (or a single quota-less pool named after the Env when `members` is empty).

    Args:
        body (CreateSandboxEnvRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxEnvEnvelope]
     """


    kwargs = _get_kwargs(
        body=body,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    *,
    client: AuthenticatedClient | Client,
    body: CreateSandboxEnvRequest,

) -> ErrorResponse | SandboxEnvEnvelope | None:
    """ Create a new SandboxEnv. The Env Reconciler materialises one member SandboxPool per entry in
    `members` (or a single quota-less pool named after the Env when `members` is empty).

    Args:
        body (CreateSandboxEnvRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxEnvEnvelope
     """


    return sync_detailed(
        client=client,
body=body,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,
    body: CreateSandboxEnvRequest,

) -> Response[ErrorResponse | SandboxEnvEnvelope]:
    """ Create a new SandboxEnv. The Env Reconciler materialises one member SandboxPool per entry in
    `members` (or a single quota-less pool named after the Env when `members` is empty).

    Args:
        body (CreateSandboxEnvRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxEnvEnvelope]
     """


    kwargs = _get_kwargs(
        body=body,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    *,
    client: AuthenticatedClient | Client,
    body: CreateSandboxEnvRequest,

) -> ErrorResponse | SandboxEnvEnvelope | None:
    """ Create a new SandboxEnv. The Env Reconciler materialises one member SandboxPool per entry in
    `members` (or a single quota-less pool named after the Env when `members` is empty).

    Args:
        body (CreateSandboxEnvRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxEnvEnvelope
     """


    return (await asyncio_detailed(
        client=client,
body=body,

    )).parsed
