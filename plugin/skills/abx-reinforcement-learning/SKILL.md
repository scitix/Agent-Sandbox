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
already reach for works unchanged. The call's fields belong to that SDK, not to
this CLI: read them from the SDK source the image ships
(`/opt/agentbox/source/sdk/`) or the E2B spec beside it, rather than from a
sketch in a document.

Before the first sandbox, read where to point it. `abx envs <env> docs` prints
the env's documentation, rendered for its cluster: the E2B API URL, the
data-plane domain, and the scheme — the three things the SDK cannot guess and
will not complain about. Where more than one way in is offered, use the public
one unless the trainer runs inside the cluster. The key stays `${AGBX_API_KEY}`
in that document; the SDK reads the same one from `E2B_API_KEY`.

```python
from e2b import Sandbox

sbx = Sandbox.create("<env-name>", …)     # the env name; the rest is the SDK's
result = sbx.commands.run("python solve.py")
sbx.kill()
```

Two things to carry over whatever the signature says: the `template` is the
**env name** (`abx envs` lists them), and tagging each sandbox is worth the
trouble — it is what tells two of them apart later, and `abx sandboxes --wide`
shows it. Both are the SDK's fields, so take their names from the SDK and not
from this line.

> SWE ReX is **deprecated** — do not reach for it. E2B is the interface.

## Sizing the pool

```bash
abx envs                                        # what exists
abx envs <env> pools                            # idleReplicas is what you can claim now
abx instancetypes                               # shapes and relative cost
abx quotas                                      # your ceiling

abx envs <env> pools <pool> scale --replicas 64
```

Then leave autoscaling on with a ceiling at your peak, so the pool drains
between runs rather than holding capacity idle:

```bash
abx envs <env> scaling-groups <group> --editable > g.json
# edit it — the fields, and what each one does, are in:
#   abx update envs <env> scaling-groups <group> --help
abx update envs <env> scaling-groups <group> -f g.json
```

`update` is a whole-object PUT, and `--editable` is the file it takes — the
object `--json` prints is a different document (nested under `spec.*`), and a
file built from it clears whatever it does not carry.

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
