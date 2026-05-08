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
from ...models.list_clusters_result import ListClustersResult
from typing import cast



def _get_kwargs(
    
) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/clusters",
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | ListClustersResult | None:
    if response.status_code == 200:
        response_200 = ListClustersResult.from_dict(response.json())



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


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | ListClustersResult]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    *,
    client: AuthenticatedClient,

) -> Response[ErrorResponse | ListClustersResult]:
    """ List clusters visible to the gateway

     Returns the cluster catalog visible to the gateway serving this request.

    The entry with `local: true` is the cluster that owns native-local
    resources (a sandbox created here without any `{clusterID}.` prefix lives
    in that cluster). Other entries are reachable via cross-cluster
    forwarding: prefix sandbox IDs with `{clusterID}.` (dot) and pool names
    with `{clusterID}::` (double colon) to route to them.

    This endpoint requires only a regular API key — it does not leak
    gateway URLs, headers, or any write-capability.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListClustersResult]
     """


    kwargs = _get_kwargs(
        
    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    *,
    client: AuthenticatedClient,

) -> ErrorResponse | ListClustersResult | None:
    """ List clusters visible to the gateway

     Returns the cluster catalog visible to the gateway serving this request.

    The entry with `local: true` is the cluster that owns native-local
    resources (a sandbox created here without any `{clusterID}.` prefix lives
    in that cluster). Other entries are reachable via cross-cluster
    forwarding: prefix sandbox IDs with `{clusterID}.` (dot) and pool names
    with `{clusterID}::` (double colon) to route to them.

    This endpoint requires only a regular API key — it does not leak
    gateway URLs, headers, or any write-capability.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListClustersResult
     """


    return sync_detailed(
        client=client,

    ).parsed

async def asyncio_detailed(
    *,
    client: AuthenticatedClient,

) -> Response[ErrorResponse | ListClustersResult]:
    """ List clusters visible to the gateway

     Returns the cluster catalog visible to the gateway serving this request.

    The entry with `local: true` is the cluster that owns native-local
    resources (a sandbox created here without any `{clusterID}.` prefix lives
    in that cluster). Other entries are reachable via cross-cluster
    forwarding: prefix sandbox IDs with `{clusterID}.` (dot) and pool names
    with `{clusterID}::` (double colon) to route to them.

    This endpoint requires only a regular API key — it does not leak
    gateway URLs, headers, or any write-capability.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListClustersResult]
     """


    kwargs = _get_kwargs(
        
    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    *,
    client: AuthenticatedClient,

) -> ErrorResponse | ListClustersResult | None:
    """ List clusters visible to the gateway

     Returns the cluster catalog visible to the gateway serving this request.

    The entry with `local: true` is the cluster that owns native-local
    resources (a sandbox created here without any `{clusterID}.` prefix lives
    in that cluster). Other entries are reachable via cross-cluster
    forwarding: prefix sandbox IDs with `{clusterID}.` (dot) and pool names
    with `{clusterID}::` (double colon) to route to them.

    This endpoint requires only a regular API key — it does not leak
    gateway URLs, headers, or any write-capability.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListClustersResult
     """


    return (await asyncio_detailed(
        client=client,

    )).parsed
