# Template image availability

Templates opt in with `agentbox.navix.sh/registry-rewrite: "true"`. The worker's
SandboxTemplate controller checks the rewritten idle, container and initContainer
images asynchronously after cache startup. Registry failures never block manager
startup or readiness. The controller resolves template pull-secret references in
the operator API-key namespace; request checks resolve them in the Env namespace.

Successful checks refresh every 24 hours; unsuccessful checks retry every 15
minutes. A lightweight reconciliation every five minutes detects changed registry
mappings without issuing a registry request while the cached plan remains valid.
Template changes enqueue reconciliation immediately. Images are deduplicated per
plan and each registry request has a 15-second timeout.

Results live in `SandboxTemplate.status.imageAvailability`, with a cluster ID,
a hash of effective images and secret references, check/next-check timestamps,
and per-image verdicts. `ImagesAvailable` is False for confirmed missing images,
Unknown for authorization/network failures, and True for successful checks.
Global catalog replication replaces only spec/metadata, preserving worker status.
No permanent scanned annotation is used.

Creating a new Env may reuse a matching successful check younger than 48 hours.
Changed image overrides invalidate the hash. Explicit request credentials or
namespace-dependent pull secrets always require a request-specific check. An
expired, unsuccessful or absent record causes an immediate check (45-second
request budget). The check precedes Env creation; existing Envs and Pods are not
revalidated, restarted or blocked by this feature.

Confirmed missing images return HTTP 422 / `TEMPLATE_IMAGE_NOT_FOUND`.
Unverifiable images return HTTP 503 / `TEMPLATE_IMAGE_CHECK_FAILED`. Both carry
`detail.cluster`, `detail.template`, and `detail.images` (field, rendered image,
reason). Registry response bodies and credentials are not exposed. The Dashboard
Env form displays these image details; API callers can consume the same payload.

`imageresolver.AvailabilityChecker.CheckAvailability` is deliberately separate from
digest resolution: even a digest-pinned image must contact the registry. HEAD is
used first, falling back to manifest GET for unsupported/incomplete HEAD replies.
This verifies manifest access, not node connectivity, layer downloads, runtime
startup, or availability of a particular node architecture.
