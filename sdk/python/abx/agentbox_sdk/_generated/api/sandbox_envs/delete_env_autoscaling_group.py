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

from ...models.delete_env_autoscaling_group_result import DeleteEnvAutoscalingGroupResult
from ...models.error_response import ErrorResponse
from typing import cast



def _get_kwargs(
    name: str,
    group_name: str,

) -> dict[str, Any]:
    

    

    

    _kwargs: dict[str, Any] = {
        "method": "delete",
        "url": "/envs/{name}/autoscaling/groups/{group_name}".format(name=quote(str(name), safe=""),group_name=quote(str(group_name), safe=""),),
    }


    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> DeleteEnvAutoscalingGroupResult | ErrorResponse | None:
    if response.status_code == 200:
        response_200 = DeleteEnvAutoscalingGroupResult.from_dict(response.json())



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


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[DeleteEnvAutoscalingGroupResult | ErrorResponse]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    name: str,
    group_name: str,
    *,
    client: AuthenticatedClient | Client,

) -> Response[DeleteEnvAutoscalingGroupResult | ErrorResponse]:
    """ Remove an autoscaling group from the env.

    Args:
        name (str):
        group_name (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[DeleteEnvAutoscalingGroupResult | ErrorResponse]
     """


    kwargs = _get_kwargs(
        name=name,
group_name=group_name,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    name: str,
    group_name: str,
    *,
    client: AuthenticatedClient | Client,

) -> DeleteEnvAutoscalingGroupResult | ErrorResponse | None:
    """ Remove an autoscaling group from the env.

    Args:
        name (str):
        group_name (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        DeleteEnvAutoscalingGroupResult | ErrorResponse
     """


    return sync_detailed(
        name=name,
group_name=group_name,
client=client,

    ).parsed

async def asyncio_detailed(
    name: str,
    group_name: str,
    *,
    client: AuthenticatedClient | Client,

) -> Response[DeleteEnvAutoscalingGroupResult | ErrorResponse]:
    """ Remove an autoscaling group from the env.

    Args:
        name (str):
        group_name (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[DeleteEnvAutoscalingGroupResult | ErrorResponse]
     """


    kwargs = _get_kwargs(
        name=name,
group_name=group_name,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    name: str,
    group_name: str,
    *,
    client: AuthenticatedClient | Client,

) -> DeleteEnvAutoscalingGroupResult | ErrorResponse | None:
    """ Remove an autoscaling group from the env.

    Args:
        name (str):
        group_name (str):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        DeleteEnvAutoscalingGroupResult | ErrorResponse
     """


    return (await asyncio_detailed(
        name=name,
group_name=group_name,
client=client,

    )).parsed
