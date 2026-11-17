# Instance resources and placement

`DeriveResourceKey` derives new SandboxEnv member pool names and scaling groups
from the effective resources. CPU/memory spelling stays unchanged. The exact
resource `nvidia.com/gpu` adds its count, e.g. `8c16gi-1gpu`. CPU-only `8c16gi` and
GPU `8c16gi-1gpu` no longer share an automatically generated group.

Other nonzero qualified plugin resources use a deterministic `-r<digest>` suffix.
The digest includes sorted **full resource names and quantities**, so
`amd.com/gpu`, `huawei.com/Ascend910`, and a configured custom device resource do
not collapse into the NVIDIA suffix. RDMA contributes ordinary extended-resource
identity, never GPU count. Limits-only device quantities are included before
Kubernetes defaults matching requests. Zero device resources are ignored; native
storage/hugepage naming is unchanged. Long generated names are bounded with a
whole-name digest, rather than truncating away device identity.

Group names are returned by the API; clients should use those real names rather
than synthesize names. These rules apply to newly added members. Stored member
names and `Config.ScalingGroup` are not automatically rewritten, so old pool
references and routes remain valid. A new member cannot join an old group whose
frozen extended-resource shape differs; migrate an idle task-owned group
explicitly or use a separate Env.

## Generic named-instance placement

A provider may supply `InstanceType.NodeSelector`. This is a generic internal
provider field, not a new public REST input. Providers populate real label keys
and values from their own catalog contract. Open-source code does not interpret
`Extensions`, guess device models from resources, or hardcode vendor/node labels.

On member creation, only an explicitly selected `InstanceType` is looked up.
The selector is serialized to the server-owned frozen member metadata annotation
`agentbox.navix.sh/instance-type-node-selector`. Caller Config annotations and
incoming metadata cannot supply that key. The ordinary member update API cannot
edit this metadata, and shape/config fields remain immutable.

Both API-time pool rendering and template-refresh rendering consume this frozen
snapshot. Selector entries are AND-merged with the template's `nodeSelector`;
conflicting values fail closed. Existing `nodeAffinity` is retained and Kubernetes
ANDs it with `nodeSelector`. A provider/catalog edit does not retarget existing
members. A template annotation cannot forge the snapshot. Legacy members without
a snapshot are not retroactively assigned constraints during reconciliation.

Two NVIDIA models may legitimately expose the same `nvidia.com/gpu` count. Their
resource key remains the requested compact `8c16gi-1gpu`; however, adding a member
to that same Env/group with a different frozen provider selector is refused,
including unknown legacy placement versus a known model constraint. Use separate
Envs for such model groups or explicitly rebuild the empty group. The check is
repeated inside the optimistic Env update to prevent concurrent mixed-model adds.
Do not rename or silently move existing members to resolve this conflict.

A shared envd template can therefore back CPU and GPU members: CPU members have
no provider device selector, while each GPU member freezes its named-instance
selector. The shared template must itself permit those nodes; these rules do not
remove existing template constraints to force placement.
