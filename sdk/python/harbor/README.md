# agent-sandbox-harbor

A [Harbor](https://github.com/harbor-framework/harbor) environment plugin that runs Harbor
benchmarks (Terminal-Bench, SWE-bench, custom datasets) on
[Agent Sandbox](https://github.com/scitix/agent-sandbox) pre-warmed pools — no fork of Harbor
required.

Highlights:

- **Zero Harbor source changes.** Plugs into Harbor via the official
  `--environment-import-path` extension point.
- **Skips Template Build.** Agent Sandbox uses a pre-warmed Pod pool with in-place image swap,
  so the per-task Template Build step that E2B / Novita require is replaced by a single
  `POST /v1/sandboxes` call.
- **Internal-mirror friendly.** A configurable image-prefix rewrites `docker.io/...` to your
  private Distribution / Harbor registry.

## Installation

```bash
pip install 'harbor[e2b]' agent-sandbox-harbor
```

The plugin pulls [`agent-sandbox-e2b`](https://pypi.org/project/agent-sandbox-e2b/) as a hard
dependency (it calls `patch_e2b()` at import). `harbor` is an optional peer dependency, so the
package can be inspected / unit-tested without it; in real usage you install `harbor[e2b]`
yourself.

## Quick start

```bash
# 1. Set credentials (one-off)
cat > agentbox.env <<'EOF'
E2B_API_KEY=agbx_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
E2B_DOMAIN=agent-sandbox-data-plane.example.com/agent-sandbox/api/data
E2B_API_URL=https://agent-sandbox-data-plane.example.com/agent-sandbox/api/e2b
AGBX_CLUSTER_ID=cluster-a
AGBX_POOL_NAME=terminal-bench-pool
AGBX_IMAGE_PREFIX=registry.internal/agent-sandbox
EOF

# 2. Run Harbor (use the plugin via the official --environment-import-path flag)
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -n 16 -y \
  --env-file agentbox.env
```

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `E2B_API_KEY` | yes | Agent Sandbox API key (`agbx_...`). |
| `AGBX_POOL_NAME` | yes | Pre-warmed pool name. |
| `E2B_DOMAIN` | no | Data-plane gateway, host[:port][/path]. Default is the in-cluster service. |
| `E2B_API_URL` | no | E2B-compatible control-plane URL, including scheme. |
| `AGBX_CLUSTER_ID` | no | Cluster id prefix (e.g. `cluster-a`). Omit for single-cluster setups. |
| `AGBX_IMAGE_PREFIX` | no | Mirror prefix (e.g. `registry.internal/agent-sandbox`). `docker.io/` is stripped before the prefix is applied. |
| `AGBX_HTTPS` | no | `true`/`false` for the data-plane scheme (default `true`). |
| `AGBX_STARTUP_TIMEOUT` | no | Sandbox startup timeout, seconds (default `300`). |
| `AGBX_READY_TIMEOUT` | no | Cold-image readiness ceiling, seconds (default `600`). |

## How it works

`AgentSandboxEnvironment` subclasses Harbor's `E2BEnvironment` and overrides three methods:

- `_does_template_exist` → always returns `True`
- `_create_template` → no-op
- `_create_sandbox` → calls `AsyncSandbox.create(template="cluster::pool//image", secure=False, ...)`

`__init__` calls `super().__init__()` first, so Harbor's stock Dockerfile parsing still runs
(and sets `self._workdir` from the image's `WORKDIR`). The constructor then overrides
`self._template_name` with the Agent Sandbox pool shorthand.

At module import, `patch_e2b()` from
[`agent-sandbox-e2b`](https://pypi.org/project/agent-sandbox-e2b/) redirects the e2b SDK to
your Agent Sandbox endpoints.

See [INTEGRATION.md](INTEGRATION.md) for full design notes, the `--environment-import-path`
mechanism explanation, and operational guidance.

## Compatibility

Each release build is tested against the latest published versions of
`harbor` and `e2b`. The pinned upper bound in `[project.optional-dependencies]` is updated
automatically by the release CI to reflect the highest verified `harbor` version.

## License

Apache 2.0.
