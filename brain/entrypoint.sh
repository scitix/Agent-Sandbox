#!/bin/bash
# The Brain container's process contract.
#
# Four processes, one lifetime. If any of them dies the rest are killed and the
# container exits non-zero so Kubernetes restarts the whole thing:
#
#   sandbox daemon  127.0.0.1:8765  binds a session to a sandbox and serves the
#                                   agent's tool calls. LOOPBACK ONLY — it holds
#                                   the sandbox API credential and takes the
#                                   caller's word for which session is asking.
#   workspace-fs    0.0.0.0:8766    attachment staging and the workspace file
#                                   browser. The one process here the browser
#                                   reaches directly.
#   opencode serve  127.0.0.1:4096  the OpenCode harness, when it is not
#                                   withdrawn. Claude Code needs no port: it is
#                                   the Claude Agent SDK running IN-PROCESS
#                                   inside the gateway.
#   gateway         0.0.0.0:4099    threads, turns and the event stream. The
#                                   pod's public entry and its health authority.
#
# Killing them together is deliberate. A pod that has lost its daemon still
# answers /healthz on the gateway and still accepts turns — every one of which
# fails on its first tool call, with no signal anywhere that the pod is the
# problem. Better to restart.
set -uo pipefail

BRAIN_HOME=${HOME:-/home/agents}
GATEWAY_DIR=${BRAIN_GATEWAY_DIR:-$BRAIN_HOME/gateway}

# --- persistent state --------------------------------------------------------
# One volume, and every owner's path derived from the SAME variable its owner
# reads. A path prepared here that its owner does not read is worse than useless:
# the owner falls back to a default on the container filesystem, keeps working,
# and loses everything on the next restart.
#
#   $OPENCODE_DB            OpenCode's SQLite file: sessions, messages, parts.
#                           WAL mode, so -wal/-shm sit beside it.
#   $CLAUDE_CONFIG_DIR      Claude Code's config dir: one JSONL transcript per
#                           session under projects/<cwd-slug>/.
#   $ASSISTANT_THREAD_STORE The gateway's thread map. EVERY backend builds its
#                           history list from it, so losing this loses the
#                           history even when the transcripts survive.
#
# All three must resolve UNDER the mounted volume. The controller renders them
# that way; these defaults match, for a container run outside Kubernetes.
STATE_DIR=${AGENTBOX_STATE_DIR:-$BRAIN_HOME/state}
OC_DB=${OPENCODE_DB:-$STATE_DIR/opencode/opencode.db}
CC_CONFIG_DIR=${CLAUDE_CONFIG_DIR:-$STATE_DIR/claude}
THREAD_STORE=${ASSISTANT_THREAD_STORE:-$STATE_DIR/gateway/threads.json}

# EXPORTED, not merely resolved. Each default above is only real if the child
# process that reads it can see it: deriving the path, preparing the directory and
# then leaving the variable unset means the owner falls back to its own default on
# the container filesystem — which is exactly what the OpenCode assertion below
# caught the first time this image ran outside Kubernetes.
export OPENCODE_DB="$OC_DB"
export CLAUDE_CONFIG_DIR="$CC_CONFIG_DIR"
export ASSISTANT_THREAD_STORE="$THREAD_STORE"

if ! mkdir -p "$(dirname "$OC_DB")" "$CC_CONFIG_DIR" "$(dirname "$THREAD_STORE")"; then
    echo "[entrypoint] FATAL: cannot create the state directories under $STATE_DIR." >&2
    echo "[entrypoint] The volume must be writable by uid $(id -u): for a local" >&2
    echo "[entrypoint] volume that is the prepare hook's chown, otherwise fsGroup." >&2
    exit 1
fi
# 0700: the transcripts are conversations. Only the runtime user reads them.
chmod 0700 "$CC_CONFIG_DIR" "$(dirname "$THREAD_STORE")" 2>/dev/null || true
echo "[entrypoint] state: db=$OC_DB claude=$CC_CONFIG_DIR threads=$THREAD_STORE"

# Every process that creates a user's directory inside this root is
# unprivileged, so the root itself has to exist and be owned by the runtime
# user. The image does that; this is the safety net for a deployment that
# mounts a volume over it.
USER_DIR_ROOT=/home/agents/u
mkdir -p "$USER_DIR_ROOT" || {
    echo "[entrypoint] FATAL: $USER_DIR_ROOT is not writable by uid $(id -u);" >&2
    echo "[entrypoint] no session can get a workspace." >&2
    exit 1
}

# --- sandbox daemon ----------------------------------------------------------
echo "[entrypoint] starting sandbox daemon on 127.0.0.1:8765"
python3 -m uvicorn agentbox_hands.daemon:app \
    --host 127.0.0.1 --port 8765 --workers 1 &
SBX_PID=$!

