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
from ...models.list_volumes_result import ListVolumesResult
from typing import cast



def _get_kwargs(
    
) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/volumes",
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | ListVolumesResult | None:
    if response.status_code == 200:
        response_200 = ListVolumesResult.from_dict(response.json())



        return response_200

    if response.status_code == 401:
        response_401 = ErrorResponse.from_dict(response.json())



        return response_401

    if response.status_code == 500:
        response_500 = ErrorResponse.from_dict(response.json())



        return response_500

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | ListVolumesResult]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[ErrorResponse | ListVolumesResult]:
    """ List mountable PersistentVolumeClaims

     Returns the Bound PersistentVolumeClaims in the caller's namespace, which are exactly the claims a
    SandboxEnv may mount through `overrides.volumes`. There is deliberately no namespace parameter: the
    namespace is derived from the caller's identity, and that derivation is the authorisation boundary.
    Empty when the feature is disabled (see `volumes` on `/feature-gates`).

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListVolumesResult]
     """


    kwargs = _get_kwargs(
        
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    *,
    client: AuthenticatedClient | Client,

) -> ErrorResponse | ListVolumesResult | None:
    """ List mountable PersistentVolumeClaims

     Returns the Bound PersistentVolumeClaims in the caller's namespace, which are exactly the claims a
    SandboxEnv may mount through `overrides.volumes`. There is deliberately no namespace parameter: the
    namespace is derived from the caller's identity, and that derivation is the authorisation boundary.
    Empty when the feature is disabled (see `volumes` on `/feature-gates`).

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListVolumesResult
     """


    return sync_detailed(
        client=client,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient | Client,

) -> Response[ErrorResponse | ListVolumesResult]:
    """ List mountable PersistentVolumeClaims

     Returns the Bound PersistentVolumeClaims in the caller's namespace, which are exactly the claims a
    SandboxEnv may mount through `overrides.volumes`. There is deliberately no namespace parameter: the
    namespace is derived from the caller's identity, and that derivation is the authorisation boundary.
    Empty when the feature is disabled (see `volumes` on `/feature-gates`).

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListVolumesResult]
     """


    kwargs = _get_kwargs(
        
    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    *,
    client: AuthenticatedClient | Client,

) -> ErrorResponse | ListVolumesResult | None:
    """ List mountable PersistentVolumeClaims

     Returns the Bound PersistentVolumeClaims in the caller's namespace, which are exactly the claims a
    SandboxEnv may mount through `overrides.volumes`. There is deliberately no namespace parameter: the
    namespace is derived from the caller's identity, and that derivation is the authorisation boundary.
    Empty when the feature is disabled (see `volumes` on `/feature-gates`).

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListVolumesResult
     """


    return (await asyncio_detailed(
        client=client,

    )).parsed
