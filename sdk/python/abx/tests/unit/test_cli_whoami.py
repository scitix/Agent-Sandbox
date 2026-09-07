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

"""`abx whoami` — the command that answers "whose view am I looking at?".

It matters more than its size suggests. Everything else the CLI prints is
scoped to the credential's identity — `quotas` is that person's quota, `envs`
and `pools` are the ones in their namespace — so an agent that cannot state who
it is cannot describe what it is showing. The runtime prompt tells it to run
this first, using the bare spelling, which is why the bare spelling is pinned
here rather than left as an alias nobody checks.
"""

from __future__ import annotations

from typing import Any

import pytest

from agentbox_sdk.cli import main as M
from agentbox_sdk.cli.parser import UsageError


@pytest.fixture
def whoami(monkeypatch: pytest.MonkeyPatch) -> list[str]:
    """Record which paths a run fetches, answering every one identically."""
    seen: list[str] = []

    def fake_get_json(self: Any, path: str, **_kwargs: Any) -> dict:
        seen.append(path)
        return {"role": "tenant", "user": "bob", "team": "team1"}

    monkeypatch.setattr(M.Context, "get_json", fake_get_json, raising=True)
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://api.example.com")
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    return seen


def test_bare_whoami_asks_the_identity_endpoint(whoami: list[str]) -> None:
    result = M.run(["whoami"])
    assert whoami == ["/auth/whoami"]
    assert "bob" in result.text
    assert "team1" in result.text


def test_both_spellings_do_the_same_thing(whoami: list[str]) -> None:
    bare = M.run(["whoami"])
    grouped = M.run(["auth", "whoami"])
    assert bare.text == grouped.text
    assert whoami == ["/auth/whoami", "/auth/whoami"]


def test_json_format_is_honoured(whoami: list[str]) -> None:
    # An agent parsing the answer asks for JSON; the human-readable default
    # would have it regex a table.
    result = M.run(["whoami", "--format", "json"])
    assert '"user"' in result.text and '"bob"' in result.text


def test_help_needs_no_endpoint(monkeypatch: pytest.MonkeyPatch) -> None:
    # `--help` must answer before any credential or endpoint is resolved: an
    # agent that has to guess the syntax is told to run help first, and help
    # that fails without a configured endpoint teaches it to guess instead.
    monkeypatch.delenv("AGENTBOX_ENDPOINT", raising=False)
    monkeypatch.delenv("AGENTBOX_API_KEY", raising=False)
    for argv in (["whoami", "--help"], ["auth", "whoami", "--help"]):
        assert "authenticates as" in M.run(argv).text

def test_whoami_is_listed_in_the_root_help() -> None:
    # Every discovery surface is generated, and this command is not a registered
    # resource — so being absent here means an agent never learns it exists.
    assert "whoami" in M.root_help()


def test_an_unknown_auth_subcommand_fails(whoami: list[str]) -> None:
    with pytest.raises(UsageError):
        M.run(["auth", "nonsense"])
    assert whoami == [], "a rejected subcommand must not reach the API"
