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

"""`-f` — the request body as a file.

The body is the API's own: what the console POSTs, what the OpenAPI describes.
That is what makes it worth having (a flag per field cannot express an object)
and also what has to be held in place — an example in the help that no longer
matches the schema teaches the wrong shape, and nothing would notice.
"""

import json

import pytest

from agentbox_sdk._generated.models.create_env_sandbox_pool_request import (
    CreateEnvSandboxPoolRequest,
)
from agentbox_sdk._generated.models.create_sandbox_env_request import (
    CreateSandboxEnvRequest,
)
from agentbox_sdk.cli import main as M
from agentbox_sdk.cli._body_docs import BODY_DOCS
from agentbox_sdk.cli.context import Context
from agentbox_sdk.cli.parser import UsageError


@pytest.fixture
def sent(monkeypatch):
    """Capture the body a create would POST."""
    box = {}

    def fake_post(self, path, body):
        box["path"], box["body"] = path, body
        return {"name": "x"}

    monkeypatch.setattr(Context, "post_json", fake_post)
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://example.invalid")
    return box


def write(tmp_path, obj):
    p = tmp_path / "body.json"
    p.write_text(json.dumps(obj), encoding="utf-8")
    return str(p)


# ── the body reference is generated, and must stay generated ─────────────────

def test_the_checked_in_reference_matches_the_spec(tmp_path):
    """The one check that matters: regenerate, and compare.

    Everything the help says about the body comes from the OpenAPI schema. If
    the spec changes and nobody re-runs `make gen`, the help starts describing
    a request the API no longer takes — and nothing else would notice, because
    the CLI keeps working and only the documentation is wrong.
    """
    import subprocess
    import sys
    from pathlib import Path

    root = Path(__file__).resolve().parents[2]
    spec = (
        root.parents[2] / "pkg" / "openapi" / "native" / "openapi.yaml"
    )
    if not spec.exists():  # pragma: no cover - absent when packaged
        pytest.skip("openapi.yaml is not in this checkout")
    fresh = tmp_path / "_body_docs.py"
    subprocess.run(
        [sys.executable, str(root / "scripts" / "gen_body_docs.py"),
         str(spec), str(fresh)],
        check=True,
    )
    current = (root / "agentbox_sdk" / "cli" / "_body_docs.py").read_text()
    assert fresh.read_text() == current, (
        "_body_docs.py is stale — run `make gen-all-api`"
    )


@pytest.mark.parametrize(
    "kind,model",
    [("env", CreateSandboxEnvRequest), ("pool", CreateEnvSandboxPoolRequest)],
)
def test_the_generated_example_is_a_valid_body(kind, model):
    # The example is authored in the spec, so this asks whether the spec's own
    # example still matches the spec's own schema.
    body = json.loads(str(BODY_DOCS[kind]["example"]))
    leftover = model.from_dict(dict(body)).additional_properties
    assert leftover == {}, (
        f"the {kind} example in openapi.yaml has fields the schema does not "
        f"accept: {sorted(leftover)}"
    )


@pytest.mark.parametrize("kind", ["env", "pool"])
def test_the_help_renders_the_generated_reference(kind):
    plural = "envs" if kind == "env" else "pools"
    text = M.run([plural, "create", "--help"]).text
    assert "-f body.json" in text
    # A description that only exists in the spec: proof the help is rendering
    # generated data rather than a copy someone typed here.
    described = [f for f in BODY_DOCS[kind]["fields"] if f[3]]  # type: ignore[index]
    assert described, "no field carries a description"
    assert described[0][3].split()[0] in text


# ── the body reaches the API unchanged ───────────────────────────────────────

def test_a_body_is_sent_as_written(sent, tmp_path):
    body = {
        "name": "from-file",
        "templateRef": {"name": "e2b-envd", "version": "0.2.26"},
        "overrides": {"gateway": {"enabled": True}},
        "labels": {"team": "ai-infra"},
    }
    M.run(["envs", "create", "-f", write(tmp_path, body)])
    assert sent["body"] == body


def test_flags_override_the_body_without_disturbing_the_rest(sent, tmp_path):
    body = {
        "name": "base",
        "templateRef": {"name": "e2b-envd"},
        "overrides": {
            "gateway": {"enabled": False},
            "volumes": [{"name": "v"}],
        },
    }
    M.run(["envs", "create", "-f", write(tmp_path, body), "--name", "staging",
           "--gateway"])
    # The flag set one leaf. Everything beside it survived — a flag that
    # replaced `overrides` wholesale would silently drop the volumes.
    assert sent["body"]["name"] == "staging"
    assert sent["body"]["overrides"]["gateway"]["enabled"] is True
    assert sent["body"]["overrides"]["volumes"] == [{"name": "v"}]


def test_a_pool_body_can_do_what_no_flag_can(sent, tmp_path):
    body = {
        "instanceType": "sci.c23-2",
        "inlineResources": {
            "requests": {"cpu": "100m", "memory": "500Mi"},
            "limits": {"cpu": "100m", "memory": "500Mi"},
        },
        "updateStrategy": {"autoUpdate": False},
    }
    M.run(["pools", "create", "--env", "e", "-f", write(tmp_path, body)])
    assert sent["body"] == body


# ── refusing what the API would ──────────────────────────────────────────────

def test_unknown_fields_are_refused_by_name(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://example.invalid")
    path = write(tmp_path, {"name": "x", "templateRef": {"name": "t"},
                            "replicas": 3, "gateway": True})
    with pytest.raises(UsageError) as e:
        M.run(["envs", "create", "-f", path])
    msg = str(e.value)
    # Both offenders named, and the accepted set offered.
    assert "gateway" in msg and "replicas" in msg


def test_yaml_is_refused_by_name_not_by_parse_error(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://example.invalid")
    p = tmp_path / "body.yaml"
    p.write_text(
        "name: my-env\ntemplateRef:\n  name: e2b-envd\n", encoding="utf-8"
    )
    with pytest.raises(UsageError) as e:
        M.run(["envs", "create", "-f", str(p)])
    assert "YAML" in str(e.value)


def test_a_body_missing_the_essentials_says_which(tmp_path, monkeypatch):
    monkeypatch.setenv("AGENTBOX_API_KEY", "agbx_test")
    monkeypatch.setenv("AGENTBOX_ENDPOINT", "http://example.invalid")
    with pytest.raises(UsageError) as e:
        M.run(["envs", "create", "-f", write(tmp_path, {"mode": "WarmPool"})])
    msg = str(e.value)
    assert "name" in msg and "template" in msg


def test_stdin_is_a_body_source(sent, monkeypatch, capsys):
    import io

    monkeypatch.setattr(
        "sys.stdin",
        io.StringIO(
            json.dumps({"name": "piped", "templateRef": {"name": "t"}})
        ),
    )
    M.run(["envs", "create", "-f", "-"])
    assert sent["body"]["name"] == "piped"
