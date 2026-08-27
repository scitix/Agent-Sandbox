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

"""Durable answer to one question: has this session ever had a sandbox?

Sandboxes are provisioned lazily, on a session's first tool call, and are reclaimed
when they go idle. So a tool call arriving at a freshly built sandbox means one of
two opposite things:

  * the session never had one — nothing was lost, and there is nothing to say;
  * the session's previous sandbox is gone — every file, package and bit of
    working state the conversation built is gone with it, and an agent that is not
    told will keep referring to files that no longer exist.

The manager decides which by asking here. What makes this a separate module rather
than a field is durability: the in-process session map cannot answer after a
restart, and a restart is exactly when the question gets interesting.

Note what is deliberately NOT used as the signal: how many turns the conversation
has had. A session can talk for a long time without ever calling a tool, so "this
thread has history" and "this session has had a sandbox" are different facts, and
substituting one for the other reports loss to conversations that never had
anything to lose.

The ledger answers in three states, and only one of them suppresses the notice:

  True   this is the session's first sandbox   -> stay quiet
  False  the session had one before            -> tell the agent
  None   cannot tell (no durable storage)      -> tell the agent

`None` is not a failure to handle later. Being told about a new sandbox that
happens to be the session's first costs the agent one sentence; not being told
about a replacement costs it the rest of the conversation. When the ledger cannot
answer, the caller speaks.
"""

from __future__ import annotations

import hashlib
import json
import os
import threading
import time
from typing import Optional

# Where the per-session records live. Point this at storage whose lifetime is at
# least the conversation's — in a co-located deployment that is the same volume the
# gateway keeps its thread state on, which gives exactly the right property: if the
# volume is gone the conversations are gone too, so there is no one left to notify.
#
# Unset means no ledger: every new sandbox is announced. That is the correct
# default for a deployment that has not thought about this yet.
STATE_DIR = os.environ.get("HANDS_STATE_DIR", "").strip()


class SessionLedger:
    """Records which sessions have been given a sandbox.

    Thread-safe. Every method is best-effort: storage problems degrade to "cannot
    tell" rather than raising, because a tool call must not fail over bookkeeping.
    """

    def __init__(self, state_dir: Optional[str] = None) -> None:
        self._dir = (state_dir if state_dir is not None else STATE_DIR) or ""
        self._lock = threading.Lock()
        self._warned = False

    @property
    def enabled(self) -> bool:
        return bool(self._dir)

    def _path(self, sid: str) -> str:
        # Session ids reach us from a harness and are not guaranteed to be safe
        # path segments, so hash rather than sanitise: sanitising has to be
        # reversible to stay collision-free, and nothing here needs to read the id
        # back off the filesystem.
        digest = hashlib.sha256(sid.encode("utf-8")).hexdigest()[:32]
        return os.path.join(self._dir, f"session-{digest}.json")

    def _warn_once(self, err: Exception) -> None:
        if self._warned:
            return
        self._warned = True
        print(
            f"[ledger] {self._dir!r} unusable ({err}); every new sandbox will be "
            f"announced to the agent. Set HANDS_STATE_DIR to durable storage to "
            f"suppress the notice on a session's first sandbox.",
            flush=True,
        )

    def claim_first(self, sid: str, sandbox_id: str, generation: int) -> Optional[bool]:
        """Record that `sid` has a sandbox; report whether it is the session's first.

        Returns True on the first sandbox for this session, False on a later one,
        and None when the ledger has no usable storage.

        The first-ness test and the write are the same operation: an exclusive
        create either succeeds, which proves nothing had claimed this session
        before, or raises FileExistsError, which proves something had. Reading and
        then writing would leave a window for two concurrent binds of the same
        session to both call themselves first.
        """
        if not self.enabled:
            return None
        record = {
            "sessionKey": sid,
            "sandboxId": sandbox_id,
            "generation": generation,
            "boundAt": time.time(),
        }
        path = self._path(sid)
        with self._lock:
            try:
                os.makedirs(self._dir, exist_ok=True)
                fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            except FileExistsError:
                self._update(path, record)
                return False
            except OSError as err:
                self._warn_once(err)
                return None
            try:
                with os.fdopen(fd, "w") as fh:
                    json.dump(record, fh)
                return True
            except OSError as err:
                self._warn_once(err)
                return None

    def _update(self, path: str, record: dict) -> None:
        """Overwrite an existing record, preserving the original bind time.

        Written to a sibling then renamed: a torn record would read as absent, and
        absent means "first sandbox", which is the one answer that suppresses the
        notice. A half-written file must never be able to silence it.
        """
        try:
            with open(path) as fh:
                previous = json.load(fh)
            if isinstance(previous, dict) and "boundAt" in previous:
                record = {**record, "firstBoundAt": previous.get(
                    "firstBoundAt", previous["boundAt"]
                )}
        except (OSError, ValueError):
            pass
        tmp = f"{path}.{os.getpid()}.tmp"
        try:
            with open(tmp, "w") as fh:
                json.dump(record, fh)
            os.replace(tmp, path)
        except OSError as err:
            self._warn_once(err)
            try:
                os.unlink(tmp)
            except OSError:
                pass

    def read(self, sid: str) -> Optional[dict]:
        """The stored record for a session, or None if there is none."""
        if not self.enabled:
            return None
        try:
            with open(self._path(sid)) as fh:
                record = json.load(fh)
            return record if isinstance(record, dict) else None
        except (OSError, ValueError):
            return None

    def forget(self, sid: str) -> None:
        """Drop a session's record. Called when a session is explicitly ended."""
        if not self.enabled:
            return
        with self._lock:
            try:
                os.unlink(self._path(sid))
            except OSError:
                pass
