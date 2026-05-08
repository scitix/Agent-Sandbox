# CLAUDE.md

This file provides guidance and conventions for Claude Code when working with this repository.

---

## Project Overview

Agent Sandbox is a **Kubernetes Operator** that manages AI Agent sandbox Pod lifecycles. The core design uses a pre-warmed Pod pool combined with in-place image upgrades, avoiding the overhead of frequent Pod creation and deletion.

- **Go module**: `github.com/scitix/agent-sandbox`
- **Go version**: 1.25
- **CRD Group**: `agents.navix.sh`

**Cluster topology**: A worker cluster runs the AgentBox core (Operator + ExtProc + Sandbox Pods); an optional master cluster runs `/dashboard` (see `dashboard/CLAUDE.md`).

### Three Binaries

Each `cmd/*/main.go` is a thin stub that calls `Run()` from its sibling
`cmd/*/app` package — closed-source forks should import these packages rather
than re-implement the bootstrap.

| Binary | Entry | Bootstrap package | Responsibilities |
|--------|-------|-------------------|-----------------|
| `cmd/sandbox` | Operator / API Server | `cmd/sandbox/app` | REST API (`:8080`) + Controller Manager, optional E2B-compatible API (`:8090`) + Prometheus metrics (`:8082`) |
| `cmd/envoyextproc` | Data-plane ExtProc | `cmd/envoyextproc/app` | Envoy ExternalProcessor gRPC (`:9002`) + internal control-plane gRPC (`:9003`) |
| `cmd/wsproxy` | WS reverse-proxy sidecar | `cmd/wsproxy/app` | Listens on `:9003`, routes `/ws/clusters/{cid}/sandboxes/{id}/terminal` to the AgentBox API WebSocket endpoint; optional sync manager on `:9004` |

---

## Common Commands

```bash
# Code generation
make manifests          # Regenerate CRD YAML + RBAC (required after modifying api/v1alpha1/)
make generate           # Regenerate DeepCopy methods
make gen-all-api        # openapi.yaml → Go + TS (dashboard) + Python SDK (all in one)
make sync-crds-to-helm  # Sync CRDs + manager ClusterRole into Helm charts (run after make manifests)

# Build
make build              # Build all binaries (VERSION injected via -ldflags automatically)
make build-controller   # sandbox linux/amd64
make build-extproc      # envoyextproc linux/amd64
make build-wsproxy      # wsproxy binary
make lint-fix           # Run linter with auto-fix

# Version
make sync-version       # Sync VERSION file to openapi.yaml / pyproject.toml / __init__.py

# Testing
make test               # Unit tests (envtest, no cluster needed); packages run concurrently
make test-e2e           # E2E tests (requires a real cluster)
go test -tags=e2e ./test/e2e/ -v -ginkgo.v --ginkgo.focus="xxx"  # Run a single E2E test
```

> `make test` / `make build` automatically run `manifests → generate → gen-all-api → fmt → vet` first.

---

## Change Checklist

1. Changed a default port in code → update the corresponding Service definition in Helm charts
2. Changed `api/v1alpha1/` CRD types → `make manifests generate` then `make sync-crds-to-helm`
3. Changed `pkg/openapi/native/openapi.yaml` → `make gen-all-api` (syncs Go + TS + Python SDK)
4. **Added/modified an API response field** → follow the full "API Field Addition SOP" below
5. Releasing a new version → see the "Version Management" section below

### API Field Addition SOP

> **Lesson learned**: Fields have repeatedly been defined in OpenAPI + domain models but omitted from conversion functions like `sandboxToGen()` / `poolToGen()`, causing the frontend to receive no data. **Every new field must go through the full chain below.**

Using Sandbox as an example, the complete change path for adding a field:

```
openapi.yaml          ← 1. Define field schema
    ↓ make gen-all-api
gen/agentbox.gen.go   ← 2. Auto-generated (do not edit manually)
domain/sandbox.go     ← 3. Add domain field + json tag
sandbox_pod.go        ← 4. Extract field value from Pod → domain
sandbox_service.go    ← 5. Confirm List/Get logic passes the field through
handlers/server.go    ← 6. ⚠️ Assign domain → gen in sandboxToGen() (most often missed!)
e2bcompat/convert.go  ← 7. If E2B compat is needed, add metadata mapping
```

**Step-by-step checklist** (none can be skipped):

| # | File | Action | Verify |
|---|------|--------|--------|
| 1 | `pkg/openapi/native/openapi.yaml` | Add field definition to schema | `make gen-all-api` succeeds |
| 2 | `pkg/apiserver/gen/agentbox.gen.go` | Auto-generated; confirm new field exists | grep new field name |
| 3 | `pkg/apiserver/domain/sandbox.go` | Add field + `json:"xxx,omitempty"` tag | compiles |
| 4 | `pkg/controllers/sandboxpool/sandbox_pod.go` | Extract value in `SandboxBaseFromPod()` | unit test |
| 5 | `pkg/apiserver/service/sandbox_service.go` | Confirm `sandboxFromPod()` / `List()` don't drop the field | code review |
| 6 | **`pkg/apiserver/handlers/server.go`** | **Add `domain → gen` assignment in `sandboxToGen()`** | `go build` + API test |
| 7 | `pkg/e2bcompat/domain/convert.go` | If E2B needed, add metadata mapping | E2B API test |

> **SandboxPool fields follow the same pattern**: update `poolToGen()` accordingly.
>
> **Core principle**: A field travels through 3 transformation layers from K8s Pod to HTTP response (Pod → domain → gen). **Each layer must explicitly pass the field**; it is never inherited automatically.

