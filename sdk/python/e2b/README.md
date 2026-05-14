# agent-sandbox-e2b

A one-line patch that redirects the [E2B Python SDK](https://e2b.dev/docs) to a self-hosted [Agent Sandbox](https://github.com/scitix/agent-sandbox) deployment — no code changes to your existing E2B workflows required.

AgentBox is a Kubernetes-native sandbox service for agentic AI scenarios (reasoning evaluation, training rollouts, code execution). It exposes an E2B-compatible API so you can reuse the E2B SDK while running on your own infrastructure.

## Installation

```bash
pip install e2b==2.19.0 agent-sandbox-e2b
```

## Quick Start

Call `patch_e2b()` **before** importing `Sandbox`. It redirects all SDK requests to your AgentBox cluster.

```python
import os

os.environ["E2B_API_KEY"] = "agbx_your_api_key"

from agentbox.patch_e2b import patch_e2b
patch_e2b()  # zero-argument: uses default in-cluster addresses

from e2b import Sandbox

sandbox = Sandbox.create("POOL_NAME", timeout=3600, secure=False)
sandbox.commands.run("echo hello")
sandbox.kill()
```

> **Note**: `patch_e2b()` must be called before any `e2b` import; otherwise the SDK connects to the official E2B service.

## Cross-Cluster Usage

Prefix the pool name with a cluster ID to route requests across clusters:

```python
sandbox = Sandbox.create("CLUSTER_ID::POOL_NAME", timeout=3600, secure=False)
```

## Custom Image Override

Override the pool's default image at creation time:

```python
# Using metadata key
sandbox = Sandbox.create(
    "POOL_NAME",
    timeout=3600,
    secure=False,
    metadata={"agentbox.scitix.ai/image": "registry.example.com/my-env:v2"},
)

# Shorthand syntax
sandbox = Sandbox.create(
    "CLUSTER_ID::POOL_NAME//registry.example.com/my-env:v2",
    timeout=3600,
    secure=False,
)
```

## Configuration

`patch_e2b()` accepts optional arguments to override the target addresses:

| Parameter | Default (in-cluster) | Description |
|-----------|----------------------|-------------|
| `domain` | `agentbox-data-plane.agentbox-system.svc.cluster.local` | Data plane Envoy gateway address |
| `api_url` | `http://agentbox-e2b-api.agentbox-system.svc.cluster.local` | Control plane E2B-compatible API URL |
| `https` | `False` | Use HTTPS for the data plane |

**Priority**: explicit argument > `E2B_DOMAIN` / `E2B_API_URL` environment variables > built-in defaults.

```python
# Local debugging via port-forward
patch_e2b(https=False, domain="localhost:9081", api_url="http://localhost:9082")

# Via environment variables (CI/CD)
# export E2B_DOMAIN=agentbox-data-plane.agentbox-system.svc.cluster.local
# export E2B_API_URL=http://agentbox-e2b-api.agentbox-system.svc.cluster.local
# export E2B_API_KEY=agbx_your_key
patch_e2b()
```

## Compatibility

Tested against `e2b==2.19.0`. After patching, all standard E2B SDK operations work unchanged:

```python
sandbox.commands.run("python --version")
sandbox.commands.run("id", user="root")
sandbox.files.write("/tmp/hello.py", b"print('hello')\n")
sandbox.files.read("/tmp/hello.py")
sandbox.set_timeout(1800)   # extend idle timeout dynamically
sandbox.kill()
```

## Links

- [AgentBox documentation](https://github.com/scitix/agent-sandbox)
- [E2B SDK reference](https://e2b.dev/docs/sdk-reference/python-sdk/v2.15.2/sandbox_sync)
- [License: Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0)
