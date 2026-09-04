#!/bin/bash
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

# --- Configuration Variables ---
REGISTRY_PREFIX=${REGISTRY_PREFIX:-"ghcr.io/scitix/"}
ENVD_IMAGE_NAME="agent-sandbox-envd"
TINI_IMAGE_NAME="agent-sandbox-tini"
# The e2b-infra repo is cloned into a temporary directory by default.
# Override INFRA_DIR to use a pre-cloned checkout.
INFRA_DIR=${INFRA_DIR:-""}
INFRA_REPO="https://github.com/e2b-dev/infra"
# Pin to an exact commit so the build is reproducible AND so the patches below
# (which are line-addressed) apply deterministically. e2b-infra's default
# branch moves fast (date-based release tags), so an unpinned clone would
# silently change the envd version and break patch application. This commit is
# envd 0.7.0. Bump this together with regenerating the patches (see
# patches/README.md) when you intentionally move to a newer envd.
# Full SHA is required: `git fetch --depth 1 origin <sha>` rejects abbreviated SHAs.
# Keep this in lockstep with .github/workflows/build-envd.yml's infra_ref default.
INFRA_REF=${INFRA_REF:-"8a3f69da6f822c2de2b310dd1076d2c309eef919"}
CURRENT_DIR="$(cd "$(dirname "$0")" && pwd)"
PATCHES_DIR="$CURRENT_DIR/patches"

# --- 1. Clone or use existing e2b-infra repo ---
if [ -z "$INFRA_DIR" ]; then
    TMPDIR="$(mktemp -d)"
    trap 'rm -rf "$TMPDIR"' EXIT
    echo "--- Step 1: Cloning e2b-infra @ ${INFRA_REF} ---"
    # Fetch just the pinned commit instead of a shallow clone of the tip.
    git init -q "$TMPDIR/infra"
    git -C "$TMPDIR/infra" remote add origin "$INFRA_REPO"
    git -C "$TMPDIR/infra" fetch -q --depth 1 origin "$INFRA_REF"
    git -C "$TMPDIR/infra" checkout -q FETCH_HEAD
    INFRA_DIR="$TMPDIR/infra"
else
    echo "--- Step 1: Using existing e2b-infra at $INFRA_DIR ---"
fi

ENVD_SRC_DIR="$INFRA_DIR/packages/envd"

# --- 1b. Apply ScitiX patches to envd ---
# These are our carried-forward fixes on top of upstream envd. They MUST apply
# cleanly; a reject means upstream drifted from INFRA_REF and a human needs to
# rebase the patch (do NOT ship an unpatched binary — it reintroduces the
# Kubernetes OOM-wrapper bug). See each patch's header for what it does.
if [ -d "$PATCHES_DIR" ]; then
    for patch in "$PATCHES_DIR"/*.patch; do
        [ -e "$patch" ] || continue
        echo "--- Step 1b: Applying patch $(basename "$patch") ---"
        git -C "$INFRA_DIR" apply --verbose "$patch" || {
            echo "ERROR: failed to apply $(basename "$patch") to e2b-infra@${INFRA_REF}." >&2
            echo "       Upstream likely drifted; rebase the patch and update INFRA_REF." >&2
            exit 1
        }
    done
fi

# --- 2. Build envd binary ---
echo "--- Step 2: Building envd binary from source ---"
cd "$ENVD_SRC_DIR"
make build

if [ ! -f "bin/envd" ]; then
    echo "Error: Binary bin/envd not found after make build."
    exit 1
fi

# --- 3. Get the version number from the compiled binary ---
ENVD_VERSION=$(./bin/envd --version | awk '{print $NF}')
echo "Detected envd version: $ENVD_VERSION"

# Verify the tag exists in e2b-infra to ensure alignment
if git -C "$INFRA_DIR" rev-parse "refs/tags/$ENVD_VERSION" >/dev/null 2>&1; then
    echo "Tag $ENVD_VERSION found in e2b-infra — version aligned."
else
    echo "Warning: tag $ENVD_VERSION not found in e2b-infra. Proceeding anyway."
fi

# --- 4. Build and push envd image ---
echo "--- Step 3: Building envd image ---"
cd "$CURRENT_DIR"
cp "$ENVD_SRC_DIR/bin/envd" ./envd

ENVD_VERSIONED_TAG="${REGISTRY_PREFIX}${ENVD_IMAGE_NAME}:${ENVD_VERSION}"
ENVD_LATEST_TAG="${REGISTRY_PREFIX}${ENVD_IMAGE_NAME}:latest"

docker build \
    --build-arg ENVD_VERSION="$ENVD_VERSION" \
    -t "$ENVD_VERSIONED_TAG" \
    -t "$ENVD_LATEST_TAG" \
    -f Dockerfile.envd .

rm ./envd

echo "--- Step 4: Pushing envd images ---"
docker push "$ENVD_VERSIONED_TAG"
docker push "$ENVD_LATEST_TAG"

# --- 5. Build and push tini image ---
echo "--- Step 5: Building tini image ---"
# tini version is extracted from the e2b-infra Dockerfile or a known constant
TINI_VERSION=${TINI_VERSION:-"v0.19.0"}
TINI_IMAGE_TAG=${TINI_IMAGE_TAG:-"${TINI_VERSION}-static"}

TINI_VERSIONED_TAG="${REGISTRY_PREFIX}${TINI_IMAGE_NAME}:${TINI_IMAGE_TAG}"
TINI_LATEST_TAG="${REGISTRY_PREFIX}${TINI_IMAGE_NAME}:latest"

docker build \
    --build-arg TINI_VERSION="$TINI_VERSION" \
    -t "$TINI_VERSIONED_TAG" \
    -t "$TINI_LATEST_TAG" \
    -f Dockerfile.tini .

echo "--- Step 6: Pushing tini images ---"
docker push "$TINI_VERSIONED_TAG"
docker push "$TINI_LATEST_TAG"

echo "------------------------------------------------"
echo "Success! Pushed:"
echo "  1. $ENVD_VERSIONED_TAG"
echo "  2. $ENVD_LATEST_TAG"
echo "  3. $TINI_VERSIONED_TAG"
echo "  4. $TINI_LATEST_TAG"
