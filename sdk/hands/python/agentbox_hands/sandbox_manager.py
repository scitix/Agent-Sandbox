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

"""Session-keyed sandbox manager. Thread-safe lazy creation.

A session's sandbox is provisioned lazily on first tool call. Pre-warming is
handled by the ScitiX Agent Sandbox pool itself (the `*-ondemand` pool keeps hot
replicas), so the daemon does no application-level pooling — it just requests a
sandbox and the platform serves one from its pool (near-instant) or cold-creates.

On bind the daemon seeds the workspace and keeps the entry until the session ends.
Sandboxes are single-use: killed when the session ends.

The sandbox holds no credential of its own. Where a deployment's agent has to
reach an authenticated service, the platform's egress injection substitutes the
real token into outbound requests, so this daemon writes nothing into the sandbox
and the agent has nothing to read even if it looks. Handing the sandbox a real
token instead — as an earlier version did, written as root under /root — makes
every process in it, and anything the agent can be talked into running, a holder
of that credential.
"""
from __future__ import annotations

import json
import os
import shlex
import threading
import time
import urllib.request
from dataclasses import dataclass, field
from typing import Dict, Optional

# patch_e2b must run before importing Sandbox.
from agent_sandbox_e2b import patch_e2b

patch_e2b(https=os.environ.get("AGBX_HTTPS", "").lower() == "true")

from e2b import Sandbox  # noqa: E402

from .seed import seed  # noqa: E402
from .session_ledger import SessionLedger  # noqa: E402
from .session_marker import marker_matches, write_marker  # noqa: E402

# Deadline handed to a re-attach, in seconds.
#
# Always sent explicitly. The E2B API applies a connect's timeout as `expiry = now +
# timeout`, and the SDK supplies a default of its own when the caller omits it — so a
# bare connect() silently SHORTENS a long-lived sandbox to that default. Re-attaching
# is supposed to be transparent, and cutting an hour-long sandbox to five minutes
# because we asked to look at it is not.
SBX_REATTACH_TIMEOUT = int(os.environ.get("SBX_TIMEOUT", "3600"))

# Environment injected into every sandbox this daemon creates, as a JSON object.
#
# It exists because a sandbox's environment is fixed at CREATE time — there is no
# way to add a variable to a running one — so anything the agent's tools need to
# find there has to be decided here, before the first tool call.
#
# Deliberately opaque to this package: it is a deployment's way of telling the
# sandbox image about itself (an API base for a CLI the image carries, a feature
# flag), and this daemon has no business knowing which keys mean what. A malformed
# value is ignored rather than fatal — the daemon's job is serving tool calls, and
# refusing to start over an unparseable extra would take that down for a variable
# nothing may even read.
#
# NEVER put a credential here. The value reaches the sandbox's own environment,
# where the agent can read it; secrets belong in the platform's egress injection,
# which substitutes them into outbound requests without the sandbox ever holding
# them.
def _sandbox_env_from_environ() -> Dict[str, str]:
    raw = os.environ.get("SBX_SANDBOX_ENV", "").strip()
    if not raw:
        return {}
    try:
        parsed = json.loads(raw)
    except ValueError as exc:
        print(f"[sandbox] ignoring unparseable SBX_SANDBOX_ENV: {exc}", flush=True)
        return {}
    if not isinstance(parsed, dict):
        print("[sandbox] ignoring SBX_SANDBOX_ENV: not a JSON object", flush=True)
        return {}
    return {str(k): str(v) for k, v in parsed.items()}


SANDBOX_ENV = _sandbox_env_from_environ()

