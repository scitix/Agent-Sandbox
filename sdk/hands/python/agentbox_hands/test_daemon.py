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

"""Unit tests for the sandbox-proxy daemon's pure path/exec helpers.

Runnable with the standard library alone (no pytest):

    cd sdk/hands/python && python -m unittest agentbox_hands.test_daemon

Importing the daemon pulls in `sandbox_manager`, which imports the `e2b` /
`agent_sandbox_e2b` SDKs (and calls `patch_e2b` at module load). Those SDKs are
only present in the built sandbox image, so we stub them in `sys.modules` BEFORE
importing the daemon — the helpers under test don't touch the SDK.
"""
import os
import sys
import types
import unittest
from unittest import mock

# --- stub the e2b SDKs so `import daemon` works without them installed --------
_agbx = types.ModuleType("agent_sandbox_e2b")
_agbx.patch_e2b = lambda *a, **k: None  # no-op; called at sandbox_manager import
sys.modules.setdefault("agent_sandbox_e2b", _agbx)

_e2b = types.ModuleType("e2b")
_e2b.Sandbox = object  # only referenced as a type annotation / create() target
sys.modules.setdefault("e2b", _e2b)

from . import daemon, sandbox_manager  # noqa: E402
from fastapi import HTTPException  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402


class ResolveTests(unittest.TestCase):
    BASE = "/home/agents/u/alice"

    def test_empty_path_returns_base(self):
        self.assertEqual(daemon._resolve("", self.BASE), self.BASE)

    def test_absolute_path_passes_through(self):
        self.assertEqual(daemon._resolve("/opt/volcano", self.BASE), "/opt/volcano")

    def test_relative_path_joins_base(self):
        self.assertEqual(daemon._resolve("pkg/x.go", self.BASE), f"{self.BASE}/pkg/x.go")

    def test_base_is_honoured_per_call(self):
        # The workspace is per-session now, not a module global.
        self.assertEqual(daemon._resolve("a", "/tmp/workspace"), "/tmp/workspace/a")


class _FakeCommands:
    """Mimics e2b's commands.run: returns a result on success, RAISES on failure
    (carrying exit_code/stdout/stderr), which is exactly why _run_capture exists."""

    def __init__(self, *, result=None, exc=None):
        self._result = result
        self._exc = exc

    def run(self, cmd, cwd=None, timeout=None, user=None):
        if self._exc is not None:
            raise self._exc
        return self._result


class _FakeSandbox:
    def __init__(self, commands):
        self.commands = commands


class _FakeEntry:
    def __init__(self, commands):
        self.sandbox = _FakeSandbox(commands)


def _result(exit_code, stdout, stderr):
    return types.SimpleNamespace(exit_code=exit_code, stdout=stdout, stderr=stderr)


class RunCaptureTests(unittest.TestCase):
    def test_success(self):
        entry = _FakeEntry(_FakeCommands(result=_result(0, "hit\n", "")))
        self.assertEqual(daemon._run_capture(entry, "grep x", "/w", 30), (0, "hit\n", ""))

    def test_none_streams_normalise_to_empty(self):
        entry = _FakeEntry(_FakeCommands(result=_result(0, None, None)))
        self.assertEqual(daemon._run_capture(entry, "c", "/w", 30), (0, "", ""))

    def test_nonzero_exit_no_match_is_tolerated(self):
        # grep exit 1 = "no matches": the SDK raises, we surface it as a result
        # (exit_code 1, empty stdout -> the grep route reports "(no matches)").
        exc = Exception("no match")
        exc.exit_code, exc.stdout, exc.stderr = 1, "", ""
        entry = _FakeEntry(_FakeCommands(exc=exc))
        code, out, _ = daemon._run_capture(entry, "grep x", "/w", 30)
        self.assertEqual((code, out), (1, ""))

    def test_nonzero_exit_bad_path_is_tolerated(self):
        # grep exit 2 = bad path (e.g. a path that doesn't exist in the sandbox).
        exc = Exception("No such file or directory")
        exc.exit_code, exc.stdout, exc.stderr = 2, "", "grep: /nope: ..."
        entry = _FakeEntry(_FakeCommands(exc=exc))
        code, out, err = daemon._run_capture(entry, "grep x /nope", "/w", 30)
        self.assertEqual((code, out), (2, ""))
        self.assertIn("grep:", err)

    def test_exception_without_attrs_falls_back(self):
        entry = _FakeEntry(_FakeCommands(exc=RuntimeError("boom")))
        code, out, err = daemon._run_capture(entry, "c", "/w", 30)
        self.assertEqual((code, out), (-1, ""))
        self.assertIn("boom", err)


