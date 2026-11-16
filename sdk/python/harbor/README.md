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
| `AGBX_ENV_NAME` | unless profiles specify it | Existing SandboxEnv to run tasks in. |
| `AGBX_POOL_NAME` | no | Legacy alias for `AGBX_ENV_NAME`; both must agree if set. |
| `AGBX_POOL_PROFILES` | no | JSON capacity profiles selecting an existing Env and scaling group; see below. |
| `AGBX_SCALING_GROUP` | no | Exact existing group name. With profiles, restrict selection to this group and validate capacity; without profiles, pin placement only. |
| `AGBX_DIND_ENV_NAME` | for Compose, unless profiles specify it | Existing Docker-in-Docker SandboxEnv. `AGBX_DIND_POOL_NAME` is its legacy alias. |
| `AGBX_DIND_POOL_PROFILES` | no | Separate capacity profiles for Compose's outer DinD sandbox. |
| `AGBX_DIND_SCALING_GROUP` | no | Fixed group restriction for the DinD environment, independent of ordinary-task routing. |
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
- `_create_sandbox` → calls `AsyncSandbox.create(template="cluster::pool//image", ...)`

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

## Pre-provisioned environments and scaling groups

Provision the `SandboxEnv` and its member `SandboxPool`s before running Harbor.
The plugin only creates sandboxes from these resources; it does not create or
resize environments, groups, or pools. Use the actual group names reported for
the environment. Names are case-sensitive and are never synthesized from CPU or
memory values.

There are two routing values: the E2B template is
`<cluster>::<env>//<image>`, while create metadata
`agentbox.scitix.ai/scaling-group` carries the selected group. The server only
selects members of that Env in that group. An unavailable or nonexistent group
fails instead of falling back to another member. This requires a server version
that supports the reserved scaling-group metadata key; do not point the group
mode at a direct member-pool name that bypasses Env routing.

For one fixed pre-provisioned group:

```bash
export AGBX_ENV_NAME=benchmark-env
export AGBX_SCALING_GROUP=balanced-workers
```

Without profiles this explicitly pins placement, but the plugin has no capacity
inventory to validate. The operator must confirm the fixed group's real resource
shape covers every task. GPU tasks always require profiles. To validate resource
requirements and choose a group automatically, set `AGBX_POOL_PROFILES` to:

```json
[
  {"env":"benchmark-env","scaling_group":"small-workers","cpus":1,"memory_mb":2048,"storage_mb":16384,"gpus":0},
  {"env":"benchmark-env","scaling_group":"balanced-workers","cpus":4,"memory_mb":8192,"storage_mb":16384,"gpus":0},
  {"env":"accelerator-env","scaling_group":"single-gpu","cpus":8,"memory_mb":32768,"storage_mb":65536,"gpus":1,"gpu_type":"H100"}
]
```

A profile may omit `env` when `AGBX_ENV_NAME` (or its legacy alias) supplies the
default. An explicit profile `env` takes precedence over that default. Every
Env-style profile must specify `scaling_group`, preventing capacity selection
from being lost when the request reaches a multi-group Env. CPU, memory, and
storage are compared with Harbor's effective task values, including resource
overrides; missing declared capacity counts as zero. GPU count must match exactly,
and a requested GPU model must be allowed. CPU-only tasks do not select GPU groups.
Among fitting profiles selection is deterministic: CPU, memory, storage, Env,
then group name in ascending order.

If `AGBX_SCALING_GROUP` is also set, it filters profiles by that exact group
**before** selecting a resource fit. A too-small fixed group or absent group
raises an error before `AsyncSandbox.create`; neither a larger group nor the
legacy default target is silently substituted. Profiles are declarations that
must match provisioned resources, not live capacity discovery. Invalid/empty
profiles and non-finite or negative capacities fail closed.

Legacy `{"pool":"target","cpus":...,"memory_mb":...,"storage_mb":...}`
profiles remain supported and retain their historical template-target meaning.
They omit group metadata and therefore do not guarantee group-level placement
inside a multi-group Env. Migrate them to `env` plus `scaling_group` for that
purpose. If a profile contains both `env` and `pool`, they must agree.

Compose uses the same selection rules with `AGBX_DIND_ENV_NAME`,
`AGBX_DIND_POOL_PROFILES`, and `AGBX_DIND_SCALING_GROUP`; it never inherits an
ordinary task's group accidentally. Its template names the DinD Env without a
task-image override, and its group applies to the outer sandbox, not individual
Compose services.

## GPU and separate verifier pools

