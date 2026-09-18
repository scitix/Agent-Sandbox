---
name: abx-resource-capacity
description: Size AgentBox warm pools, configure autoscaling groups, read quota, and recover when a pool will not grow. Use when sandboxes queue or fail to start, when a pool sits below its target, when scaling is too slow or runs away, or when you need N concurrent sandboxes and must work out whether the capacity exists. Authentication and cluster selection live in abx-common.
---

# Capacity: replicas, autoscaling, quota

## The objects, in the order they matter

```
SandboxEnv          what a sandbox is made from (one template)
  └─ SandboxPool    a warm pool of one resource shape — this is what has a size
       └─ group     the autoscaling policy several pools share
```

A pool holds **idle** Pods. Claiming one is fast precisely because it already
exists; that is the whole design. `replicas` is how many Pods the pool keeps,
`idleReplicas` how many are claimable right now, `runningReplicas` how many are
in use.

```bash
abx envs
abx envs demo pools
abx envs demo scaling-groups
```

## Setting a size

If the pool's group has autoscaling **off**, the size is yours:

```bash
abx scale envs demo pools demo-1c2gi --replicas 40
```

If the group is **on**, `replicas` belongs to the autoscaler and setting it is
refused — the lever is the group's bounds instead:

```bash
abx envs demo scaling-groups 1c2gi --editable > g.json
# edit it — `abx update envs demo scaling-groups 1c2gi --help` lists the fields
abx update envs demo scaling-groups 1c2gi -f g.json
```

Remember `update` is the whole object: a bound you delete from the file is a
bound you are removing. That is how you take a ceiling **off**, which is worth
knowing because it is the one thing an "update just this field" API could never
express.

## How the autoscaler decides

Scale-up is **demand-anchored**, not step-anchored. It looks at what is
actually being asked for — running sandboxes plus queued requests — and grows
toward that, plus a buffer, capped by a per-mode ceiling relative to demand.
The mode sets the aggressiveness of both directions:

| mode | scale-up buffer | scale-down step |
|---|---|---|
| `Conservative` | smallest | 1 replica per window |
| `Default` | proportional | a quarter of the pool |
| `Aggressive` | largest | half the pool |

Two consequences worth carrying:

- **A wide `maxReplicas` is not an instruction.** Demand is the anchor; the
  ceiling only stops growth. Setting 0–2560 does not ask for 2560.
- **Scale-down is proportional**, so a pool that over-grew drains in minutes
  rather than one replica per stabilisation window.

## When a pool will not grow

Check in this order — the first two are most of the cases:

```bash
abx envs demo pools demo-1c2gi          # phase, and desired vs actual
abx envs demo pools demo-1c2gi --json | jq '.status'
abx quotas                              # is the team's ceiling in the way
abx envs demo events                    # what Kubernetes said about it
```

**The recovery is counter-intuitive and worth stating plainly: reduce the
replica count, then grow again.** A pool asking for more than the cluster can
place stays stuck asking; nothing retries it into existence. Dropping the
target below what is available lets the reservation succeed, and you climb from
there. Doubling down on the number that already failed does nothing.

If quota is the limit, no amount of pool configuration helps — that is a
request to whoever owns the quota, not a setting.

## Defaults worth stating out loud

- **Autoscaling on, with a ceiling.** A fixed pool holds capacity nobody is
  using between runs. A group with `maxReplicas` at the expected peak drains
  and comes back, and the ceiling is what stops a runaway — not a substitute
  for asking how much they need.
- **A wide ceiling is not a request.** Scale-up is anchored on demand; setting
  0–2560 does not ask for 2560, and someone who read it as a target has the
  wrong model of the autoscaler.
- **Never raise a target that just failed.** Reduce it, let the reservation
  succeed, then climb. This is the one procedure people reliably get backwards.

## Planning for N concurrent sandboxes

You need `N` claimable Pods at peak, not `N` over the run:

1. `abx instancetypes` — what shapes exist and what each costs per unit.
2. `abx quotas` — what is committed and what the ceiling is.
3. Size the pool for peak concurrency, not total work. A rollout that runs
   1000 episodes 50 at a time needs 50.
4. Leave the group enabled with a `maxReplicas` at your peak, so the pool
   drains between runs instead of holding capacity nobody is using.

Once the Pods are there, the caller has to reach them: `abx envs <env> docs`
prints the E2B endpoints of that env's cluster, which is what the SDK is
pointed at before the first create.

## Related

- `abx-observe` — the metrics and logs behind "it is slow" or "it failed"
- `abx-reinforcement-learning` — the rollout-shaped version of this
- `abx-common` — endpoint, key, cluster, approval

## Read more

[Autoscaling](https://scitix.github.io/Agent-Sandbox/docs/concepts/autoscaling.md)
