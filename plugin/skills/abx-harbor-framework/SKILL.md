---
name: abx-harbor-framework
description: Run Harbor benchmarks (Terminal-Bench, SWE-bench, custom datasets) on AgentBox pre-warmed pools via the agent-sandbox-harbor environment plugin. Use when asked to evaluate an agent, run a benchmark suite, reproduce a leaderboard number, or work out why benchmark tasks are being rejected or timing out. Pool sizing lives in abx-resource-capacity; endpoint and key in abx-common.
---

# Running evaluations on AgentBox

[Harbor](https://github.com/harbor-framework/harbor) already knows how to drive
a benchmark. `agent-sandbox-harbor` is an environment plugin that makes it claim
sandboxes from a warm pool instead of building one per task — which is where the
time goes in a normal Harbor run.

**No Harbor fork, and no Template Build step.** The plugin attaches through
Harbor's official `--environment-import-path`, and because AgentBox pools swap
the image in place on an already-running Pod, a task starts with one API call
rather than an image build.

## The shape of a run

```bash
pip install 'harbor[e2b]' agent-sandbox-harbor

cat > agentbox.env <<'EOF'
E2B_API_KEY=agbx_…
E2B_API_URL=https://<data-plane host>/agent-sandbox/api/e2b
E2B_DOMAIN=<data-plane host>/agent-sandbox/api/data
AGBX_POOL_NAME=terminal-bench-pool
AGBX_CLUSTER_ID=cluster-a
AGBX_IMAGE_PREFIX=registry.internal/agent-sandbox
EOF

harbor run \
  -d terminal-bench@2.0 -a oracle -n 16 -y \
  --environment-import-path agent_sandbox_harbor:AgentSandboxEnvironment \
  --env-file agentbox.env
```

The two endpoint lines are the env's, not a sketch: `abx envs <env> docs` prints
them for the cluster you are actually reaching, including whether the data plane
is http or https, and `E2B_API_KEY` is the key you authenticate `abx` with (what
that document writes as `${AGBX_API_KEY}`). Where it offers several ways in, use
the public one unless you are already inside the cluster.

`-n 16` is concurrency, and it is the number that has to exist as **idle Pods**
before the run starts moving. Size the pool for it first:

```bash
abx envs <env> pools                          # idleReplicas is the real answer
abx scale envs <env> pools <pool> --replicas 16
```

See `abx-resource-capacity` if it will not grow.

## Images: the part that actually bites

Every task needs a **pre-built** image. This environment does not build from a
Dockerfile and does not mutate a running sandbox, so an image is chosen in
exactly this order:

1. **`AGBX_IMAGE_MAP`** — a `<task-name> <image>` file. Used verbatim, no
   rewriting. This is how you run a dataset whose `task.toml` has no
   `docker_image` at all, which is the case for **SWE-bench**, where the task
   *is* a Dockerfile.
2. **`task.toml`'s `docker_image`** — Terminal-Bench's case. Rewritten by
   `AGBX_IMAGE_PREFIX` (with `docker.io/` stripped first) and `AGBX_IMAGE_TAG`.
3. Neither → **the task is rejected**, deliberately and loudly.

So a SWE-bench run is really two jobs: mirror or build the images once and write
the map file; then run Harbor against it. Budget for the first.

## Settings that matter under load

| Variable | Why you would touch it |
|---|---|
| `AGBX_STARTUP_TIMEOUT` | default 300s; raise for heavy images |
| `AGBX_READY_TIMEOUT` | default 600s; a cold SWE-bench image can exceed it |
| `AGBX_IMAGE_PREFIX` | point every `docker.io/…` at an internal mirror |
| `AGBX_HTTPS` | `false` when the data plane is plain HTTP — a mismatch shows up as "never connects", never as a scheme error |

One version note worth checking before blaming the platform: e2b SDK ≥ 2.24
rejects non-`e2b_` keys client-side. `agent-sandbox-e2b >= 0.0.4` neutralises
that so `agbx_` keys work, and `harbor >= 0.13` pulls a new enough e2b to need it.

## When tasks fail rather than the run

```bash
abx sandboxes --filter status=Failed
abx sandboxes <id> logs
abx envs <env> events
```

A whole dataset failing the same way is almost always the image map or the
registry; individual tasks failing is usually the task.

## Related

- `abx-resource-capacity` — making the concurrency you asked for exist
- `abx-observe` — reading what failed
- Reference: `sdk/python/harbor/README.md` and `INTEGRATION.md` in the
  agent-sandbox repository

## Read more

[Sandbox pools](https://scitix.github.io/Agent-Sandbox/docs/concepts/pools.md)
