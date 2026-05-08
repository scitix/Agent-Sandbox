## pkg/envoy/extproc — Envoy ExternalProcessor

This gRPC service implements the Envoy **ExternalProcessor** protocol. For every inbound HTTP request, it performs the following steps:

1.  **Validates** the API Key (`SecretKeyStore`).
2.  **Parses** the request target (Sandbox ID + Port).
3.  **Queries** the corresponding Pod IP.
4.  **Returns** Header mutations to route traffic to the correct Sandbox Pod.

---

## File Structure

| File | Description |
| :--- | :--- |
| `server.go` | gRPC service entry point; contains the `ProcessingRequest` main loop. |
| `router.go` | Envoy ExtProc router (bridges gRPC and handlers). |
| `helper.go` | Core logic: `authenticate()`, `extractTarget()`, and `RouteTarget`. |
| `activity_tracker.go` | Tracks Sandbox activity (used for idle timeout management). |
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