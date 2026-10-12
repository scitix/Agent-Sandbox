# agent-sandbox-e2b

A one-line patch that redirects the [E2B Python SDK](https://e2b.dev/docs) to a self-hosted [Agent Sandbox](https://github.com/scitix/agent-sandbox) deployment — no code changes to your existing E2B workflows required.

Agent Sandbox is a Kubernetes-native sandbox service for agentic AI scenarios (reasoning evaluation, training rollouts, code execution). It exposes an E2B-compatible API so you can reuse the E2B SDK while running on your own infrastructure.

## Installation

```bash
pip install agent-sandbox-e2b        # or: uv pip install agent-sandbox-e2b
```

`e2b` comes with it, capped at the newest version this release was checked
against (see [Compatibility](#compatibility)). Don't pin `e2b` separately to
something newer: the resolver will refuse it, and that refusal is the point.

## Quick Start

Call `patch_e2b()` **before** importing `Sandbox`. It redirects all SDK requests to your Agent Sandbox cluster.

```python
import os

os.environ["E2B_API_KEY"] = "agbx_your_api_key"

from agent_sandbox_e2b import patch_e2b
patch_e2b()  # zero-argument: uses default in-cluster addresses

from e2b import Sandbox

sandbox = Sandbox.create("POOL_NAME", timeout=3600)
sandbox.commands.run("echo hello")
sandbox.kill()
```

> **Note**: `patch_e2b()` must be called before any `e2b` import; otherwise the SDK connects to the official E2B service.

## Cross-Cluster Usage

Prefix the pool name with a cluster ID to route requests across clusters:

```python
sandbox = Sandbox.create("CLUSTER_ID::POOL_NAME", timeout=3600)
```

## Custom Image Override

Override the pool's default image at creation time:

```python
# Using metadata key
sandbox = Sandbox.create(
    "POOL_NAME",
    timeout=3600,
    metadata={"agentbox.scitix.ai/image": "registry.example.com/my-env:v2"},
)

# Shorthand syntax
sandbox = Sandbox.create(
    "CLUSTER_ID::POOL_NAME//registry.example.com/my-env:v2",
    timeout=3600,
)
```

## Configuration

`patch_e2b()` accepts optional arguments to override the target addresses:

| Parameter | Default (in-cluster) | Description |
|-----------|----------------------|-------------|
| `domain` | `agent-sandbox-data-plane.agentbox-system.svc.cluster.local` | Data-plane Envoy gateway address |
| `api_url` | `http://agent-sandbox-e2b-api.agentbox-system.svc.cluster.local` | E2B-compatible API URL |
| `https` | `False` | Use HTTPS for the data plane |

**Priority**: explicit argument > `E2B_HTTPS` / `E2B_DOMAIN` / `E2B_API_URL` environment variables > built-in defaults. `E2B_HTTPS` is truthy for `1`/`true`/`yes`/`on` (case-insensitive).

```python
# Local debugging via port-forward
patch_e2b(https=False, domain="localhost:9081", api_url="http://localhost:9082")

# Via environment variables (CI/CD)
# export E2B_DOMAIN=agent-sandbox-data-plane.agentbox-system.svc.cluster.local
# export E2B_API_URL=http://agent-sandbox-e2b-api.agentbox-system.svc.cluster.local
# export E2B_API_KEY=agbx_your_key
# export E2B_HTTPS=true   # required when the data plane is behind an HTTPS ingress
patch_e2b()
```

When using the Helm chart in a namespace other than `agentbox-system`, pass the
installed namespace explicitly:

```python
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    domain="agent-sandbox-data-plane.my-namespace.svc.cluster.local",
    api_url="http://agent-sandbox-e2b-api.my-namespace.svc.cluster.local",
)
```

## API key format (`agbx_` keys)

Agent Sandbox issues API keys with an `agbx_` prefix. Newer E2B SDKs
(>= ~2.24.0) added a **client-side** key-format check
(`e2b.api.validate_api_key`, pattern `\Ae2b_[0-9a-f]+\Z`) that rejects any key
not starting with `e2b_`, raising `AuthenticationException` before a request is
ever sent. `patch_e2b()` **automatically neutralizes this local check** (the
real authentication happens at the Agent Sandbox gateway), so `agbx_` keys work
across both old and new E2B SDKs with no extra configuration:

```python
os.environ["E2B_API_KEY"] = "agbx_..."   # accepted after patch_e2b()
patch_e2b()
```

The override is applied inside `patch_e2b()` and is a no-op on older SDKs that
don't have the check. If you ever need the original behavior, do not call
`patch_e2b()` (or re-assign `e2b.api.validate_api_key` back yourself).

## Compatibility

The patch hooks into SDK internals, and the SDK decides which API routes to
call. A new `e2b` can break either one, so each release states what it was
checked against and caps its dependency to match:

- **Dependency cap.** `pip install agent-sandbox-e2b` resolves `e2b` to at most
  `SUPPORTED_E2B_MAX_VERSION`. Asking for a newer `e2b` alongside it is an
  unsatisfiable requirement, which you see at install time rather than as a
  404 at runtime.
- **Runtime warning.** If the cap was bypassed (`--no-deps`, a prebuilt image,
  a source checkout), `patch_e2b()` warns that the installed `e2b` is newer
  than the one checked.
- **What gets checked.** The release workflow installs the latest `e2b`,
  confirms the hook points still exist, and runs `check_sdk_routes.py`. That
  script fails if `Sandbox` calls any route the server's E2B spec doesn't
  declare.

```python
import agent_sandbox_e2b
print(agent_sandbox_e2b.SUPPORTED_E2B_MAX_VERSION)
```

### Matrix

The server side matters as much as the patch. `e2b` 2.51.0 moved create and
connect to `POST /v2/sandboxes`. A server that doesn't serve those routes
answers every `Sandbox.create` with `404: 404 page not found`, whatever patch
version you have.

| agent-sandbox-e2b | Checked up to `e2b` | Notes |
|---|---|---|
| 0.0.11+ | stamped at release (`SUPPORTED_E2B_MAX_VERSION`) | `e2b` is a direct, capped dependency. Route coverage is checked. |
| 0.0.10 | 2.46.4 | Up to and including this release, the cap applies only through the `[e2b]` extra, so `pip install e2b agent-sandbox-e2b` installs the newest `e2b` regardless. Use `pip install 'agent-sandbox-e2b[e2b]'` to get the cap. |
| 0.0.8 – 0.0.9 | 2.37.0 | |
| 0.0.7 | 2.32.1 | |
| 0.0.5 – 0.0.6 | 2.28.0 | |
| 0.0.4 | 2.25.1 | |
| 0.0.2 – 0.0.3 | 2.21.1 | |
| 0.0.1 | `<3.0.0` (no check) | |

Older rows come from each release's published `Requires-Dist`. The check back
then covered hook points only, not routes, which made no difference until
2.51.0.

| `e2b` SDK | Server needs |
|---|---|
| ≤ 2.50.x | any Agent Sandbox release (v1 create/connect) |
| ≥ 2.51.0 | a release that serves `POST /v2/sandboxes` and `POST /v2/sandboxes/{id}/connect` |

If the server can't be upgraded yet, pin the SDK: `pip install 'e2b<2.51'`.

To check a given SDK against a given server spec yourself (from a source
checkout; the script is not shipped in the wheel):

```bash
python check_sdk_routes.py path/to/pkg/openapi/e2b/openapi.yaml
```

After patching, all standard E2B SDK operations work unchanged:

```python
sandbox.commands.run("python --version")
sandbox.commands.run("id", user="root")
sandbox.files.write("/tmp/hello.py", b"print('hello')\n")
sandbox.files.read("/tmp/hello.py")
sandbox.set_timeout(1800)   # extend idle timeout dynamically
sandbox.kill()
```

## Links

- [Agent Sandbox documentation](https://github.com/scitix/agent-sandbox)
- [E2B SDK reference](https://e2b.dev/docs/sdk-reference/python-sdk/v2.15.2/sandbox_sync)
- [License: Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0)
