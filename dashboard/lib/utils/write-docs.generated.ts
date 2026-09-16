/**
 * GENERATED — do not edit. `oss/scripts/gen-body-docs.ts`, via `make gen-all-api`.
 *
 * Which fields a write may change, read out of `pkg/openapi/native/openapi.yaml`
 * — the same table `abx create <address> --help` renders, generated a second
 * time into this tree because the bundler cannot reach `../headless` (see the
 * generator). The console's forms and the CLI therefore lock the same fields.
 */

/** One field of a write body. `fields` is the one level of nesting worth showing. */
export interface WriteDocField {
  name: string
  /** How the value is spelled in JSON: `string`, `int`, `bool`, or an enum's values. */
  type: string
  /** Required by the schema — for a create that is the whole of "you must say". */
  required: boolean
  /** Fixed after create: an update has to carry it back unchanged. */
  fixed: boolean
  /** What to do instead, when a fixed field cannot be changed. */
  lever?: string
  /** The accepted values, when the field is an enum. */
  values?: string[]
  describe: string
  fields?: WriteDocField[]
}

export interface WriteDocBody {
  schema: string
  describe: string
  example?: unknown
  fields: WriteDocField[]
}

export interface WriteDoc {
  plural: string
  kind: string
  create?: WriteDocBody
  update?: WriteDocBody
}