The integration has been tested with Harbor 0.25.0 and 0.26.0. Configure `AGBX_POOL_PROFILES`
with a JSON array describing **already provisioned** pools. The plugin chooses
an exact GPU count and an allowed GPU model, with enough CPU, memory and storage.
A GPU task without a matching profile fails before a sandbox is created; it must
never fall back to a CPU pool. For example:

```json
[
  {"pool":"cpu-pool","cpus":8,"memory_mb":16384,"storage_mb":30720,"gpus":0},
  {"pool":"gpu-pool","cpus":8,"memory_mb":16384,"storage_mb":30720,"gpus":1,"gpu_type":"H100"}
]
```

These are capacity declarations, not a provisioning API. Verify them against
actual pool resources before running. Separate verifier environments select their
own profiles and use their own pinned `docker_image`. Prefer the official
prebuilt task bundle, which contains both image references, over a task-name
image override that could incorrectly select the agent image for its verifier.
Task startup environment variables and network policy are forwarded to E2B.

GPU routing unit tests do not establish live API access or full dataset support.
Compose tasks use a separate pre-provisioned DinD pool; see the setup below.


## Terminal-Bench 4: local Harbor, remote sandboxes

This section describes the GPU/Compose integration under development in this
source tree. Do not assume an older PyPI release contains it. Check release notes
for the verified plugin and Harbor versions before installing.

Install Python 3.12+ and the compatible Harbor/E2B extras. During development:

```bash
uv venv --python 3.12 .venv
uv pip install --python .venv/bin/python 'harbor[e2b]==0.25.0' -e .
```

Provision isolated pools before running: a normal sandbox pool for CPU tasks, a
GPU pool with `nvidia.com/gpu` requests/limits and appropriate node placement,
and a DinD pool with a working Docker daemon and Compose v2. The GPU task image
must also be compatible with the node driver. A CUDA image alone does not grant
device access. Size the pools and their storage from the task requirements,
including sidecars and the separate verifier. The example capacities above are
for a small subset, not a capacity plan for the whole benchmark.

Set credentials privately in the local process. Copy the API URL and data-plane
domain from your environment's connection documentation. Never commit keys:

```bash
# E2B_API_KEY must already be set through your private credential configuration.
export E2B_API_URL=https://sandbox.example.com/api/e2b
export E2B_DOMAIN=sandbox.example.com/api/data
export AGBX_CLUSTER_ID=example-cluster
export AGBX_POOL_PROFILES="$PWD/pools.json"
export AGBX_DIND_POOL_PROFILES="$PWD/dind-pools.json"
export AGBX_IMAGE_PREFIX=registry.example.com/benchmarks
export AGBX_STARTUP_TIMEOUT=900
export AGBX_READY_TIMEOUT=900
```

Both profile files use the same capacity schema. DinD profiles refer to pools
whose default image starts dockerd. For a single known DinD environment,
`AGBX_DIND_POOL_NAME=dind-env` can be used instead; the operator must ensure it
has enough capacity. Every Compose trial claims a separate sandbox, so identical
Compose project names do not collide across trials. Inner containers, networks
and volumes are removed at stop; the outer sandbox is then released.

Use the official **prebuilt release bundle**, not the source tasks whose
Dockerfiles still require builds. Terminal-Bench release assets include the
bundle and a `prebuilt-images.json` manifest. Mirror every listed image using a
manifest-preserving registry copier; verify target digests against that manifest
and retain tags so registry garbage collection cannot remove untagged content.
The prefix is applied to agent, verifier, and literal Compose service image
references. Leave `AGBX_IMAGE_TAG` unset to preserve the official pins.

Run one task first, then the whole local task directory:

```bash
.venv/bin/harbor run -p ./tasks/math-eval-grader -a oracle -n 1 \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment
.venv/bin/harbor run -p ./tasks -a oracle -n 1 \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment
```

The controller and result files stay local. Commands, model inference and Docker
Compose services run in the remote sandboxes. `oracle` exercises the published
reference solutions; it does not measure an LLM agent's score. Inspect each
trial's exception, verifier result and reward. Successful sandbox creation, image
pulls, or process exit codes alone do not establish a passing benchmark.

Compose mode requires prebuilt service images. `force_build` and additional
Compose overlays are not accepted by this adapter. Preserve authored service
networks, health checks, dependencies and bind mounts in the release bundle.
Per-service exec/download/stop operations use Harbor's DinD operations layer.

For release procedure and compatibility checks, see [PUBLISHING.md](PUBLISHING.md).

### CPU library thread budgets

CPU quotas do not necessarily restrict the host core count visible inside a
container. OpenMP/BLAS libraries may otherwise create too many workers for a
small pool. Set an explicit budget when needed:

