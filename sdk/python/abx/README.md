# AgentBox Python SDK

`agentbox-sdk` is the asynchronous Python client library for AgentBox, designed to manage the complete lifecycle of AI Agent **Sandboxes**, **SandboxPools**, and **SandboxTemplates**.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Client Configuration](#client-configuration)
-[API Reference](#api-reference)
  - [Auth](#auth)
  - [Sandboxes](#sandboxes)
  - [Pools](#pools)
  - [Templates](#templates)
- [Data Models](#data-models)
- [Exceptions](#exceptions)
- [Full Examples](#full-examples)
-[Packaging & Development](#packaging--development)

---

## Installation

### Install from wheel (Recommended for cluster Pods)

```bash
# Install using uv (Fastest)
uv pip install --system agentbox_sdk-0.1.0-py3-none-any.whl

# Or using pip
pip install agentbox_sdk-0.1.0-py3-none-any.whl
```

### Build from source

```bash
cd sdk/python/abx
uv build --out-dir /tmp/dist/
```

---

## Quick Start

```python
import asyncio
import agentbox_sdk as abx

async def main():
    async with abx.AgentBoxClient(
        endpoint="http://agentbox-control-plane.agentbox-system.svc.cluster.local",
        api_key="agbx_xxx...",
    ) as client:

        # 1. Authenticate
        me = await client.auth.whoami()
        print(f"Logged in: user={me.user}, team={me.team}, role={me.role}")

        # 2. View available templates
        templates = await client.templates.list()
        for t in templates.items:
            print(f"Template: {t.name} v{t.version}")

        # 3. View sandbox pools
        pools = await client.pools.list()
        for p in pools.items:
            print(f"Pool: {p.name}, idle={p.status.idle_replicas}, running={p.status.running_replicas}")

        # 4. Request a sandbox (await mode, automatically waits for readiness)
        sb = await client.sandboxes.create(pool="my-pool")
        print(f"Sandbox ready: id={sb.id}, status={sb.status.value}")
        print(f"envd endpoint: {sb.endpoints}")

        # 5. Release after use
        await client.sandboxes.delete(sb.id)

asyncio.run(main())
```

---

## Client Configuration

```python
client = abx.AgentBoxClient(
    endpoint="http://agentbox-control-plane.agentbox-system.svc.cluster.local",
    api_key="agbx_xxx...",
    timeout=30.0,      # HTTP request timeout (seconds), default is 30
    verify_ssl=True,   # Whether to verify TLS certificates, default is True
)
```

It can also be configured via environment variables (consistent with the `abx` CLI):

```bash
export AGENTBOX_ENDPOINT=http://agentbox-control-plane.agentbox-system.svc.cluster.local
export AGENTBOX_API_KEY=agbx_xxx...
```

```python
# Automatically reads from environment variables when no arguments are passed
async with abx.AgentBoxClient() as client:
    ...
```

---

## API Reference

### Auth

```python
me = await client.auth.whoami()
# WhoAmIData
# me.role   → "tenant" | "admin"
# me.user   → Username
# me.team   → Team name
```

---

### Sandboxes

#### Create a Sandbox

Supports two usage patterns:

**Pattern 1: Direct await (Manual lifecycle management)**

```python
sb = await client.sandboxes.create(
    pool="my-pool",
    metadata={"app": "my-agent", "session": "abc123"},
    idle_timeout="30m",       # Idle timeout, supports "30s"/"5m"/"2h"/"0" (no timeout)
    startup_timeout="120s",   # Startup timeout
    wait=True,                # Wait until Running status before returning, default is True
    wait_timeout=120.0,       # Wait timeout in seconds, default is 120
)
print(sb.id)            # Sandbox ID (UUID)
print(sb.status)        # SandboxStatus.RUNNING
print(sb.endpoints)     # {"envd": "http://..."}
print(sb.pool_name)     # "my-pool"

# Release after use
await sb.close()        # Equivalent to client.sandboxes.delete(sb.id)
```

**Pattern 2: async with (Automatic release)**

```python
async with client.sandboxes.create(pool="my-pool") as sb:
    endpoint = sb.get_endpoint(49983)   # Get the full URL for the specified port
    host = sb.get_host(49983)           # Get only the host:port part
    # Automatically calls close() when exiting the with block
```

#### Query Sandboxes

```python
# Get by ID
sb = await client.sandboxes.get("d3b523ad-d780-46b2-b6b7-d298cc29805d")

# List (supports filtering)
result = await client.sandboxes.list()           # All
result = await client.sandboxes.list(pool="my-pool")
result = await client.sandboxes.list(status="Running")
result = await client.sandboxes.list(limit=20, offset=0)

print(result.total)   # Total count
for sb_data in result.items:  # List[SandboxData]
    print(sb_data.sandbox_id, sb_data.status.value)
```

#### Refresh Status

```python
await sb.refresh()          # Update sb status in-place
await sb.wait_for_ready(    # Poll until Running
    timeout=120.0,
    poll_interval=2.0,
)
```

#### Get Logs

```python
logs = await sb.get_logs(
    tail_lines=100,          # Last N lines
    since_seconds=60,        # Last N seconds
    container="main",        # Specify container (optional)
)
for entry in logs.entries:
    print(f"[{entry.container}] {entry.log}")
```

#### Set Timeout

```python
await sb.set_timeout("30m")   # Set idle timeout
await sb.set_timeout("0")     # Disable timeout (never automatically reclaimed)
```

#### Delete Sandbox

```python
await client.sandboxes.delete(sandbox_id)
# Or via the Sandbox object
await sb.close()
```

---

### Pools

```python
# List
result = await client.pools.list()
for pool_data in result.items:   # List[SandboxPoolData]
    print(pool_data.name, pool_data.spec.replicas)

# Get single pool
pool = await client.pools.get("my-pool")
print(pool.name)
print(pool.replicas)             # Desired replicas
print(pool.idle_replicas)        # Current idle replicas
print(pool.running_replicas)     # Current running replicas
print(pool.failed_replicas)      # Failed replicas

# Elastic scaling
await pool.scale(3)
# Or
await client.pools.scale("my-pool", 3)

# Create (Usually recommended to use abx CLI)
pool = await client.pools.create(
    name="new-pool",
    template_name="envd-code-interpreter",
    replicas=2,
)

# Create a Pool that requires quota (some templates require a quota_url)
pool = await client.pools.create(
    name="gpu-pool",
    template_name="gpu-sandbox",
    replicas=1,
    quota_url="https://quota.example.com/api/v1/check",
)

# Delete
await pool.delete()
# Or
await client.pools.delete("my-pool")
```

---

### Templates

```python
# List
result = await client.templates.list()
for t in result.items:   # List[SandboxTemplateData]
    print(t.name, t.version, t.description)
    if t.spec.runtimes:
        for rt in t.spec.runtimes:
            print(f"  runtime: {rt.name} port={rt.port}")

# Get single template
tmpl = await client.templates.get("envd-code-interpreter")
print(tmpl.name)
print(tmpl.version)
print(tmpl.spec.idle_image)
```

---

## Data Models

| Model | Description |
|------|------|
| `WhoAmIData` | `role`, `user`, `team` |
| `SandboxData` | `sandbox_id`, `status`, `pool_name`, `pod_name`, `endpoints`, `metadata`, `container_images`, `cpu`, `memory`, `claimed_at`, `started_at` |
| `SandboxStatus` | Enum: `Pending`, `Starting`, `Running`, `Stopping`, `Failed`, `Completed`, `Canceled` |
| `SandboxLogsData` | `entries` (list of `SandboxLogEntry`), `truncated`, `source` |
| `SandboxLogEntry` | `container`, `log`, `timestamp` |
| `SandboxPoolData` | `name`, `namespace`, `spec` (`SandboxPoolSpec`), `status` (`SandboxPoolStatus`) |
| `SandboxPoolSpec` | `replicas`, `min_replicas`, `max_replicas`, `template_name` |
| `SandboxPoolStatus` | `idle_replicas`, `running_replicas`, `starting_replicas`, `stopping_replicas`, `failed_replicas` |
| `SandboxTemplateData` | `name`, `version`, `description`, `spec` (`SandboxTemplateSpec`) |
| `PagedResult[T]` | `items`, `total`, `limit`, `offset` |

---

## Exceptions

```
AgentBoxError
├── AgentBoxAPIError               # Base HTTP error, contains status_code / context / raw_body
│   ├── NotFoundError (404)
│   │   ├── SandboxNotFoundError
│   │   ├── PoolNotFoundError
│   │   └── TemplateNotFoundError
│   ├── AuthenticationError (401)  # Invalid API Key
│   ├── PermissionError (403)      # Insufficient permissions
│   ├── ConflictError (409)        # Resource conflict
│   ├── ValidationError (422)      # Request parameter validation failed
│   ├── RateLimitError (429)       # Rate limit triggered
│   └── ServerError (5xx)          # Server error
├── SandboxTimeoutError            # wait_for_ready timeout
├── SandboxStartupError            # Sandbox enters Failed/Canceled state
└── EndpointNotFoundError          # get_endpoint(port) port does not exist
```

**Usage Example:**

```python
try:
    sb = await client.sandboxes.create(pool="my-pool", wait=True)
except abx.SandboxStartupError as e:
    print(f"Startup failed: {e.status} - {e.message}")
except abx.SandboxTimeoutError as e:
    print(f"Wait timeout: {e.timeout}s")
except abx.ServerError as e:
    print(f"Server error {e.status_code}: {e}")
except abx.AuthenticationError:
    print("Invalid API Key")
```

---

## Full Examples

### Scenario: Request a Sandbox and call the envd Runtime

```python
import asyncio
import agentbox_sdk as abx

async def run_code_in_sandbox():
    async with abx.AgentBoxClient(
        endpoint="http://agentbox-control-plane.agentbox-system.svc.cluster.local",
        api_key="agbx_xxx...",
    ) as client:
        # Automatically request + automatically release upon expiration
        async with client.sandboxes.create(
            pool="my-pool",
            metadata={"session": "demo"},
            idle_timeout="10m",
        ) as sb:
            print(f"Sandbox ID: {sb.id}")

            # Get the envd runtime endpoint (port 49983)
            envd_url = sb.get_endpoint(49983)
            print(f"envd: {envd_url}")

            # View logs
            logs = await sb.get_logs(tail_lines=50)
            for entry in logs.entries:
                print(f"  [{entry.container}] {entry.log}")

        print("Sandbox automatically released")

asyncio.run(run_code_in_sandbox())
```

### Scenario: Request multiple Sandboxes concurrently

```python
import asyncio
import agentbox_sdk as abx

async def allocate_sandboxes(n: int):
    async with abx.AgentBoxClient() as client:
        tasks =[client.sandboxes.create(pool="my-pool") for _ in range(n)]
        sandboxes = await asyncio.gather(*tasks)
        print(f"Successfully requested {len(sandboxes)} Sandboxes")
        for sb in sandboxes:
            await sb.close()

asyncio.run(allocate_sandboxes(3))
```

### Scenario: Elastically scale a Pool

```python
import asyncio
import agentbox_sdk as abx

async def scale_pool():
    async with abx.AgentBoxClient() as client:
        pool = await client.pools.get("my-pool")
        print(f"Current scale: replicas={pool.replicas}, idle={pool.idle_replicas}")

        await pool.scale(5)
        await pool.refresh()
        print(f"After scaling: replicas={pool.replicas}")

asyncio.run(scale_pool())
```

---

## Packaging & Development

### Build wheel

```bash
cd sdk/python/abx
uv build --out-dir /tmp/dist/
```

### Using inside a cluster Pod

```bash
# 1. Copy whl to the Pod
kubectl cp /tmp/agentbox_sdk-0.1.0-py3-none-any.whl <namespace>/<pod>:/tmp/

# 2. Install
kubectl exec <pod> -- uv pip install --system /tmp/agentbox_sdk-0.1.0-py3-none-any.whl
```

### Running integration tests

The repository provides an integration test script `test_sdk.py` corresponding to `sdk/python/abx`:

```bash
python test_sdk.py \
  --api-key agbx_xxx... \
  --endpoint http://agentbox-control-plane.agentbox-system.svc.cluster.local \
  --pool my-pool
```

Parameters description:

| Parameter | Default Value | Description |
|------|--------|------|
| `--api-key` | `$AGENTBOX_API_KEY` | API Key |
| `--endpoint` | `http://agentbox-control-plane.agentbox-system.svc.cluster.local` | API service endpoint |
| `--pool` | `my-pool` | The Pool name used for requesting Sandboxes |
| `--no-create` | false | Skip Sandbox creation/deletion tests |

### Regenerating the SDK

The `_generated/` directory of the SDK is automatically generated from the OpenAPI spec, **manual modification is strictly prohibited**:

```bash
# Execute from the project root directory
uvx openapi-python-client generate \
  --path pkg/openapi/native/openapi.yaml \
  --config sdk/python/abx/openapi-gen-config.yaml \
  --output-path /tmp/agentbox_sdk_gen \
  --overwrite

# Replace _generated
rm -rf sdk/python/abx/agentbox_sdk/_generated
mv /tmp/agentbox_sdk_gen/agentbox_sdk._generated sdk/python/abx/agentbox_sdk/_generated
```

### Running unit tests

```bash
cd sdk/python/abx
uv run pytest tests/unit/ -v
```

---

## Dependencies

| Package | Version Requirement | Purpose |
|----|----------|------|
| `httpx` | ≥0.27.0 | Async HTTP client |
| `pydantic` | ≥2.0.0 | Data modeling and validation |
| `anyio` | ≥4.0.0 | Asynchronous tools |
| `attrs` | ≥22.0.0 | Generated layer data classes |
| `python-dateutil` | ≥2.8.0 | Timestamp parsing |

Python ≥ 3.9 required.