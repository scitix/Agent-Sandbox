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
from ...models.exec_command_request import ExecCommandRequest
from ...models.exec_command_result import ExecCommandResult
from typing import cast



def _get_kwargs(
    sandbox_id: str,
    *,
    body: ExecCommandRequest,

) -> dict[str, Any]:
    headers: dict[str, Any] = {}


    

    

    _kwargs: dict[str, Any] = {
        "method": "post",
        "url": "/sandboxes/{sandbox_id}/exec".format(sandbox_id=quote(str(sandbox_id), safe=""),),
    }

    _kwargs["json"] = body.to_dict()

    headers["Content-Type"] = "application/json"

    _kwargs["headers"] = headers
    return _kwargs



def _parse_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> ErrorResponse | ExecCommandResult | None:
    if response.status_code == 200:
        response_200 = ExecCommandResult.from_dict(response.json())



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


def _build_response(*, client: AuthenticatedClient | Client, response: httpx.Response) -> Response[ErrorResponse | ExecCommandResult]:
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
    body: ExecCommandRequest,

) -> Response[ErrorResponse | ExecCommandResult]:
    """ Execute a command inside a sandbox

     **DEPRECATED** — will be removed in a future version.

    Prefer opening an interactive exec session with the WebSocket endpoint
    (see `POST /sandboxes/{sandboxId}/exec-token` + `wss://.../terminal`) for
    any non-trivial command execution. This REST call is limited to 300 seconds
    and does not stream output incrementally.

    Executes a shell command inside the sandbox and returns stdout, stderr, and exit code.
    The sandbox must be in Running phase. The command runs in a non-interactive shell (no TTY).

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        body (ExecCommandRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ExecCommandResult]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,
body=body,

    )

    response = client.get_httpx_client().request(
        **kwargs,
    )

    return _build_response(client=client, response=response)

def sync(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ExecCommandRequest,

) -> ErrorResponse | ExecCommandResult | None:
    """ Execute a command inside a sandbox

     **DEPRECATED** — will be removed in a future version.

    Prefer opening an interactive exec session with the WebSocket endpoint
    (see `POST /sandboxes/{sandboxId}/exec-token` + `wss://.../terminal`) for
    any non-trivial command execution. This REST call is limited to 300 seconds
    and does not stream output incrementally.

    Executes a shell command inside the sandbox and returns stdout, stderr, and exit code.
    The sandbox must be in Running phase. The command runs in a non-interactive shell (no TTY).

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        body (ExecCommandRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ExecCommandResult
     """


    return sync_detailed(
        sandbox_id=sandbox_id,
client=client,
body=body,

    ).parsed

async def asyncio_detailed(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ExecCommandRequest,

) -> Response[ErrorResponse | ExecCommandResult]:
    """ Execute a command inside a sandbox

     **DEPRECATED** — will be removed in a future version.

    Prefer opening an interactive exec session with the WebSocket endpoint
    (see `POST /sandboxes/{sandboxId}/exec-token` + `wss://.../terminal`) for
    any non-trivial command execution. This REST call is limited to 300 seconds
    and does not stream output incrementally.

    Executes a shell command inside the sandbox and returns stdout, stderr, and exit code.
    The sandbox must be in Running phase. The command runs in a non-interactive shell (no TTY).

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        body (ExecCommandRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        Response[ErrorResponse | ExecCommandResult]
     """


    kwargs = _get_kwargs(
        sandbox_id=sandbox_id,
body=body,

    )

    response = await client.get_async_httpx_client().request(
        **kwargs
    )

    return _build_response(client=client, response=response)

async def asyncio(
    sandbox_id: str,
    *,
    client: AuthenticatedClient | Client,
    body: ExecCommandRequest,

) -> ErrorResponse | ExecCommandResult | None:
    """ Execute a command inside a sandbox

     **DEPRECATED** — will be removed in a future version.

    Prefer opening an interactive exec session with the WebSocket endpoint
    (see `POST /sandboxes/{sandboxId}/exec-token` + `wss://.../terminal`) for
    any non-trivial command execution. This REST call is limited to 300 seconds
    and does not stream output incrementally.

    Executes a shell command inside the sandbox and returns stdout, stderr, and exit code.
    The sandbox must be in Running phase. The command runs in a non-interactive shell (no TTY).

    Args:
        sandbox_id (str):  Example: 5de15c92-8fb5-440f-a9ea-7f62f734f1b9.
        body (ExecCommandRequest):

    Raises:
        errors.UnexpectedStatus: If the server returns an undocumented status code and Client.raise_on_unexpected_status is True.
        httpx.TimeoutException: If the request takes longer than Client.timeout.

    Returns:
        ErrorResponse | ExecCommandResult
     """


    return (await asyncio_detailed(
        sandbox_id=sandbox_id,
client=client,
body=body,

    )).parsed
