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

**Agent Sandbox** is an open-source sandbox engine for AI agents. It is purpose-built for three classes of workload:

- **Lightning Fast** — pre-warmed pools keep isolated environments on standby, eliminating cold-start latency for high-frequency agent loops, evaluations, and RL rollouts
- **Enterprise Grade** — deploy on any cloud using native Kubernetes CRDs, RBAC, and multi-cluster routing, without vendor lock-in
- **Agentic RL** — stateful environments with deterministic resets and any-image runtimes, built for complex multi-turn agent training

---

## Key Features

| | Feature | Description |
|---|---------|-------------|
| ⚡ | **Speed — Sub-60ms allocation** | Pre-warmed pools deliver idle sandboxes instantly, unblocking high-volume agent loops and multi-turn RL rollouts |
| ☸️ | **Infrastructure — Containers or microVMs** | Run on your existing estate using CRDs, namespaces, RBAC, and autoscaling to manage warm capacity efficiently |
| 🌐 | **Routing — Cross-region and cross-cloud** | Dispatch requests across clouds, clusters, and regions without forcing application teams to manage routing logic |
| 🧪 | **Runtime — Zero-rebuild runtimes** | Run any Docker image for SWE tasks, RL environments, and internal tools without building custom VM images |
| 🔌 | **Ecosystem — Drop-in agent SDKs** | Seamless compatibility with E2B clients, SWE-ReX workflows, and popular reinforcement learning frameworks |
| 📊 | **Observability — Console-grade visibility** | Complete view of pools, active sessions, logs, and metrics through a unified product console |

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
| API Reference (OpenAPI) | [/docs/api/sandboxes/CreateSandbox](https://scitix.github.io/Agent-Sandbox/docs/api/sandboxes/CreateSandbox/) |
| Installation guide | [/docs/installation](https://scitix.github.io/Agent-Sandbox/docs/installation) |
| Integrations | [/docs/integrations](https://scitix.github.io/Agent-Sandbox/docs/integrations) |
| Changelog | [/docs/changelog](https://scitix.github.io/Agent-Sandbox/docs/changelog) |

---

## Contributing

Contributions are welcome — bug reports, feature requests, documentation, and code. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a pull request.

All commits must include a `Signed-off-by` line ([DCO](https://developercertificate.org/)). Use `git commit -s`.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

Copyright © 2026 ScitiX.