---

## Version Management

### Architecture

All components share a single version number. The **single source of truth** is the `VERSION` file in the repo root.

```
VERSION                            ← sole version source (semver, e.g. 0.2.0)
    │
    ├─ Makefile (LDFLAGS)          → Go binaries get pkg/version.Version injected via -ldflags
    ├─ make sync-version           → openapi.yaml info.version
    ├─ make sync-version           → sdk/python/abx/pyproject.toml version
    └─ make sync-version           → sdk/python/abx/agentbox_sdk/__init__.py __version__
```

### Client Version Negotiation

- **SDK** sends `X-AgentBox-Client-Version` header on every request automatically
- **Server middleware** checks this header for write operations (POST/PUT/PATCH/DELETE):
  - Version < `MinClientVersion` (defined in `pkg/apiserver/router/middleware/version.go`) → returns `426 Upgrade Required`
  - Dashboard/JWT users are exempt (requests without `AGENTBOX-API-KEY` header are skipped)
- **Grace period**: when `MinClientVersion = "0.0.0"` all clients are allowed

### Releasing a New Version (with Breaking API Changes)

```bash
# 1. Update version number
echo "0.2.0" > VERSION

# 2. Sync to all component version declarations
make sync-version

# 3. Raise the minimum compatible version
#    pkg/apiserver/router/middleware/version.go → MinClientVersion = "0.2.0"

# 4. Regenerate API code + build
make gen-all-api
make build

# 5. After deployment, old clients (< 0.2.0) will receive 426 Upgrade Required on writes
```

### Key Files

| File | Role |
|------|------|
| `VERSION` | Sole version source |
| `pkg/version/version.go` | Go version variable (injected via `-ldflags`) |
| `pkg/apiserver/router/middleware/version.go` | Server-side version check middleware + `MinClientVersion` constant |
| `sdk/python/abx/agentbox_sdk/_http.py` | Python SDK sends `X-AgentBox-Client-Version` header |

---

## Testing Conventions

- Unit tests: **Ginkgo v2 + Gomega** (BDD style) + `envtest`; some packages use standard `testing`
- E2E tests: `test/e2e/`, requires a Kind cluster
- Build tags: `unit` / `e2e`; coverage output written to `cover.out`
- **Write tests alongside new functionality whenever possible** (unit or integration)

---

## Auto-Generated Files (do not edit manually)

| File | Generated by |
|------|-------------|
| `api/v1alpha1/zz_generated.deepcopy.go` | `make generate` |
| `config/crd/bases/*.yaml` | `make manifests` |
| `config/rbac/role.yaml` | `make manifests` |
| `installer/helm/agent-sandbox-worker/crds/*.yaml` | `make sync-crds-to-helm` (via `hack/scripts/generate-helm.py`) |
| `installer/helm/agent-sandbox-hub/crds/*.yaml` | `make sync-crds-to-helm` (SandboxTemplate only) |
| `installer/helm/agent-sandbox-worker/templates/rbac-manager-role.yaml` | `make sync-crds-to-helm` (converts `config/rbac/role.yaml`) |
| `docs/openapi/swagger.{json,yaml}` | `make openapi` |
| `pkg/apiserver/gen/agentbox.gen.go` | `make gen-all-api` |
| `pkg/proto/sandbox/internal/v1/*.pb.go` | `make gen-internal-proto` (chained into `gen-all-api`) |
| `pkg/e2bcompat/gen/e2b.gen.go` | Generated from E2B OpenAPI spec |
| `dashboard/lib/api/schema.d.ts` | `make gen-all-api` or `cd dashboard && pnpm run gen:types` |
| `sdk/python/abx/agentbox_sdk/_generated/` | `make gen-all-api` or `cd sdk/python/abx && make gen` |

---

## Module Index

| Module | Docs | Notes |
|--------|------|-------|
| E2B compatibility layer | [`pkg/e2bcompat/`](pkg/e2bcompat/) | E2B SDK-compatible server (`:8090`) |
| Prometheus metrics | [`pkg/metrics/README.md`](pkg/metrics/README.md) | Metric definitions, addition workflow, scrape config |
| ExtProc routing | [`pkg/envoy/extproc/README.md`](pkg/envoy/extproc/README.md) | Envoy ExternalProcessor + routing policy |
| Native REST API | [`pkg/apiserver/README.md`](pkg/apiserver/README.md) | OpenAPI-first generation flow, field mapping |
| Plugin system | [`pkg/framework/`](pkg/framework/) | Plugin (lifecycle hooks) + Provider (data source abstraction) interfaces |
| HTTP logging | [`pkg/utils/httplog.go`](pkg/utils/httplog.go) | RequestID middleware + AppError structured logging |
| Version management | [`CLAUDE.md #version-management`](#version-management) | VERSION file + sync-version + client version negotiation |

---

## Core Data Structures

**In-place upgrade Pod state machine** (`agentbox.navix.sh/sandbox-phase` label):
```
Idle → Starting → Running → Stopping → Idle
                               ↘ Failed
```

**CRDs**:
- `SandboxPool` (namespace-scoped, short: `sbp`) — pre-warmed Pod pool with `Replicas`, optional autoscaling, inline or referenced template
- `SandboxTemplate` (cluster-scoped, short: `sbt`) — reusable Pod template with `idleImage`, `runtimes`

**API Key**: `agbx_` prefix + 32 random bytes; SHA-256 hash stored in K8s Secret; in-memory cache TTL 1min (`pkg/utils/apikey/`)
