# E2B compatibility layer

AgentBox serves a subset of the [E2B](https://e2b.dev) API on `:8090`, so an
application written against the E2B SDK runs against a self-hosted AgentBox
cluster without code changes.

It is a subset, and this document is the boundary. Anything not listed as
supported answers **HTTP 501** with a message naming what to use instead — the
message is the only thing the SDK surfaces to the caller, and increasingly the
caller is a model deciding what to do next.

The spec is vendored, pinned by `E2B_SPEC_VERSION` in the repo's Makefile;
`make sync-e2b-spec generate-api` re-pulls and regenerates. Code generation
uses oapi-codegen's strict server, so an operation added upstream is a
**compile error** until it is either implemented or given a 501 stub — which is
the only reason this list can be trusted to be current.

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

`PUT /sandboxes/{id}/network` replaces the egress filtering of a running
sandbox, so an agent can install its dependencies and then lock down before it
runs anything untrusted. Two limits: `rules` cannot be changed there (the CA the
gateway uses to intercept TLS is minted per claim and installed into the
sandbox's trust store while it starts), and connections already open are not
re-evaluated — tightening applies to what the sandbox does next, it is not a
kill switch.

### Reaching an internal service

The anti-SSRF baseline has two tiers. The cloud metadata endpoints and
link-local (169.254.0.0/16, 100.100.100.200, fd00:ec2::254) are denied
unconditionally — no field opens them, because an unauthenticated GET there
hands out instance credentials. RFC1918 / CGNAT / ULA are denied by default but
reachable when the request names them:

```python
network={"allowOut": ["harbor.internal", "10.20.0.0/16", "pypi.org"]}
```

A named host or CIDR lifts the baseline for that destination. A wildcard does
not: `allowOut: ["*"]` means the internet, not the cluster network. For
"everything, cluster network included", pass
`metadata={"agentbox.scitix.ai/allow-private-networks": "true"}`.

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
`network.egressProxy`, `network.httpsPorts`, `network.maskRequestHost`, and
wildcard hosts in `network.rules`. Each error names the alternative.

`network.httpsPorts` tells the platform which sandbox ports speak TLS rather
than plaintext, so the proxy can reach them accordingly. AgentBox routes to a
sandbox port through Envoy + ExtProc header rewriting and has no per-sandbox
upstream-TLS path, so it is refused rather than accepted and ignored. Serve
plain HTTP inside the sandbox; the public URL is HTTPS either way.

These used to be dropped silently. For a human that is a confusing afternoon;
for an agent it is unrecoverable, because there is no signal to correct from.

## Not supported (HTTP 501)

Three kinds, and the distinction is the `category` label on the metric below:

| Category | Meaning | Operations |
|---|---|---|
| `architectural` | No counterpart here; the caller should stop asking | pause / resume / snapshots / fork (a sandbox is a claimed Pod, not a microVM, so there is no memory image), per-sandbox volumes, nodes, teams, rigs, `admin/*` |
| `platform` | The capability exists, but not on this API | template builds (`POST /templates`, v2/v3, builds, files/hash, build logs/status), template tags |
| `unimplemented` | A genuine gap, worth counting | `GET /templates/aliases/{alias}`, `GET /v2/templates`, `PATCH /api-keys/{id}` |

Rig inventory and capacity are the infrastructure operator's view of a fleet of
machines. AgentBox schedules onto Kubernetes nodes, so the questions they
answer are asked of the cluster itself.

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
