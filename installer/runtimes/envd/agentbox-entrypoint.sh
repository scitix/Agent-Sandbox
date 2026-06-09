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

# PID 1 of every agent-sandbox pod. It runs inside the USER's image (the image
# is swapped in by the pool), does a small amount of environment preparation
# that the e2b stack assumes but does not itself provide, then hands off to the
# envd daemon which serves the e2b API.
#
# Two pieces of preparation, each documented at its function:
#   create_user        - ensure an unprivileged `user` account exists
#   persist_image_env  - make the image's ENV reach exec'd commands
#
# NOTE: there used to be a third step (install_sh_shim) that rewrote /bin/sh to
# survive envd's OOM-score wrapper failing under Kubernetes. That is now fixed
# properly in our patched envd (skips the wrapper in not-FC mode; see
# patches/0001-skip-oom-nice-wrapper-when-not-firecracker.patch), so the shim —
# and the whole-image /bin/sh mutation it required — is gone.

set -e

AGENTBOX_DIR=${AGENTBOX_DIR:-/mnt/agentbox}


# IdleImage: a placeholder pod that holds a pool slot warm but should do nothing
# until its image is swapped for a real one. Just sleep; never start envd.
if [ "$AGENTBOX_IS_IDLE_IMAGE" = "true" ] || [ -f /etc/agentbox_is_idle_image ]; then
    echo "[INFO] IdleImage detected. Entering sleep mode."
    exec sleep infinity
fi

# Ensure an unprivileged `user` account exists.
#
# The e2b stack defaults to running commands as `user` (e.g. the SDK's default
# exec user), but arbitrary base images often don't ship that account. Without
# it, exec-as-user fails. We create it best-effort across the toolchains images
# actually have — useradd, then adduser (busybox/alpine), then a raw
# /etc/passwd append as a last resort — and give it passwordless sudo so it can
# still perform root actions when a task needs them. Failure is non-fatal: an
# image that already runs everything as root works without this account.
create_user() {
    local username="user"
    local home_dir="/home/$username"
    local default_shell="/bin/bash"

    # Already present (image baked it in, or a previous run created it).
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
persist_image_env || echo "Warning: could not persist image env, continuing..." >&2

# Disable MPTCP: some kernels/images mis-handle it and it breaks envd's port
# forwarding; envd does not need it.
export GODEBUG=multipathtcp=0

# Start envd daemon. -isnotfc = "not a Firecracker microVM": logs go to stdout
# (no MMDS/Firecracker log shipping) AND, with our patch, the process handler
# skips the Firecracker-only OOM/nice wrapper that fails under Kubernetes.
exec "$AGENTBOX_DIR/envd" -isnotfc