class _FakeManager:
    """Just enough of SandboxManager for the read route: one entry serving a
    fixed file body, a no-op session lock, and no pending notice."""

    class _Entry:
        def __init__(self, body):
            self.sandbox = types.SimpleNamespace(
                files=types.SimpleNamespace(read=lambda p, user=None: body)
            )

        def workspace(self):
            return "/w"

        def take_notice(self):
            return None

    class _Lock:
        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

    def __init__(self, body):
        self._entry = self._Entry(body)

    def get_or_create(self, sid):
        return self._entry

    def session_lock(self, sid):
        return self._Lock()


class ReadFileTests(unittest.TestCase):
    """`count` is the file's TOTAL line count and must be returned whether or not
    the caller paged — the read tool needs it for its "showing lines A-B of C"
    note, and that note is what makes default paging usable."""

    BODY = "".join(f"line {i}\n" for i in range(1, 11))

    def _read(self, body_text, **kw):
        original = daemon.manager
        daemon.manager = _FakeManager(body_text)
        try:
            return daemon.read_file("s1", daemon.ReadIn(path="f.txt", **kw))
        finally:
            daemon.manager = original

    def test_full_read_reports_total_lines(self):
        out = self._read(self.BODY)
        self.assertEqual(out["count"], 10)
        self.assertEqual(out["content"], self.BODY)

    def test_paged_read_still_reports_the_total(self):
        out = self._read(self.BODY, offset=3, limit=2)
        self.assertEqual(out["count"], 10)
        self.assertEqual(out["content"], "line 3\nline 4\n")

    def test_empty_file(self):
        out = self._read("")
        self.assertEqual(out["count"], 0)
        self.assertEqual(out["content"], "")

    def test_last_line_without_trailing_newline_is_counted(self):
        out = self._read("a\nb")
        self.assertEqual(out["count"], 2)


class BindSessionTest(unittest.TestCase):
    """An explicitly bound identity must beat the loopback lookup.

    The lookup is OpenCode-shaped, and every failure returns None silently, which
    degrades both the working directory and the attachment flush. Binding is how a
    different harness avoids that, so precedence is the property worth pinning.
    """

    def setUp(self):
        sandbox_manager.unbind_session("ses_bind")

    def tearDown(self):
        sandbox_manager.unbind_session("ses_bind")

    def test_bound_directory_wins_over_lookup(self):
        # No opencode is listening here, so a lookup would return None. A bind
        # therefore proves precedence on its own: a non-None answer can only have
        # come from the bind.
        sandbox_manager.bind_session("ses_bind", "/home/agents/u/alice")
        self.assertEqual(
            sandbox_manager._session_directory("ses_bind"), "/home/agents/u/alice"
        )

    def test_unbound_session_falls_back_to_lookup(self):
        # No bind, no reachable opencode: None, so callers use their default.
        self.assertIsNone(sandbox_manager._session_directory("ses_never_bound"))

    def test_bind_route_records_the_identity(self):
        client = TestClient(daemon.app)
        res = client.post(
            "/sessions/ses_bind/bind",
            json={"directory": "/home/agents/u/bob"},
        )
        self.assertEqual(res.status_code, 200)
        self.assertEqual(
            sandbox_manager.bound_session("ses_bind"),
            {"directory": "/home/agents/u/bob"},
        )


