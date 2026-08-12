"""Browser-facing workspace server for per-user session isolation + attachments.

A harness groups its sessions by working directory, and OpenCode's run-time file
picker REQUIRES that directory to exist on disk. To give each user their own
namespace (<ROOT>/<user>) we must create the directory before a session is created
there. This server does that (`/ensure`).

It also STAGES chat attachments (`/attach`, `/attach-read`). The dashboard uploads
a text attachment's content here instead of inlining it into the prompt; we write
it to a per-session staging dir on the opencode pod's own disk (instant — no
sandbox spin-up), and hand back the path the file will ultimately have INSIDE the
agent's sandbox. The sandbox-proxy daemon later flushes the staged bytes into the
sandbox as root on the session's first tool call (see daemon.ensure_attachments).
Download/preview reads the same staged copy back (`/attach-read`).

This is the ONLY surface exposed to the browser (the sandbox-proxy daemon stays
loopback-only). Every path is built from strictly-validated, single-segment
components under the writable per-user root, so it can't traverse or touch
anything sensitive.
"""
from __future__ import annotations

import base64
import json
import os
import re
import threading
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timezone
from typing import Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

# Writable per-user session root: each user gets <ROOT>/<name>. The image creates
# ROOT owned by the runtime user, which is what lets this unprivileged process
# mkdir a user's directory inside it (it could not create one at / or under /home).
#
# The path is fixed, not configurable, and defined for the whole product in
# brain/gateway/workspace.ts — this is one of the copies that file lists, and
# user-dir.test.ts fails if the two drift. Change both together: the gateway hands
# the agent a cwd under its own copy of the root, and anything this server rejects
# here fails that turn with a 400.
ROOT = "/home/agents/u"
# A single path segment of safe chars under ROOT — no traversal, no nesting.
# Derived from ROOT so the two cannot disagree.
ALLOWED = re.compile(rf"^{re.escape(ROOT)}/[A-Za-z0-9._-]{{1,128}}$")
# opencode session id (ses_...) — a single, safe segment.
SESSION_ID = re.compile(r"^[A-Za-z0-9._-]{1,200}$")
# One path segment of a sandbox-side attachment name.
SANDBOX_SEGMENT = re.compile(r"^[A-Za-z0-9._-]{1,128}$")


def valid_sandbox_name(name: str) -> bool:
    """A file name, optionally under ONE sub-directory ("<dir>/<file>").

    The nested form lets a set of related files (a collected evidence bundle) keep
    its own folder in the sandbox instead of being flattened into long prefixed
    names that would overrun the 128-char segment cap. Each segment must be safe
    charset AND not a relative-path element: "." and ".." pass the charset (both are
    made of allowed characters) but would let a name walk out of its staging dir.
    """
    parts = name.split("/")
    if len(parts) > 2:
        return False
    return all(SANDBOX_SEGMENT.match(p) and p not in (".", "..") for p in parts)

# Where staged attachments ultimately land INSIDE the agent's sandbox. Must match
# the daemon's SBX_ATTACH_ROOT (both read the same env). A root-owned, world-
# readable location OUTSIDE the user-writable workspace, so the agent can read but
# not modify/delete its attachments.
ATTACH_ROOT = os.environ.get("SBX_ATTACH_ROOT", "/opt/agentbox/attachments").rstrip("/")
# Reject oversized text attachments (bytes of UTF-8 content). Parse defensively so
# a malformed env value falls back to the default rather than crashing on import.
def _max_attach_bytes() -> int:
    try:
        return int(float(os.environ.get("SBX_MAX_ATTACH_BYTES", "") or 8 * 1024 * 1024))
    except (TypeError, ValueError):
        return 8 * 1024 * 1024


MAX_ATTACH_BYTES = _max_attach_bytes()
# Per-session staging dir name under the user's session directory (pod-local).
STAGE_SUBDIR = ".pending-attachments"

