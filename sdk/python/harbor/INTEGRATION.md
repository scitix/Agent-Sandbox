# Harbor × Agent Sandbox — Integration Guide

> Run [Harbor](https://github.com/harbor-framework/harbor) benchmarks on
> [Agent Sandbox](https://github.com/scitix/agent-sandbox) pre-warmed pools.
>
> Reference validation: `terminal-bench@2.0` × `oracle` agent, 89 tasks, full concurrency.

---

## Table of Contents

1. [Does this fork Harbor?](#1-does-this-fork-harbor)
2. [What `--environment-import-path` actually does](#2-what---environment-import-path-actually-does)
3. [Package layout & naming](#3-package-layout--naming)
4. [User workflow — straight `harbor` CLI](#4-user-workflow--straight-harbor-cli)
5. [Running the full Terminal-Bench 2.0 suite](#5-running-the-full-terminal-bench-20-suite)
6. [Design decisions](#6-design-decisions)
7. [Upstream PR path](#7-upstream-pr-path-future-work)

---

## 1. Does this fork Harbor?

**No.** The plugin lives entirely outside Harbor as an independent PyPI package
(`agent-sandbox-harbor`). It uses Harbor's official `--environment-import-path` extension
point, so no Harbor source changes are required.

```
$ git -C path/to/harbor status
nothing to commit, working tree clean
```

---

## 2. What `--environment-import-path` actually does

`harbor run` exposes two mutually exclusive ways to select the sandbox backend:

```text
--environment-type / -e         e.g. e2b, docker, daytona, novita, …       (built-in registry)
--environment-import-path       e.g. mypkg.module:MyEnvironment            (Python import path)
```

When you pass `--environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment`, Harbor
runs this code (`src/harbor/environments/factory.py:165-211`, unmodified upstream):

```python
def create_environment_from_import_path(cls, import_path, ...):
    if ":" not in import_path:
        raise ValueError("Import path must be in format 'module.path:ClassName'")

    module_path, class_name = import_path.split(":", 1)
    module = importlib.import_module(module_path)        # 1. import the plugin module
    Environment = getattr(module, class_name)             # 2. fetch the class
    return Environment(                                   # 3. instantiate with Harbor's args
        environment_dir=environment_dir,
        environment_name=environment_name,
        session_id=session_id,
        trial_paths=trial_paths,
        task_env_config=task_env_config,
        logger=logger,
        **kwargs,
    )
```

For us this means:

- `agent_sandbox_harbor` is a normal Python package; once `pip install`'d it's importable.
- `AgentSandboxEnvironment` must implement Harbor's `BaseEnvironment` contract (`start`,
  `stop`, `exec`, `upload_file/dir`, `download_file/dir`, …). We get this for free by
  subclassing the built-in `E2BEnvironment` and only overriding the methods we need to change.

### Import-time sequence

```text
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -i fix-git \
  -y --env-file agentbox.env
        │
        ▼
Harbor CLI parses --env-file, populating os.environ with AGBX_* / E2B_*
        │
        ▼
Harbor factory: importlib.import_module("agent_sandbox_harbor")
        │
        ├── triggers agent_sandbox_harbor/environment.py module body
        │     - patch_e2b(https, domain, api_url)      # redirect e2b SDK to your cluster
        │     - from e2b import AsyncSandbox            # already patched
        │     - from harbor.environments.e2b import E2BEnvironment
        │
        ▼
Harbor factory: getattr(agent_sandbox_harbor, "AgentSandboxEnvironment")
        │
        ▼
Per-trial:
  env = AgentSandboxEnvironment(
      environment_dir=<task_dir>/environment,     # contains Dockerfile
      task_env_config=<parsed task.toml>,         # docker_image, workdir, cpus, …
      ...
  )
  await env.start(...)                            # overridden: skip build, create sandbox
  await env.exec(...)                             # inherited from E2BEnvironment
  await env.upload_dir(...)                       # inherited from E2BEnvironment
  ...
  await env.stop(...)                             # inherited from E2BEnvironment
```

> **`patch_e2b()` ordering.** It must run before the e2b SDK is used. The plugin places it at
> **module top-level**, with every `from e2b ...` and `from harbor.environments.e2b ...`
> import placed _after_ the `patch_e2b()` call. Because Harbor lazy-loads the e2b backend
> (only on demand inside its factory), the plugin module is always imported (and patched)
> first.

---

## 3. Package layout & naming

```
sdk/python/harbor/
├── pyproject.toml                                     # PyPI metadata
├── README.md
├── INTEGRATION.md                                     # this file
├── Makefile
└── agent_sandbox_harbor/
    ├── __init__.py                                    # exports AgentSandboxEnvironment
    └── environment.py                                 # ~150 lines of logic
```

| Concept | Name |
|---|---|
| PyPI package | `agent-sandbox-harbor` |
| Python module | `agent_sandbox_harbor` |
| Entry class | `AgentSandboxEnvironment` |
| Harbor entry string | `agent_sandbox_harbor:AgentSandboxEnvironment` |
| Environment-variable prefix | `AGBX_*` (matches the `agbx_` API-key prefix) |

Server-side identifiers (the `agbx_` API-key prefix, the `agentbox.scitix.ai/...` Pod metadata
keys) are unchanged — they belong to the Agent Sandbox service, not this plugin.

---

## 4. User workflow — straight `harbor` CLI

The plugin is invoked via `harbor`'s standard CLI; no wrapper script needed.

### Single task

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -i fix-git \
  -n 1 -y \
  --env-file agentbox.env
```

### A handful of tasks

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -i cancel-async-tasks -i chess-best-move -i fix-git \
  -n 3 -y \
  --env-file agentbox.env
```

### First N tasks of the dataset

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -l 10 -n 4 -y \
  --env-file agentbox.env
```

### Exclude specific tasks

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -x crack-7z-hash -x torch-pipeline-parallelism \
  -n 16 -y \
  --env-file agentbox.env
```

### Different agent (real LLM, not oracle)

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a claude-code \
  --model anthropic/claude-opus-4-5 \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -n 16 -y \
  --env-file agentbox.env
```

### Key flags (cheat sheet)

| Flag | Purpose |
|---|---|
| `-d`, `--dataset` | Dataset, `name@version` (e.g. `terminal-bench@2.0`). |
| `-a`, `--agent` | Agent name (`oracle`, `nop`, `claude-code`, `claude-agent`, …). |
| `--environment-import-path` | **Plugin selector.** `agent_sandbox_harbor:AgentSandboxEnvironment`. |
| `--env-file` | `.env` file loaded into `os.environ` before plugin import. |
| `-i`, `--include-task-name` | Run only the listed tasks (repeatable). |
| `-x`, `--exclude-task-name` | Skip the listed tasks (repeatable). |
| `-l`, `--n-tasks` | Limit to first N tasks of the dataset. |
| `-n`, `--n-concurrent` | Concurrency (default 4). |
| `-y`, `--yes` | Skip all confirmation prompts. |
| `--job-name` | Custom subdirectory under `jobs/`; default is a timestamp. |

---

## 5. Running the full Terminal-Bench 2.0 suite

```bash
harbor run \
  -d terminal-bench@2.0 \
  -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  -n 16 -y \
  --job-name oracle-tb2-full \
  --env-file agentbox.env
```

> **Concurrency vs. pool capacity.** `-n` is the Harbor-side concurrency. The actual ceiling
> is the number of **idle** Pods in the pool at any moment. If you raise `-n` above the pool's
> `replicas`, expect `500: no idle sandboxes available in the pool` errors. Either lower `-n`,
> increase the `SandboxPool.spec.replicas`, or rely on Harbor's `--max-retries` /
> `--retry-include` to retry the affected trials.

### Approximate runtime (89 tasks, oracle agent)

| Bucket | Count | Per-task wall time |
|---|---|---|
| Fast (≤ 60 s) | ~70 | 15–60 s |
| Medium (1–3 min) | ~15 | 60–180 s |
| Slow (≥ 5 min) | ~4 (`crack-7z-hash`, `torch-pipeline-parallelism`, …) | 5–15 min |

With `-n 16` the full sweep typically finishes in **15–25 min**, gated by the slowest task.

### Reviewing results

```bash
harbor view jobs/oracle-tb2-full
cat jobs/oracle-tb2-full/result.json
```

---

## 6. Design decisions

### 6.1 What the override actually changes

```
AgentSandboxEnvironment(E2BEnvironment):
├── module top-level: patch_e2b()         # route the e2b SDK to your cluster
├── __init__:
│   ├── super().__init__()                # parent parses Dockerfile → _workdir
│   └── self._template_name = f"{cluster}::{pool}//{mirror_image}"
├── _does_template_exist → True           # skip alias_exists probe
├── _create_template     → no-op          # skip Template Build
└── _create_sandbox:
    ├── AsyncSandbox.create(secure=False, metadata={...})
    └── cold-image readiness poll         # retry `true` exec until envd responds
```

All other methods — `exec`, `upload_file`, `upload_dir`, `download_file`, `download_dir`,
`stop`, mount handling — are inherited verbatim from Harbor's stock `E2BEnvironment`.

### 6.2 Why this works

- **Skip Template Build.** Agent Sandbox's value is the pre-warmed Pod pool with in-place
  image swap. Passing the image straight into `POST /v1/sandboxes` is 1–2 orders of magnitude
  faster than E2B's "build a Template per image" flow for benchmarks with many distinct
  images (Terminal-Bench 2.0 has 89).
- **Preserve Harbor's Dockerfile parsing.** Terminal-Bench 2.0 ships a real
  `environment/Dockerfile` with `WORKDIR` declarations for every task (some tasks use a
  non-default subdir, e.g. `fix-git` → `/app/personal-site`). Letting `super().__init__()`
  run gives us the correct `_workdir` for free — **no manifest probing needed.**
- **Cold-image readiness poll.** When a pool node sees a new image for the first time it has
  to pull from the mirror registry. `AsyncSandbox.create` may return before envd is reachable;
  the first `commands.run()` then fails with `Code.unknown`. A trivial `true` exec retry-loop
  is the cheapest reliable backstop.

### 6.3 Empirical reward parity

Oracle on Terminal-Bench 2.0, 7-task representative subset, executed twice with the same
plugin, validates the design:

| Task | Reward | Elapsed |
|---|---|---|
| cancel-async-tasks | ✅ 1.0 | 25–28 s |
| chess-best-move | ✅ 1.0 | 26 s |
| headless-terminal | ✅ 1.0 | 27–32 s |
| polyglot-c-py | ✅ 1.0 | 14–17 s |
| regex-log | ✅ 1.0 | 15–22 s |
| **fix-git** *(non-default WORKDIR)* | ✅ 1.0 | 12–17 s |
| crack-7z-hash | ✅ 1.0 | 445–757 s (`john` is CPU-bound) |

All 7 pass — fix-git included, with the WORKDIR coming from `super().__init__()` parsing the
shipped Dockerfile, not from any client-side registry probe.

---

## 7. Upstream PR path (future work)

If this plugin is eventually merged into Harbor (along the lines of Novita's
[PR #1025](https://github.com/harbor-framework/harbor/pull/1025)), four changes would be
needed in Harbor itself:

1. New `src/harbor/environments/agent_sandbox.py` housing the class.
2. New entry in `src/harbor/models/environment_type.py`, e.g.
   `AGENT_SANDBOX = "agent-sandbox"`.
3. Register the class in `src/harbor/environments/factory.py` `_ENVIRONMENT_REGISTRY`.
4. Add an optional dependency block in `pyproject.toml`:
   `agent-sandbox = ["agent-sandbox-e2b>=0.0.2"]`.

After merge, the user-facing command shortens from:

```bash
harbor run -d terminal-bench@2.0 -a oracle \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment ...
```

to:

```bash
harbor run -d terminal-bench@2.0 -a oracle --environment-type agent-sandbox ...
```

`--environment-import-path` and `--environment-type` are parallel by Harbor's design; the
former is the "temporary plug-in" path, the latter the "officially registered backend" path.
