---
name: abx-sandbox-docker
description: Run Docker inside an AgentBox sandbox — building images, docker compose, and reaching an internal registry from in there. Use when a workload needs a container runtime of its own, when `docker` is not found inside a sandbox, or when someone asks about Docker-in-Docker isolation. Pool sizing lives in abx-resource-capacity.
---

# Docker inside a sandbox

A sandbox can run `docker` and `docker compose`. It is not on by default: the
template decides it, because a container runtime changes the Pod spec — a
dockerd, a data directory on node disk, and either a microVM or a privileged
container around it.

## The choice that matters, and it is not a preference

| Runtime | Isolation | Use it |
|---|---|---|
| **kata** (Firecracker microVM) | dockerd runs inside a guest VM; `privileged` applies to the guest | **This one.** Full functionality, compose included. |
| **runc + privileged** | dockerd shares the host kernel and holds every host capability | Only where the cluster has no kata runtime. |

Say the second one's consequence plainly whenever you recommend it: **escape
means the host**. It is not "slightly less isolated", and a user choosing it
should be choosing it knowingly.

Rootless dind is not the middle ground people expect — it has been measured on
these clusters and does not work under runc. The way to avoid dangerous
privilege is kata, where the privilege is confined to a microVM, not a rootless
daemon.

## Finding out what this deployment has

```bash
abx templates                 # which templates exist here
abx templates <name>          # what it carries
```

Look for a template whose description names dind or kata. If there is none, the
answer is that this deployment has not published one — not that you should hand
the user a template to install, which is an admin action.

## What it looks like from the SDK

Nothing special. It is the same E2B create; the template is what differs. The
call's fields are the SDK's, not this CLI's — read them off the E2B spec in the
image or the installed SDK, the way `abx-common` describes. What matters here
is one number:

```python
sbx = Sandbox.create("<env-name>", …)   # with a longer timeout than a plain sandbox
print(sbx.commands.run("docker version").stdout)
sbx.commands.run("docker compose up -d", cwd="/home/user/project")
```

The endpoint this factory talks to is the env's: `abx envs <env> docs` prints it
for the cluster the env lives on — the E2B API URL, the data-plane domain and
the scheme. Take the public entry from that document unless you are already
inside the cluster.

Give it a longer timeout than you would a plain sandbox: dockerd starts in the
background while envd comes up in front, and the first `docker` call can arrive
before the daemon is listening. A short retry around the first command is
ordinary, not a symptom.

## Registries

Public images may not be reachable — many deployments are on internal networks.
Two things to check before concluding the image is broken:

- an internal mirror, with a prefix to rewrite `docker.io/...` onto;
- registry credentials on the env, which the platform materialises as an
  image-pull secret. Those cover the **sandbox's own** image, not what `docker`
  pulls from inside it — inside, `docker login` is the user's to run.

## When `docker` is not found

The env is on a template without it. Check which:

```bash
abx envs <env> --json | jq '.spec.templateRef'
abx templates
```

Moving an env to another template is a template change, not a sandbox one, and
it rolls the pool.

## Related

- `abx-sandbox-network` — letting the sandbox reach a registry while the agent
  inside it cannot reach the internet
- `abx-resource-capacity` — a dind sandbox is a bigger sandbox; size for it
- `abx-common` — endpoint, key, cluster, approval
