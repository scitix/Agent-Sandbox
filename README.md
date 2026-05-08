<p align="center">
  <img src="dashboard/app/icon.svg" alt="Agent Sandbox" width="140" height="140" />
</p>

<h1 align="center">Agent Sandbox</h1>

<p align="center">
  <strong>Kubernetes-native Sandbox Engine for AI Agents</strong>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-green.svg" alt="License: Apache 2.0" /></a>
  <a href="https://github.com/scitix/agent-sandbox/issues"><img src="https://img.shields.io/github/issues/scitix/agent-sandbox" alt="Issues" /></a>
  <a href="https://github.com/scitix/agent-sandbox/pulls"><img src="https://img.shields.io/badge/PRs-welcome-brightgreen.svg" alt="PRs Welcome" /></a>
  <a href="https://github.com/scitix/agent-sandbox/actions/workflows/test.yml"><img src="https://github.com/scitix/agent-sandbox/actions/workflows/test.yml/badge.svg" alt="Tests" /></a>
  <a href="https://github.com/scitix/agent-sandbox/actions/workflows/lint.yml"><img src="https://github.com/scitix/agent-sandbox/actions/workflows/lint.yml/badge.svg" alt="Lint" /></a>
</p>

<p align="center">
  <a href="https://scitix.github.io/Agent-Sandbox/"><img src="https://img.shields.io/badge/Documentations-Online-orange.svg" alt="Website" /></a>
  <a href="https://scitix.github.io/Agent-Sandbox/docs/api/sandboxes/CreateSandbox/"><img src="https://img.shields.io/badge/OpenAPI-Documents-yellow.svg?logo=openapiinitiative&logoColor=white" alt="OpenAPI Documents" /></a>
  <a href="https://deepwiki.com/scitix/Agent-Sandbox"><img src="https://img.shields.io/badge/DeepWiki-scitix%2FAgent--Sandbox-blue.svg?logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACwAAAAyCAYAAAAnWDnqAAAAAXNSR0IArs4c6QAAA05JREFUaEPtmUtyEzEQhtWTQyQLHNak2AB7ZnyXZMEjXMGeK/AIi+QuHrMnbChYY7MIh8g01fJoopFb0uhhEqqcbWTp06/uv1saEDv4O3n3dV60RfP947Mm9/SQc0ICFQgzfc4CYZoTPAswgSJCCUJUnAAoRHOAUOcATwbmVLWdGoH//PB8mnKqScAhsD0kYP3j/Yt5LPQe2KvcXmGvRHcDnpxfL2zOYJ1mFwrryWTz0advv1Ut4CJgf5uhDuDj5eUcAUoahrdY/56ebRWeraTjMt/00Sh3UDtjgHtQNHwcRGOC98BJEAEymycmYcWwOprTgcB6VZ5JK5TAJ+fXGLBm3FDAmn6oPPjR4rKCAoJCal2eAiQp2x0vxTPB3ALO2CRkwmDy5WohzBDwSEFKRwPbknEggCPB/imwrycgxX2NzoMCHhPkDwqYMr9tRcP5qNrMZHkVnOjRMWwLCcr8ohBVb1OMjxLwGCvjTikrsBOiA6fNyCrm8V1rP93iVPpwaE+gO0SsWmPiXB+jikdf6SizrT5qKasx5j8ABbHpFTx+vFXp9EnYQmLx02h1QTTrl6eDqxLnGjporxl3NL3agEvXdT0WmEost648sQOYAeJS9Q7bfUVoMGnjo4AZdUMQku50McDcMWcBPvr0SzbTAFDfvJqwLzgxwATnCgnp4wDl6Aa+Ax283gghmj+vj7feE2KBBRMW3FzOpLOADl0Isb5587h/U4gGvkt5v60Z1VLG8BhYjbzRwyQZemwAd6cCR5/XFWLYZRIMpX39AR0tjaGGiGzLVyhse5C9RKC6ai42ppWPKiBagOvaYk8lO7DajerabOZP46Lby5wKjw1HCRx7p9sVMOWGzb/vA1hwiWc6jm3MvQDTogQkiqIhJV0nBQBTU+3okKCFDy9WwferkHjtxib7t3xIUQtHxnIwtx4mpg26/HfwVNVDb4oI9RHmx5WGelRVlrtiw43zboCLaxv46AZeB3IlTkwouebTr1y2NjSpHz68WNFjHvupy3q8TFn3Hos2IAk4Ju5dCo8B3wP7VPr/FGaKiG+T+v+TQqIrOqMTL1VdWV1DdmcbO8KXBz6esmYWYKPwDL5b5FA1a0hwapHiom0r/cKaoqr+27/XcrS5UwSMbQAAAABJRU5ErkJggg==" alt="DeepWiki" /></a>
</p>