# Liveness probing for a CACHED sandbox (distinct from the minutes-long cold-start
# readiness gate in get_or_create). A live sandbox answers its health check in
# milliseconds, so the per-tool-call probe uses a short request timeout. is_running()
# returns False on a definitively-gone sandbox (envd health 502) — that never retries.
# It RAISES on network timeout / transient unreachability — that path retries a
# bounded number of times so a brief blip does not destroy a still-live sandbox's
# state; only after the retries are exhausted is the sandbox declared dead and rebuilt.
SBX_PROBE_TIMEOUT = float(os.environ.get("SBX_PROBE_TIMEOUT", "5"))
SBX_PROBE_RETRIES = int(os.environ.get("SBX_PROBE_RETRIES", "2"))
SBX_PROBE_BACKOFF = float(os.environ.get("SBX_PROBE_BACKOFF", "1"))

# Injected at the head of the FIRST tool result served by a newly built sandbox,
# then consumed once (see SessionEntry.take_notice). English only (OSS hygiene); the
# agent relays it to the user in their language.
#
# Announced by DEFAULT, on every new sandbox, and suppressed only when the ledger
# can prove this is the session's first. The asymmetry is the point: a session that
# never had a sandbox loses one sentence to a notice it did not need, while a
# session whose sandbox was replaced and is not told spends the rest of the
# conversation reasoning about files that are not there. So the case we cannot
# distinguish is resolved by speaking.
#
# The wording therefore has to be true in both cases. It states what happened — a
# new sandbox — and what follows from it, without asserting a cause it does not
# know (idle reclaim, a restart, a pool move) or claiming that files were lost when
# there may never have been any.
NEW_SANDBOX_NOTICE = (
    "A newly created sandbox is now bound to this session. Nothing from earlier in "
    "the session exists inside it: any files written, packages installed, or "
    "processes started before now are gone. Re-create whatever you still need, and "
    "do not assume an earlier path is still valid without checking."
)




OPENCODE_URL = os.environ.get("OPENCODE_URL", "http://127.0.0.1:4096").rstrip("/")

# Identities handed to us explicitly, keyed by session id. Populated by
# POST /sessions/{sid}/bind. Checked BEFORE the loopback lookup, so a harness that
# does not serve an OpenCode-shaped API still gets the right cwd and the right
# attachment staging path.
_BOUND: Dict[str, Dict[str, str]] = {}


# Second names for the SAME sandbox, alias -> canonical session id.
#
# One sandbox has one identity: the gateway's thread id. That is what the browser
# asks about (the workspace panel, attachment staging) and what the gateway binds.
# But a harness may address the daemon by an id of its own — OpenCode's tools are
# handed OpenCode's session id, which they cannot translate — and an unaliased
# second name is a SECOND sandbox: the thread's panel reports `inactive` while the
# agent works in a sandbox nobody can see, and attachments staged under the thread
# id never flush into it. The gateway declares the harness's id as an alias when it
# binds, so both names resolve here.
_ALIASES: Dict[str, str] = {}


def alias_session(alias: str, canonical: str) -> None:
    """Make `alias` resolve to `canonical`. Self-aliases are ignored."""
    if alias and alias != canonical:
        _ALIASES[alias] = canonical


def resolve_sid(sid: str) -> str:
    """The canonical session id for any name the daemon is addressed by."""
    return _ALIASES.get(sid, sid)


def bind_session(sid: str, directory: str) -> None:
    """Record a session's identity. Idempotent; last write wins."""
    _BOUND[sid] = {"directory": directory}


def unbind_session(sid: str) -> None:
    sid = resolve_sid(sid)
    _BOUND.pop(sid, None)
    for alias in [a for a, c in _ALIASES.items() if c == sid]:
        _ALIASES.pop(alias, None)


def bound_session(sid: str) -> Optional[Dict[str, str]]:
    return _BOUND.get(resolve_sid(sid))


def _session_directory(sid: str) -> Optional[str]:
    """Full working directory for a session.

    An explicit bind wins; otherwise fall back to the loopback lookup that the
    OpenCode path has always used (opencode returns the session's directory on a
    plain GET and isolates users under /home/agents/u/<user>). Returns None on
    any failure so callers fall back to the default.
    """
    sid = resolve_sid(sid)
    bound = _BOUND.get(sid)
    if bound and bound.get("directory"):
        return bound["directory"]
    try:
        req = urllib.request.Request(f"{OPENCODE_URL}/session/{sid}")
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.load(resp)
        directory = data.get("directory") if isinstance(data, dict) else None
        return directory or None
    except Exception:
        return None


