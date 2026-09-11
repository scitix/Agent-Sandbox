---
name: abx-reinforcement-learning
description: Set up and run RL rollouts on AgentBox — choosing a template, sizing a warm pool for the concurrency a trainer needs, and driving the sandboxes with the E2B SDK. Use when asked to run rollouts, collect trajectories, scale an environment for RL training, or work out why a rollout loop is stalling on sandbox creation. Pool mechanics live in abx-resource-capacity; benchmarks in abx-harbor-framework.
---

# RL rollouts on AgentBox

## Before configuring anything, ask what concurrency they need

It is the only number that decides the whole shape of the answer, it is never
in the question, and guessing it wastes the conversation: a pool sized for 8
when they wanted 200 looks like it worked right up until the run stalls.

Ask for **peak concurrent sandboxes**, not total episodes. People usually know
the second and have to be walked to the first: 10,000 episodes at 64 in flight
needs 64.

Two more defaults worth stating rather than deciding silently:

- **Leave autoscaling on**, with `maxReplicas` at their peak. A fixed pool
  holds capacity between runs and bills for it; a group with a ceiling drains
  and comes back.
- **Set `minReplicas` to the steady-state floor** when the run ramps faster
  than the scale-up cooldown, so the autoscaler only handles the tail.

## The division of labour

**`abx` provisions, the E2B SDK executes.** You use `abx` once to make sure
there is capacity, and then your trainer talks E2B for the rest of the run.

```
trainer process                     AgentBox
  ├─ abx envs … pools … scale        provision the pool once, up front
  └─ e2b.Sandbox(...)  × N           claim / run / discard, per episode
```

Sandboxes are **claimed from a warm pool**, not built. That is why a rollout
gets an environment in about a second instead of a minute, and it is also why
the pool has to be the right size before the trainer starts.

## Driving sandboxes

Standard E2B — the platform serves the E2B-compatible API, so the SDK you would
already reach for works unchanged:

```python
from e2b import Sandbox

sbx = Sandbox(template="<env-name>", timeout=600, metadata={"run": run_id})
result = sbx.commands.run("python solve.py")
sbx.kill()
```

`metadata` is worth using: it is what tells two sandboxes apart later, and
`abx sandboxes --wide` shows it.

> SWE ReX is **deprecated** — do not reach for it. E2B is the interface.

## Sizing the pool

```bash
abx envs                                        # what exists
abx envs <env> pools                            # idleReplicas is what you can claim now
abx instancetypes                               # shapes and relative cost
abx quotas                                      # your ceiling

abx envs <env> pools <pool> scale --replicas 64
```

Then leave autoscaling on with a `maxReplicas` at your peak, so the pool drains
between runs rather than holding capacity idle:

```bash
abx envs <env> scaling-groups <group> --json > g.json
# set enabled: true, maxReplicas: 64, mode to taste
abx envs <env> scaling-groups <group> apply -f g.json
```

`apply` is a whole-object PUT — start from the `--json` you just read, never
from a blank file, or you will clear the fields you left out.

## The three stalls, in the order they happen

1. **Claims queue and the pool does not grow.** Demand-anchored scaling grows
   toward what is actually being asked for; a wide `maxReplicas` is a ceiling,
   not a request. If the pool is stuck, **reduce the replica target and grow
   again** — a pool asking for more than the cluster can place stays stuck
   asking. See `abx-resource-capacity`.
2. **Quota is the ceiling.** `abx quotas`. No pool setting fixes this.
3. **Sandboxes are created but commands fail.** The runtime inside the Pod did
   not come up. `abx sandboxes <id> logs` first; see `abx-observe`.

## Running a rollout loop against a pool that is also being scaled

Claims are served from idle Pods, so a trainer at steady state and an autoscaler
adjusting the pool do not fight — but a trainer that ramps faster than the
scale-up cooldown will see queueing. If the run's concurrency is known up front,
set the floor with `minReplicas` on the group and let the autoscaler only handle
the tail.

## Related

- `abx-resource-capacity` — the full autoscaler model and the recovery procedure
- `abx-harbor-framework` — running an actual benchmark rather than free-form rollouts
- `abx-observe` — logs, events and E2B-native metrics
- `abx-common` — endpoint, key, cluster, approval
