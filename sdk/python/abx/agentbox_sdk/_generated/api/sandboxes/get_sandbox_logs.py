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
from ...models.sandbox_logs_result import SandboxLogsResult
from ...types import UNSET, Unset
from typing import cast



def _get_kwargs(
    sandbox_id: str,
    *,
    container: str | Unset = UNSET,
    lines: int | Unset = UNSET,
    source: str | Unset = UNSET,

) -> dict[str, Any]:
    

    

    params: dict[str, Any] = {}

    params["container"] = container

    params["lines"] = lines

    params["source"] = source


    params = {k: v for k, v in params.items() if v is not UNSET and v is not None}


    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/sandboxes/{sandbox_id}/logs".format(sandbox_id=quote(str(sandbox_id), safe=""),),
        "params": params,
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | SandboxLogsResult | None:
    if response.status_code == 200:
        response_200 = SandboxLogsResult.from_dict(response.json())



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

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | SandboxLogsResult]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    sandbox_id: str,
    *,
    client: AuthenticatedClient,
    container: str | Unset = UNSET,
    lines: int | Unset = UNSET,
    source: str | Unset = UNSET,

) -> Response[ErrorResponse | SandboxLogsResult]:
    """ Get sandbox logs

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        container (str | Unset):
        lines (int | Unset):
        source (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxLogsResult]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,
container=container,
lines=lines,
source=source,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    sandbox_id: str,
    *,
    client: AuthenticatedClient,
    container: str | Unset = UNSET,
    lines: int | Unset = UNSET,
    source: str | Unset = UNSET,

) -> ErrorResponse | SandboxLogsResult | None:
    """ Get sandbox logs

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        container (str | Unset):
        lines (int | Unset):
        source (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxLogsResult
     """


    return sync_detailed(
        sandbox_id=sandbox_id,
client=client,
container=container,
lines=lines,
source=source,

    ).parsed

async def asyncio_detailed(
    sandbox_id: str,
    *,
    client: AuthenticatedClient,
    container: str | Unset = UNSET,
    lines: int | Unset = UNSET,
    source: str | Unset = UNSET,

) -> Response[ErrorResponse | SandboxLogsResult]:
    """ Get sandbox logs

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        container (str | Unset):
        lines (int | Unset):
        source (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | SandboxLogsResult]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,
container=container,
lines=lines,
source=source,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    sandbox_id: str,
    *,
    client: AuthenticatedClient,
    container: str | Unset = UNSET,
    lines: int | Unset = UNSET,
    source: str | Unset = UNSET,

) -> ErrorResponse | SandboxLogsResult | None:
    """ Get sandbox logs

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        container (str | Unset):
        lines (int | Unset):
        source (str | Unset):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | SandboxLogsResult
     """


    return (await asyncio_detailed(
        sandbox_id=sandbox_id,
client=client,
container=container,
lines=lines,
source=source,

    )).parsed
