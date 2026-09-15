# logclient — reading a finished sandbox's logs

A sandbox's Pod holds its logs only while it exists. Once the sandbox is
released the Pod is recycled and the Kubernetes log API has nothing left to
return, so anyone asking "what did that run print" has to be sent to the central
log service the lines were shipped to. This package is that query.

`ProjectFor` and `QueryOptions.Scope` are the two things worth knowing here; the
package doc in `logclient.go` explains both. This file is about the chain around
them, because the query being right is not sufficient for a log to reach a human.

## Everything on this path fails the same way

The service answers a query that matches nothing with **200 and an empty body**.
So all of these are one indistinguishable outcome at the transport:

- the sandbox genuinely printed nothing
- the cluster was not scoped into the query (no `filters`)
- the wrong log store was asked (`project`)
- the cluster ships no container output at all

Nothing in this package can tell them apart. That is why `QueryOptions.Scope`
exists: **every caller reports what it asked alongside what came back**, so a
reader can tell which of the four they are looking at. Keep that property when
adding a caller — an empty result with no explanation costs hours.

## The four consumers, and the order they fail in

Reaching a user's screen takes four independent hops, and any one of them going
wrong renders the same empty panel:

| # | Hop | Where |
|---|---|---|
| 1 | The console decides whether to ask at all | `dashboard/app/api/sandbox-logs/config/route.ts` |
| 2 | The endpoint the console calls answers for a dead Pod | `handlers/logs_stream.go` → `streamCentralLogsToEnc` |
| 3 | The cluster's scope reaches the Worker | `proto/.../sync.proto` `LogsConfig` → both converters |
| 4 | The query itself is right | this package |

Only #4 is here, and it is the last one to suspect. All four have been broken at
once; see `docs/troubleshooting/central-logs-empty-project-mismatch.md` in the
deployment repo for the symptom-first version.

Two invariants that are easy to break without noticing:

- **The gate (#1) must match `getLogConfig()` in the route that serves the
  query.** It once required `LOG_APP_ID` — the variable whose *absence* selects
  Bearer auth — so every Bearer deployment reported "not configured" and the
  console silently fell back to the live-stream endpoint, which has nothing to
  say about a recycled Pod.
- **Both log endpoints must answer for a finished sandbox.** `GET /logs` and
  `GET /logs/stream` are separate code paths with separate fallbacks. The
  streaming one is what the console calls.

## Sharding

Where a deployment splits container output across two stores, the store is a
property of the *query* (which namespace) rather than of the deployment, so it
cannot be configured once per Worker. `ProjectFor` holds the rule; the flag that
turns it on (`ClusterEntry.Logs.SplitProject`) is hub configuration that reaches
a Worker only over the sync snapshot. `dashboard/lib/cluster-config.ts` carries
the same rule for the console — both halves query the same gateway, so a
disagreement shows up as the console and the API answering differently about one
sandbox. There is a test on each side running the same table.
