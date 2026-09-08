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

"""The half of the approval flow that runs on the caller's machine.

What is worth testing here is the seam: a refusal only helps if the CLI can
tell it apart from a failure and print something the reader — usually an agent —
can act on without knowing the protocol.
"""

import json

import httpx
import pytest

from agentbox_sdk.cli import render as R
from agentbox_sdk.cli.context import (
    ApiError,
    ApprovalRequired,
    _approval_of,
    _parse,
)


def response(status: int, payload: dict) -> httpx.Response:
    return httpx.Response(
        status_code=status,
        content=json.dumps(payload).encode(),
        request=httpx.Request("POST", "http://x/v1/envs"),
    )


REFUSAL = {
    "error": "approval required: Create an environment",
    "errorCode": "APPROVAL_REQUIRED",
    "detail": {
        "approvalId": "apr_deadbeef",
        "operation": "env.create",
        "summary": "Create an environment",
        "onceOnly": False,
        "url": "https://console.example.com/agentbox/clusters/c1/approvals?id=apr_deadbeef",
        "pollUrl": "/v1/approvals/apr_deadbeef",
        "expiresAt": "2026-01-01T12:15:00Z",
    },
}


def test_a_refusal_is_its_own_exception():
    # A caller that saw ApiError here would report "the command failed" for
    # something a person can unblock in seconds.
    with pytest.raises(ApprovalRequired) as excinfo:
        _parse(response(428, REFUSAL), "/envs")
    assert excinfo.value.approval.id == "apr_deadbeef"
    assert excinfo.value.status == 428


def test_an_ordinary_error_is_still_an_ordinary_error():
    with pytest.raises(ApiError) as excinfo:
        _parse(response(403, {"error": "forbidden"}), "/envs")
    assert not isinstance(excinfo.value, ApprovalRequired)


def test_the_code_is_what_identifies_a_refusal_not_the_status():
    # A 428 can also come from a proxy. The errorCode is only ever written by
    # the gate itself.
    assert _approval_of({"error": "gateway said no"}) is None
    # And a body that claims the code but carries no id has nothing to poll or
    # point at, so it is not usable as one.
    assert (
        _approval_of({"errorCode": "APPROVAL_REQUIRED", "detail": {}}) is None
    )


def test_the_trailer_names_the_thing_the_link_and_the_next_command():
    approval = _approval_of(REFUSAL)
    lines = R.approval_block(approval)
    text = "\n".join(lines)
    # Same shape as every other trailer: a label, then indented content.
    assert lines[0] == "approval:"
    assert "Create an environment" in text
    assert "https://console.example.com" in text
    # The hint tells the agent to WAIT, not to retry. Retrying immediately
    # produces a second refusal and a loop.
    assert "abx approvals wait apr_deadbeef" in text
    assert "hint:" in lines


def test_a_once_only_refusal_says_the_wider_scope_is_not_on_offer():
    payload = json.loads(json.dumps(REFUSAL))
    payload["detail"]["onceOnly"] = True
    text = "\n".join(R.approval_block(_approval_of(payload)))
    assert "(once)" in text
    assert "session" not in text


def test_a_refusal_without_a_console_link_still_renders():
    # The console address reaches a worker from the hub, so an early refusal
    # legitimately carries no link. Printing a bare "None" there would be worse
    # than printing nothing.
    payload = json.loads(json.dumps(REFUSAL))
    payload["detail"]["url"] = ""
    text = "\n".join(R.approval_block(_approval_of(payload)))
    assert "http" not in text
    assert "abx approvals wait" in text
