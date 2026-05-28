# poolmigration

Steady-state reconciler that keeps every legacy same-name `SandboxPool` ↔
`SandboxEnv` pair in sync. Replaces the original one-shot adopter: drift
on already-adopted Pools is fixed in the same code path as the initial
wrap-into-Env, so there is no separate "backfill" loop.

## Which Pools does it touch?

Two populations:

- **Legacy / orphan**: created directly as a `SandboxPool` CR. May or may
  not already have an owning same-name `SandboxEnv`. This reconciler is
  authoritative for them.
- **Env-managed**: created by the `SandboxEnv` reconciler from a member
  entry. Pool name differs from the owning Env's name. This reconciler
  exits early on these — their `Member.Spec` is the post-`PreCreatePool`
  frozen snapshot and must not be re-derived.

Discriminator: a Pool with any `OwnerReference` to a `SandboxEnv` whose
`Name` differs from the Pool's own name is Env-managed.

## What it does per legacy Pool

1. Looks up the same-name `SandboxEnv`; creates it (with `TemplateRef` +
   autoscaling derived from the Pool) when missing.
2. Computes the desired `EnvClusterMember` from the live Pool and patches
   `env.Spec.Clusters[localClusterID].Members[me]` when it differs.
3. Stamps a controlling `OwnerReference` from Pool → Env.

Each step is independently idempotent so a crash between steps heals on
the next pass.

## Member field ownership

`Member.Metadata` and `Member.Spec` are *frozen snapshots*. Each field is
filled exactly once — at first sync, or when the existing Member has it
empty — and never overwritten afterwards. This is what keeps "Template
upgrades do not auto-propagate to legacy Pools" honest.

`Member.Config.ScalingGroup`, `InstanceType`, `Multiplier`, and
`InlineResources` are adopter-derived and overwritten when the
derivation drifts from the stored value. User-supplied Config fields
(`Labels`, `Annotations`, `MaxReplicas`, priorities) are preserved.

When the Pool only carries `TemplateName` and the existing
`Member.Spec.Template` is empty, the reconciler fetches the referenced
`SandboxTemplate` and copies its `EmbeddedSandboxTemplate` into the
Member. The resolved `(name, version)` is then stamped onto the Pool's
annotations as the version anchor — subsequent reconciles see the
non-empty `Member.Spec.Template` and skip the fetch entirely.

## Ownership signal

The authoritative "this Pool is owned by an Env" signal is the
`OwnerReference` itself — not a label. The Pool Reconciler gates its
legacy autoscaler on `agentsv1alpha1.HasEnvOwner(pool)` which checks
only the owner refs.

This is intentionally robust against the "admin deletes the Env"
scenario: the next reconcile re-creates the Env and re-stamps the ref.

## Removal path

Once every legacy same-name Pool has been migrated to a Phase-2 member
(Pool name distinct from Env name), this reconciler becomes a no-op and
the package can be deleted. Removal checklist:

- Delete `pkg/controllers/sandboxenv/poolmigration/`.
- Drop the `PoolAdoptionReconciler` registration from
  `cmd/sandbox/app/app.go`.
- The Pool Reconciler's `HasEnvOwner` gate keeps working unchanged
  because the Env-creates-Pool flow stamps the same OwnerReference.

No other code paths depend on this package.
