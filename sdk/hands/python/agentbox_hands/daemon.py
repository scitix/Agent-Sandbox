"""HTTP daemon: routes OpenCode tool calls into a remote sandbox.

Listens on loopback only (intended to run as a Pod-local sidecar).
"""
from __future__ import annotations

import base64
import os
import re
import shlex
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from .sandbox_manager import alias_session, bind_session, manager, resolve_sid


# Working directory inside the sandbox. It is resolved PER SESSION to the
# session `directory` (/home/agents/u/<user>) so the path the tools run in is
# the same path the agent is told is its cwd (see sandbox_manager.ensure_workspace
# / SessionEntry.workspace). SBX_WORKSPACE is only the fallback when that dir
# can't be resolved. Read the effective value off the session entry, never a
# module global.

# The single unprivileged identity EVERY agent-facing tool call runs as (the
# sandbox image's uid 1001 "user"). It is passed EXPLICITLY on every
# files.read/write and commands.run below so the effective uid never drifts:
# without it the e2b SDK sends no user and envd falls back to its own server-side
# default (which is version-dependent and not guaranteed to be this user). A
# consistent uid is what makes "whoever created a file can also overwrite it"
# hold — otherwise a file born under one identity can't be rewritten under
# another, and in a sticky dir like /tmp that surfaces as a create-ok /
# overwrite-EACCES split. Two operations still run as root, and neither hands the
# agent anything it could not already read: creating the session directory under
# /home/agents (which this user cannot create) and writing attachments 0444 into a
# root-owned dir. Credentials used to be the third — that write is gone, because the
# sandbox no longer holds a credential at all.
SBX_USER = os.environ.get("SBX_USER", "user")

# Hard ceiling for a single workspace-browser fetch (read-file / download). The
# frontend already gates preview by the listed size; this is the backstop that
# keeps a huge file from being pulled into the daemon's memory. Over it → 413.
def _max_fetch_bytes() -> int:
    try:
        return int(float(os.environ.get("SBX_MAX_FETCH_BYTES", "") or 25 * 1024 * 1024))
    except (TypeError, ValueError):
        return 25 * 1024 * 1024


MAX_FETCH_BYTES = _max_fetch_bytes()


def _resolve(path: str, base: str) -> str:
    if not path:
        return base
    if path.startswith("/"):
        return path
    return f"{base}/{path}"


def _run_capture(entry, cmd: str, cwd: str, timeout: int):
    """Run a command tolerating a non-zero exit code.

    The e2b SDK RAISES on any non-zero exit (that's why bash catches it and reads
    getattr(e, ...)). grep and glob legitimately exit non-zero — grep returns 1 on
    "no matches" and 2 on a bad path, glob's shell may too — so a raw commands.run
    would turn those normal outcomes into an HTTP 500. Catch the exception and
    surface the carried exit_code/stdout/stderr as a normal result instead.
    Returns (exit_code, stdout, stderr).
    """
    try:
        r = entry.sandbox.commands.run(cmd, cwd=cwd, timeout=timeout, user=SBX_USER)
        return r.exit_code, (r.stdout or ""), (r.stderr or "")
    except Exception as e:
        return (
            getattr(e, "exit_code", -1) if getattr(e, "exit_code", None) is not None else -1,
            getattr(e, "stdout", "") or "",
            getattr(e, "stderr", "") or str(e),
        )


@asynccontextmanager
async def lifespan(app: FastAPI):
    print("[daemon] starting up", flush=True)
    yield
    print("[daemon] shutting down; killing all sandboxes ...", flush=True)
    manager.shutdown()


app = FastAPI(title="sandbox-proxy", version="0.1.0", lifespan=lifespan)


# --------------------------------------------------------------------------- #
class BashIn(BaseModel):
    command: str
    cwd: Optional[str] = None
    timeout_seconds: Optional[int] = 60


class ReadIn(BaseModel):
    path: str
    offset: Optional[int] = None
    limit: Optional[int] = None