# The loopback sandbox-proxy daemon (same pod). It is the ONLY component that
# talks to the E2B sandbox; the browser can't reach it (8765 is loopback-only),
# so the workspace-browser endpoints below proxy list/fetch through it.
DAEMON_URL = os.environ.get("SANDBOX_PROXY_URL", "http://127.0.0.1:8765").rstrip("/")


def _check_rel(path: str) -> None:
    """Reject a browser-supplied relative path that is absolute or traverses up.

    Filenames may be non-ASCII (the agent can create unicode-named files), so we
    only guard structure — no leading slash, no ".." segment. The daemon
    re-confirms the resolved path stays inside the real workspace root.
    """
    if path.startswith("/"):
        raise HTTPException(status_code=400, detail="path must be relative")
    if any(seg == ".." for seg in path.split("/")):
        raise HTTPException(status_code=400, detail="path must not traverse up")


app = FastAPI()


class EnsureRequest(BaseModel):
    dir: str


class AttachRequest(BaseModel):
    sessionID: str
    dir: str
    sandboxName: str
    content: str


class AttachReadRequest(BaseModel):
    sessionID: str
    dir: str
    sandboxName: str


class ListRequest(BaseModel):
    sessionID: str
    dir: str
    path: str = ""


class ReadFileRequest(BaseModel):
    sessionID: str
    dir: str
    path: str = ""
    mode: str = "text"


def _validate(dir_: str, session_id: str, name: str) -> None:
    if not ALLOWED.match(dir_):
        raise HTTPException(status_code=400, detail=f"dir must match {ALLOWED.pattern}")
    if not SESSION_ID.match(session_id):
        raise HTTPException(status_code=400, detail="invalid sessionID")
    if not valid_sandbox_name(name):
        raise HTTPException(status_code=400, detail="invalid sandboxName")


def _stage_dir(dir_: str, session_id: str) -> str:
    return os.path.join(dir_, STAGE_SUBDIR, session_id)


@app.get("/healthz")
def healthz() -> dict:
    return {"ok": True}


@app.post("/ensure")
def ensure(req: EnsureRequest) -> dict:
    path = req.dir
    if not ALLOWED.match(path):
        raise HTTPException(
            status_code=400,
            detail=f"dir must match {ALLOWED.pattern}",
        )
    os.makedirs(path, exist_ok=True)
    return {"ok": True, "dir": path}


@app.post("/attach")
def attach(req: AttachRequest) -> dict:
    """Stage a text attachment on the pod (instant) and return its future sandbox path.

    The bytes are NOT pushed into the sandbox here — that would trigger a
    (possibly minutes-long) cold start and block the send. The daemon flushes them
    lazily on the session's first tool call. We only persist a pod-local copy and
    compute the sandbox path from ATTACH_ROOT so the caller can reference it now.
    """
    _validate(req.dir, req.sessionID, req.sandboxName)
    if len(req.content.encode("utf-8")) > MAX_ATTACH_BYTES:
        raise HTTPException(
            status_code=413,
            detail=f"attachment exceeds {MAX_ATTACH_BYTES} bytes",
        )
    stage = _stage_dir(req.dir, req.sessionID)
    dest = os.path.join(stage, req.sandboxName)
    # sandboxName may carry one sub-directory (validated to safe segments above),
    # so create the parent rather than assuming the stage dir is it.
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    with open(dest, "w", encoding="utf-8") as f:
        f.write(req.content)
    # The sandbox path has NO session segment: a sandbox only ever serves one
    # session, so nesting by session id inside it is redundant. (Pod-side staging
    # still keys by session id — the pod is shared across a user's sessions.)
    return {
        "ok": True,
        "sandboxName": req.sandboxName,
        "path": f"{ATTACH_ROOT}/{req.sandboxName}",
    }


