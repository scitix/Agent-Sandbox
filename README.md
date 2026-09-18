<p align="center">
  <img src="dashboard/app/icon.svg" alt="Agent Sandbox" width="140" height="140" />
</p>

<h1 align="center">Agent Sandbox</h1>

<p align="center">
  <strong>Fast, Multi-Cloud Sandbox Engine for AI Agents</strong>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-brightgreen.svg" alt="License: Apache 2.0" /></a>
  <a href="https://github.com/scitix/agent-sandbox/issues"><img src="https://img.shields.io/github/issues/scitix/agent-sandbox" alt="Issues" /></a>
  <a href="https://github.com/scitix/agent-sandbox/pulls"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome" /></a>
  <a href="https://github.com/scitix/agent-sandbox/actions/workflows/test.yml"><img src="https://github.com/scitix/agent-sandbox/actions/workflows/test.yml/badge.svg" alt="Tests" /></a>
  <a href="https://github.com/scitix/agent-sandbox/actions/workflows/lint.yml"><img src="https://github.com/scitix/agent-sandbox/actions/workflows/lint.yml/badge.svg" alt="Lint" /></a>
</p>

<p align="center">
  <a href="https://scitix.github.io/Agent-Sandbox/"><img src="https://img.shields.io/badge/Documentation-000764.svg?style=for-the-badge&logo=mdbook" alt="Website" /></a>
  <a href="https://scitix.github.io/Agent-Sandbox/docs/api/sandboxes/CreateSandbox/"><img src="https://img.shields.io/badge/OpenAPI-orange.svg?style=for-the-badge&logo=openapiinitiative&logoColor=white" alt="OpenAPI Documents" /></a>
  <a href="https://deepwiki.com/scitix/Agent-Sandbox"><img src="https://img.shields.io/badge/DeepWiki-scitix%2FAgent--Sandbox-blue.svg?style=for-the-badge&logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACwAAAAyCAYAAAAnWDnqAAAAAXNSR0IArs4c6QAAA05JREFUaEPtmUtyEzEQhtWTQyQLHNak2AB7ZnyXZMEjXMGeK/AIi+QuHrMnbChYY7MIh8g01fJoopFb0uhhEqqcbWTp06/uv1saEDv4O3n3dV60RfP947Mm9/SQc0ICFQgzfc4CYZoTPAswgSJCCUJUnAAoRHOAUOcATwbmVLWdGoH//PB8mnKqScAhsD0kYP3j/Yt5LPQe2KvcXmGvRHcDnpxfL2zOYJ1mFwrryWTz0advv1Ut4CJgf5uhDuDj5eUcAUoahrdY/56ebRWeraTjMt/00Sh3UDtjgHtQNHwcRGOC98BJEAEymycmYcWwOprTgcB6VZ5JK5TAJ+fXGLBm3FDAmn6oPPjR4rKCAoJCal2eAiQp2x0vxTPB3ALO2CRkwmDy5WohzBDwSEFKRwPbknEggCPB/imwrycgxX2NzoMCHhPkDwqYMr9tRcP5qNrMZHkVnOjRMWwLCcr8ohBVb1OMjxLwGCvjTikrsBOiA6fNyCrm8V1rP93iVPpwaE+gO0SsWmPiXB+jikdf6SizrT5qKasx5j8ABbHpFTx+vFXp9EnYQmLx02h1QTTrl6eDqxLnGjporxl3NL3agEvXdT0WmEost648sQOYAeJS9Q7bfUVoMGnjo4AZdUMQku50McDcMWcBPvr0SzbTAFDfvJqwLzgxwATnCgnp4wDl6Aa+Ax283gghmj+vj7feE2KBBRMW3FzOpLOADl0Isb5587h/U4gGvkt5v60Z1VLG8BhYjbzRwyQZemwAd6cCR5/XFWLYZRIMpX39AR0tjaGGiGzLVyhse5C9RKC6ai42ppWPKiBagOvaYk8lO7DajerabOZP46Lby5wKjw1HCRx7p9sVMOWGzb/vA1hwiWc6jm3MvQDTogQkiqIhJV0nBQBTU+3okKCFDy9WwferkHjtxib7t3xIUQtHxnIwtx4mpg26/HfwVNVDb4oI9RHmx5WGelRVlrtiw43zboCLaxv46AZeB3IlTkwouebTr1y2NjSpHz68WNFjHvupy3q8TFn3Hos2IAk4Ju5dCo8B3wP7VPr/FGaKiG+T+v+TQqIrOqMTL1VdWV1DdmcbO8KXBz6esmYWYKPwDL5b5FA1a0hwapHiom0r/cKaoqr+27/XcrS5UwSMbQAAAABJRU5ErkJggg==" alt="DeepWiki" /></a>
</p>

---

## What is Agent Sandbox?

**Agent Sandbox** is an open-source sandbox engine for AI agents. It runs on
Kubernetes, serves the E2B API, and gives an agent, an evaluation or a training
loop its own isolated environment in the time it takes to swap a container
image.

---

## How it works

**Sandboxes are claimed, not created.** A pool holds Pods that are already
running, so serving a request is an in-place image swap rather than a
scheduling round-trip. Capacity is a number you decide — a pool's replica count
— and the autoscaler moves it as demand changes.