class WriteIn(BaseModel):
    path: str
    content: str


class EditIn(BaseModel):
    path: str
    old_string: str
    new_string: str
    replace_all: Optional[bool] = False


class GrepIn(BaseModel):
    pattern: str
    path: Optional[str] = None
    include: Optional[str] = None
    fixed_strings: Optional[bool] = False


class GlobIn(BaseModel):
    pattern: str
    path: Optional[str] = None


class PatchIn(BaseModel):
    patch: str
    cwd: Optional[str] = None


# --- workspace browser (read-only file explorer) --------------------------- #
class LsIn(BaseModel):
    # Path RELATIVE to the session workspace root (""/absent = the root itself).
    path: Optional[str] = ""


class FetchIn(BaseModel):
    path: Optional[str] = ""
    # "text" → utf-8 string; "base64" → base64 of the raw bytes (images/download).
    mode: Optional[str] = "text"


# --------------------------------------------------------------------------- #
class BindRequest(BaseModel):
    """Identity for a session, handed over instead of reverse-looked-up."""

    directory: str
    # Other ids that mean THIS session, because the harness addresses the daemon by
    # an id of its own (OpenCode hands its tools OpenCode's session id). Without
    # them that id is a separate session with a separate sandbox — see _ALIASES.
    aliases: Optional[List[str]] = None


@app.post("/sessions/{sid}/bind")
def bind(sid: str, req: BindRequest) -> dict:
    """Bind a session's working directory and alternative ids.

    The gateway calls this on every run. Without it the daemon falls back to an
    OpenCode-shaped loopback lookup, which under another harness fails silently and
    degrades both the working directory and the attachment flush.
    """
    bind_session(sid, req.directory)
    for alias in req.aliases or []:
        alias_session(alias, sid)
    return {
        "ok": True,
        "directory": req.directory,
        "aliases": req.aliases or [],
    }


@app.get("/healthz")
def healthz() -> dict:
    return {"ok": True}


@app.get("/sessions")
def list_sessions() -> list[dict]:
    return manager.list()


@app.delete("/sessions/{sid}")
def kill_session(sid: str) -> dict:
    return {"killed": manager.kill(sid)}


@app.get("/sessions/{sid}/canonical")
def canonical(sid: str) -> dict:
    """The canonical id for a session, resolving harness aliases.

    A pure lookup — unlike /info it creates nothing. It exists for callers that
    must name a session to a party outside this pod: the gateway binds a thread
    with the harness's own session id as an alias, so only the daemon can map
    OpenCode's `ses_…` back to the `th_…` the scheduling center bound its
    analysis job to.
    """
    return {"session_id": resolve_sid(sid)}


@app.post("/sessions/{sid}/info")
def session_info(sid: str) -> dict:
    e = manager.get_or_create(sid)
    return {**e.info(), "workspace": e.workspace(), "notice": e.take_notice()}


def _serialised(sid: str):
    return manager.session_lock(sid)


@app.post("/sessions/{sid}/bash")
def bash(sid: str, body: BashIn) -> dict:
    entry = manager.get_or_create(sid)
    notice = entry.take_notice()
    ws = entry.workspace()
    cwd = body.cwd or ws
    if not cwd.startswith("/"):
        cwd = _resolve(cwd, ws)
    timeout = int(body.timeout_seconds or 60)
    full = body.command
    with _serialised(sid):
        try:
            r = entry.sandbox.commands.run(
                full, cwd=cwd, timeout=timeout, user=SBX_USER
            )
            return {
                "exit_code": r.exit_code,
                "stdout": r.stdout,
                "stderr": r.stderr,
                "cwd": cwd,
                "notice": notice,
            }
        except Exception as e:
            return {
                "exit_code": getattr(e, "exit_code", -1),
                "stdout": getattr(e, "stdout", "") or "",
                "stderr": getattr(e, "stderr", "") or str(e),
                "cwd": cwd,
                "notice": notice,
            }


