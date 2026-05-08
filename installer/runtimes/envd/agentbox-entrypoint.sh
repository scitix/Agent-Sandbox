#!/bin/sh
# Copyright 2026 ScitiX
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

AGENTBOX_DIR=${AGENTBOX_DIR:-/mnt/agentbox}


if [ "$AGENTBOX_IS_IDLE_IMAGE" = "true" ] || [ -f /etc/agentbox_is_idle_image ]; then
    echo "[INFO] IdleImage detected. Entering sleep mode."
    exec sleep infinity
fi

create_user() {
    local username="user"
    local home_dir="/home/$username"
    local default_shell="/bin/bash"

    if id "$username" >/dev/null 2>&1; then return 0; fi
    [ ! -x "$default_shell" ] && default_shell="/bin/sh"

    local user_created=false
    if command -v useradd >/dev/null 2>&1; then
        useradd -m -s "$default_shell" "$username" 2>/dev/null && user_created=true
    fi
    if ! $user_created && command -v adduser >/dev/null 2>&1; then
        adduser -D -s "$default_shell" -h "$home_dir" "$username" 2>/dev/null && user_created=true
    fi
    if ! $user_created; then
        local uid=1000
        while grep -q ":$uid:" /etc/passwd 2>/dev/null; do uid=$((uid+1)); done
        echo "$username:x:$uid:$uid:$username:$home_dir:$default_shell" >> /etc/passwd
        mkdir -p "$home_dir"
    fi

    if [ -d /etc/sudoers.d ]; then
        echo "$username ALL=(ALL:ALL) NOPASSWD: ALL" > /etc/sudoers.d/$username 2>/dev/null || true
        chmod 440 /etc/sudoers.d/$username 2>/dev/null || true
    fi
    chmod 777 -R "$home_dir" 2>/dev/null || true
    chown -R "$username:$username" "$home_dir" 2>/dev/null || true
}

# Install a /bin/sh shim so that envd's OOM wrapper script doesn't fail.
#
# envd wraps every executed command as:
#   /bin/sh -c "echo 100 > /proc/$$/oom_score_adj && exec /usr/bin/nice -n 0 ..." -- CMD
#
# In a Kubernetes container, the cgroup controller sets oom_score_adj=999 and prevents
# any process from writing a lower value (Permission denied / I/O error), causing the
# wrapper to exit immediately with an error without running the actual command.
#
# Fix: /bin/sh is a symlink chain ending at the real shell binary (e.g. /usr/bin/dash).
# We REPLACE that final binary with a bash shim that intercepts any invocation
# containing "oom_score_adj", makes the write non-fatal (2>/dev/null ;), then
# re-executes the fixed script via bash.  All other invocations are forwarded unchanged.
#
# The shim is pure bash (no python3 dependency):
#   - sed with single-quoted replacement string keeps literal $$ from being expanded
#   - exec replaces the shim process with bash, preserving correct exit codes
install_sh_shim() {
    local real_sh
    real_sh="$(readlink -f /bin/sh 2>/dev/null)" || real_sh="/bin/sh"
    if head -1 "$real_sh" 2>/dev/null | grep -q 'bash'; then return 0; fi

    local exec_sh="/bin/bash"
    [ ! -x "$exec_sh" ] && exec_sh="/usr/bin/bash"
    [ ! -x "$exec_sh" ] && return 1

    rm -f "$real_sh"
    cat > "$real_sh" << 'SHIMEOF'
#!/bin/bash
EXEC_SH=/bin/bash
[ ! -x "$EXEC_SH" ] && EXEC_SH=/usr/bin/bash
if [ "$1" = "-c" ] && [[ "$2" == *"oom_score_adj"* ]]; then
    fixed="$(printf '%s' "$2" | sed 's|echo [0-9-]\+ > /proc/[^ ]*/oom_score_adj &&|echo 100 > /proc/$$/oom_score_adj 2>/dev/null ;|g')"
    shift 2
    exec "$EXEC_SH" -c "$fixed" "$@"
fi
exec "$EXEC_SH" "$@"
SHIMEOF
    chmod +x "$real_sh"
}

create_user || echo "Warning: user creation had issues, continuing..." >&2
mkdir -p /run/e2b 2>/dev/null || true
install_sh_shim || echo "Warning: could not install sh shim, continuing..." >&2

export GODEBUG=multipathtcp=0

# Start envd daemon (non-FC mode: stdout logs, no MMDS, no Firecracker cgroups)
exec "$AGENTBOX_DIR/envd" -isnotfc
