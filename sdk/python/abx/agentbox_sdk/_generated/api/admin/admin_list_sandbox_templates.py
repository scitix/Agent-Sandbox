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
from ...models.list_sandbox_templates_result import ListSandboxTemplatesResult
from ...types import UNSET, Unset
from typing import cast



def _get_kwargs(
    *,
    team: str | Unset = UNSET,
    user: str | Unset = UNSET,

) -> dict[str, Any]:
    

    

    params: dict[str, Any] = {}

    params["team"] = team

    params["user"] = user


    params = {k: v for k, v in params.items() if v is not UNSET and v is not None}


    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/admin/sandbox-templates",
        "params": params,
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | ListSandboxTemplatesResult | None:
    if response.status_code == 200:
        response_200 = ListSandboxTemplatesResult.from_dict(response.json())



        return response_200

    if response.status_code == 401:
        response_401 = ErrorResponse.from_dict(response.json())



        return response_401

    if response.status_code == 403:
        response_403 = ErrorResponse.from_dict(response.json())



        return response_403

    if response.status_code == 500:
        response_500 = ErrorResponse.from_dict(response.json())



        return response_500

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | ListSandboxTemplatesResult]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient,
    team: str | Unset = UNSET,
    user: str | Unset = UNSET,

) -> Response[ErrorResponse | ListSandboxTemplatesResult]:
    """ List sandbox templates (admin)

     Returns all SandboxTemplate resources. Admin callers see every template
    regardless of origin; optional `team` / `user` query parameters narrow
    results to templates accessible by a specific identity.

    Args:
        team (str | Unset):
        user (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListSandboxTemplatesResult]
     """


    kwargs = _get_kwargs(
        team=team,
user=user,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    *,
    client: AuthenticatedClient,
    team: str | Unset = UNSET,
    user: str | Unset = UNSET,

) -> ErrorResponse | ListSandboxTemplatesResult | None:
    """ List sandbox templates (admin)

     Returns all SandboxTemplate resources. Admin callers see every template
    regardless of origin; optional `team` / `user` query parameters narrow
    results to templates accessible by a specific identity.

    Args:
        team (str | Unset):
        user (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListSandboxTemplatesResult
     """


    return sync_detailed(
        client=client,
team=team,
user=user,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient,
    team: str | Unset = UNSET,
    user: str | Unset = UNSET,

) -> Response[ErrorResponse | ListSandboxTemplatesResult]:
    """ List sandbox templates (admin)

     Returns all SandboxTemplate resources. Admin callers see every template
    regardless of origin; optional `team` / `user` query parameters narrow
    results to templates accessible by a specific identity.

    Args:
        team (str | Unset):
        user (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListSandboxTemplatesResult]
     """


    kwargs = _get_kwargs(
        team=team,
user=user,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    *,
    client: AuthenticatedClient,
    team: str | Unset = UNSET,
    user: str | Unset = UNSET,

) -> ErrorResponse | ListSandboxTemplatesResult | None:
    """ List sandbox templates (admin)

     Returns all SandboxTemplate resources. Admin callers see every template
    regardless of origin; optional `team` / `user` query parameters narrow
    results to templates accessible by a specific identity.

    Args:
        team (str | Unset):
        user (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListSandboxTemplatesResult
     """


    return (await asyncio_detailed(
        client=client,
team=team,
user=user,

    )).parsed
