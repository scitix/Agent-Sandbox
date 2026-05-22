# poolmigration

Transitional reconciler that wraps every existing `SandboxPool` in a
same-named `SandboxEnv` during the SandboxEnv Phase 1 rollout.

## What it does

For each `SandboxPool` it sees:

1. Computes the Pool's `(InstanceType, Multiplier)` via the
   `instancetype.Provider` if enabled. Falls back to copying the Pool's
   first-container resources into `member.InlineResources`.
2. Looks up an existing `SandboxEnv` named `pool.Name`. Creates one
   (with `TemplateRef` + autoscaling derived from the Pool) when absent.
3. Appends the Pool to `env.Spec.Clusters[localClusterID].Members` when
   not already present.
4. Patches the Pool with a non-controlling `OwnerReference` back to the
   Env.

Each step is independently idempotent so a crash between steps heals on
the next pass.

## Ownership signal

The authoritative "this Pool is owned by an Env" signal is the
`OwnerReference` itself — not a label. The Pool Reconciler gates its
legacy autoscaler on `agentsv1alpha1.HasEnvOwner(pool)` which checks
only the owner refs.

This is intentionally robust against scenario D ("admin deletes the
Env"): the next reconcile re-creates the Env and re-stamps the ref.

## Removal path

This package is the transitional half of the migration. The steady-state
half (status aggregation + Env-level autoscaler) lives in the parent
`pkg/controllers/sandboxenv` package and stays.

Once we cut over to a flow where SandboxEnv is the user-facing primary
and the Pool is created **by** the Env (Phase 2+), this whole package
becomes dead code. Removal checklist:

- Delete `pkg/controllers/sandboxenv/poolmigration/`.
- Drop the `PoolAdoptionReconciler` registration from
  `cmd/sandbox/app/app.go`.
- The Pool Reconciler's `HasEnvOwner` gate keeps working unchanged
  because the Env-creates-Pool flow stamps the same OwnerReference.

No other code paths depend on this package.
