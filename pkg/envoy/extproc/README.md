## pkg/envoy/extproc — Envoy ExternalProcessor

This gRPC service implements the Envoy **ExternalProcessor** protocol. For every inbound HTTP request, it performs the following steps:

1.  **Validates** the API Key (`SecretKeyStore`) — only with `--extproc-enable-auth`, off by default; the E2B SDK sends no API key on the data plane.
2.  **Parses** the request target (Sandbox ID + Port).
3.  **Refuses** envd control paths (`/init`, freeze/thaw, `/upgrade`) on port 49983 with 403.
4.  **Queries** the corresponding Pod IP.
5.  **Checks** the traffic access token on non-envd ports of a traffic-secured sandbox (401 without it).
6.  **Returns** Header mutations to route traffic to the correct Sandbox Pod.

---

## Nothing tells this process about a sandbox

Step 3 reads the Pod informer's sandbox-id index and nothing else. That index is
built on a label the **claim** writes, in the same CAS that moves the Pod from
Idle to Starting — so a sandbox is resolvable from the moment it is claimed,
well before it is routable, and no notification is needed to make it so.
Releasing one strips the same label in one atomic update, so the mapping
disappears without anyone being told either.

There was once a cache the Controller pushed into (`PushRoute` / `EvictRoute`),
to skip the milliseconds between that label write and the watch event arriving
here. It is gone. Create now returns only after the sandbox is armed — seconds
of round trips, all downstream of the label write — so nothing is waiting in
that window any more, and a pushed cache cannot survive replication: one gRPC
connection pins to a single backend Pod, so a push reaches one replica and
leaves the others disagreeing.

Two consequences worth knowing:

- **This process can be rebuilt and restarted freely.** It holds no state the
  control plane has to re-send. (It once could not: a gate here read an
  annotation whose writer had been deleted, so any rebuild 502'd every sandbox
  in the cluster. See `docs/troubleshooting/extproc-rebuild-bricks-the-data-plane.md`
  in the deployment repo.)
- **Routing is replica-safe.** `ActivityTracker` is not yet — it is in-memory
  and per-replica, and the Controller polls one replica for it. That is the
  remaining blocker for `replicas > 1`.

Every routing failure is served as **502**, including a sandbox that does not
exist: this router's only source is an informer cache, so "gone" and "not yet
visible" are indistinguishable here and a 404 would assert a permanence it
cannot know. The two error values stay distinct in log messages.

---

## Data-plane authentication: who checks what

A sandbox ID is not a credential, so neither the ID nor this gateway's API-key
check is what protects a sandbox. Two per-sandbox tokens do, both
`HMAC-SHA256(AGENTBOX_SECRET, rawSandboxUUID)` (`pkg/utils/accesstoken`):

| Surface | Token | Enforced by | Turned on by |
|---|---|---|---|
| port 49983 (envd: files, process/filesystem RPCs) | envd access token, header `X-Access-Token` | **envd itself** — the operator delivers it on the arming `/init` | E2B `secure` (default `true`; v2 create is always secure) |
| every other port | traffic access token, header `e2b-traffic-access-token` | **this gateway** (`route.TrafficSecured` from the same Pod read) | E2B `network.allowPublicTraffic=false` |

This gateway also refuses envd control paths outright. Without that, an
unauthenticated `/init` racing in before arming could *set* envd's token
(envd under `-isnotfc` accepts a first tokenless `/init`). The legitimate
`/init` is dialled straight to the Pod IP, so it never passes through here.

The operator, the E2B API and this gateway each recompute the tokens from the
shared secret — nothing is stored or exchanged. **All three must key off the raw
UUID**, never a cluster-prefixed ID. Without `AGENTBOX_SECRET` this process skips
the traffic-token check (the control-path guard still applies).

## File Structure

| File | Description |
| :--- | :--- |
| `server.go` | gRPC service entry point; contains the `ProcessingRequest` main loop. |
| `router.go` | Envoy ExtProc router (bridges gRPC and handlers). |
| `helper.go` | Core logic: `authenticate()`, `extractTarget()`, and `RouteTarget`. |
| `activity_tracker.go` | Tracks Sandbox activity (used for idle timeout management). |
| `internal_grpc.go` | The Controller channel — one RPC, `GetLastActive`. |
| `helper_test.go` | Unit tests for `extractTarget`. |

---

## Routing Resolution Strategy (`extractTarget`)

Strategies are evaluated in order of priority (highest to lowest). The process stops at the first match:

| Priority | Strategy | Format / Source |
| :--- | :--- | :--- |
| **1** | Explicit Headers | `x-sandbox-id` + `x-sandbox-port` |
| **2** | Standard URL Path | `/sandboxes/<id>/<port>/...` |

---

## Testing

To run the test suite:

```bash
go test ./pkg/envoy/extproc/... -v
```

> **Note:** If you modify the `extractTarget` logic, you **must** update the corresponding test cases in `helper_test.go` to ensure alignment.