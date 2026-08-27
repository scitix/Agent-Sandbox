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

"""Tests for the new-sandbox notice decision.

The behaviour under test is a risk trade, not a lookup: the notice is announced by
default and suppressed only on proof that a session's sandbox is its first. So the
assertions worth having are about which way each uncertainty falls — an unconfigured
ledger, a torn record, two concurrent binds — and every one of them has to fall
towards speaking.
"""

import concurrent.futures
import json
import os
import sys
import tempfile
import types
import unittest

# --- stub the e2b SDKs so importing sandbox_manager works without them ---------
_agbx = types.ModuleType("agent_sandbox_e2b")
_agbx.patch_e2b = lambda *a, **k: None  # no-op; called at sandbox_manager import
sys.modules.setdefault("agent_sandbox_e2b", _agbx)

_e2b = types.ModuleType("e2b")
_e2b.Sandbox = object  # only referenced as a type annotation / create() target
sys.modules.setdefault("e2b", _e2b)

from . import sandbox_manager  # noqa: E402
from .session_ledger import SessionLedger  # noqa: E402


class LedgerTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = self._tmp.name
        self.addCleanup(self._tmp.cleanup)

    def test_first_call_claims_the_session(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        self.assertIs(led.claim_first("ses_a", "sbx_1", 1), True)

    def test_second_call_reports_not_first(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        led.claim_first("ses_a", "sbx_1", 1)
        self.assertIs(led.claim_first("ses_a", "sbx_2", 2), False)

    def test_sessions_do_not_interfere(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        self.assertIs(led.claim_first("ses_a", "sbx_1", 1), True)
        self.assertIs(led.claim_first("ses_b", "sbx_2", 1), True)

    # A fresh process on the same volume must see the earlier claim; that is the
    # whole reason this is on disk rather than in the session map.
    def test_a_new_ledger_sees_an_earlier_claim(self):
        path = os.path.join(self.dir, "state")
        SessionLedger(path).claim_first("ses_a", "sbx_1", 1)
        self.assertIs(SessionLedger(path).claim_first("ses_a", "sbx_2", 2), False)

    def test_unconfigured_ledger_cannot_tell(self):
        self.assertIsNone(SessionLedger("").claim_first("ses_a", "sbx_1", 1))

    def test_unwritable_storage_cannot_tell(self):
        blocked = os.path.join(self.dir, "blocked")
        os.makedirs(blocked)
        os.chmod(blocked, 0o500)
        self.addCleanup(os.chmod, blocked, 0o700)
        led = SessionLedger(os.path.join(blocked, "state"))
        self.assertIsNone(led.claim_first("ses_a", "sbx_1", 1))

    # An unreadable record reads as absent, and absent means "first", which is the
    # one answer that silences the notice. Claiming again must still report False.
    def test_a_torn_record_does_not_silence_the_notice(self):
        path = os.path.join(self.dir, "state")
        led = SessionLedger(path)
        led.claim_first("ses_a", "sbx_1", 1)
        with open(led._path("ses_a"), "w") as fh:
            fh.write("{ this is not json")
        self.assertIs(led.claim_first("ses_a", "sbx_2", 2), False)

    def test_exactly_one_of_many_concurrent_binds_is_first(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
            results = list(
                pool.map(lambda i: led.claim_first("ses_a", f"sbx_{i}", i), range(32))
            )
        self.assertEqual(results.count(True), 1)
        self.assertEqual(results.count(False), 31)

    def test_record_keeps_the_original_bind_time(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        led.claim_first("ses_a", "sbx_1", 1)
        first = led.read("ses_a")["boundAt"]
        led.claim_first("ses_a", "sbx_2", 2)
        record = led.read("ses_a")
        self.assertEqual(record["sandboxId"], "sbx_2")
        self.assertEqual(record["generation"], 2)
        self.assertEqual(record["firstBoundAt"], first)

    def test_forget_lets_a_session_be_first_again(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        led.claim_first("ses_a", "sbx_1", 1)
        led.forget("ses_a")
        self.assertIs(led.claim_first("ses_a", "sbx_2", 2), True)

    def test_a_session_id_that_is_not_a_safe_path_segment_still_works(self):
        led = SessionLedger(os.path.join(self.dir, "state"))
        weird = "../../etc/passwd\x00 spaces/and:colons"
        self.assertIs(led.claim_first(weird, "sbx_1", 1), True)
        self.assertIs(led.claim_first(weird, "sbx_2", 2), False)
        self.assertEqual(os.listdir(os.path.join(self.dir, "state")), [
            os.path.basename(led._path(weird))
        ])


class NoticeDecisionTests(unittest.TestCase):
    """The manager's mapping from a ledger answer to a notice and a counter."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.state = os.path.join(self._tmp.name, "state")

    def decide(self, ledger: SessionLedger, sid: str, sandbox_id: str):
        """Run only the notice decision, without provisioning anything.

        Mirrors get_or_create's tail. Kept as a helper rather than a call into
        get_or_create because everything else that method does — readiness gating,
        seeding, attachment flushing — needs a live sandbox, and none of it
        participates in this decision.
        """
        mgr = sandbox_manager.SandboxManager(ledger=ledger)
        first = ledger.claim_first(sid, sandbox_id, 1)
        notice = None if first is True else sandbox_manager.NEW_SANDBOX_NOTICE
        mgr._count("first" if first is True else ("replaced" if first is False else "unknown"))
        return notice, mgr.counters

    def test_a_sessions_first_sandbox_is_not_announced(self):
        led = SessionLedger(self.state)
        notice, counters = self.decide(led, "ses_a", "sbx_1")
        self.assertIsNone(notice)
        self.assertEqual(counters["first"], 1)

    def test_a_replacement_sandbox_is_announced(self):
        led = SessionLedger(self.state)
        led.claim_first("ses_a", "sbx_1", 1)
        notice, counters = self.decide(led, "ses_a", "sbx_2")
        self.assertEqual(notice, sandbox_manager.NEW_SANDBOX_NOTICE)
        self.assertEqual(counters["replaced"], 1)

    # The regression that motivated all of this: with no durable record, a restarted
    # daemon used to treat a session it had never seen as brand new and say nothing.
    def test_without_a_ledger_every_new_sandbox_is_announced(self):
        notice, counters = self.decide(SessionLedger(""), "ses_a", "sbx_1")
        self.assertEqual(notice, sandbox_manager.NEW_SANDBOX_NOTICE)
        self.assertEqual(counters["unknown"], 1)

    def test_the_notice_claims_no_cause_and_no_prior_files(self):
        text = sandbox_manager.NEW_SANDBOX_NOTICE
        # It must not assert why the previous sandbox went away — idle reclaim, a
        # restart and a pool move all land here and the manager cannot tell them
        # apart. Nor may it assert that files were lost, because the case it cannot
        # distinguish includes a session that never had any.
        for forbidden in ("expired", "idle", "restart", "your files are gone"):
            self.assertNotIn(forbidden, text.lower())
        self.assertIn("newly created sandbox", text.lower())

    def test_the_notice_is_delivered_once(self):
        entry = sandbox_manager.SessionEntry(sid="ses_a", sandbox=object())
        entry.pending_notice = sandbox_manager.NEW_SANDBOX_NOTICE
        self.assertEqual(entry.take_notice(), sandbox_manager.NEW_SANDBOX_NOTICE)
        self.assertIsNone(entry.take_notice())


class MetadataTests(unittest.TestCase):
    def test_metadata_carries_session_and_generation(self):
        mgr = sandbox_manager.SandboxManager(ledger=SessionLedger(""))
        meta = mgr._sandbox_metadata("ses_a", 3)
        self.assertEqual(meta["hands.session"], "ses_a")
        self.assertEqual(meta["hands.generation"], "3")

    # The platform strips its own reserved namespace out of caller metadata, so a
    # key landing there would be silently dropped rather than rejected.
    def test_metadata_avoids_the_platform_reserved_namespace(self):
        mgr = sandbox_manager.SandboxManager(ledger=SessionLedger(""))
        for key in mgr._sandbox_metadata("ses_a", 1):
            self.assertFalse(key.startswith("agentbox.scitix.ai/"))

    def test_metadata_values_are_all_strings(self):
        mgr = sandbox_manager.SandboxManager(ledger=SessionLedger(""))
        for key, value in mgr._sandbox_metadata("ses_a", 1).items():
            self.assertIsInstance(value, str, msg=key)

    def test_metadata_is_json_serialisable(self):
        mgr = sandbox_manager.SandboxManager(ledger=SessionLedger(""))
        json.dumps(mgr._sandbox_metadata("ses_a", 1))


if __name__ == "__main__":
    unittest.main()


class FakeSandbox:
    """Minimal stand-in for an e2b Sandbox handle."""

    def __init__(self, sandbox_id="sbx_1", marker=None, alive=True):
        self.sandbox_id = sandbox_id
        self.files = _FakeFiles(marker)
        self._alive = alive
        self.killed = False

    def is_running(self, request_timeout=None):
        return self._alive

    def kill(self):
        self.killed = True


class _FakeFiles:
    def __init__(self, marker):
        self.stored = marker

    def write(self, path, content):
        self.stored = content

    def read(self, path):
        if self.stored is None:
            raise FileNotFoundError(path)
        return self.stored


class ReattachTests(unittest.TestCase):
    """Each guard rules out a different way of re-attaching to the wrong thing."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.led = SessionLedger(os.path.join(self._tmp.name, "state"))
        self.mgr = sandbox_manager.SandboxManager(ledger=self.led)
        self._connect_calls = []

    def arrange(self, sandbox, *, record=True):
        """Point the ledger at a sandbox and stub Sandbox.connect to return it.

        `sandbox=None` stands for a sandbox the ledger still remembers but that
        connect can no longer reach, so the record is written from a literal id.
        """
        if record:
            self.led.claim_first(
                "ses_a", sandbox.sandbox_id if sandbox else "sbx_gone", 1
            )

        def fake_connect(sandbox_id, **kwargs):
            self._connect_calls.append((sandbox_id, kwargs))
            if sandbox is None:
                raise RuntimeError("sandbox not found")
            return sandbox

        original = sandbox_manager.Sandbox
        stub = types.SimpleNamespace(connect=staticmethod(fake_connect))
        sandbox_manager.Sandbox = stub
        self.addCleanup(setattr, sandbox_manager, "Sandbox", original)

    def marker_for(self, sid, sandbox_id="sbx_1", generation=1):
        from .session_marker import marker_payload

        return marker_payload(sid, sandbox_id, generation)

    def test_reattaches_to_a_live_sandbox_carrying_this_session(self):
        sbx = FakeSandbox(marker=self.marker_for("ses_a"))
        self.arrange(sbx)
        entry = self.mgr._reattach("ses_a")
        self.assertIsNotNone(entry)
        self.assertIs(entry.sandbox, sbx)

    # A bare connect() lets the SDK send its own default, and the API applies a
    # connect's timeout as now + timeout — so omitting it shortens a long-lived
    # sandbox to that default just for being looked at.
    def test_reattach_sends_an_explicit_timeout(self):
        self.arrange(FakeSandbox(marker=self.marker_for("ses_a")))
        self.mgr._reattach("ses_a")
        self.assertEqual(len(self._connect_calls), 1)
        _, kwargs = self._connect_calls[0]
        self.assertEqual(kwargs.get("timeout"), sandbox_manager.SBX_REATTACH_TIMEOUT)
        self.assertGreater(kwargs["timeout"], 300)

    def test_no_recorded_sandbox_means_no_attempt(self):
        self.arrange(FakeSandbox(), record=False)
        self.assertIsNone(self.mgr._reattach("ses_a"))
        self.assertEqual(self._connect_calls, [])

    def test_a_failed_connect_falls_through(self):
        self.arrange(None)
        self.assertIsNone(self.mgr._reattach("ses_a"))

    # connect answers from the history store once the sandbox is gone, handing back
    # a handle that looks fine and fails on first use, so the probe is required.
    def test_an_unresponsive_sandbox_is_not_adopted(self):
        self.arrange(FakeSandbox(marker=self.marker_for("ses_a"), alive=False))
        self.assertIsNone(self.mgr._reattach("ses_a"))

    # The id is right and the sandbox is alive, but the filesystem belongs to
    # someone else — the outcome the marker exists to prevent.
    def test_a_sandbox_carrying_another_session_is_not_adopted(self):
        self.arrange(FakeSandbox(marker=self.marker_for("ses_other")))
        self.assertIsNone(self.mgr._reattach("ses_a"))

    def test_an_unmarked_sandbox_is_not_adopted(self):
        self.arrange(FakeSandbox(marker=None))
        self.assertIsNone(self.mgr._reattach("ses_a"))

    def test_a_corrupt_marker_is_not_adopted(self):
        self.arrange(FakeSandbox(marker="{not json"))
        self.assertIsNone(self.mgr._reattach("ses_a"))

    def test_marker_write_failure_does_not_raise(self):
        from .session_marker import write_marker

        class Broken(FakeSandbox):
            def __init__(self):
                super().__init__()
                self.files = types.SimpleNamespace(
                    write=lambda *a, **k: (_ for _ in ()).throw(OSError("read-only"))
                )

        self.assertFalse(write_marker(Broken(), "ses_a", "sbx_1", 1))