@app.post("/sessions/{sid}/read")
def read_file(sid: str, body: ReadIn) -> dict:
    entry = manager.get_or_create(sid)
    p = _resolve(body.path, entry.workspace())
    with _serialised(sid):
        try:
            content = entry.sandbox.files.read(p, user=SBX_USER)
        except Exception as e:
            raise HTTPException(404, f"read failed: {e}")
    if isinstance(content, (bytes, bytearray)):
        content = content.decode("utf-8", "replace")
    # `count` is the file's TOTAL line count, always returned: the read tool pages
    # by default and needs it to tell the caller how much is left ("showing lines
    # 1-2000 of 4312"). The whole file is already in memory, so this is free.
    lines = content.splitlines(keepends=True)
    count = len(lines)
    if body.offset or body.limit:
        start = max(0, (body.offset or 1) - 1)
        end = start + (body.limit or count)
        content = "".join(lines[start:end])
    return {
        "content": content,
        "path": p,
        "count": count,
        "notice": entry.take_notice(),
    }


@app.post("/sessions/{sid}/write")
def write_file(sid: str, body: WriteIn) -> dict:
    entry = manager.get_or_create(sid)
    p = _resolve(body.path, entry.workspace())
    with _serialised(sid):
        parent = os.path.dirname(p)
        if parent and parent != "/":
            entry.sandbox.commands.run(
                f"mkdir -p {shlex.quote(parent)}", timeout=10, user=SBX_USER
            )
        try:
            entry.sandbox.files.write(p, body.content, user=SBX_USER)
        except Exception as e:
            raise HTTPException(500, f"write failed: {e}")
    return {
        "bytes_written": len(body.content.encode("utf-8")),
        "path": p,
        "notice": entry.take_notice(),
    }


@app.post("/sessions/{sid}/edit")
def edit_file(sid: str, body: EditIn) -> dict:
    entry = manager.get_or_create(sid)
    p = _resolve(body.path, entry.workspace())
    with _serialised(sid):
        try:
            content = entry.sandbox.files.read(p, user=SBX_USER)
        except Exception as e:
            raise HTTPException(404, f"edit: read failed: {e}")
        if isinstance(content, (bytes, bytearray)):
            content = content.decode("utf-8", "replace")
        if body.replace_all:
            new_content = content.replace(body.old_string, body.new_string)
            replacements = content.count(body.old_string)
        else:
            occurrences = content.count(body.old_string)
            if occurrences == 0:
                raise HTTPException(400, "edit: old_string not found")
            if occurrences > 1:
                raise HTTPException(
                    400,
                    f"edit: old_string matched {occurrences} times; set replace_all=true to confirm",
                )
            new_content = content.replace(body.old_string, body.new_string, 1)
            replacements = 1
        try:
            entry.sandbox.files.write(p, new_content, user=SBX_USER)
        except Exception as e:
            raise HTTPException(500, f"edit: write failed: {e}")
    return {"replacements": replacements, "path": p, "notice": entry.take_notice()}


@app.post("/sessions/{sid}/grep")
def grep(sid: str, body: GrepIn) -> dict:
    entry = manager.get_or_create(sid)
    ws = entry.workspace()
    where = _resolve(body.path or "", ws)
    flag = "-F" if body.fixed_strings else "-E"
    include = f"--include={shlex.quote(body.include)}" if body.include else ""
    cmd = (
        f"grep -RIn --color=never {flag} {include} "
        f"-- {shlex.quote(body.pattern)} {shlex.quote(where)}"
    )
    with _serialised(sid):
        exit_code, stdout, _ = _run_capture(entry, cmd, cwd=ws, timeout=30)
    matches: list[dict] = []
    for line in stdout.splitlines():
        m = re.match(r"^(?P<path>[^:]+):(?P<line>\d+):(?P<text>.*)$", line)
        if m:
            matches.append({"path": m["path"], "line": int(m["line"]), "text": m["text"]})
        else:
            matches.append({"path": "", "line": 0, "text": line})
    return {"matches": matches, "exit_code": exit_code, "notice": entry.take_notice()}


