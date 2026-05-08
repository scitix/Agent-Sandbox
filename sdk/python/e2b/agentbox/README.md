# sdk/python/agentbox — E2B Python SDK Patch

Redirect API requests from the standard E2B Python SDK to an AgentBox deployment, accessing the data plane using standard URL paths.

## Quick Start

```python
from agentbox.patch_e2b import patch_e2b

# Zero-argument call inside the cluster (automatically uses the default ClusterIP address)
patch_e2b()

from e2b import Sandbox
sb = Sandbox.create("my-pool", timeout=300, secure=False)
```

Custom address (e.g., for local port-forward debugging):

```python
patch_e2b(https=False, domain="localhost:9081", api_url="http://localhost:9082")
```

## What `patch_e2b.py` does

1. Sets the `E2B_API_URL` environment variable → SDK's control plane requests are sent to the AgentBox E2B API (`:8090`). The default value is the in-cluster `agentbox-e2b-api` ClusterIP address.
2. Monkey-patches `SandboxBase.get_host()` → Returns `<domain>/sandboxes/<sandboxID>/<port>` (standard URL path format, routed through Envoy ExtProc). The default domain is the in-cluster `agentbox-data-plane` ClusterIP address.
3. Monkey-patches `ConnectionConfig.get_host()` and `get_sandbox_url()` → Same as above, ignoring the domain returned by the API (which might be an internal address).

## Parameters

| Parameter | Default Value | Description |
|------|--------|------|
| `https` | `False` | Whether the data plane uses HTTPS. Keep the default for local/in-cluster HTTP development. |
| `domain` | `agentbox-data-plane.agentbox-system.svc.cluster.local` | Data plane Envoy gateway address. Overrides the `E2B_DOMAIN` environment variable. |
| `api_url` | `http://agentbox-e2b-api.agentbox-system.svc.cluster.local` | Full URL of the control plane. Overrides the `E2B_API_URL` environment variable. |

**Priority**: Explicit parameters > Environment variables (`E2B_DOMAIN` / `E2B_API_URL`) > Built-in defaults