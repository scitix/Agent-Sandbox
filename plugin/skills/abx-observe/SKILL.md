---
name: abx-observe
description: Work out why an AgentBox sandbox, pool or environment is failing or slow — events, pool status, sandbox logs, and the E2B-native metrics endpoints. Use when a sandbox will not start, a pool sits below target, commands inside a sandbox fail, or someone asks how much CPU and memory sandboxes are using. Authentication and cluster selection live in abx-common.
---

# Observing: what broke, and what it is costing

## Start from the object, not the symptom

```bash
abx envs demo                     # is the env itself ready
abx envs demo pools               # desired vs idle vs running, per shape
abx envs demo events              # what Kubernetes said, most recent first
abx sandboxes --filter status=Failed
abx sandboxes <id> logs
```

`events` is the highest-yield of these and the most often skipped. A pool that
will not grow almost always has a Warning event saying why, in Kubernetes'
words rather than the platform's.

## Metrics come from E2B, not from abx

The platform serves the **E2B-compatible** metrics endpoints, so the way to
read resource usage is the E2B SDK you already have in the sandbox — not a
separate `abx` command, and not a Prometheus query you have to construct:

Which API that is depends on the env's cluster: `abx envs <env> docs` prints the
E2B API URL and data-plane domain the SDK should be pointed at.

```python
from e2b import Sandbox

sbx = Sandbox.connect(sandbox_id)
metrics = sbx.get_metrics()          # cpu, memory, disk over the sandbox's life
```

Two endpoints, both E2B's own:

| | |
|---|---|
| `GET /sandboxes/{id}/metrics` | a time series for one sandbox |
| `GET /sandboxes/metrics?sandbox_ids=…` | the latest point for several |

**`/teams/{id}/metrics` is not implemented** — it answers `501`, deliberately:
team and cluster administration is not exposed through the E2B surface. For
usage across a team, read `abx statistics` or the console.

Deliberately no new convention for the two that do exist: a caller who already
speaks E2B needs nothing extra, and one who does not is better served learning
E2B than a bespoke metrics dialect.

**Units, because the two surfaces differ and it has caught people out:**

- `cpuUsedPct` from the API is a **percentage of the sandbox's own cores**
  (`cores_used / cpuCount * 100`). A 16-core sandbox with 8 busy cores reads
  `50.08`, not `8` and not `5008`.
- The console's charts plot **cores**, not a percentage. The same moment reads
  `50%` in one place and `8` in the other, and both are right.
- `diskTotal` / `diskUsed` are `0` wherever the backend does not collect
  `container_fs_*`. Zero here means "not collected", not "empty disk".

Time-series **charts** live in the console. `abx` prints a `view:` link on
every command, and for an env or pool that link lands on the page with the
charts. When asked for a trend rather than a number, hand over the link.

## The failures that look like something else

| Symptom | Usually |
|---|---|
| Sandbox created, commands fail to connect | the runtime never came up inside the Pod — check `abx sandboxes <id> logs` before anything else |
| Pool stuck at 0 available | reservation or quota refused the Pods; see `abx-resource-capacity` — reduce the target, then grow |
| Env lists a running sandbox the sandbox list does not show | two different counts: the env's is cluster-wide, the list is filtered to your tenant |
| Everything empty but nothing errors | the credential's namespace does not exist on THIS cluster — `abx whoami` shows which namespace it resolved to |
| `--cluster` refused | that endpoint serves one cluster; use an endpoint whose path carries `{cluster}` |

## Reading a pool's status honestly

```bash
abx envs demo pools demo-1c2gi --json | jq '.status'
```

`idleReplicas` is the only number that answers "can I claim one right now".
`replicas` is intent, `updatedReplicas` is rollout progress, and a pool can
report `Ready` while having nothing claimable.

## Related

- `abx-resource-capacity` — the fix, once you know it is capacity
- `abx-common` — endpoint, key, cluster, approval

## Read more

[Sandbox pools](https://scitix.github.io/Agent-Sandbox/docs/concepts/pools.md)