@app.post("/attach-read")
def attach_read(req: AttachReadRequest) -> dict:
    """Read a staged attachment back (for download / preview of a sent message).

    Reads the pod-local staged copy — the exact bytes the user uploaded — so it is
    instant and needs no sandbox. Absent (session reclaimed / pod restarted) → 404,
    which the UI surfaces as an "attachment expired" notice.
    """
    _validate(req.dir, req.sessionID, req.sandboxName)
    path = os.path.join(_stage_dir(req.dir, req.sessionID), req.sandboxName)
    try:
        with open(path, "r", encoding="utf-8") as f:
            content = f.read()
    except OSError:
        raise HTTPException(
            status_code=404,
            detail="attachment not found (session may have been reclaimed)",
        )
    return {"sandboxName": req.sandboxName, "content": content}


def _daemon_post(sid: str, endpoint: str, payload: dict) -> tuple[int, dict]:
    """Loopback-POST to the sandbox-proxy daemon and return (status, json body).

    HTTP 4xx/5xx from the daemon are returned as (code, body) so the caller can
    propagate semantics (404 inactive / 410 expired / 413 too large). Only a
    genuine connection failure raises (502) — the daemon is a same-pod sibling
    that should always be up; unreachable means a real fault, not an empty state.
    """
    url = f"{DAEMON_URL}/sessions/{sid}/{endpoint}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url, data=data, method="POST", headers={"content-type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.status, json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        try:
            body = json.loads(e.read().decode("utf-8"))
        except (ValueError, OSError):
            body = {"detail": str(e)}
        return e.code, body if isinstance(body, dict) else {"detail": str(body)}
    except (urllib.error.URLError, OSError) as e:
        raise HTTPException(status_code=502, detail=f"sandbox daemon unreachable: {e}")


@app.post("/list")
def list_workspace(req: ListRequest) -> dict:
    """List one directory of the session's agent workspace (read-only).

    Proxies to the daemon's non-creating `ls`, so opening the panel never spins
    up a sandbox: `{status: inactive}` when none exists yet, `{status: expired}`
    when it was reclaimed, else `{status: ok, entries: [...]}`.
    """
    if not ALLOWED.match(req.dir):
        raise HTTPException(status_code=400, detail=f"dir must match {ALLOWED.pattern}")
    if not SESSION_ID.match(req.sessionID):
        raise HTTPException(status_code=400, detail="invalid sessionID")
    _check_rel(req.path)
    code, body = _daemon_post(req.sessionID, "ls", {"path": req.path})
    if code >= 400:
        raise HTTPException(status_code=code, detail=body.get("detail", "list failed"))
    return body


@app.post("/read-file")
def read_workspace_file(req: ReadFileRequest) -> dict:
    """Read one workspace file for preview/download (text or base64)."""
    if not ALLOWED.match(req.dir):
        raise HTTPException(status_code=400, detail=f"dir must match {ALLOWED.pattern}")
    if not SESSION_ID.match(req.sessionID):
        raise HTTPException(status_code=400, detail="invalid sessionID")
    _check_rel(req.path)
    if req.mode not in ("text", "base64"):
        raise HTTPException(status_code=400, detail="mode must be 'text' or 'base64'")
    code, body = _daemon_post(
        req.sessionID, "fetch", {"path": req.path, "mode": req.mode}
    )
    if code >= 400:
        raise HTTPException(status_code=code, detail=body.get("detail", "read failed"))
    return body


# --- Optional tenant extensions ---------------------------------------------
# The topic-switch classifier used to be part of this module. It is a product
# feature belonging to whichever service owns the conversation, not to the sandbox
# file API, and it needs a model provider and an observability endpoint that nothing
# else here does — so it lives in its own module and is mounted only on request.
#
# Env-gated rather than import-time so the default surface carries no model
# configuration, and so a deployment that still calls POST /classify can migrate by
# setting one variable rather than by pinning an old version.
if os.environ.get("HANDS_ENABLE_TENANT_CLASSIFIER") == "1":
    from ._tenant_classifier import router as _tenant_classifier_router

    app.include_router(_tenant_classifier_router)
