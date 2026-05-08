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
CURRENT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- 1. Clone or use existing e2b-infra repo ---
if [ -z "$INFRA_DIR" ]; then
    TMPDIR="$(mktemp -d)"
    trap 'rm -rf "$TMPDIR"' EXIT
    echo "--- Step 1: Cloning e2b-infra ---"
    git clone --depth 1 "$INFRA_REPO" "$TMPDIR/infra"
    INFRA_DIR="$TMPDIR/infra"
else
    echo "--- Step 1: Using existing e2b-infra at $INFRA_DIR ---"
fi

ENVD_SRC_DIR="$INFRA_DIR/packages/envd"

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

TINI_VERSIONED_TAG="${REGISTRY_PREFIX}${TINI_IMAGE_NAME}:${TINI_VERSION}"
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