echo "[entrypoint] starting workspace-fs on 0.0.0.0:8766"
python3 -m uvicorn agentbox_hands.fs:app \
    --host 0.0.0.0 --port 8766 --workers 1 &
FS_PID=$!

# Wait for the daemon's health check so the first tool call of the first turn
# does not race startup. Best effort with a 30s ceiling: if it never comes up we
# still launch the rest and let the failure surface as a failed tool call, which
# is more informative than a container that exits before it logs anything.
for _ in $(seq 1 30); do
    if python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8765/healthz', timeout=1)" >/dev/null 2>&1; then
        echo "[entrypoint] sandbox daemon healthy"
        break
    fi
    sleep 1
done

# --- harnesses ---------------------------------------------------------------
# Which harness a conversation starts under when the browser expresses no
# preference. It does NOT decide what runs here: the gateway serves every
# harness it is configured for, and the browser has a picker.
ASSISTANT_BACKEND=${ASSISTANT_BACKEND:-opencode}
ASSISTANT_GATEWAY_PORT=${ASSISTANT_GATEWAY_PORT:-4099}
# Read from the same variable the gateway resolves its base URL from. Writing
# the port out twice is how the two would come to disagree.
OPENCODE_INTERNAL_PORT=${OPENCODE_INTERNAL_PORT:-4096}
OC_PID=""

# The OpenCode server starts whenever the harness is not withdrawn — NOT only
# when it is the default. Gating it on being the default contradicts the gateway
# serving every configured harness: picking OpenCode in the browser of a
# claude-code-default pod would hit a port nobody listens on.
#
# `ASSISTANT_OC_ENABLED=0` withdraws it, and the gateway reads the same variable,
# so the picker and this launch decision cannot disagree.
if [ "${ASSISTANT_OC_ENABLED:-1}" != "0" ] &&
    [ "${ASSISTANT_OC_ENABLED:-1}" != "false" ]; then
    echo "[entrypoint] starting opencode serve on 127.0.0.1:${OPENCODE_INTERNAL_PORT}"
    opencode serve --hostname 127.0.0.1 --port "$OPENCODE_INTERNAL_PORT" \
        --print-logs --log-level INFO &
    OC_PID=$!

    # Assert that OPENCODE_DB actually moved the session database onto the
    # volume. The failure this exists for is silent: OpenCode keeps working,
    # writing every session to the container filesystem instead, and the loss
    # only surfaces as an empty history after the next restart. It costs nothing
    # on a healthy boot, and it only reports what it can prove — a database
    # appearing at the DEFAULT path while the configured one stays absent.
    DEFAULT_OC_DB="$BRAIN_HOME/.local/share/opencode/opencode.db"
    if [ "$OC_DB" != "$DEFAULT_OC_DB" ]; then
        for _ in $(seq 1 15); do
            [ -e "$OC_DB" ] && break
            if [ -e "$DEFAULT_OC_DB" ]; then
                echo "[entrypoint] FATAL: OpenCode opened $DEFAULT_OC_DB," >&2
                echo "[entrypoint] not $OC_DB — OPENCODE_DB had no effect, so no" >&2
                echo "[entrypoint] conversation would survive a restart." >&2
                kill -TERM "$OC_PID" 2>/dev/null
                exit 1
            fi
            sleep 1
        done
        if [ ! -e "$OC_DB" ]; then
            # Neither path exists yet, so nothing is proven either way. Not a
            # reason to fail the pod — but the first thing to check if history
            # later comes back empty.
            echo "[entrypoint] NOTE: $OC_DB not created yet; OpenCode may open" \
                "it lazily on the first session"
        fi
    fi
fi

# --- gateway -----------------------------------------------------------------
# It preflights every configured harness BEFORE it binds, and refuses to start
# only when the gateway itself cannot come up. A harness that fails preflight is
# reported to the picker with its reason — one missing model credential must not
# take down attachment upload and the file browser with it.
echo "[entrypoint] starting gateway (default backend=$ASSISTANT_BACKEND) on 0.0.0.0:${ASSISTANT_GATEWAY_PORT}"
ASSISTANT_GATEWAY_PORT="$ASSISTANT_GATEWAY_PORT" \
    bun "$GATEWAY_DIR/main.ts" &
GW_PID=$!

# Forward signals; on TERM/INT kill the children and wait.
trap 'echo "[entrypoint] received signal, terminating"; kill -TERM "$SBX_PID" "$FS_PID" $OC_PID "$GW_PID" 2>/dev/null; wait; exit 0' TERM INT

# Block until ANY child exits, then take the rest down with it.
wait -n
EXIT_CODE=$?
echo "[entrypoint] a child exited with status $EXIT_CODE; terminating the others"
kill -TERM "$SBX_PID" "$FS_PID" $OC_PID "$GW_PID" 2>/dev/null || true
wait
exit "$EXIT_CODE"