def _session_user(sid: str) -> Optional[str]:
    """Best-effort basename (<user> segment) of a session's opencode directory."""
    directory = _session_directory(sid)
    return directory.rsplit("/", 1)[-1] if directory else None


# --- chat attachments (proposal 0055) ---------------------------------------
# The dashboard stages a text attachment's bytes on the opencode pod (fs.py
# /attach) instead of inlining them into the prompt, and the message carries only
# a one-line reference to the file's future sandbox path. We flush the staged
# bytes into the sandbox on the session's FIRST tool call (see
# SandboxManager.ensure_attachments) — writing them as root into a world-readable
# root-owned dir OUTSIDE the user-writable workspace, so the agent (SBX_USER) can
# read but not modify or delete its attachments.
STAGE_SUBDIR = ".pending-attachments"


def _attach_root() -> str:
    """Root-owned, world-readable dir the attachments land in (matches fs.py)."""
    return os.environ.get("SBX_ATTACH_ROOT", "/opt/agentbox/attachments").rstrip("/")


def _staged_names(stage: str) -> list[str]:
    """Staged attachment names relative to `stage`, one directory level deep.

    fs.py accepts an attachment name of the form "<dir>/<file>" so a bundle of
    related files (a collected evidence set) keeps its own folder in the sandbox;
    a flat listdir would return the folder itself and try to write it as a file.
    Depth is capped at one because that is exactly what the name validator allows.
    """
    out: list[str] = []
    for entry in os.listdir(stage):
        path = os.path.join(stage, entry)
        if os.path.isdir(path):
            out.extend(f"{entry}/{child}" for child in sorted(os.listdir(path)))
        else:
            out.append(entry)
    return out


# --- working directory alignment --------------------------------------------
# The harness presents each session's `directory` (/home/agents/u/<user>) to the
# agent as its working directory, but the agent's file/shell tools actually run
# in the remote sandbox. To keep the two path namespaces identical — so a path
# the agent reasons about is the same path the sandbox sees — we make the
# sandbox's working directory THAT SAME opencode `directory`, creating it in the
# sandbox on bind (ensure_workspace). SBX_WORKSPACE is only the fallback used
# when the opencode directory can't be resolved (e.g. loopback lookup failed).
SBX_USER = os.environ.get("SBX_USER", "user")


def _workspace_fallback() -> str:
    """Fixed sandbox workspace used only when the opencode dir is unresolvable."""
    return os.environ.get("SBX_WORKSPACE", "/tmp/workspace").rstrip("/") or "/"


def _sandbox_envs(_sid: str) -> Dict[str, str]:
    """Environment injected into a session's sandbox at create time (e2b `envs`).

    Copied on every call so a caller cannot mutate the parsed value and change
    what every later sandbox is created with. The session id is accepted but
    unused: sandbox environment is a deployment-level decision, and making it
    per-session would mean two sandboxes of one deployment differing in a way
    nothing records.
    """
    return dict(SANDBOX_ENV)


