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

from ...models.approval_decision_request import ApprovalDecisionRequest
from ...models.approval_request import ApprovalRequest
from ...models.error_response import ErrorResponse
from typing import cast



def _get_kwargs(
    approval_id: str,
    *,
    body: ApprovalDecisionRequest,

) -> dict[str, Any]:
    headers: dict[str, Any] = {}


    

    

    _kwargs: dict[str, Any] = {
        "method": "post",
        "url": "/approvals/{approval_id}/decision".format(approval_id=quote(str(approval_id), safe=""),),
    }

    _kwargs["json"] = body.to_dict()

    headers["Content-Type"] = "application/json"

    _kwargs["headers"] = headers
    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ApprovalRequest | ErrorResponse | None:
    if response.status_code == 200:
        response_200 = ApprovalRequest.from_dict(response.json())



        return response_200

    if response.status_code == 400:
        response_400 = ErrorResponse.from_dict(response.json())



        return response_400

    if response.status_code == 403:
        response_403 = ErrorResponse.from_dict(response.json())



        return response_403

    if response.status_code == 404:
        response_404 = ErrorResponse.from_dict(response.json())



        return response_404

    if client.raise_on_unexpected_status:
        raise errors.UnexpectedStatus(response.status_code, response.content)
    else:
        return None


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ApprovalRequest | ErrorResponse]:
    return Response(
        status_code=HTTPStatus(response.status_code),
        content=response.content,
        headers=response.headers,
        parsed=_parse_response(client=client, response=response),
    )


def sync_detailed(
    approval_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ApprovalDecisionRequest,

) -> Response[ApprovalRequest | ErrorResponse]:
    """ Approve or deny a pending request

     Console sessions only. A credential must not be able to approve its own
    writes — that would make the gate a formality the agent walks through by
    itself.

    Args:
        approval_id (str):
        body (ApprovalDecisionRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ApprovalRequest | ErrorResponse]
     """


    kwargs = _get_kwargs(
        approval_id=approval_id,
body=body,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    approval_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ApprovalDecisionRequest,

) -> ApprovalRequest | ErrorResponse | None:
    """ Approve or deny a pending request

     Console sessions only. A credential must not be able to approve its own
    writes — that would make the gate a formality the agent walks through by
    itself.

    Args:
        approval_id (str):
        body (ApprovalDecisionRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ApprovalRequest | ErrorResponse
     """


    return sync_detailed(
        approval_id=approval_id,
client=client,
body=body,

    ).parsed

async def asyncio_detailed(
    approval_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ApprovalDecisionRequest,

) -> Response[ApprovalRequest | ErrorResponse]:
    """ Approve or deny a pending request

     Console sessions only. A credential must not be able to approve its own
    writes — that would make the gate a formality the agent walks through by
    itself.

    Args:
        approval_id (str):
        body (ApprovalDecisionRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ApprovalRequest | ErrorResponse]
     """


    kwargs = _get_kwargs(
        approval_id=approval_id,
body=body,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    approval_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ApprovalDecisionRequest,

) -> ApprovalRequest | ErrorResponse | None:
    """ Approve or deny a pending request

     Console sessions only. A credential must not be able to approve its own
    writes — that would make the gate a formality the agent walks through by
    itself.

    Args:
        approval_id (str):
        body (ApprovalDecisionRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ApprovalRequest | ErrorResponse
     """


    return (await asyncio_detailed(
        approval_id=approval_id,
client=client,
body=body,

    )).parsed