class SandboxEnvTests(unittest.TestCase):
    """What a deployment can put in a sandbox's environment, and what it cannot.

    A sandbox's env is fixed at create time, so this is the only chance to tell the
    image anything. The daemon treats the value as opaque — it belongs to whoever
    built the sandbox image — which is why the parsing is defensive rather than
    validating: a key this package does not recognise is the normal case.
    """

    def test_absent_or_empty_yields_nothing(self):
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("SBX_SANDBOX_ENV", None)
            self.assertEqual(sandbox_manager._sandbox_env_from_environ(), {})
        with mock.patch.dict(os.environ, {"SBX_SANDBOX_ENV": "   "}):
            self.assertEqual(sandbox_manager._sandbox_env_from_environ(), {})

    def test_values_are_stringified(self):
        # A deployment writing a port as a number is not making a mistake, and the
        # sandbox API only accepts strings.
        with mock.patch.dict(
            os.environ, {"SBX_SANDBOX_ENV": '{"PORT": 8080, "FLAG": true}'}
        ):
            self.assertEqual(
                sandbox_manager._sandbox_env_from_environ(),
                {"PORT": "8080", "FLAG": "True"},
            )

    def test_a_malformed_value_is_ignored_rather_than_fatal(self):
        # Serving tool calls matters more than an extra variable nothing may read.
        # Refusing to start here would take the whole agent down for a typo.
        for bad in ('{"unclosed": ', '["a", "b"]', '"a string"', "42"):
            with mock.patch.dict(os.environ, {"SBX_SANDBOX_ENV": bad}):
                self.assertEqual(
                    sandbox_manager._sandbox_env_from_environ(), {}, f"for {bad!r}"
                )

    def test_the_caller_cannot_mutate_what_later_sandboxes_get(self):
        with mock.patch.object(sandbox_manager, "SANDBOX_ENV", {"A": "1"}):
            first = sandbox_manager._sandbox_envs("ses_x")
            first["A"] = "tampered"
            first["B"] = "added"
            self.assertEqual(sandbox_manager._sandbox_envs("ses_y"), {"A": "1"})


class AliasTests(unittest.TestCase):
    """Two names, ONE sandbox.

    The browser asks about the gateway's thread id; OpenCode's tools can only send
    OpenCode's session id. Unaliased, that second id is a second session — the
    workspace panel reports `inactive` for a thread whose agent is working, and
    attachments staged under the thread id flush into a sandbox nobody reads.
    """

    def setUp(self):
        sandbox_manager.unbind_session("th_alias")

    def tearDown(self):
        sandbox_manager.unbind_session("th_alias")

    def test_alias_resolves_to_the_canonical_id(self):
        sandbox_manager.bind_session("th_alias", "/home/agents/u/alice")
        sandbox_manager.alias_session("ses_alias", "th_alias")
        self.assertEqual(sandbox_manager.resolve_sid("ses_alias"), "th_alias")
        # The identity — cwd and staging dir — is the canonical one for both.
        self.assertEqual(
            sandbox_manager._session_directory("ses_alias"), "/home/agents/u/alice"
        )
        self.assertEqual(
            sandbox_manager.bound_session("ses_alias"),
            {"directory": "/home/agents/u/alice"},
        )

    def test_unknown_id_resolves_to_itself(self):
        self.assertEqual(sandbox_manager.resolve_sid("ses_lonely"), "ses_lonely")

    def test_bind_route_registers_aliases(self):
        client = TestClient(daemon.app)
        res = client.post(
            "/sessions/th_alias/bind",
            json={
                "directory": "/home/agents/u/bob",
                "aliases": ["ses_from_harness"],
            },
        )
        self.assertEqual(res.status_code, 200)
        self.assertEqual(res.json()["aliases"], ["ses_from_harness"])
        self.assertEqual(
            sandbox_manager.resolve_sid("ses_from_harness"), "th_alias"
        )

    def test_canonical_route_maps_a_harness_alias_back_to_the_thread(self):
        # A background job is bound to the THREAD id, but a tool running under
        # OpenCode only knows OpenCode's session id. Reporting that id back joins
        # to nothing, so this lookup is what keeps a job's work attached to the
        # job that started it.
        client = TestClient(daemon.app)
        client.post(
            "/sessions/th_report/bind",
            json={
                "directory": "/home/agents/u/diag-bot",
                "aliases": ["ses_report"],
            },
        )
        res = client.get("/sessions/ses_report/canonical")
        self.assertEqual(res.status_code, 200)
        self.assertEqual(res.json()["session_id"], "th_report")

    def test_canonical_route_is_identity_for_an_unaliased_id(self):
        client = TestClient(daemon.app)
        res = client.get("/sessions/th_plain/canonical")
        self.assertEqual(res.json()["session_id"], "th_plain")

    def test_unbinding_drops_the_aliases_pointing_at_it(self):
        # Else a killed thread's aliases keep resolving to a session that is gone.
        sandbox_manager.bind_session("th_alias", "/home/agents/u/alice")
        sandbox_manager.alias_session("ses_alias", "th_alias")
        sandbox_manager.unbind_session("th_alias")
        self.assertEqual(sandbox_manager.resolve_sid("ses_alias"), "ses_alias")