@dataclass
class SessionEntry:
    sid: str
    sandbox: Sandbox
    created_at: float = field(default_factory=time.time)
    # Set to NEW_SANDBOX_NOTICE unless the ledger proved this is the session's first
    # sandbox. take_notice() reads-and-clears it so exactly one tool result carries
    # it — a notice repeated on every result reads as a live warning rather than a
    # one-time fact, and the agent starts re-checking paths it already checked.
    pending_notice: Optional[str] = None
    # Attachment file names already flushed into this sandbox (proposal 0055). A
    # fresh entry (incl. one born from a rebuild) starts empty, so its first tool
    # call re-flushes every staged attachment from the pod's staging dir.
    flushed: set = field(default_factory=set)
    # The session's opencode `directory`, resolved once and cached so the
    # per-tool-call attachment check needs no repeated loopback lookup. Also the
    # sandbox's working directory (see ensure_workspace / workspace()).
    directory: Optional[str] = None
    # True once the working directory has been created in the sandbox. A fresh
    # entry (incl. one born from a rebuild) starts False, so the next bind
    # re-creates the dir in the new sandbox.
    workspace_ready: bool = False
    _notice_lock: threading.Lock = field(
        default_factory=threading.Lock, repr=False, compare=False
    )

    def workspace(self) -> str:
        """Effective sandbox working directory: the opencode dir, else fallback."""
        return self.directory or _workspace_fallback()

    def take_notice(self) -> Optional[str]:
        """Return the one-shot rebuild notice, clearing it (consume-once)."""
        with self._notice_lock:
            n = self.pending_notice
            self.pending_notice = None
            return n

    def set_notice(self, msg: str) -> None:
        """Append a one-shot notice (kept until the next tool result consumes it)."""
        with self._notice_lock:
            self.pending_notice = (
                f"{self.pending_notice}\n{msg}" if self.pending_notice else msg
            )

    def info(self) -> dict:
        return {
            "session_id": self.sid,
            "sandbox_id": self.sandbox.sandbox_id,
            "created_at": self.created_at,
            "uptime_s": round(time.time() - self.created_at, 1),
        }