# Characters that would let a glob pattern do something other than match files.
# The pattern CANNOT be quoted — a quoted word is not expanded, which is exactly
# the bug this guards replaced: `for f in '*.txt'` iterates the literal string, so
# every glob returned its own pattern back and never a real match. So instead of
# quoting, reject anything that is not glob syntax or a path.
_GLOB_FORBIDDEN = set(";&|`$()<>\n\r\"'\\")


@app.post("/sessions/{sid}/glob")
def glob_(sid: str, body: GlobIn) -> dict:
    entry = manager.get_or_create(sid)
    ws = entry.workspace()
    where = _resolve(body.path or "", ws)
    bad = sorted(_GLOB_FORBIDDEN & set(body.pattern))
    if bad:
        # A 400 rather than silent emptiness: a pattern the tool refuses to run
        # must say so, or the agent concludes the files do not exist.
        raise HTTPException(
            400,
            "glob pattern may only contain path and glob characters; "
            f"remove {''.join(bad)!r}",
        )
    cmd = (
        f'shopt -s globstar nullglob dotglob; '
        f'for f in {body.pattern}; do echo "$f"; done'
    )
    cmd_full = f"cd {shlex.quote(where)} && {cmd}"
    with _serialised(sid):
        exit_code, stdout, _ = _run_capture(entry, cmd_full, cwd=ws, timeout=20)
    paths = [p for p in stdout.splitlines() if p]
    return {"paths": paths, "exit_code": exit_code, "notice": entry.take_notice()}


@app.post("/sessions/{sid}/apply_patch")
def apply_patch(sid: str, body: PatchIn) -> dict:
    entry = manager.get_or_create(sid)
    ws = entry.workspace()
    cwd = body.cwd or ws
    if not cwd.startswith("/"):
        cwd = _resolve(cwd, ws)
    with _serialised(sid):
        tmp = f"/tmp/patch_{sid[:8]}_{int(time.time())}.patch"
        entry.sandbox.files.write(tmp, body.patch, user=SBX_USER)
        r = entry.sandbox.commands.run(
            f"cd {shlex.quote(cwd)} && patch -p1 < {shlex.quote(tmp)}",
            timeout=60,
            user=SBX_USER,
        )
        entry.sandbox.commands.run(f"rm -f {shlex.quote(tmp)}", timeout=5, user=SBX_USER)
    return {
        "exit_code": r.exit_code,
        "stdout": r.stdout,
        "stderr": r.stderr,
        "notice": entry.take_notice(),
    }


# --------------------------------------------------------------------------- #
# Workspace browser: read-only listing / fetch for the dashboard's file panel.
#
# CRITICAL: these use manager.get (NON-creating) — merely opening the workspace
# panel must never spin up a sandbox. No sandbox yet → "inactive"; a reclaimed
# one → "expired". Every path is confined to the session's workspace root, and
# every op runs as the unprivileged SBX_USER, same as the agent's own tools.
def _scoped(entry, rel: Optional[str]) -> str:
    """Resolve a browser-supplied relative path inside the workspace root.

    Rejects absolute paths and any `..` traversal that would escape the root, so
    the panel can only ever see the agent's own working directory subtree.
    """
    base = entry.workspace().rstrip("/") or "/"
    rel = (rel or "").strip()
    if not rel:
        return base
    if rel.startswith("/"):
        raise HTTPException(400, "path must be relative to the workspace root")
    candidate = os.path.normpath(os.path.join(base, rel))
    if candidate != base and not candidate.startswith(base + "/"):
        raise HTTPException(400, "path escapes the workspace root")
    return candidate


