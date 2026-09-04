# E2B compatibility layer

AgentBox serves a subset of the [E2B](https://e2b.dev) API on `:8090`, so an
application written against the E2B SDK runs against a self-hosted AgentBox
cluster without code changes.

It is a subset, and this document is the boundary. Anything not listed as
supported answers **HTTP 501** with a message naming what to use instead — the
message is the only thing the SDK surfaces to the caller, and increasingly the
caller is a model deciding what to do next.

## Supported

| Area | Operations |
|---|---|
| Sandboxes | `POST /sandboxes`, `GET /sandboxes`, `GET /v2/sandboxes`, `GET /sandboxes/{id}`, `DELETE /sandboxes/{id}`, `POST /sandboxes/{id}/timeout`, `POST /sandboxes/{id}/refreshes`, `POST /sandboxes/{id}/connect` |
| Logs | `GET /sandboxes/{id}/logs`, `GET /v2/sandboxes/{id}/logs` |
| Metrics | `GET /sandboxes/metrics`, `GET /sandboxes/{id}/metrics` (requires a metrics backend, see below) |
| Templates | `GET /templates`, `GET /templates/{id}` (read-only) |
| Secrets | `GET/POST /secrets`, `GET/POST/DELETE /secrets/{id}` |
| API keys | `GET/POST /api-keys`, `DELETE /api-keys/{id}` |
| Health | `GET /health` |

## Deliberately different

### `create` returns a usable sandbox

`Sandbox.create()` returns only once the sandbox is **armed**: its runtime
answers, the `envVars` are in place, and (where configured) the injected CA,
egress policy and credentials are loaded. Upstream returns as soon as the
sandbox record exists.

The practical difference is that the first command after create works. The cost
is that create takes as long as the sandbox actually takes to be ready —
typically a few seconds on a warm pool. Pass
`metadata={"agentbox.scitix.ai/no-wait": "true"}` to opt out per request.

### A template is a SandboxEnv

`templateID` names a `SandboxEnv`, and `GET /templates` lists them. Templates
are not built through this API: the image is built by your own CI and
registered as a SandboxEnv. Other clusters' Envs appear as `cluster::env`, and
that id can be passed straight back to `create`.

### Secrets are resolved at egress, never handed to the sandbox

`Secret.create(name, value)` stores a credential; `Secret.fill(name)` produces
the `${e2b.secrets.<name>}` placeholder, which goes in a `network.rules`
transform header. The egress gateway sets the real value on each matching
request, so the sandbox can use the credential without being able to read it.

Values are write-only: no read surface returns one. Header values in
`network.rules` must be built from placeholders — a literal is refused, because
it would put the credential in the request body and the access log.

Secrets are scoped to (namespace, user) and replicated to every cluster, so a
sandbox placed on another cluster resolves the same credential.

The environment has to have the gateway on for any of this: without the sidecar
there is nothing to intercept the request, so a create carrying `network.rules`
— or any egress filtering — is refused rather than silently unenforced. That
switch (`overrides.gateway.enabled`) is the environment's whole say in the
matter; the rules themselves are per sandbox and arrive on the create call.

## Accepted and ignored

| Field | Why |
|---|---|
| `secure` | Governs whether envd requires its own access token; AgentBox authenticates at the gateway instead. Rejecting it would break every caller passing the SDK default. |
| `autoPauseMemory` | Only selects the snapshot kind for an auto-pause, and `autoPause` is already refused. |

## Refused at create (HTTP 400)

`autoPause`, `autoResume`, `iam.tokens`, `mcp`, `volumeMounts`,
`network.egressProxy`, and wildcard hosts in `network.rules`. Each error names
the alternative.

These used to be dropped silently. For a human that is a confusing afternoon;
for an agent it is unrecoverable, because there is no signal to correct from.

## Not supported (HTTP 501)

Pause, resume, snapshots and fork have no counterpart: an AgentBox sandbox is a
claimed Pod from a pre-warmed pool, not a Firecracker microVM, so there is no
memory image to capture. Template builds, volumes, nodes, teams and the admin
surface live in the AgentBox native API or console.

`agentbox_e2b_unsupported_total{operation,category}` counts what callers
actually reach for, so the next batch of work is chosen from evidence.

## Observability backends

**Metrics** read container metrics (cAdvisor series) from a Prometheus-compatible
backend, scoped to the Pod backing the sandbox. Configure the operator with
`--prometheus-url` and `PROMETHEUS_TOKEN`; the per-cluster label matcher comes
from the cluster config's `selector`.

**Logs** come from two places, chosen by whether the sandbox still exists. A live
sandbox's lines are read from its Pod through the Kubernetes log API. Once the
sandbox is released the Pod is recycled and that source is empty, so a finished
run is served from the central log service (`--log-service-url`,
`LOG_SERVICE_TOKEN`, and `--log-service-project` where the gateway requires a
scope); the per-cluster filters come from the cluster config's `logs.filters`.

Either backend left unconfigured makes its endpoints answer 501 naming the
missing configuration, rather than an empty result — which would read as "this
sandbox is idle" or "this sandbox printed nothing".

Credentials come from the environment rather than flags so they do not appear in
the Pod spec or in `ps` output; the worker chart moves them into its Secret.

## Layout

| Path | Contents |
|---|---|
| `handlers/server.go` | The strict-server implementation: sandboxes, templates, API keys |
| `handlers/secrets.go` | The credential vault endpoints |
| `handlers/logs.go` · `handlers/metrics.go` | Observability endpoints |
| `handlers/egress.go` | `network` → `SandboxNetworkPolicy`, and rule parsing |
| `handlers/create_validate.go` | Refusal of create fields we would otherwise drop |
| `handlers/unsupported.go` | The 501 surface and its message catalogue |
| `domain/convert.go` | Projections onto the E2B wire shapes |
| `gen/` | Generated from the vendored E2B OpenAPI spec — do not edit |