export const WRITE_DOCS: readonly WriteDoc[] = [
  {
    "plural": "envs",
    "kind": "SandboxEnv",
    "create": {
      "schema": "CreateSandboxEnvRequest",
      "describe": "What `POST /envs` takes: the same body an update does, plus the one thing only a create can say — which name to make. `name` is required here and the `pattern` it has to match is on the property itself.",
      "fields": [
        {
          "name": "name",
          "type": "string",
          "required": true,
          "fixed": false,
          "describe": "The Env this file is about. `create` requires it; `update` takes the name from the address and refuses a file whose `name` says something else. It is part of the write body so that the file a create wrote is the file an update takes — one shape, sent to one address. RFC 1123 DNS label, capped at 24 chars so derived names (PoolName = EnvName + ResourceKey + QuotaShort, PodName = PoolName + UUID) stay under the 63-char label/DNS limit."
        },
        {
          "name": "templateRef",
          "type": "object",
          "required": true,
          "fixed": true,
          "describe": "Which SandboxTemplate every member Pool is rendered from. Pin a version to hold the Env still across Template edits; omit it to follow the Template. Fixed after create.",
          "lever": "create a new Env from the other template and move the traffic over",
          "fields": [
            {
              "name": "name",
              "type": "string",
              "required": true,
              "fixed": false,
              "describe": "Name of the cluster-scoped SandboxTemplate the Env binds to."
            },
            {
              "name": "version",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Optional Template version pin. When empty, the Env tracks the Template's current spec.version."
            }
          ]
        },
        {
          "name": "mode",
          "type": "WarmPool | OnDemandJob",
          "required": false,
          "fixed": true,
          "describe": "WarmPool keeps idle Pods ready to claim. OnDemandJob creates a Pod per sandbox and tears it down after, trading start latency for holding no capacity between runs. Fixed after create.",
          "lever": "create a new Env in the other mode",
          "values": [
            "WarmPool",
            "OnDemandJob"
          ]
        },
        {
          "name": "overrides",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "SandboxTemplate fields this Env replaces uniformly for every member Pool. The Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.",
          "fields": [
            {
              "name": "image",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Override the main container (containers[0]) image of the rendered Template. Applied before any per-Member overrides."
            },
            {
              "name": "podCreationImagePolicy",
              "type": "PoolDefaultImage | IdleImage",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.podCreationImagePolicy.",
              "values": [
                "PoolDefaultImage",
                "IdleImage"
              ]
            },
            {
              "name": "defaultStartupTimeout",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.defaultStartupTimeout. Duration string, e.g. '5m'."
            },
            {
              "name": "defaultIdleTimeout",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.defaultIdleTimeout. Duration string, e.g. '30m'."
            },
            {
              "name": "imagePullSecret",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Write-only image-pull credentials. On create / update the server materialises a dockerconfigjson Secret named `ips-{envName}` in the Env's namespace with an OwnerReference back to the Env (cascade-deleted by Kubernetes GC). At render time the Env Reconciler appends that Secret's reference to every member Pool's spec.template.spec.imagePullSecrets so the kubelet can pull private images uniformly across the Env. Not returned on GET — check imagePullSecretConfigured for read-state."
            },
            {
              "name": "imagePullSecretConfigured",
              "type": "bool",
              "required": false,
              "fixed": false,
              "describe": "Server-set on GET: true when the `ips-{envName}` Secret exists in the Env's namespace. On PUT it is the KEEP signal, and the only way to say it. The request is desired state, and the credentials cannot be read back to be echoed — so a PUT carrying neither `imagePullSecret` nor this flag is asking for the Secret to be DELETED. Sending back what GET returned therefore preserves the stored material, exactly as it does for every other field; a client that strips this flag while editing an unrelated setting revokes the registry credentials as a side effect. `imagePullSecret` present wins: new credentials replace the old ones whatever this says."
            },
            {
              "name": "gateway",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Egress gateway for every member Pool's sandbox Pods. Enabling it injects a transparent proxy sidecar, which is what makes per-sandbox egress filtering (network.allowOut / denyOut) and credential injection (network.rules with Secret.fill) possible on the create call. It carries no rules of its own: what a sandbox may reach, and what gets injected into which request, belong to that one sandbox and arrive with it. Changing this switch changes the Pod spec and therefore rolls the Env's pools."
            },
            {
              "name": "envd",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Tunes the sandbox agent (envd) every member Pool's Pods run. These are process flags fixed when a Pod starts, so changing them re-renders the pod template and rolls the Env's idle pools — the same way an image or gateway change does."
            },
            {
              "name": "updateStrategy",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Env-wide default rollout policy for member Pools when their idle-Pod identity changes. Overridable per member via EnvClusterMemberConfig.updateStrategy."
            },
            {
              "name": "volumes",
              "type": "object[]",
              "required": false,
              "fixed": false,
              "describe": "Mount existing PersistentVolumeClaims from this Env's namespace into the sandbox container. The claim must already exist and be Bound; the server never creates or deletes a PVC. Discover mountable claims with GET /volumes. Mounts are fixed at Pod creation — Kubernetes forbids mutating spec.volumes on a live Pod — so editing this list rolls the member Pools' idle Pods. Sandboxes already running keep their previous mounts until they are returned. In-place image upgrades never touch volumes."
            }
          ]
        },
        {
          "name": "labels",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Metadata stamped onto the Env's objects. Use for plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix).",
          "lever": "create a new Env with the labels you want"
        },
        {
          "name": "annotations",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Annotations stamped onto the Env's objects.",
          "lever": "create a new Env with the annotations you want"
        }
      ],
      "example": {
        "name": "my-env",
        "templateRef": {
          "name": "e2b-envd"
        },
        "mode": "WarmPool",
        "overrides": {
          "gateway": {
            "enabled": true
          }
        },
        "labels": {
          "team": "ai-infra"
        }
      }
    },
    "update": {
      "schema": "UpsertSandboxEnvRequest",
      "describe": "What a client may set on an Env — the SAME body for create and update, and the body `GET /envs/{name}` returns as `editable`. One shape rather than two subsets, because two subsets drift: while create accepted `mode` and update did not, a person editing an Env had no way to send back what the API had just handed them, and the file a client exported from a read was not a file a write accepted. Fields marked `x-immutable` are fixed at create. An update must carry them UNCHANGED: leaving one out, or sending a different value, is a 400 that names the value in force rather than a silent ignore — a body that drops the template is far more likely to be a bug in the caller than a request to have no template.",
      "fields": [
        {
          "name": "name",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "The Env this file is about. `create` requires it; `update` takes the name from the address and refuses a file whose `name` says something else. It is part of the write body so that the file a create wrote is the file an update takes — one shape, sent to one address. RFC 1123 DNS label, capped at 24 chars so derived names (PoolName = EnvName + ResourceKey + QuotaShort, PodName = PoolName + UUID) stay under the 63-char label/DNS limit."
        },
        {
          "name": "templateRef",
          "type": "object",
          "required": true,
          "fixed": true,
          "describe": "Which SandboxTemplate every member Pool is rendered from. Pin a version to hold the Env still across Template edits; omit it to follow the Template. Fixed after create.",
          "lever": "create a new Env from the other template and move the traffic over",
          "fields": [
            {
              "name": "name",
              "type": "string",
              "required": true,
              "fixed": false,
              "describe": "Name of the cluster-scoped SandboxTemplate the Env binds to."
            },
            {
              "name": "version",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Optional Template version pin. When empty, the Env tracks the Template's current spec.version."
            }
          ]
        },
        {
          "name": "mode",
          "type": "WarmPool | OnDemandJob",
          "required": false,
          "fixed": true,
          "describe": "WarmPool keeps idle Pods ready to claim. OnDemandJob creates a Pod per sandbox and tears it down after, trading start latency for holding no capacity between runs. Fixed after create.",
          "lever": "create a new Env in the other mode",
          "values": [
            "WarmPool",
            "OnDemandJob"
          ]
        },
        {
          "name": "overrides",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "SandboxTemplate fields this Env replaces uniformly for every member Pool. The Env represents a single class of sandbox runtime, so image, image policy, default timeouts and image-pull credentials are expected to be shared; per-Pool variation lives on each EnvClusterMember.",
          "fields": [
            {
              "name": "image",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Override the main container (containers[0]) image of the rendered Template. Applied before any per-Member overrides."
            },
            {
              "name": "podCreationImagePolicy",
              "type": "PoolDefaultImage | IdleImage",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.podCreationImagePolicy.",
              "values": [
                "PoolDefaultImage",
                "IdleImage"
              ]
            },
            {
              "name": "defaultStartupTimeout",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.defaultStartupTimeout. Duration string, e.g. '5m'."
            },
            {
              "name": "defaultIdleTimeout",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Mirrored onto every member Pool's spec.defaultIdleTimeout. Duration string, e.g. '30m'."
            },
            {
              "name": "imagePullSecret",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Write-only image-pull credentials. On create / update the server materialises a dockerconfigjson Secret named `ips-{envName}` in the Env's namespace with an OwnerReference back to the Env (cascade-deleted by Kubernetes GC). At render time the Env Reconciler appends that Secret's reference to every member Pool's spec.template.spec.imagePullSecrets so the kubelet can pull private images uniformly across the Env. Not returned on GET — check imagePullSecretConfigured for read-state."
            },
            {
              "name": "imagePullSecretConfigured",
              "type": "bool",
              "required": false,
              "fixed": false,
              "describe": "Server-set on GET: true when the `ips-{envName}` Secret exists in the Env's namespace. On PUT it is the KEEP signal, and the only way to say it. The request is desired state, and the credentials cannot be read back to be echoed — so a PUT carrying neither `imagePullSecret` nor this flag is asking for the Secret to be DELETED. Sending back what GET returned therefore preserves the stored material, exactly as it does for every other field; a client that strips this flag while editing an unrelated setting revokes the registry credentials as a side effect. `imagePullSecret` present wins: new credentials replace the old ones whatever this says."
            },
            {
              "name": "gateway",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Egress gateway for every member Pool's sandbox Pods. Enabling it injects a transparent proxy sidecar, which is what makes per-sandbox egress filtering (network.allowOut / denyOut) and credential injection (network.rules with Secret.fill) possible on the create call. It carries no rules of its own: what a sandbox may reach, and what gets injected into which request, belong to that one sandbox and arrive with it. Changing this switch changes the Pod spec and therefore rolls the Env's pools."
            },
            {
              "name": "envd",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Tunes the sandbox agent (envd) every member Pool's Pods run. These are process flags fixed when a Pod starts, so changing them re-renders the pod template and rolls the Env's idle pools — the same way an image or gateway change does."
            },
            {
              "name": "updateStrategy",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Env-wide default rollout policy for member Pools when their idle-Pod identity changes. Overridable per member via EnvClusterMemberConfig.updateStrategy."
            },
            {
              "name": "volumes",
              "type": "object[]",
              "required": false,
              "fixed": false,
              "describe": "Mount existing PersistentVolumeClaims from this Env's namespace into the sandbox container. The claim must already exist and be Bound; the server never creates or deletes a PVC. Discover mountable claims with GET /volumes. Mounts are fixed at Pod creation — Kubernetes forbids mutating spec.volumes on a live Pod — so editing this list rolls the member Pools' idle Pods. Sandboxes already running keep their previous mounts until they are returned. In-place image upgrades never touch volumes."
            }
          ]
        },
        {
          "name": "labels",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Metadata stamped onto the Env's objects. Use for plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix).",
          "lever": "create a new Env with the labels you want"
        },
        {
          "name": "annotations",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Annotations stamped onto the Env's objects.",
          "lever": "create a new Env with the annotations you want"
        }
      ]
    }
  },
  {
    "plural": "pools",
    "kind": "SandboxPool",
    "create": {
      "schema": "CreateEnvSandboxPoolRequest",
      "describe": "Add a member SandboxPool to an Env. The Pool's name and scalingGroup are derived from the effective resources — see `UpsertSandboxPoolRequest`, which is the same body this takes.",
      "fields": [
        {
          "name": "instanceType",
          "type": "string",
          "required": false,
          "fixed": true,
          "describe": "InstanceType catalog entry. Required when the Pool's Env is billed (env.poolSizing=billed); rejected when its Env is free-form (env.poolSizing=free-form); the caller's choice when the Env is unmanaged (env.poolSizing=either). May be combined with inlineResources to reserve a whole instance while running a smaller (rounded-down) Pod.",
          "lever": "add a member in the size you want, then remove this one"
        },
        {
          "name": "multiplier",
          "type": "int",
          "required": false,
          "fixed": true,
          "describe": "Multiplier applied to the InstanceType base resources to form the reservation envelope. Defaults to 1.",
          "lever": "add a member with the multiplier you want, then remove this one"
        },
        {
          "name": "inlineResources",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Explicit per-Pool resource requests/limits. Required — and used alone — when the Pool's Env is free-form (env.poolSizing=free-form), where it is the whole of the Pod's sizing; on a billed Env it is optional and instead combined with instanceType as the rounded-down actual Pod request (must fit within instanceType × multiplier). Sets both requests and limits.",
          "lever": "add a member with the resources you want, then remove this one",
          "fields": [
            {
              "name": "requests",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Resource requests keyed by resource name (e.g. cpu, memory). Values use Kubernetes Quantity strings, e.g. '500m', '1Gi'."
            },
            {
              "name": "limits",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Resource limits keyed by resource name."
            }
          ]
        },
        {
          "name": "replicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Initial replica count. Autoscaling, once enabled on this scalingGroup, owns subsequent changes."
        },
        {
          "name": "minReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Lower bound on this pool's replicas, enforced as a per-member scale-down floor by the Env autoscaler."
        },
        {
          "name": "maxReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Upper bound on this pool's replicas, enforced when the Env autoscaler distributes scale-up delta."
        },
        {
          "name": "labels",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Labels stamped onto this member's SandboxPool. Use for plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix). Required when the Pool's Env is billed (env.poolSizing=billed) — the Pool is rejected without it, because the scheduler it is submitted to has nothing to charge; rejected when the Env is free-form.",
          "lever": "add a member with the labels you want, then remove this one"
        },
        {
          "name": "annotations",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Annotations stamped onto this member's SandboxPool.",
          "lever": "add a member with the annotations you want, then remove this one"
        },
        {
          "name": "updateStrategy",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "Per-member rollout policy override. Unset inherits the Env overrides.updateStrategy, then autoUpdate=true / maxUnavailable=20%.",
          "fields": [
            {
              "name": "autoUpdate",
              "type": "bool",
              "required": false,
              "fixed": false,
              "describe": "Whether the member auto-rolls when its revision changes. Resolution order: member → env → default true. Set false to freeze a member on its current revision."
            },
            {
              "name": "maxUnavailable",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Rollout unavailability budget as an absolute count (\"3\") or a percentage of desired idle replicas (\"20%\"). Rounded down, floored at 1. Resolution order: member → env → default \"20%\"."
            }
          ]
        }
      ],
      "example": {
        "instanceType": "sci.c23-2",
        "multiplier": 1,
        "replicas": 1,
        "minReplicas": 0,
        "maxReplicas": 4,
        "inlineResources": {
          "requests": {
            "cpu": "100m",
            "memory": "500Mi"
          },
          "limits": {
            "cpu": "100m",
            "memory": "500Mi"
          }
        },
        "labels": {
          "quota.scitix.ai/url": "https://quota.example/q/1"
        }
      }
    },
    "update": {
      "schema": "UpsertSandboxPoolRequest",
      "describe": "Add a member SandboxPool to an Env. The server derives: - `name` = \"{envName}-{resourceKey}[-{quotaShort}]\" - `scalingGroup` = `resourceKey` (e.g. \"2c8Gi\") where `resourceKey` is `instancetype.DeriveResourceKey(effective resources)` and `quotaShort` (when a quota label is supplied) is `quotaProvider.DeriveShortName(quotaID)`. Members in the same `scalingGroup` share an autoscaling policy. WHICH OF THE SHAPES BELOW IS ACCEPTED IS THE ENV'S TO SAY, not the caller's — read `poolSizing` off the Env this Pool joins (Template detail and Env detail both carry it; `abx envs <name>` prints it). Billed Env (`poolSizing: billed`) — the Template is billed, so the Pool must name what it spends: - `instanceType` (+ optional `multiplier`) alone → the Pod is sized to the full `instanceType × multiplier` envelope (default `multiplier` = 1). - `instanceType` (+ `multiplier`) AND `inlineResources` together → `instanceType × multiplier` is the reservation/billing envelope, while `inlineResources` is the actual (possibly rounded-down) Pod request. Every dimension of `inlineResources` must be ≤ the envelope (round down allowed, round up rejected with 400); the reservation still charges quota for the whole instance. - `labels` must carry `quota.scitix.ai/url`. Free-form Env (`poolSizing: free-form`) — the Template is one the deployment does not bill, so the Pool is sized directly: - `inlineResources` alone → explicit per-Pool resource requests/limits. - `instanceType`, `multiplier` and the quota label are REJECTED (400): an instance type buys an instance nobody reserved, and a quota label on a Pool that is never submitted for reservation is a claim the server cannot honour. Unmanaged Env (`poolSizing: either`) — this deployment states no rule, so both shapes are accepted and the caller picks. Every deployment behaved this way before the rule existed. Under the two managed values there is no per-Pool choice: the shape follows from the Env, so two Pools of one Env are always sized the same way. `scalingGroup` / pool name are derived from the effective Pod request (the rounded-down `inlineResources` when supplied, else the full envelope), so the name reflects the Pod's real size and Pools downsized differently land in distinct scaling groups. This is also what an update takes, and what `GET` returns as `editable`: one body for create, update and export, so a client edits what the API handed it rather than translating between two subsets that drift. The fields marked `x-immutable` describe the Pool's SHAPE and are fixed at create. An update must carry them back unchanged — omitting one or changing one is a 400 that names the value in force, because a body that loses the instance type is a caller bug, not a request for a smaller machine.",
      "fields": [
        {
          "name": "instanceType",
          "type": "string",
          "required": false,
          "fixed": true,
          "describe": "InstanceType catalog entry. Required when the Pool's Env is billed (env.poolSizing=billed); rejected when its Env is free-form (env.poolSizing=free-form); the caller's choice when the Env is unmanaged (env.poolSizing=either). May be combined with inlineResources to reserve a whole instance while running a smaller (rounded-down) Pod.",
          "lever": "add a member in the size you want, then remove this one"
        },
        {
          "name": "multiplier",
          "type": "int",
          "required": false,
          "fixed": true,
          "describe": "Multiplier applied to the InstanceType base resources to form the reservation envelope. Defaults to 1.",
          "lever": "add a member with the multiplier you want, then remove this one"
        },
        {
          "name": "inlineResources",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Explicit per-Pool resource requests/limits. Required — and used alone — when the Pool's Env is free-form (env.poolSizing=free-form), where it is the whole of the Pod's sizing; on a billed Env it is optional and instead combined with instanceType as the rounded-down actual Pod request (must fit within instanceType × multiplier). Sets both requests and limits.",
          "lever": "add a member with the resources you want, then remove this one",
          "fields": [
            {
              "name": "requests",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Resource requests keyed by resource name (e.g. cpu, memory). Values use Kubernetes Quantity strings, e.g. '500m', '1Gi'."
            },
            {
              "name": "limits",
              "type": "object",
              "required": false,
              "fixed": false,
              "describe": "Resource limits keyed by resource name."
            }
          ]
        },
        {
          "name": "replicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Initial replica count. Autoscaling, once enabled on this scalingGroup, owns subsequent changes."
        },
        {
          "name": "minReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Lower bound on this pool's replicas, enforced as a per-member scale-down floor by the Env autoscaler."
        },
        {
          "name": "maxReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": "Upper bound on this pool's replicas, enforced when the Env autoscaler distributes scale-up delta."
        },
        {
          "name": "labels",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Labels stamped onto this member's SandboxPool. Use for plugin-driven metadata such as quota.scitix.ai/url (parsed by the server to derive the pool-name suffix). Required when the Pool's Env is billed (env.poolSizing=billed) — the Pool is rejected without it, because the scheduler it is submitted to has nothing to charge; rejected when the Env is free-form.",
          "lever": "add a member with the labels you want, then remove this one"
        },
        {
          "name": "annotations",
          "type": "object",
          "required": false,
          "fixed": true,
          "describe": "Annotations stamped onto this member's SandboxPool.",
          "lever": "add a member with the annotations you want, then remove this one"
        },
        {
          "name": "updateStrategy",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "Per-member rollout policy override. Unset inherits the Env overrides.updateStrategy, then autoUpdate=true / maxUnavailable=20%.",
          "fields": [
            {
              "name": "autoUpdate",
              "type": "bool",
              "required": false,
              "fixed": false,
              "describe": "Whether the member auto-rolls when its revision changes. Resolution order: member → env → default true. Set false to freeze a member on its current revision."
            },
            {
              "name": "maxUnavailable",
              "type": "string",
              "required": false,
              "fixed": false,
              "describe": "Rollout unavailability budget as an absolute count (\"3\") or a percentage of desired idle replicas (\"20%\"). Rounded down, floored at 1. Resolution order: member → env → default \"20%\"."
            }
          ]
        }
      ],
      "example": {
        "instanceType": "sci.c23-2",
        "multiplier": 1,
        "replicas": 1,
        "minReplicas": 0,
        "maxReplicas": 4,
        "inlineResources": {
          "requests": {
            "cpu": "100m",
            "memory": "500Mi"
          },
          "limits": {
            "cpu": "100m",
            "memory": "500Mi"
          }
        },
        "labels": {
          "quota.scitix.ai/url": "https://quota.example/q/1"
        }
      }
    }
  },
  {
    "plural": "scaling-groups",
    "kind": "ScalingGroup",
    "update": {
      "schema": "UpdateEnvAutoscalingGroupRequest",
      "describe": "Desired state of one autoscaling group. This is a PUT and it means it: a field left out is one the caller wants REMOVED. That is the only way \"take the ceiling off\" can be expressed — while an omitted field meant \"leave unchanged\" there was no request that could clear minReplicas, maxReplicas or a policy, and the form's empty box reported success without doing anything.",
      "fields": [
        {
          "name": "enabled",
          "type": "bool",
          "required": false,
          "fixed": false,
          "describe": "Whether the autoscaler acts on this group."
        },
        {
          "name": "minReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": ""
        },
        {
          "name": "maxReplicas",
          "type": "int",
          "required": false,
          "fixed": false,
          "describe": ""
        },
        {
          "name": "scaleUpPolicy",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "Scale-up behaviour for a scaling group (mode + cooldown + idle threshold + saturation cooldown).",
          "fields": [
            {
              "name": "mode",
              "type": "Conservative | Default | Aggressive",
              "required": false,
              "fixed": false,
              "describe": "How much warm headroom to keep, and how large a bite each scale-down takes. Sizing is relative to demand (claimed Pods + waiting claims), never to the pool's own replica count; waiting claims are always covered in full regardless of mode. Conservative=1 spare Pod, ceiling demand+1, scale-down -1; Default=ceil(demand/4) spare, ceiling 1.5x demand, scale-down -ceil(replicas/4); Aggressive=ceil(demand/2) spare, ceiling 2x demand, scale-down -ceil(replicas/2).",
              "values": [
                "Conservative",
                "Default",
                "Aggressive"
              ]
            },
            {
              "name": "cooldownSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Minimum seconds between two consecutive scale-up events (group-level)."
            },
            {
              "name": "idleThresholdSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Aggregate idleReplicas=0 must persist for this long before the proactive trigger fires. Zero disables proactive scale-up."
            },
            {
              "name": "idleZeroQuietWindowSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Suppresses the proactive idleZero trigger when no Sandbox.Create has been observed for the Pool within this many seconds. Reactive scale-ups (queue length > 0 with no idle Pod) ignore this window. Set to 0 to disable the gate. Default 300."
            },
            {
              "name": "saturationCooldownSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "How long a member stays marked saturated after a probe returned InsufficientResources / InvalidSpec. Default 60s."
            }
          ]
        },
        {
          "name": "scaleDownPolicy",
          "type": "object",
          "required": false,
          "fixed": false,
          "describe": "Scale-down timing for a scaling group. How many replicas each event removes comes from scaleUpPolicy.mode — scale-down mirrors the scale-up mode so a pool sheds capacity on the same scale it acquired it.",
          "fields": [
            {
              "name": "idleTimeoutSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Minimum seconds a pod must remain Idle before it becomes a scale-down candidate. Also bounds the step: a scale-down never removes more pods than have aged past this."
            },
            {
              "name": "stabilizationSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Minimum seconds between two consecutive scale-down events."
            },
            {
              "name": "protectionWindowSeconds",
              "type": "int",
              "required": false,
              "fixed": false,
              "describe": "Seconds during which a scale-down-marked pod can still be claimed (cancels deletion)."
            }
          ]
        }
      ]
    }
  },
  {
    "plural": "admin-templates",
    "kind": "SandboxTemplate",
    "create": {
      "schema": "UpsertSandboxTemplateRequest",
      "describe": "",
      "fields": [
        {
          "name": "crdJson",
          "type": "string",
          "required": true,
          "fixed": false,
          "describe": "Complete SandboxTemplate CRD object serialized as JSON. metadata.name is required. Labels, annotations, and resourceVersion are extracted from the JSON."
        }
      ]
    },
    "update": {
      "schema": "UpsertSandboxTemplateRequest",
      "describe": "",
      "fields": [
        {
          "name": "crdJson",
          "type": "string",
          "required": true,
          "fixed": false,
          "describe": "Complete SandboxTemplate CRD object serialized as JSON. metadata.name is required. Labels, annotations, and resourceVersion are extracted from the JSON."
        }
      ]
    }
  },
  {
    "plural": "api-keys",
    "kind": "APIKey",
    "create": {
      "schema": "SelfCreateAPIKeyRequest",
      "describe": "Request body for tenant self-service API key creation. Namespace/user/team are taken from the caller's auth context.",
      "fields": [
        {
          "name": "description",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Optional human-readable description for this key (e.g. its intended use)."
        },
        {
          "name": "expiresAt",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Optional RFC 3339 expiry timestamp. Omit for a non-expiring key."
        },
        {
          "name": "mode",
          "type": "unrestricted | agent",
          "required": false,
          "fixed": false,
          "describe": "What this key is for, and therefore what it may do unattended. * `unrestricted` — a person's own credential. Behaves exactly as every key did before this field existed. * `agent` — handed to something that acts while nobody is watching. Its platform WRITES (create an environment, add a pool, delete anything) are held until a person approves them, and the refusal names an approval to act on. The sandbox surface is unaffected in both modes: an agent-mode key starts sandboxes, runs commands and moves files exactly as an unrestricted one does. It is the same key the E2B SDK uses, and the gate deliberately does not sit on that path — only on the platform operations `abx` performs.",
          "values": [
            "unrestricted",
            "agent"
          ]
        }
      ]
    }
  },
  {
    "plural": "admin-api-keys",
    "kind": "AdminAPIKey",
    "create": {
      "schema": "CreateAPIKeyRequest",
      "describe": "",
      "fields": [
        {
          "name": "namespace",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Kubernetes namespace to associate the key with."
        },
        {
          "name": "user",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Username to associate the key with."
        },
        {
          "name": "team",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Team name to associate the key with."
        },
        {
          "name": "description",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Optional human-readable description for this key."
        },
        {
          "name": "expiresAt",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Optional RFC 3339 expiry timestamp. Omit for a non-expiring key."
        },
        {
          "name": "tokenHash",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Full SHA-256 hex hash of the raw token (64 hex chars). When provided together with hashPrefix, the key is imported using the given hash instead of generating a new random token. The operation is idempotent — if a key with the same hash already exists it is silently accepted. Admin-only."
        },
        {
          "name": "hashPrefix",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "First 16 hex characters of the tokenHash. Required when tokenHash is provided."
        },
        {
          "name": "issuedAt",
          "type": "string",
          "required": false,
          "fixed": false,
          "describe": "Original issue timestamp (import mode). Used to preserve the original creation time. Ignored when tokenHash is not provided."
        }
      ]
    }
  }
]