def _iso_utc(mt) -> str:
    """Serialize a modified-time as a tz-aware UTC ISO string.

    The browser renders relative/absolute time in its own timezone by parsing an
    absolute instant, so the wire value MUST carry an offset. A naive datetime
    (or the find fallback's naive local time) is assumed UTC — otherwise the
    client reads it as browser-local and shows an offset-hour skew.
    """
    if not hasattr(mt, "isoformat"):
        return str(mt) if mt else ""
    if getattr(mt, "tzinfo", None) is None:
        mt = mt.replace(tzinfo=timezone.utc)
    return mt.astimezone(timezone.utc).isoformat()


def _entry_dict(i) -> dict:
    t = getattr(i, "type", None)
    tv = getattr(t, "value", None)
    is_dir = (tv == "dir") if tv else str(t).upper().endswith("DIR")
    modified = _iso_utc(getattr(i, "modified_time", None))
    return {
        "name": i.name,
        "type": "dir" if is_dir else "file",
        "size": int(getattr(i, "size", 0) or 0),
        "modifiedTime": modified,
        "mode": getattr(i, "permissions", "") or "",
    }


def _ls_fallback(entry, target: str) -> list[dict]:
    """List a directory via `find` when the SDK's files.list is unavailable.

    Tab-delimited `-printf` keeps the name (which may contain spaces) as the last,
    single field, so parsing is unambiguous.
    """
    # %T@ = mtime as epoch seconds (timezone-independent), so the client shows the
    # correct time in its own tz regardless of the sandbox's local timezone.
    cmd = (
        f"find {shlex.quote(target)} -mindepth 1 -maxdepth 1 "
        r"-printf '%y\t%s\t%T@\t%f\n'"
    )
    _code, out, _err = _run_capture(entry, cmd, cwd=target, timeout=20)
    entries: list[dict] = []
    for line in out.splitlines():
        parts = line.split("\t", 3)
        if len(parts) != 4:
            continue
        ftype, size, epoch, name = parts
        try:
            modified = datetime.fromtimestamp(float(epoch), tz=timezone.utc).isoformat()
        except (TypeError, ValueError):
            modified = ""
        entries.append(
            {
                "name": name,
                "type": "dir" if ftype == "d" else "file",
                "size": int(size) if size.isdigit() else 0,
                "modifiedTime": modified,
                "mode": "",
            }
        )
    return entries


@app.post("/sessions/{sid}/ls")
def ls_dir(sid: str, body: LsIn) -> dict:
    entry = manager.get(sid)
    if entry is None:
        return {"status": "inactive"}
    if not manager.is_alive(entry):
        return {"status": "expired"}
    target = _scoped(entry, body.path)
    with _serialised(sid):
        try:
            infos = entry.sandbox.files.list(target, user=SBX_USER)
            entries = [_entry_dict(i) for i in infos]
        except Exception:
            entries = _ls_fallback(entry, target)
    return {"status": "ok", "entries": entries}


@app.post("/sessions/{sid}/fetch")
def fetch_file(sid: str, body: FetchIn) -> dict:
    entry = manager.get(sid)
    if entry is None:
        raise HTTPException(404, "workspace inactive")
    if not manager.is_alive(entry):
        raise HTTPException(410, "sandbox expired")
    target = _scoped(entry, body.path)
    with _serialised(sid):
        if body.mode == "base64":
            try:
                raw = entry.sandbox.files.read(target, format="bytes", user=SBX_USER)
            except Exception as e:
                raise HTTPException(404, f"read failed: {e}")
            data = bytes(raw)
            if len(data) > MAX_FETCH_BYTES:
                raise HTTPException(413, f"file exceeds {MAX_FETCH_BYTES} bytes")
            return {"content": base64.b64encode(data).decode("ascii"), "size": len(data)}
        try:
            content = entry.sandbox.files.read(target, user=SBX_USER)
        except Exception as e:
            raise HTTPException(404, f"read failed: {e}")
        if isinstance(content, (bytes, bytearray)):
            content = content.decode("utf-8", "replace")
        if len(content.encode("utf-8")) > MAX_FETCH_BYTES:
            raise HTTPException(413, f"file exceeds {MAX_FETCH_BYTES} bytes")
        return {"content": content, "size": len(content.encode("utf-8"))}
