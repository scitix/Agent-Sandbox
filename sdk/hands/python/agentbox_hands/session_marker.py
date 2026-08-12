"""A record, written inside the sandbox, of which session the sandbox belongs to.

The ledger says which sandbox a session had. This says which session a sandbox
has, and the difference matters when the two disagree: re-attaching by id trusts a
record kept elsewhere, and if that record is stale — an id reused, a sandbox
replaced underneath us — the attach succeeds and hands back a filesystem that is
not the one the conversation left. Everything after that is quietly wrong, because
the tools work fine; they are just operating somewhere else.

This file is the one piece of state whose lifetime is exactly the sandbox's
filesystem, so it is the only thing that can answer "is this the same filesystem"
rather than "is this the same id". It lives under /tmp for that reason: nothing
carries it across a rebuild, which is precisely the property wanted. A marker that
survived would be worse than none at all.
"""

from __future__ import annotations

import json
import os
from typing import Optional

# Under /tmp so it cannot outlive the sandbox, and dot-prefixed so it stays out of
# a bare `ls` in the agent's workspace.
MARKER_PATH = os.environ.get(
    "HANDS_MARKER_PATH", "/tmp/.agentbox-hands-session.json"
)


def marker_payload(sid: str, sandbox_id: str, generation: int) -> str:
    return json.dumps(
        {
            "sessionKey": sid,
            "sandboxId": sandbox_id,
            "generation": generation,
        }
    )


def write_marker(sbx, sid: str, sandbox_id: str, generation: int) -> bool:
    """Stamp the sandbox with the session it now belongs to.

    Returns whether it was written. A failure is not fatal — it only costs the
    ability to detect a mismatched re-attach later, and `read_marker` treats an
    absent marker as a mismatch, so the failure mode is a rebuild rather than a
    wrong filesystem.
    """
    try:
        sbx.files.write(MARKER_PATH, marker_payload(sid, sandbox_id, generation))
        return True
    except Exception as err:  # noqa: BLE001 - never fail a bind over bookkeeping
        print(f"[marker] could not write {MARKER_PATH}: {err}", flush=True)
        return False


def read_marker(sbx) -> Optional[dict]:
    """The marker inside this sandbox, or None if there is none to read."""
    try:
        raw = sbx.files.read(MARKER_PATH)
    except Exception:  # noqa: BLE001 - a missing marker is an expected answer
        return None
    if isinstance(raw, bytes):
        raw = raw.decode("utf-8", "replace")
    try:
        record = json.loads(raw)
    except (TypeError, ValueError):
        return None
    return record if isinstance(record, dict) else None


def marker_matches(sbx, sid: str) -> bool:
    """Does this sandbox's filesystem belong to `sid`?

    False for a sandbox carrying no marker as well as one carrying somebody else's.
    An unmarked sandbox is not necessarily an impostor — it could predate the
    marker, or its write could have failed — but re-attaching to a filesystem we
    cannot confirm is the risk this exists to remove, so the benefit of the doubt
    goes the other way. The cost of being wrong here is one rebuilt sandbox; the
    cost of being wrong the other way is a conversation operating on the wrong
    files without knowing it.
    """
    record = read_marker(sbx)
    if record is None:
        return False
    return record.get("sessionKey") == sid
