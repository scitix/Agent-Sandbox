# Code generated from the OpenAPI spec. DO NOT EDIT.
#   source: openapi.yaml
#   regenerate: make -C sdk/python/abx gen
#
# The `-f` body is the API's own request. Describing it anywhere but
# the schema would be describing a copy, and a copy of a schema stops
# being true without anyone noticing.

from __future__ import annotations

from typing import Final

# kind -> schema name, POST path, fields, nested tables,
#         example
# field = (wire name, type, required, description)
BODY_DOCS: Final[dict[str, dict[str, object]]] = {
    'env': {
        'schema': 'CreateSandboxEnvRequest',
        'path': '/envs',
        'fields': (
            ('name', 'string', True, 'RFC 1123 DNS label. Capped at 24 chars so derived names (PoolName = EnvName + ResourceKey + QuotaShort, PodName = PoolName + UUID) stay under the 63-char label/DNS limit.'),
            ('templateRef', 'SandboxEnvTemplateRef', True, 'Which SandboxTemplate every member Pool is rendered from. Pin a version to hold the Env still across Template edits; omit it to follow the Template.'),
            ('mode', 'WarmPool | OnDemandJob', False, 'WarmPool keeps idle Pods ready to claim. OnDemandJob creates a Pod per sandbox and tears it down after, trading start latency for holding no capacity between runs.'),
            ('overrides', 'EnvOverrides', False, 'SandboxTemplate fields this Env replaces uniformly for every member Pool. The Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.'),
            ('labels', 'map', False, ''),
            ('annotations', 'map', False, ''),
        ),
        'nested': {
            'overrides': (
                ('image', 'string', False, 'Override the main container (containers[0]) image of the rendered Template. Applied before any per-Member overrides.'),
                ('podCreationImagePolicy', 'PoolDefaultImage | IdleImage', False, "Mirrored onto every member Pool's spec.podCreationImagePolicy."),
                ('defaultStartupTimeout', 'string', False, "Mirrored onto every member Pool's spec.defaultStartupTimeout. Duration string, e.g. '5m'."),
                ('defaultIdleTimeout', 'string', False, "Mirrored onto every member Pool's spec.defaultIdleTimeout. Duration string, e.g. '30m'."),
                ('imagePullSecret', 'ImagePullSecretInput', False, "Write-only image-pull credentials. On create / update the server materialises a dockerconfigjson Secret named `ips-{envName}` in the Env's namespace with an OwnerReference back to the Env (cascade-deleted by Kubernetes GC). At render time the Env Reconciler appends that Secret's reference to every member Pool's spec.template.spec.imagePullSecrets so the kubelet can pull private images uniformly across the Env. Not returned on GET — check imagePullSecretConfigured for read-state."),
                ('imagePullSecretConfigured', 'boolean', False, "Server-set on GET: true when the ips-{envName} Secret exists in the Env's namespace. Write attempts via PATCH are ignored."),
                ('gateway', 'GatewaySpec', False, "Egress gateway for every member Pool's sandbox Pods. Enabling it injects a transparent proxy sidecar, which is what makes per-sandbox egress filtering (network.allowOut / denyOut) and credential injection (network.rules with Secret.fill) possible on the create call. It carries no rules of its own: what a sandbox may reach, and what gets injected into which request, belong to that one sandbox and arrive with it. Changing this switch changes the Pod spec and therefore rolls the Env's pools."),
                ('updateStrategy', 'EnvUpdateStrategy', False, 'Env-wide default rollout policy for member Pools when their idle-Pod identity changes. Overridable per member via EnvClusterMemberConfig.updateStrategy.'),
                ('volumes', 'EnvVolumeMount[]', False, "Mount existing PersistentVolumeClaims from this Env's namespace into the sandbox container. The claim must already exist and be Bound; the server never creates or deletes a PVC. Discover mountable claims with GET /volumes. Mounts are fixed at Pod creation — Kubernetes forbids mutating spec.volumes on a live Pod — so editing this list rolls the member Pools' idle Pods. Sandboxes already running keep their previous mounts until they are returned. In-place image upgrades never touch volumes."),
            ),
            'templateRef': (
                ('name', 'string', True, 'Name of the cluster-scoped SandboxTemplate the Env binds to.'),
                ('version', 'string', False, "Optional Template version pin. When empty, the Env tracks the Template's current spec.version."),
            ),
        },
        'example': '{\n  "name": "my-env",\n  "templateRef": {\n    "name": "e2b-envd"\n  },\n  "mode": "WarmPool",\n  "overrides": {\n    "gateway": {\n      "enabled": true\n    }\n  },\n  "labels": {\n    "team": "ai-infra"\n  }\n}',
    },
    'pool': {
        'schema': 'CreateEnvSandboxPoolRequest',
        'path': '/envs/{name}/sandboxpools',
        'fields': (
            ('instanceType', 'string', False, 'InstanceType catalog entry. Required when the catalog is enabled and inlineResources is not supplied. May be combined with inlineResources to reserve a whole instance while running a smaller (rounded-down) Pod.'),
            ('multiplier', 'integer', False, 'Multiplier applied to the InstanceType base resources to form the reservation envelope. Defaults to 1.'),
            ('inlineResources', 'ResourceRequirements', False, 'Explicit per-Pool resource requests/limits. Used alone when instanceType is not supplied, or combined with instanceType as the rounded-down actual Pod request (must fit within instanceType × multiplier). Sets both requests and limits.'),
            ('replicas', 'integer', False, 'Initial replica count. Autoscaling, once enabled on this scalingGroup, owns subsequent changes.'),
            ('minReplicas', 'integer', False, "Lower bound on this pool's replicas, enforced as a per-member scale-down floor by the Env autoscaler."),
            ('maxReplicas', 'integer', False, "Upper bound on this pool's replicas, enforced when the Env autoscaler distributes scale-up delta."),
            ('labels', 'map', False, "Labels stamped onto this member's SandboxPool. Use for plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix)."),
            ('annotations', 'map', False, "Annotations stamped onto this member's SandboxPool."),
            ('updateStrategy', 'EnvUpdateStrategy', False, 'Per-member rollout policy override. Unset inherits the Env overrides.updateStrategy, then autoUpdate=true / maxUnavailable=20%.'),
        ),
        'nested': {
            'inlineResources': (
                ('requests', 'map', False, "Resource requests keyed by resource name (e.g. cpu, memory). Values use Kubernetes Quantity strings, e.g. '500m', '1Gi'."),
                ('limits', 'map', False, 'Resource limits keyed by resource name.'),
            ),
            'updateStrategy': (
                ('autoUpdate', 'boolean', False, 'Whether the member auto-rolls when its revision changes. Resolution order: member → env → default true. Set false to freeze a member on its current revision.'),
                ('maxUnavailable', 'string', False, 'Rollout unavailability budget as an absolute count ("3") or a percentage of desired idle replicas ("20%"). Rounded down, floored at 1. Resolution order: member → env → default "20%".'),
            ),
        },
        'example': '{\n  "instanceType": "sci.c23-2",\n  "multiplier": 1,\n  "replicas": 1,\n  "minReplicas": 0,\n  "maxReplicas": 4,\n  "inlineResources": {\n    "requests": {\n      "cpu": "100m",\n      "memory": "500Mi"\n    },\n    "limits": {\n      "cpu": "100m",\n      "memory": "500Mi"\n    }\n  },\n  "labels": {\n    "quota.scitix.ai/url": "https://quota.example/q/1"\n  }\n}',
    },
}
