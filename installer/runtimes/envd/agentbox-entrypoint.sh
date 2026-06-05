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

# Persist the image ENV for exec'd commands.
#
# The E2B SDK wraps every command as `/bin/bash -l -c CMD` (login shell), and
# Debian's /etc/profile unconditionally RESETS PATH at login — so the image's
# ENV PATH (e.g. Go in /usr/local/go/bin, nvm-installed node) never reaches
# exec'd commands, even though envd passes it correctly into the process env.
# Furthermore, envd only forwards PATH/HOME/USER/LOGNAME from its own
# environment, so other image ENV vars (GOPATH, PYTHONPATH, ...) are lost too.
#
# Fix: this entrypoint IS the container's PID 1 lineage, so its own environ
# is exactly the image ENV (plus orchestrator vars, filtered below). Write it
# to /etc/profile.d/zzz-agentbox-image-env.sh — /etc/profile sources profile.d
# AFTER its PATH reset, so these exports win; ~/.profile then only PREPENDS
# user-local dirs (e.g. ~/.local/bin), which is harmless.
#
# Precedence: every non-PATH var is guarded with `[ -z "${VAR+x}" ]`, i.e. it
# is only set when ABSENT from the process env. Caller-provided envs (sandbox
# EnvVars via /init, per-exec `envs`) are already in the process env when the
# login shell starts, so they keep priority over the image ENV — same ordering
# envd itself implements. PATH is the one exception: by the time profile.d
# runs, /etc/profile has already clobbered whatever PATH the process had, so
# there is nothing left to preserve and it is restored unconditionally.
persist_image_env() {
    [ -d /etc/profile.d ] || mkdir -p /etc/profile.d 2>/dev/null || return 0
    {
        echo "# Generated by agentbox-entrypoint.sh: image ENV for login shells."
        cat /proc/self/environ | tr '\0' '\n' | while IFS= read -r kv; do
            case "$kv" in
                *=*) ;;
                *) continue ;;  # skip values with embedded newlines (split tails)
            esac
            key=${kv%%=*}
            case "$key" in
                # Orchestrator/runtime vars — not part of the image ENV.
                HOSTNAME|HOME|PWD|SHLVL|_|PORT|AGENTBOX_DIR|GODEBUG|KUBERNETES_*) continue ;;
            esac
            val=${kv#*=}
            # Single-quote the value, escaping embedded single quotes.
            esc=$(printf '%s' "$val" | sed "s/'/'\\\\''/g")
            if [ "$key" = "PATH" ]; then
                # /etc/profile already reset PATH; restore the image PATH.
                printf "export PATH='%s'\n" "$esc"
            else
                # Set only if absent so caller-provided envs keep priority.
                printf "[ -z \"\${%s+x}\" ] && export %s='%s'\n" "$key" "$key" "$esc"
            fi
        done
        # The guarded lines end with `&&`, whose result must not leak into the
        # sourcing shell's exit status.
        echo "true"
    } > /etc/profile.d/zzz-agentbox-image-env.sh 2>/dev/null || true
}

create_user || echo "Warning: user creation had issues, continuing..." >&2
mkdir -p /run/e2b 2>/dev/null || true
install_sh_shim || echo "Warning: could not install sh shim, continuing..." >&2
persist_image_env || echo "Warning: could not persist image env, continuing..." >&2

export GODEBUG=multipathtcp=0

# Start envd daemon (non-FC mode: stdout logs, no MMDS, no Firecracker cgroups)
exec "$AGENTBOX_DIR/envd" -isnotfc