class _GlobEntry(_FakeEntry):
    """glob_ only needs the workspace and the one-shot notice."""

    def __init__(self):
        super().__init__(_FakeCommands(result=_result(0, "", "")))

    def workspace(self):
        return "/tmp/workspace"

    def take_notice(self):
        return None


class GlobPatternTests(unittest.TestCase):
    """The pattern must reach bash UNQUOTED, or nothing is ever matched.

    This is not hypothetical: the pattern used to be shell-quoted, so
    `for f in \'*.txt\'` iterated the literal two-word string and every glob
    answered with its own pattern. An agent reads that as "the file exists and is
    named *.txt", or — with the tool reporting no real paths — as "there are no
    matching files". Unquoted expansion is therefore the contract, and the
    character guard is what keeps that from being a shell injection.
    """

    def test_the_command_interpolates_the_pattern_unquoted(self):
        captured = {}

        def fake_run_capture(entry, cmd, cwd=None, timeout=None):
            captured["cmd"] = cmd
            return 0, "/tmp/workspace/a.txt\n", ""

        entry = _GlobEntry()
        original_run, original_get = daemon._run_capture, daemon.manager.get_or_create
        daemon._run_capture = fake_run_capture
        daemon.manager.get_or_create = lambda sid: entry
        try:
            out = daemon.glob_("s1", daemon.GlobIn(pattern="**/*.txt"))
        finally:
            daemon._run_capture, daemon.manager.get_or_create = original_run, original_get

        self.assertIn("for f in **/*.txt;", captured["cmd"])
        self.assertNotIn("'**/*.txt'", captured["cmd"])
        # globstar/nullglob are what make ** work and an unmatched pattern vanish.
        self.assertIn("shopt -s globstar nullglob dotglob", captured["cmd"])
        self.assertEqual(out["paths"], ["/tmp/workspace/a.txt"])

    def test_a_pattern_carrying_shell_syntax_is_refused_loudly(self):
        entry = _GlobEntry()
        original_get = daemon.manager.get_or_create
        daemon.manager.get_or_create = lambda sid: entry
        try:
            for pattern in ["*.txt; rm -rf /", "$(id)", "`id`", "a|b", "x>y"]:
                with self.assertRaises(HTTPException) as ctx:
                    daemon.glob_("s1", daemon.GlobIn(pattern=pattern))
                # 400, not an empty result: silence would read as "no such files".
                self.assertEqual(ctx.exception.status_code, 400)
        finally:
            daemon.manager.get_or_create = original_get


if __name__ == "__main__":
    unittest.main()
