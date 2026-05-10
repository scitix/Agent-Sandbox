# Publishing Guide

This document explains how to publish a new release of agent-sandbox, including container images to GHCR, Helm charts, Python SDK to PyPI, and TypeScript SDK to npm.

---

## Release Overview

A single semver tag `v<major>.<minor>.<patch>` (e.g. `v0.0.1`) triggers all publish workflows simultaneously:

| Workflow | What it publishes | Target registry |
|----------|------------------|-----------------|
| `build-controller.yml` | Controller image | `ghcr.io/<org>/agent-sandbox-controller` |
| `build-dashboard.yml` | Dashboard image | `ghcr.io/<org>/agent-sandbox-dashboard` |
| `build-extproc.yml` | ExtProc image | `ghcr.io/<org>/agent-sandbox-envoyextproc` |
| `build-wsproxy.yml` | WSProxy image | `ghcr.io/<org>/agent-sandbox-wsproxy` |
| `build-idleimage.yml` | Idle image | `ghcr.io/<org>/agent-sandbox-idle` |
| `helm-chart-publish.yml` | Helm charts | `ghcr.io/<org>` (OCI) |
| `sdk-python-publish.yml` | `agent-sandbox-e2b` Python package | [PyPI](https://pypi.org/project/agent-sandbox-e2b/) |
| `sdk-npm-publish.yml` | `agent-sandbox-e2b` npm package | [npmjs.com](https://www.npmjs.com/package/agent-sandbox-e2b) |

### Non-release builds (develop branch)

Pushes to `develop` that touch SDK files run a **build-only dry run** — the wheel/package is built and validated but never published. This ensures every commit is publishable without polluting the registries.

Container image builds on `develop` are published to GHCR with a short SHA tag; the cleanup job keeps only the 3 most recent non-release versions per image.

---

## One-time Setup (do this before the first release)

### 1. PAT_TOKEN — required for GHCR cleanup

The `quartx-analytics/ghcr-cleaner` action cannot use the default `GITHUB_TOKEN` to delete packages in an organization. You need a Personal Access Token with `delete:packages` scope.

**Steps:**

1. Go to **GitHub → Settings → Developer settings → Personal access tokens → Fine-grained tokens**
2. Click **Generate new token**
3. Set resource owner to your org (`scitix`)
4. Under **Permissions → Repository permissions**, grant:
   - `Packages: Read and write` (includes delete)
5. Copy the token
6. Go to your repo → **Settings → Secrets and variables → Actions**
7. Click **New repository secret**, name it `PAT_TOKEN`, paste the token

> This secret is already referenced in `helm-chart-publish.yml` as well, so if it was already configured there it is already done.

---

### 2. PyPI — OIDC Trusted Publisher (no token needed)

This project uses [PyPI Trusted Publisher](https://docs.pypi.org/trusted-publishers/) which lets GitHub Actions publish without storing a secret. OIDC verification happens automatically.

**Steps:**

1. Register at [pypi.org](https://pypi.org) if you do not have an account
2. Go to **PyPI → Your account → Publishing**
3. Under **Add a new pending publisher**, fill in:
   - **PyPI project name**: `agent-sandbox-e2b`
   - **Owner**: `scitix` (your GitHub org)
   - **Repository name**: `agent-sandbox`
   - **Workflow name**: `sdk-python-publish.yml`
   - **Environment name**: `pypi`
4. Click **Add**
5. In the GitHub repo, go to **Settings → Environments** and create an environment named **`pypi`**
   - No secrets needed in this environment — OIDC handles authentication

> The first publish creates the PyPI project automatically. No prior `pip install` or manual project creation is needed.

---

### 3. npm — Access Token

**Steps:**

1. Register at [npmjs.com](https://www.npmjs.com) if you do not have an account (or use an org account)
2. Go to **npm → Account → Access Tokens**
3. Click **Generate New Token → Automation**
   - Use **Automation** type (not Classic), it is not blocked by 2FA
4. Copy the token
5. Go to your GitHub repo → **Settings → Secrets and variables → Actions**
6. Click **New repository secret**, name it `NPM_TOKEN`, paste the token

> On first publish, npm will create the `agent-sandbox-e2b` package automatically under your account. If you want to publish under an npm organization (e.g. `@scitix/agent-sandbox-e2b`), update the `name` field in `sdk/typescript/e2b/package.json` and re-run with `--access public`.

---

## Release Checklist

Before tagging, run through this list:

```
[ ] VERSION file updated to the new version (e.g. echo "0.0.1" > VERSION)
[ ] make sync-version run (syncs openapi.yaml, pyproject.toml, __init__.py)
[ ] sdk/python/e2b/agentbox/__init__.py __version__ matches the release version
[ ] sdk/typescript/e2b/package.json version matches (the workflow stamps it at build time,
    but you can update it manually to keep the file consistent in the repo)
[ ] Helm chart Chart.yaml versions updated if bumping charts
[ ] All CI checks green on develop
[ ] CHANGELOG / release notes drafted
```

## Tagging and Triggering the Release

```bash
# Make sure you are on main and it is up to date
git checkout main
git pull

# Create and push the annotated tag — this triggers ALL publish workflows
git tag -a v0.0.1 -m "Release v0.0.1"
git push origin v0.0.1
```

Then go to **GitHub → Releases → Draft a new release**, select the tag, and publish it. This also triggers the Helm chart workflow (which listens on `release: published`).

---

## Image Tags Produced per Release

For a tag `v0.0.1`, each container image workflow produces:

| Tag | Example |
|-----|---------|
| Full semver | `0.0.1` |
| Major.minor | `0.0` |
| Major | `0` |
| `latest` | only on the default branch |
| Short SHA | `sha-a1b2c3d` |
| Branch name | `develop` / `main` |

---

## GHCR Cleanup Policy

All five container image workflows run a `cleanup` job after each push (not on PRs). The policy is:

- `delete-untagged: true` — remove any digest with no tag
- `keep-at-most: 3` — retain the 3 most recent non-skipped versions
- `skip-tags: v*` — never delete release-tagged versions

This means develop builds accumulate at most 3 versions per image; formal releases (`v*`) are kept indefinitely.

The `helm-chart-publish.yml` uses the same cleaner with `keep-at-most: 10` for Helm charts.

---

## Version Injection

### How versions flow

Every Go binary is stamped at compile time via `-ldflags`:

```
-X github.com/scitix/agent-sandbox/pkg/version.Version=<version>
```

The CI workflows (`build-controller`, `build-extproc`, `build-wsproxy`) compute the version as:

| Trigger | Injected version | Example |
|---------|-----------------|---------|
| Tag `v0.1.0` | `0.1.0` | Formal release |
| Branch push | `<VERSION file>+dev.<short-sha>` | `0.0.0+dev.abc1234` |
| Local `go run` | `0.0.0` (default) | Development |

The `+dev.<sha>` suffix is build metadata per semver §10. The `Masterminds/semver` library ignores it during `LessThan()` comparisons, so dev builds always satisfy `MinClientVersion = "0.0.0"` and are never blocked by the version check middleware.

The dashboard receives the same version as `NEXT_PUBLIC_APP_VERSION` at build time.

### Server version header

Every HTTP response from both the native API (`:8080`) and the E2B-compatible API (`:8090`) includes:

```
X-AgentBox-Server-Version: 0.1.0
```

This header is set before authentication, so clients can read it on unauthenticated endpoints like `/ping` to detect mismatches before making write requests.

---

## API Compatibility Matrix

### Client categories and version enforcement

| Client | Transport | Version header sent | Subject to MinClientVersion |
|--------|-----------|--------------------|-----------------------------|
| Native SDK (`abx`, Python SDK) | `:8080` native API | `X-AgentBox-Client-Version` | **Yes** |
| E2B patch (`agent-sandbox-e2b` Python/TS) | `:8090` E2B-compat API | None (E2B SDK) | **No** — exempt by design |
| Dashboard / JWT users | `:8080` native API | None | **No** — exempt (no `AGENTBOX-API-KEY`) |

The E2B patch SDKs are intentionally exempt: they are thin wiring adapters, not versioned API clients. Breaking changes in the E2B-compat layer are handled separately by pinning the `e2b` peer dependency version.

### OpenAPI vendor extensions

Two extensions track per-operation compatibility:

```yaml
/v1/sandboxes:
  post:
    x-since: "0.1.0"          # version this operation was introduced
    x-breaking-change: "0.2.0" # version a breaking change was made; "" or absent = none
```

**Rules:**
- `x-since` is required on all **new** operations (added after the initial release). The `compat-check` CI job enforces this on PRs.
- `x-breaking-change` must be set when an existing operation introduces a breaking change (see definition below).
- Existing operations without `x-since` emit a **warning** (not an error) in CI — they predate this annotation system.

**Breaking change definition:**
- A response field is removed
- A previously optional request field becomes required
- An operation (path + method) is removed
- A 2xx success status code changes

**Non-breaking** (no annotation required): adding new optional fields, new endpoints, new optional query parameters.

### Release series and compatibility convention

This project treats **minor** versions (`0.x.0`) as the primary compatibility boundary, not patch versions:

| Release series | `MinClientVersion` | Meaning |
|---------------|-------------------|---------|
| `0.0.x` (all patch releases) | `"0.0.0"` | Every `0.0.x` server accepts every `0.0.x` SDK. No breaking changes are introduced within a patch series. |
| `0.1.0` (first minor bump) | `"0.1.0"` only if breaking changes are introduced | `0.0.x` clients receive `426 Upgrade Required` on writes if the server enforces this. |
| `0.2.0`, `0.3.0`, … | Bump only when breaking changes land | Same rule — minor bump + breaking change together triggers a `MinClientVersion` raise. |

**Practical rule**: during active `0.0.x` development, `MinClientVersion` stays `"0.0.0"`. The first time you cut a `0.1.0` release *with breaking API changes*, raise it to `"0.1.0"`. Additive-only minor releases leave `MinClientVersion` unchanged.

### When to bump MinClientVersion

`MinClientVersion` is defined in `pkg/apiserver/router/middleware/version.go`.

Only bump it when **all three conditions** are true:

1. An endpoint has `x-breaking-change` set in `openapi.yaml`
2. That change is included in the release being tagged
3. You want to force all older SDK users to upgrade before they can make write requests

```go
// 0.0.x series — all patch releases are mutually compatible
MinClientVersion = "0.0.0"

// First minor bump with a breaking change:
MinClientVersion = "0.1.0"
```

When set, clients with `X-AgentBox-Client-Version` below this value receive `426 Upgrade Required` on all write operations (POST/PUT/PATCH/DELETE).

### Scenario table

| Server | SDK / client | MinClientVersion | Outcome |
|--------|-------------|-----------------|---------|
| `0.0.1` | `0.0.1` | `0.0.0` | ✅ All calls succeed |
| `0.0.2` (new endpoint) | `0.0.1` | `0.0.0` | ✅ Old SDK works; cannot call the new endpoint (it doesn't know about it) |
| `0.0.2` (breaking change) | `0.0.1` | `0.0.0` | ✅ Still passes — operator chose not to block old clients |
| `0.0.2` (breaking change) | `0.0.1` | `0.0.2` | ❌ Write requests from old SDK get `426 Upgrade Required` |
| `0.0.2` | E2B patch `0.0.1` | `0.0.2` | ✅ E2B hits `:8090`, completely unaffected |
| `0.0.2` | Dashboard | `0.0.2` | ✅ No `AGENTBOX-API-KEY` header, always exempt |

### Running the compatibility check locally

```bash
# On a feature branch, check operations added/changed vs main:
python3 hack/scripts/check-openapi-compat.py --diff-base origin/main

# Audit all operations for missing x-since (warnings only):
python3 hack/scripts/check-openapi-compat.py --check-all
```