---

## Overview

**Agent Sandbox** is a Kubernetes Operator that manages AI agent sandbox Pod lifecycles using a **pre-warmed Pod pool** with **in-place image upgrades**. Instead of scheduling a new Pod for every sandbox request — which incurs 15–60 seconds of cold-start latency — Agent Sandbox pre-warms a pool of idle Pods and reassigns one to an incoming request in under 100ms.

It is purpose-built for workloads where sandbox allocation speed is critical:

- **Reinforcement learning** training pipelines (SWE-bench, Terminal-bench, and custom RL environments)
- **AI coding agents** that need on-demand isolated execution environments
- **Multi-agent systems** requiring dozens or hundreds of sandboxes simultaneously

---

## Key Features

| Feature | Description |
|---------|-------------|
| **< 100ms Allocation** | Pre-warmed Pod pool eliminates scheduling overhead; sandboxes are ready in milliseconds |
| **In-Place Image Upgrade** | Running Pods are updated with a new image without recreation, preserving pool warmth |
| **Cross-Cluster & Multi-Region** | ExtProc-based routing dispatches requests transparently across multiple clusters |
| **E2B SDK Compatible** | Drop-in replacement for the E2B API — existing E2B clients work without code changes |
| **Optimized for RL Training** | Purpose-built for SWE-bench, Terminal-bench, and large-scale RL environment rollouts |
| **Kubernetes Native** | Managed via CRDs (`SandboxPool`, `SandboxTemplate`); integrates with RBAC, namespaces, and autoscaling |
| **Any Image, No Rebuild** | Bring any container image; no custom base image or agent installation required |
| **Prometheus Metrics** | First-class observability with a Prometheus endpoint and pre-built Grafana dashboards |

## Architecture

### Components

| Binary | Purpose | Ports |
|--------|---------|-------|
| `cmd/sandbox` | Operator + REST API Server | `:8080` (API), `:8090` (E2B-compat), `:8082` (metrics) |
| `cmd/envoyextproc` | Data-plane ExtProc for cross-cluster routing | `:9002` (gRPC), `:9003` (control-plane) |
| `cmd/wsproxy` | WebSocket reverse-proxy sidecar for terminal access | `:9003` (WS), `:9004` (sync) |

### CRDs

- **`SandboxPool`** (`sbp`, namespace-scoped) — defines a pre-warmed Pod pool with `Replicas`, optional autoscaling, and an inline or referenced template
- **`SandboxTemplate`** (`sbt`, cluster-scoped) — reusable Pod template with `idleImage` and `runtimes`

## Performance

| Metric | Traditional Kubernetes | Agent Sandbox |
|--------|----------------------|---------------|
| Sandbox allocation latency | 15–60 s | **< 100 ms** |
| Pod churn per request | 1 create + 1 delete | 0 (pool reuse) |
| Image pull on every request | Yes (cold start) | No (pre-warmed) |
| Autoscaling to zero | Supported | Supported |
| Cross-cluster routing | Manual / external LB | Built-in ExtProc |

## Quick Start

### Prerequisites

- Kubernetes 1.26+
- `kubectl` configured against your cluster
- `helm` (optional, for chart-based install)

## Use Cases

### Reinforcement Learning (SWE-bench / Terminal-bench)

Agent Sandbox is designed to serve as the environment backend for large-scale RL training runs. Thousands of rollout workers can each request a fresh isolated sandbox in milliseconds, dramatically reducing the environment-reset bottleneck:

### Cross-Cluster Scheduling

Deploy sandbox pools across multiple clusters or regions. The ExtProc component routes API requests to the appropriate cluster transparently — no changes needed in client code:

## Development

### Prerequisites

- Go 1.25+
- `make`
- Docker (for image builds)
- `controller-gen`, `oapi-codegen` (installed automatically by `make`)

### Build

```bash
# Build all binaries
make build

# Build individual binaries
make build-controller   # sandbox operator + API server (linux/amd64)
make build-extproc      # envoy extproc (linux/amd64)
make build-wsproxy      # websocket proxy
```

### Code Generation

```bash
make manifests          # Regenerate CRD YAML + RBAC
make generate           # Regenerate DeepCopy methods
make gen-all-api        # openapi.yaml → Go + TypeScript + Python SDK
make build-installer    # Regenerate installer/k8s/install.yaml
```

### Test

```bash
make test               # Unit tests (no cluster required)
make test-e2e           # E2E tests (requires a real cluster)
```

### Lint

```bash
make lint-fix
```

---

## Contributing

We welcome contributions of all kinds — bug reports, feature requests, documentation improvements, and code. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a pull request.

All commits must include a `Signed-off-by` line (see [DCO](https://developercertificate.org/)). Use `git commit -s` to add it automatically.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

Copyright © 2026 ScitiX.
