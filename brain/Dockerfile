# The Brain image: the conversation layer plus both agent harnesses.
#
# Build context is the REPOSITORY ROOT, not this directory — the image needs
# `brain/` and `sdk/hands/` and they must come from the same commit:
#
#   docker build -f brain/Dockerfile -t agentbox-brain:dev .
#
# Four processes end up running here; entrypoint.sh is the contract between them
# and states which port each one owns and why they live and die together.

# ---------------------------------------------------------------------------
# OpenCode CLI.
#
# Fetched as a platform package rather than installed through `opencode-ai`,
# whose postinstall does two things this build does not want: it shells out to
# `node` (absent from a bun image), and it picks between the AVX2 and baseline
# builds by reading /proc/cpuinfo — on the MACHINE DOING THE BUILD. A builder
# with AVX2 would bake a binary that dies with an illegal instruction on an older
# node, at a point far from anything that looks like an image problem.
#
# So the choice is made here instead, explicitly: `-baseline` on x64, which runs
# anywhere. The version must match `@opencode-ai/sdk` in brain/package.json — the
# gateway speaks to this server over that SDK.
# ---------------------------------------------------------------------------
FROM debian:trixie-slim AS opencode
ARG OPENCODE_VERSION=1.18.16
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
    amd64) pkg="opencode-linux-x64-baseline" ;; \
    arm64) pkg="opencode-linux-arm64" ;; \
    *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://registry.npmjs.org/${pkg}/-/${pkg}-${OPENCODE_VERSION}.tgz" \
    | tar -xz -C /tmp; \
    install -m 0755 /tmp/package/bin/opencode /usr/local/bin/opencode; \
    /usr/local/bin/opencode --version

# ---------------------------------------------------------------------------
# The Python daemon and workspace file API, into a virtualenv.
#
# A venv rather than a system install because Debian marks its interpreter
# externally managed, and the alternative (--break-system-packages) is a name that
# should be taken at face value. Copied whole into the final stage, so pip and its
# build dependencies never ship.
# ---------------------------------------------------------------------------
FROM debian:trixie-slim AS python
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 python3-venv \
    && rm -rf /var/lib/apt/lists/*
RUN python3 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
# The dependency set comes from the package, so the image cannot pin a different
# one from what the daemon was tested against.
COPY sdk/hands/python /src/hands
RUN pip install --no-cache-dir /src/hands \
    && python -c "import agentbox_hands.daemon, agentbox_hands.fs"

# ---------------------------------------------------------------------------
# Gateway dependencies.
#
# `bun install` in its own stage so the final image carries node_modules without
# the lockfile churn and cache that producing them needs. `--frozen-lockfile`
# because a dependency resolving differently here than in CI is exactly the class
# of difference that shows up as a harness misbehaving at runtime.
# ---------------------------------------------------------------------------
FROM oven/bun:1-debian AS deps
WORKDIR /build
COPY brain/package.json brain/bun.lock ./
# The hands package is a file: dependency, so it has to be in place before the
# install and at the path the manifest names.
COPY sdk/hands/typescript /sdk/hands/typescript
# Then MATERIALISED, because bun links a file: dependency as a directory of
# ABSOLUTE symlinks into the source path. Those dangle the moment node_modules is
# copied into an image that does not have the source at that same absolute path —
# and the failure is a module resolution error at an agent's first tool call,
# nowhere near the build.
#
# Replacing the link farm with a real copy makes node_modules self-contained, so
# the final image needs neither the source tree nor a path that agrees with this
# stage's layout. The check afterwards is the general form of the same problem:
# any remaining broken link means something else is still reaching outside.
RUN set -eu; \
    bun install --frozen-lockfile --production; \
    rm -rf node_modules/@scitix/agentbox-hands; \
    mkdir -p node_modules/@scitix; \
    cp -R /sdk/hands/typescript node_modules/@scitix/agentbox-hands; \
    rm -rf node_modules/@scitix/agentbox-hands/node_modules; \
    dangling=$(find node_modules -xtype l | head -20); \
    if [ -n "$dangling" ]; then \
    echo "node_modules has links pointing outside itself:" >&2; \
    echo "$dangling" >&2; \
    exit 1; \
    fi

# ---------------------------------------------------------------------------
# Final image.
# ---------------------------------------------------------------------------
FROM oven/bun:1-debian

# uid/gid 1000 is not cosmetic: the controller renders runAsUser/fsGroup 1000, so
# a different id here means an unwritable state volume and no history. The base
# image already has a uid-1000 user; it is renamed rather than joined by a second
# one so /etc/passwd agrees with HOME (anything resolving the home directory
# through the password database, rather than through $HOME, would otherwise
# disagree).
RUN set -eu; \
    groupmod -n agents bun; \
    usermod -l agents -d /home/agents -m bun; \
    apt-get update; \
    apt-get install -y --no-install-recommends python3 ca-certificates git curl; \
    rm -rf /var/lib/apt/lists/*

ENV HOME=/home/agents \
    PATH="/opt/venv/bin:$PATH" \
    # Unbuffered, or the daemon's log lines about which sandbox a session got sit
    # in a pipe buffer and arrive after the failure they explain.
    PYTHONUNBUFFERED=1 \
    BRAIN_GATEWAY_DIR=/home/agents/gateway

COPY --from=opencode /usr/local/bin/opencode /usr/local/bin/opencode
COPY --from=python /opt/venv /opt/venv

WORKDIR /home/agents

# The gateway, and the toolset it shares with the OpenCode harness. node_modules
# carries the toolset itself (see the deps stage), so there is no second copy of
# it here to drift from the one that gets imported.
#
# It lands at HOME rather than beside the gateway because module resolution walks
# UP from the importing file, and OpenCode's tool overrides live at
# $HOME/.config/opencode/tool/. From there the only node_modules on the way up is
# this one.
COPY --from=deps --chown=1000:1000 /build/node_modules /home/agents/node_modules
COPY --chown=1000:1000 brain/package.json /home/agents/package.json
COPY --chown=1000:1000 brain/tsconfig.json /home/agents/tsconfig.json
COPY --chown=1000:1000 brain/gateway /home/agents/gateway
COPY --chown=1000:1000 brain/entrypoint.sh /home/agents/entrypoint.sh

# OpenCode's config directory, holding the seven tool overrides.
#
# THE FILENAME IS THE OVERRIDE. A tool whose file is named after a built-in
# replaces it, which is the whole confinement mechanism for this harness — and a
# file in the wrong directory does not error, it just leaves the built-in in
# place, with the agent's bash and read acting on THIS pod instead of the sandbox.
# opencode-tools.sh writes them from one list and then verifies each one resolves,
# so a rename upstream fails the build rather than the confinement.
COPY --chown=1000:1000 brain/image/opencode-tools.sh /tmp/opencode-tools.sh
RUN /tmp/opencode-tools.sh /home/agents/.config/opencode && rm /tmp/opencode-tools.sh

# Pre-created and owned by the runtime user because every process that makes a
# user's directory inside it is unprivileged: the gateway mkdir's the session's
# own directory, and the file API refuses any path that is not under this root.
# The literal is checked against the gateway's constant by user-dir.test.ts.
RUN set -eu; \
    mkdir -p /home/agents/u /home/agents/state; \
    chown -R 1000:1000 /home/agents

USER 1000:1000

# The gateway. The other three ports are documented in entrypoint.sh; only this
# one is the pod's entry and its health authority.
EXPOSE 4099
# Attachment staging and the workspace file browser — the one other port a
# browser reaches directly.
EXPOSE 8766

ENTRYPOINT ["/home/agents/entrypoint.sh"]
