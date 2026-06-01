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
- **Bring-your-own image.** An optional task-name → image map (`AGBX_IMAGE_MAP`) lets you run
  pre-built images for any dataset — including ones whose `task.toml` has no `docker_image`
  (e.g. SWE-bench, where the task is a Dockerfile).

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
| `AGBX_IMAGE_MAP` | no | Path to a `<task-name> <image>` map file (one per line; `=` also accepted). If a task matches, that image is used **verbatim**. See [Image selection](#image-selection). |
| `AGBX_IMAGE_PREFIX` | no | Mirror prefix applied to the task's `docker_image` (e.g. `registry.internal/agent-sandbox`). `docker.io/` is stripped first. Not applied to `AGBX_IMAGE_MAP` values. |
| `AGBX_IMAGE_TAG` | no | Override the tag of the task's `docker_image` after rewriting. Not applied to `AGBX_IMAGE_MAP` values. |
| `AGBX_HTTPS` | no | `true`/`false` for the data-plane scheme (default `true`). |
| `AGBX_STARTUP_TIMEOUT` | no | Sandbox startup timeout, seconds (default `300`). |
| `AGBX_READY_TIMEOUT` | no | Cold-image readiness ceiling, seconds (default `600`). Large images (e.g. SWE-bench) may need more. |

> **e2b SDK ≥ 2.24:** newer e2b SDKs reject non-`e2b_` API keys client-side. Use
> `agent-sandbox-e2b >= 0.0.4`, whose `patch_e2b()` neutralizes that check so `agbx_` keys work
> (needed when running on `harbor >= 0.13`, which pulls a newer e2b).

## Image selection

The image for each task is chosen in this order:

1. **`AGBX_IMAGE_MAP` entry** — if the file maps the task name (Harbor's `environment_name`,
   i.e. the task / instance id) to an image, that image is used **verbatim**. This is how you
   run datasets whose `task.toml` has **no** `docker_image` (e.g. SWE-bench): pre-build / mirror
   the images once, list them here.

   ```text
   # <task-name>  <image-ref>
   astropy__astropy-7606  registry.internal/agentbox/swebench/sweb.eval.x86_64.astropy_1776_astropy-7606:260328
   django__django-11265   registry.internal/agentbox/swebench/sweb.eval.x86_64.django_1776_django-11265:260328
   ```

2. **`task.toml` `docker_image`** — if there's no map entry but the task sets
   `[environment] docker_image` (e.g. Terminal-Bench), that image is used, after optional
   `AGBX_IMAGE_PREFIX` / `AGBX_IMAGE_TAG` rewriting.

3. Otherwise the task is **rejected**. This environment only runs **pre-built** images — it does
   not build images from a Dockerfile and does not mutate a running sandbox. Datasets that ship a
   Dockerfile (with extra `RUN` layers) must be built/mirrored ahead of time and listed in
   `AGBX_IMAGE_MAP`.

### Example: SWE-bench (Dockerfile-based dataset)

```bash
# 1. Pre-build the images the dataset's Dockerfile would produce (base + your overlay),
#    push them to your registry, and write a map file:
#       astropy__astropy-7606  registry.internal/.../sweb.eval.x86_64.astropy_1776_astropy-7606:<tag>
#       ...
# 2. Point the plugin at it and run:
harbor run \
  -d swebench-verified@1.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  --env-file swebench.env          # contains AGBX_IMAGE_MAP=swebench_image_map.txt
```

## How it works

`AgentSandboxEnvironment` subclasses Harbor's `E2BEnvironment` and overrides three methods:

- `_does_template_exist` → always returns `True`
- `_create_template` → no-op
- `_create_sandbox` → calls `AsyncSandbox.create(template="cluster::pool//image", secure=False, ...)`

`__init__` calls `super().__init__()` first, so Harbor's stock Dockerfile parsing still runs
(and sets `self._workdir` from the image's `WORKDIR`). The constructor then resolves the image
(see [Image selection](#image-selection)) and overrides `self._template_name` with the Agent
Sandbox pool shorthand `cluster::pool//image`.

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
