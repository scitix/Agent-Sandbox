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

from ...models.delete_sandbox_result import DeleteSandboxResult
from ...models.error_response import ErrorResponse
from typing import cast



def _get_kwargs(
    sandbox_id: str,

) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "delete",
        "url": "/sandboxes/{sandbox_id}".format(sandbox_id=quote(str(sandbox_id), safe=""),),
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> DeleteSandboxResult | ErrorResponse | None:
    if response.status_code == 202:
        response_202 = DeleteSandboxResult.from_dict(response.json())



        return response_202

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


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[DeleteSandboxResult | ErrorResponse]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,

) -> Response[DeleteSandboxResult | ErrorResponse]:
    """ Delete a sandbox

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[DeleteSandboxResult | ErrorResponse]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,

) -> DeleteSandboxResult | ErrorResponse | None:
    """ Delete a sandbox

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        DeleteSandboxResult | ErrorResponse
     """


    return sync_detailed(
        sandbox_id=sandbox_id,
client=client,

    ).parsed

async def asyncio_detailed(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,

) -> Response[DeleteSandboxResult | ErrorResponse]:
    """ Delete a sandbox

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[DeleteSandboxResult | ErrorResponse]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,

) -> DeleteSandboxResult | ErrorResponse | None:
    """ Delete a sandbox

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        DeleteSandboxResult | ErrorResponse
     """


    return (await asyncio_detailed(
        sandbox_id=sandbox_id,
client=client,

    )).parsed