**There is no build step.** An E2B template is a snapshot compiled from a
Dockerfile; here you name a container image when you create the sandbox, and
the platform swaps it in. Any image your cluster can pull is a workload.

**Platform and sandboxes are two surfaces.** `abx` addresses the platform —
environments, warm pools, autoscaling, quotas, templates — and the E2B SDK
addresses the sandboxes themselves. The line is deliberate: sandbox work needs
no new client, and platform work needs no SDK.

---

## Install

### The platform (Kubernetes + Helm)

```bash
helm upgrade --install agent-sandbox-worker \
  oci://ghcr.io/scitix/agent-sandbox-worker \
  --namespace agentbox-system --create-namespace \
  --set controller.localClusterId=YOUR_CLUSTER_ID
```

That release is the whole platform on one cluster: the CRDs, the controller and
API, and the data plane sandboxes are reached through. `localClusterId` is
required — it is how a pool is placed on a cluster segment.

The console is a second, optional release that reads the same API:

```bash
helm upgrade --install agent-sandbox-hub \
  oci://ghcr.io/scitix/agent-sandbox-hub \
  --namespace agentbox-system \
  --set env.secret="$(openssl rand -hex 32)" \
  --set clusters[0].id=YOUR_CLUSTER_ID \
  --set clusters[0].url=http://agent-sandbox-api.agentbox-system.svc.cluster.local \
  --set ingress.host=agentbox.example.com
```

→ **[Installation guide](https://scitix.github.io/Agent-Sandbox/docs/installation)** —
prerequisites, the services you get, the shared secret between the two
releases, and how to remove it all again.

### The CLI, and the skills

```bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh

abx context set YOUR_DEPLOYMENT \
  --endpoint 'https://YOUR_CONSOLE/agentbox' \
  --api-key agbx_...

abx clusters
```

`abx` is a single binary with no runtime dependencies. The same script writes
nine skills to `~/.agents/skills` — the vocabulary an agent reads before it
touches the platform, from `abx-common` (endpoint, key, cluster, approvals) to
`abx-sandbox-secrets`. Claude Code users can install the plugin instead:
`/plugin marketplace add scitix/agent-sandbox`.

→ **[CLI guide](https://scitix.github.io/Agent-Sandbox/docs/tutorials/cli)** ·
**[Skills](https://scitix.github.io/Agent-Sandbox/docs/skills)** ·
**[E2B SDK guide](https://scitix.github.io/Agent-Sandbox/docs/tutorials/e2b)**

Every page is also a plain-text document: append `.md` to any docs URL
(`/docs/concepts/pools.md`), or hand an agent
[llms-full.txt](https://scitix.github.io/Agent-Sandbox/llms-full.txt).

---

## Use Cases

### Reinforcement Learning at Scale

RL training requires thousands of environment resets per hour. Agent Sandbox pre-warms a pool of sandboxes so each rollout worker gets a fresh, isolated environment in milliseconds — removing the environment-reset bottleneck from your training loop. Supports SWE-bench Verified, SWE-Gym, Terminal-bench, and custom task distributions.

### AI Coding Agents & Evaluations

Give every agent turn or eval call its own isolated execution environment. The E2B-compatible API means existing SWE-agent, SWE-ReX, and similar frameworks work without modification.

### Enterprise Multi-Cluster Deployment

Deploy sandbox pools across multiple clouds or regions. The built-in ExtProc routing layer dispatches requests to the most available cluster transparently — no routing logic required in application code. Supported cloud providers: AWS, Google Cloud, Azure, Alibaba Cloud, Volcengine, Cloudflare.

> **Coming soon:** microVM-backed sandboxes for stronger isolation guarantees.

---

## Documentation

| Resource | Link |
|----------|------|
| Documentation site | [scitix.github.io/Agent-Sandbox](https://scitix.github.io/Agent-Sandbox/) |
| Installation — Helm, the CLI and the skills | [/docs/installation](https://scitix.github.io/Agent-Sandbox/docs/installation) |
| Concepts — the object model, envs, pools, autoscaling | [/docs/concepts](https://scitix.github.io/Agent-Sandbox/docs/concepts) |
| CLI guide — `abx`, endpoints and keys | [/docs/tutorials/cli](https://scitix.github.io/Agent-Sandbox/docs/tutorials/cli) |
| E2B SDK guide — creating and driving sandboxes | [/docs/tutorials/e2b](https://scitix.github.io/Agent-Sandbox/docs/tutorials/e2b) |
| Skills — the nine files an agent installs | [/docs/skills](https://scitix.github.io/Agent-Sandbox/docs/skills) |
| Examples — templates and environments to apply | [/docs/examples](https://scitix.github.io/Agent-Sandbox/docs/examples) |
| API Reference (OpenAPI) | [/docs/api/sandboxes/CreateSandbox](https://scitix.github.io/Agent-Sandbox/docs/api/sandboxes/CreateSandbox/) |

---

## Contributing

Contributions are welcome — bug reports, feature requests, documentation, and code. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a pull request.

All commits must include a `Signed-off-by` line ([DCO](https://developercertificate.org/)). Use `git commit -s`.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

Copyright © 2026 ScitiX.