class SandboxManager:
    def __init__(self, ledger: Optional[SessionLedger] = None) -> None:
        self._sessions: Dict[str, SessionEntry] = {}
        self._lock = threading.Lock()
        self._session_locks: Dict[str, threading.Lock] = {}
        self._ledger = ledger if ledger is not None else SessionLedger()
        # How many sandboxes each session has been through. In-process only, so it
        # restarts at zero — it labels metadata and logs, and nothing decides
        # anything from it (the ledger owns the decision that matters).
        self._generation: Dict[str, int] = {}
        # Classification tallies. This bug was able to sit in production silently
        # because nothing counted it: the rate of `replaced` is a direct reading of
        # how many conversations are losing their files right now, and `unknown` is
        # how many are getting a notice only because no ledger is configured.
        self.counters: Dict[str, int] = {
            "first": 0,
            "replaced": 0,
            "unknown": 0,
            "reattached": 0,
        }

    def _count(self, outcome: str) -> None:
        with self._lock:
            self.counters[outcome] = self.counters.get(outcome, 0) + 1

    def _sandbox_metadata(self, sid: str, generation: int) -> Dict[str, str]:
        """Metadata stamped on the sandbox at creation.

        Carried so a sandbox can be traced back to the conversation and tenant that
        caused it without consulting this process — which matters both for operators
        reading a sandbox list and for any future replica that has no memory of the
        session. `hands.` prefixed to stay clear of the platform's reserved
        `agentbox.scitix.ai/*` keys, which the API strips out.
        """
        meta = {
            "hands.session": sid,
            "hands.generation": str(generation),
        }
        user = _session_user(sid)
        if user:
            meta["hands.user"] = user
        team = os.environ.get("HANDS_TEAM", "").strip()
        if team:
            meta["hands.team"] = team
        return meta

    def _template(self) -> str:
        # The agent-sandbox environment the daemon launches from, written
        # verbatim as AGBX_ENV_NAME: a bare pool ("agents") or a cluster-scoped
        # pool ("worker-a::agents-1c2gi") -- the "<cluster>::" prefix is
        # optional now that some gateways no longer require a cluster id.
        # Backward-compat: older deployments (and existing bring-your-own
        # Secrets) set AGBX_CLUSTER_ID + AGBX_POOL_NAME instead; synthesize the
        # env name from them when AGBX_ENV_NAME is absent.
        env = os.environ.get("AGBX_ENV_NAME", "").strip()
        if not env:
            cluster = os.environ.get("AGBX_CLUSTER_ID", "").strip()
            pool = os.environ.get("AGBX_POOL_NAME", "").strip()
            env = f"{cluster}::{pool}" if cluster and pool else pool
        if not env:
            raise RuntimeError(
                "AGBX_ENV_NAME (or AGBX_POOL_NAME) must be set in the environment"
            )
        # Optional pinned image. A member pool's default image does not run the
        # sandbox command endpoint, so an agent whose deployment sets no image
        # gets sandboxes that come up and then refuse every command.
        # The pool injects envd/tini at /mnt/agentbox regardless of base image,
        # so any glibc image with the matching UID 1001 layout works.
        image = os.environ.get("SBX_IMAGE", "").strip()
        if image:
            return f"{env}//{image}"
        return env

    def _resolve_directory(self, sid: str, entry: SessionEntry) -> Optional[str]:
        """Resolve and cache the session's opencode `directory` (loopback once)."""
        if entry.directory is None:
            entry.directory = _session_directory(sid)
        return entry.directory

    def ensure_workspace(self, sid: str, entry: SessionEntry) -> None:
        """Create the session's working directory inside the sandbox (as SBX_USER).

        The working directory is the session `directory` (/home/agents/u/<user>)
        so the sandbox path the tools run in equals the path the agent is told is
        its cwd. It does not exist in a fresh sandbox image, so we mkdir it here on
        bind (once per sandbox; the dir then persists for the sandbox's life).
        Falls back to SBX_WORKSPACE when the session dir can't be resolved.
        """
        if entry.workspace_ready:
            return
        self._resolve_directory(sid, entry)
        ws = entry.workspace()
        try:
            # mkdir as ROOT: the session dir lives under /home/agents, which the
            # unprivileged user cannot create (only /home/user exists in the image).
            # Then chown the leaf to SBX_USER so the agent owns and can write its own
            # working directory (intermediate dirs stay root-owned 0755 — traversable).
            entry.sandbox.commands.run(
                f"mkdir -p {shlex.quote(ws)} && "
                f"chown {shlex.quote(SBX_USER)} {shlex.quote(ws)}",
                user="root",
                timeout=20,
            )
            entry.workspace_ready = True
        except Exception as e:
            print(f"[sbxmgr] ensure_workspace mkdir {ws} failed: {e}", flush=True)
            return
        # Convenience symlinks in the agent's cwd (as SBX_USER, so they're owned by
        # the agent): the baked-in read-only Volcano source and the attachments dir,
        # reachable as ./volcano and ./attachments. Best-effort — a failure here must
        # not block the workspace. Idempotent (`ln -sfn`), recreated per sandbox.
        attach = _attach_root()
        try:
            entry.sandbox.commands.run(
                f"ln -sfn /opt/volcano {shlex.quote(ws + '/volcano')}; "
                f"ln -sfn {shlex.quote(attach)} {shlex.quote(ws + '/attachments')}",
                user=SBX_USER,
                timeout=20,
            )
        except Exception as e:
            print(f"[sbxmgr] ensure_workspace symlinks failed: {e}", flush=True)

    def ensure_attachments(self, sid: str, entry: SessionEntry) -> None:
        """Flush any not-yet-synced staged attachments into the sandbox as root.

        Called after get_or_create on every route, so the FIRST tool call after an
        attachment was staged blocks once while the file is written; subsequent
        calls see nothing new to flush and return immediately. Sessions with no
        attachments do a single cheap listdir and return — zero extra latency.

        Files are written as root into a world-readable, root-owned dir outside the
        user-writable workspace (0755 dir / 0444 files), so the agent (SBX_USER)
        can read them but cannot modify or delete them.
        """
        directory = self._resolve_directory(sid, entry)
        if not directory:
            return
        stage = os.path.join(directory, STAGE_SUBDIR, sid)
        try:
            names = _staged_names(stage)
        except OSError:
            return
        todo = [n for n in names if n not in entry.flushed]
        if not todo:
            return
        # No session segment in the sandbox: one sandbox = one session, so the
        # attachments live flat under the root (pod-side staging still keys by sid).
        # A name may itself carry one sub-directory (an evidence bundle keeps its own
        # folder), so each dest's parent is created rather than just the root.
        dest_dir = _attach_root()
        parents = {os.path.dirname(f"{dest_dir}/{n}") for n in todo}
        try:
            entry.sandbox.commands.run(
                " && ".join(
                    f"mkdir -p {shlex.quote(p)} && chmod 0755 {shlex.quote(p)}"
                    for p in sorted(parents)
                ),
                user="root",
                timeout=20,
            )
        except Exception as e:
            print(f"[sbxmgr] attach: mkdir {sorted(parents)} failed: {e}", flush=True)
        for name in todo:
            try:
                with open(os.path.join(stage, name), "r", encoding="utf-8") as f:
                    data = f.read()
            except OSError as e:
                print(f"[sbxmgr] attach: read staged {name} failed: {e}", flush=True)
                continue
            dest = f"{dest_dir}/{name}"
            try:
                entry.sandbox.files.write(dest, data, user="root")
                entry.sandbox.commands.run(
                    f"chmod 0444 {shlex.quote(dest)}", user="root", timeout=20
                )
                entry.flushed.add(name)
                print(f"[sbxmgr] attach: flushed {dest}", flush=True)
            except Exception as e:
                print(f"[sbxmgr] attach: write {dest} failed: {e}", flush=True)
                entry.set_notice(
                    f"attachment '{name}' could not be prepared in the sandbox: {e}"
                )

    def _reattach(self, sid: str) -> Optional[SessionEntry]:
        """Re-adopt the sandbox the ledger says this session had, if it is still there.

        Called when this process has no memory of a session — after a restart, or on
        a replica that has never served it. Without this, a rollout rebuilds every
        in-flight conversation's sandbox and discards working state that was never
        in danger; announcing that loss accurately is worth less than not causing it.

        Three things have to hold, and each rules out a different way of getting
        this wrong:
          * the ledger has a sandbox id to try at all;
          * attaching succeeds AND the sandbox answers a liveness probe — the attach
            alone is not enough, because the API answers it from a historical record
            once the sandbox is gone, returning a handle that only fails later;
          * the marker inside it names this session — the id could have been reused,
            or the sandbox replaced, and a filesystem that is not the one the
            conversation left is the failure this cannot be allowed to cause.

        Any of them failing returns None, and the caller builds a fresh sandbox and
        announces it. That is the pre-existing behaviour, so the worst outcome here
        is the old outcome.
        """
        record = self._ledger.read(sid)
        sandbox_id = (record or {}).get("sandboxId")
        if not sandbox_id:
            return None
        try:
            sbx = Sandbox.connect(sandbox_id, timeout=SBX_REATTACH_TIMEOUT)
        except Exception as err:  # noqa: BLE001 - any failure means "build a new one"
            print(
                f"[sbxmgr] cannot re-attach sid={sid} to {sandbox_id}: {err}",
                flush=True,
            )
            return None

        entry = SessionEntry(sid=sid, sandbox=sbx)
        if not self._alive(entry):
            print(
                f"[sbxmgr] re-attached sid={sid} to {sandbox_id} but it is not "
                f"responsive; rebuilding",
                flush=True,
            )
            return None
        if not marker_matches(sbx, sid):
            print(
                f"[sbxmgr] {sandbox_id} does not carry sid={sid}'s marker; its "
                f"filesystem is not this session's. Rebuilding",
                flush=True,
            )
            return None
        print(f"[sbxmgr] re-attached sid={sid} to {sandbox_id}", flush=True)
        return entry

    def _get_session_lock(self, sid: str) -> threading.Lock:
        sid = resolve_sid(sid)
        with self._lock:
            return self._session_locks.setdefault(sid, threading.Lock())

    def _alive(self, entry: SessionEntry) -> bool:
        """Probe a cached sandbox. False => rebuild.

        is_running() returns False on a definitively-gone sandbox (envd 502) — no
        retry. It raises on timeout / transient unreachability — retried up to
        SBX_PROBE_RETRIES times with linear backoff before we give up and declare
        it dead, so a brief network blip doesn't needlessly destroy live state.
        """
        attempts = SBX_PROBE_RETRIES + 1
        for i in range(attempts):
            try:
                return entry.sandbox.is_running(request_timeout=SBX_PROBE_TIMEOUT)
            except Exception as e:
                if i < attempts - 1:
                    time.sleep(SBX_PROBE_BACKOFF * (i + 1))
                    continue
                print(
                    f"[sbxmgr] liveness probe for sid={entry.sid} failed after "
                    f"{attempts} attempts: {e}",
                    flush=True,
                )
                return False
        return False

    def _evict(self, sid: str, entry: SessionEntry) -> None:
        with self._lock:
            # Drop only if still the same entry (the caller holds this sid's session
            # lock, so a concurrent rebuild of the same sid cannot race here).
            if self._sessions.get(sid) is entry:
                self._sessions.pop(sid, None)
        try:
            entry.sandbox.kill()
        except Exception:
            pass

    def get_or_create(self, sid: str) -> SessionEntry:
        # Resolve FIRST: every id below (the lock, the cache key, the staging dir
        # ensure_attachments reads) has to be the same one the browser uses.
        sid = resolve_sid(sid)
        slock = self._get_session_lock(sid)
        with slock:
            with self._lock:
                entry = self._sessions.get(sid)
            if entry is not None:
                if self._alive(entry):
                    self.ensure_workspace(sid, entry)
                    self.ensure_attachments(sid, entry)
                    self._count("reattached")
                    return entry
                # Idle-timeout release (or persistent unreachability): the cached
                # sandbox is gone. Evict it and fall through to a fresh build so the
                # session self-heals instead of wedging on a dead handle.
                print(
                    f"[sbxmgr] sandbox for sid={sid} not alive; rebuilding ...",
                    flush=True,
                )
                self._evict(sid, entry)
            else:
                # Nothing in memory for this session. Before building, check whether
                # the sandbox it already had is still running — this process may
                # simply have restarted underneath a live conversation.
                adopted = self._reattach(sid)
                if adopted is not None:
                    with self._lock:
                        self._sessions[sid] = adopted
                    self.ensure_workspace(sid, adopted)
                    self.ensure_attachments(sid, adopted)
                    self._count("reattached")
                    return adopted
            with self._lock:
                generation = self._generation.get(sid, 0) + 1
                self._generation[sid] = generation
            envs = _sandbox_envs(sid)
            # The injected KEYS are logged, never their values: this is a place a
            # deployment could put something it should not, and a log line is the
            # one copy nobody thinks to redact.
            print(
                f"[sbxmgr] creating sandbox for sid={sid} gen={generation} "
                f"(envs={sorted(envs)}) ...",
                flush=True,
            )
            sbx = Sandbox.create(
                template=self._template(),
                envs=envs,
                metadata=self._sandbox_metadata(sid, generation),
                secure=False,
                timeout=int(os.environ.get("SBX_TIMEOUT", "3600")),
            )
            print(f"[sbxmgr]   sandbox_id={sbx.sandbox_id}", flush=True)
            # Readiness gate: poll is_running() until the pool reports the
            # sandbox live (the documented AgentBox pattern). Custom images can
            # take multiple minutes for a cold image pull, so SBX_READY_TIMEOUT
            # bumps the ceiling. Any brief envd warm-up after is_running() flips
            # true is absorbed by the seed retry loop below.
            ready_timeout = int(os.environ.get("SBX_READY_TIMEOUT", "300"))
            deadline = time.time() + ready_timeout
            ready = False
            while time.time() < deadline:
                try:
                    if sbx.is_running():
                        ready = True
                        break
                except Exception:
                    pass
                time.sleep(2)
            if not ready:
                try:
                    sbx.kill()
                except Exception:
                    pass
                raise RuntimeError(
                    f"sandbox {sbx.sandbox_id} never became responsive"
                )
            # Seed with retries. If it still fails, kill the sandbox so the next
            # tool call lazily provisions a fresh one — never store a
            # half-initialised entry in the session map.
            last_err: Optional[Exception] = None
            for attempt in range(1, 4):
                try:
                    seed(sbx)
                    print(
                        f"[sbxmgr]   seeded sid={sid} (attempt {attempt})",
                        flush=True,
                    )
                    last_err = None
                    break
                except Exception as e:
                    last_err = e
                    print(
                        f"[sbxmgr]   seed attempt {attempt}/3 failed: {e}",
                        flush=True,
                    )
                    time.sleep(2 * attempt)
            if last_err is not None:
                try:
                    sbx.kill()
                except Exception:
                    pass
                raise RuntimeError(
                    f"sandbox {sbx.sandbox_id} seed failed after 3 attempts: {last_err}"
                ) from last_err

            entry = SessionEntry(sid=sid, sandbox=sbx)
            # Stamp the sandbox with the session that now owns it, so a later
            # re-attach can tell this filesystem apart from a reused id.
            write_marker(sbx, sid, sbx.sandbox_id, generation)
            # Announce the new sandbox unless the ledger can prove this is the
            # session's first. `claim_first` is deliberately not consulted before
            # the sandbox exists: it records a binding, and recording one for a
            # create that then fails its readiness gate would make the retry look
            # like a replacement.
            first = self._ledger.claim_first(sid, sbx.sandbox_id, generation)
            if first is True:
                self._count("first")
            else:
                entry.pending_notice = NEW_SANDBOX_NOTICE
                self._count("replaced" if first is False else "unknown")
            print(
                f"[sbxmgr]   sid={sid} gen={generation} "
                f"first={first} notice={'yes' if entry.pending_notice else 'no'}",
                flush=True,
            )
            with self._lock:
                self._sessions[sid] = entry
            # Create the working directory (opencode dir, aligned to what the agent
            # is told is its cwd), then flush staged attachments into the freshly-
            # built sandbox (empty `flushed` set → all staged files are (re)synced,
            # incl. after a rebuild).
            self.ensure_workspace(sid, entry)
            self.ensure_attachments(sid, entry)
            return entry

    def get(self, sid: str) -> Optional[SessionEntry]:
        with self._lock:
            return self._sessions.get(resolve_sid(sid))

    def is_alive(self, entry: SessionEntry) -> bool:
        """Public liveness probe for a cached entry (read-only browser access).

        Lets the workspace browser distinguish a still-live sandbox from one the
        pool has reclaimed, WITHOUT the rebuild that get_or_create would trigger.
        """
        return self._alive(entry)

    def kill(self, sid: str) -> bool:
        sid = resolve_sid(sid)
        unbind_session(sid)
        with self._lock:
            entry = self._sessions.pop(sid, None)
        if entry is None:
            return False
        try:
            entry.sandbox.kill()
        except Exception as e:
            print(f"[sbxmgr] kill({sid}) error: {e}", flush=True)
        return True

    def list(self) -> list[dict]:
        with self._lock:
            return [e.info() for e in self._sessions.values()]

    def shutdown(self) -> None:
        with self._lock:
            entries = list(self._sessions.values())
            self._sessions.clear()
        for e in entries:
            try:
                e.sandbox.kill()
            except Exception:
                pass

    def session_lock(self, sid: str) -> threading.Lock:
        return self._get_session_lock(sid)


manager = SandboxManager()