```bash
export AGBX_CPU_THREADS=2
```

This sets `OMP_NUM_THREADS`, `MKL_NUM_THREADS`, and `OPENBLAS_NUM_THREADS` only
when the task or persistent environment has not already set each variable. It
is opt-in and must be recorded with benchmark results. It does not change task
code, verifier thresholds, or timeouts.

### Validation scope

The development implementation has exercised local Harbor 0.25.0 / E2B 2.63.0
against Terminal-Bench 4.0.0 reference solutions, including GPU tasks and all 11
Compose tasks. Of 66 tasks, 65 have completed with reward 1 in the validation
setup. This is an infrastructure integration check, not an LLM leaderboard run.
Two adaptations must be reported alongside that count:

- `cad-model`: the agent environment preinstalls `ocp_gordon==0.1.17` with
  `build123d==0.10.0`, avoiding an incompatible unbounded dependency resolution.
  The reference solution and verifier are unchanged.
- `vpp-loss-divergence`: `AGBX_CPU_THREADS=2`, with the original verifier timeout.

The `jax-speedrun-gpu` task has not been validated in that setup because its
1000 GiB storage requirement exceeds the available node ephemeral capacity.
Do not silently reduce the task requirement or report this task as passing.
A deployment must also provide the task's CPU, memory, accelerator and storage
capacity; profiles are declarations, not capacity reservations.

Some release archives include AppleDouble `._*` metadata alongside real task
files. Preserve the original archive; when preparing a Linux working copy,
identify AppleDouble files by their header and exclude only verified metadata.
Record the paths/hashes removed. Do not indiscriminately delete task files or
change reference solutions to accommodate archive metadata.

Mirror inventory must include third-party Compose references as well as the
release's prebuilt-image manifest. For this dataset, Kafka and Redis are direct
runtime dependencies outside that manifest.

### Terminal-Bench 2.1 / Harbor 0.26 compatibility

The local development plugin was tested with Harbor 0.26.0 and E2B 2.64.0,
using Terminal-Bench 2.1 commit `12b7565679ce5006230b820666353673a133db93`.
Harbor 0.26 uses its public `prebuilt_template_id` startup path; the plugin
retains upstream runtime preparation and lease renewal and supports the renamed
sandbox timeout field. Unit tests also cover Harbor 0.25 compatibility.

The revised Golden suite has obtained reward 1 with no exception for all 89
distinct tasks across batches and targeted retries. The final `qemu-alpine-ssh`
reference-solution synchronization correction passed three independent trials.
This is **89/89 cumulative coverage of the disclosed adapted suite**, not a
single concurrent clean run, and not an unmodified official-oracle result.

The validation uses disclosed environment adaptations, not just original images:

- `build-cython-ext`: a derived image supplies the compatible planarity 0.6
  dependency; the original solution and verifier remain unchanged.
- `mcmc-sampling-stan`: a derived image adds required build dependencies and
  records the dependency-upgrade policy used with the original solution.
- `protein-assembly`: a derived image snapshots audited external RCSB input
  records and restores missing API metadata through a documented compatibility
  adapter. It contains scientific input data, not generated task answers.
- QEMU tasks use documented runtime-image adaptations. For `qemu-alpine-ssh`,
  an additional explicit oracle patch waits for real console completion output
  rather than echoed command text. This is an explicit solution modification,
  so its successful result cannot be called unmodified official Golden.
- `custom-memory-heap-crash`: a separate sandbox pool grants SYS_RESOURCE so
  the reference solution can genuinely raise its own core-dump limit. The envd
  memguard dependency otherwise initializes that inherited hard limit to zero.
  This capability is not enabled for the ordinary benchmark pool.

Original scoring assertions and thresholds are retained. The delivery audit must
include every image recipe/digest, runtime setting, oracle patch and retry; do
not pool diagnostic/modified-solution results into official-unmodified counts.
The early 12-task smoke run, dependency-control experiments, and subsequent full
batches remain separate evidence. `build-pmars` passes using the official 2.1
solution, which removes obsolete Debian package pins. A cluster mirror must have
every rendered digest: a valid client map is insufficient if registry routing
rewrites its host to a mirror missing that manifest.

These are reference-solution infrastructure checks, not LLM performance results.
These changes have not been published to PyPI. The local 0.0.14 distribution
records this validation scope; its delivery checksum identifies the exact build. Pinning
images does not freeze all packages or services used at task runtime. Harbor
0.26's E2B backend kills sandboxes at stop even with `--no-delete`; collect
diagnostics before stop.
