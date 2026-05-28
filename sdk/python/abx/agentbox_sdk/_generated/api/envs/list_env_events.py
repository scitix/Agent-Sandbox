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
from ...models.list_env_events_result import ListEnvEventsResult
from ...types import UNSET, Unset
from typing import cast



def _get_kwargs(
    name: str,
    *,
    limit: int | Unset = 100,

) -> dict[str, Any]:
    

    

    params: dict[str, Any] = {}

    params["limit"] = limit


    params = {k: v for k, v in params.items() if v is not UNSET and v is not None}


    _kwargs: dict[str, Any] = {
        "method": "get",
        "url": "/envs/{name}/events".format(name=quote(str(name), safe=""),),
        "params": params,
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | ListEnvEventsResult | None:
    if response.status_code == 200:
        response_200 = ListEnvEventsResult.from_dict(response.json())



        return response_200

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


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | ListEnvEventsResult]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    name: str,
    *,
    client: AuthenticatedClient | Client,
    limit: int | Unset = 100,

) -> Response[ErrorResponse | ListEnvEventsResult]:
    """ List recent K8s Events for a SandboxEnv and its member SandboxPools, newest first.

     Returns the K8s Events emitted against the named SandboxEnv and every
    member SandboxPool the Env owns in the caller's namespace, merged and
    sorted descending by lastTimestamp. Used by the dashboard to render an
    activity timeline (scaling decisions, phase transitions, autoscaler
    actions). Reads come straight from the apiserver — no Prometheus
    round-trip — so the latest event is visible as soon as the controllers
    emit it.

    Args:
        name (str):
        limit (int | Unset):  Default: 100.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListEnvEventsResult]
     """


    kwargs = _get_kwargs(
        name=name,
limit=limit,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    name: str,
    *,
    client: AuthenticatedClient | Client,
    limit: int | Unset = 100,

) -> ErrorResponse | ListEnvEventsResult | None:
    """ List recent K8s Events for a SandboxEnv and its member SandboxPools, newest first.

     Returns the K8s Events emitted against the named SandboxEnv and every
    member SandboxPool the Env owns in the caller's namespace, merged and
    sorted descending by lastTimestamp. Used by the dashboard to render an
    activity timeline (scaling decisions, phase transitions, autoscaler
    actions). Reads come straight from the apiserver — no Prometheus
    round-trip — so the latest event is visible as soon as the controllers
    emit it.

    Args:
        name (str):
        limit (int | Unset):  Default: 100.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListEnvEventsResult
     """


    return sync_detailed(
        name=name,
client=client,
limit=limit,

    ).parsed

async def asyncio_detailed(
    name: str,
    *,
    client: AuthenticatedClient | Client,
    limit: int | Unset = 100,

) -> Response[ErrorResponse | ListEnvEventsResult]:
    """ List recent K8s Events for a SandboxEnv and its member SandboxPools, newest first.

     Returns the K8s Events emitted against the named SandboxEnv and every
    member SandboxPool the Env owns in the caller's namespace, merged and
    sorted descending by lastTimestamp. Used by the dashboard to render an
    activity timeline (scaling decisions, phase transitions, autoscaler
    actions). Reads come straight from the apiserver — no Prometheus
    round-trip — so the latest event is visible as soon as the controllers
    emit it.

    Args:
        name (str):
        limit (int | Unset):  Default: 100.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ListEnvEventsResult]
     """


    kwargs = _get_kwargs(
        name=name,
limit=limit,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    name: str,
    *,
    client: AuthenticatedClient | Client,
    limit: int | Unset = 100,

) -> ErrorResponse | ListEnvEventsResult | None:
    """ List recent K8s Events for a SandboxEnv and its member SandboxPools, newest first.

     Returns the K8s Events emitted against the named SandboxEnv and every
    member SandboxPool the Env owns in the caller's namespace, merged and
    sorted descending by lastTimestamp. Used by the dashboard to render an
    activity timeline (scaling decisions, phase transitions, autoscaler
    actions). Reads come straight from the apiserver — no Prometheus
    round-trip — so the latest event is visible as soon as the controllers
    emit it.

    Args:
        name (str):
        limit (int | Unset):  Default: 100.

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ListEnvEventsResult
     """


    return (await asyncio_detailed(
        name=name,
client=client,
limit=limit,

    )).parsed
