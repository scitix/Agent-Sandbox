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

"""`--help` asks what a command would do. It must never be what does it.

This is pinned rather than left to review because of how it failed: the help
check sat AFTER the write dispatch and only applied when no verb was present,
so `abx envs create --name test --help` skipped the help and created the
environment. Someone reading the manual got a real object — and, with the
approval gate on, a request for a person to approve one nobody asked for.
"""

import pytest

from agentbox_sdk.cli import main as M
from agentbox_sdk.cli.context import Context


class Exploded(AssertionError):
    """Raised if anything reaches the network."""


@pytest.fixture(autouse=True)
def no_network(monkeypatch):
    def boom(*_a, **_k):
        raise Exploded("--help reached the API")

    monkeypatch.setattr(Context, "post_json", boom)
    monkeypatch.setattr(Context, "get_json", boom)
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://example.invalid")


@pytest.mark.parametrize(
    "argv",
    [
        # The exact shape that created an environment.
        ["envs", "create", "--name", "test", "--help"],
        ["envs", "create", "--help"],
        ["envs", "create", "--help", "--name", "test"],
        ["pools", "create", "--env", "e", "--instance-type", "i", "--help"],
        # Short form, and help before the verb.
        ["envs", "create", "--name", "test", "-h"],
    ],
)
def test_help_never_writes(argv):
    result = M.run(argv)
    assert not result.error
    assert "Usage:" in result.text


def test_help_for_create_shows_the_create_flags():
    # A verb narrows the help: someone who typed `create` wants the flags for
    # creating, not the columns they could filter a list by.
    text = M.run(["envs", "create", "--help"]).text
    assert "abx envs create" in text
    assert "--template" in text
    assert "--gateway" in text


def test_help_for_a_kind_still_describes_the_kind():
    # No verb: the listing help, unchanged.
    text = M.run(["envs", "--help"]).text
    assert "envs" in text


def test_a_create_without_help_still_tries_to_create():
    # The guard above must not have turned every create into a help screen.
    with pytest.raises(Exploded):
        M.run(["envs", "create", "--name", "test", "--template", "t"])
