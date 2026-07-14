# federation — cross-cluster SandboxEnv capacity sharing

Each cluster owns its own `SandboxEnv` objects. When SandboxEnvs of the same
`(namespace, name)` exist in more than one cluster, this package shares only
their **runtime capacity** (idle / running / pending / desired per scaling
group) so a Worker's router can tell whether a same-named Env in another
cluster currently has idle room. Env **spec is never synced** — management
stays local to each cluster.

## Shape

```
Worker A ─ReportFederation─▶ Hub (ws-proxy) ─WatchFederation─▶ Worker A/B/…
  Reporter                    federationStore                    Registry
  (reads local Env status)    (soft state, TTL)                  (soft state, TTL)
```

- **Registry** (`registry.go`) — the Worker's in-memory store of `Capacity`
  records, aged out on a TTL. Injectable clock; safe for concurrent use.
  `ForeignForEnv` / `ForeignIdle` deliberately exclude the local cluster.
- **CapacitySource** (`reporter.go`) — reads back the per-Env aggregates the
  Env reconciler already writes into `status.scalingGroups` and the status
  rollups, one record per scaling group plus a whole-Env row (group `""`).
- **PublishMetrics** (`metrics.go`) — exports the registry snapshot as
  `agentbox_federation_env_{idle,running,desired}{cluster,namespace,env,group}`.

## Transport

Federation rides the existing `/v1/ws/sync` gRPC connection as
`FederationService` (`pkg/proto/sandbox/sync/v1/sync.proto`). The Worker is the
gRPC client (report + watch); the Hub is the server and relays each batch to
every connected Worker. Wire freshness is **relative** (`observed_for_ms`) so
TTL expiry is immune to cross-cluster clock skew.

## Enablement

Active only in multi-cluster mode — both a sync secret and `LOCAL_CLUSTER_ID`
must be set. Single-cluster deployments never start the report/watch
goroutines. A Worker connected to a Hub that predates `FederationService` gets
`Unimplemented`, logs once, and continues without federation (no disruption to
key / template / cluster-config sync).
