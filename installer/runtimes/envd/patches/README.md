# envd patches

ScitiX-carried fixes applied on top of upstream [`e2b-dev/infra`](https://github.com/e2b-dev/infra)
`packages/envd` at build time. `build-envd.sh` clones the pinned `INFRA_REF`,
applies every `*.patch` here (in filename order) with `git apply`, then builds.

A patch that fails to apply **aborts the build** — that means upstream drifted
from `INFRA_REF`; rebase the patch and bump `INFRA_REF` together. Never ship an
unpatched envd: it reintroduces the bug below.

## Bumping envd

1. Pick the new commit, set `INFRA_REF` (full SHA) in `build-envd.sh`.
2. For each patch **in filename order, onto the same tree**: `git -C <infra>
   apply <patch>` against the new ref; if it rejects, re-make the edit by hand
   and regenerate the patch:
   `git -C <infra> diff packages/envd/... > patches/000X-....patch`.
   Regenerating out of order (or from a pristine checkout) produces a patch that
   duplicates an earlier one's changes and cannot be applied after it.
3. Rebuild + run the envd unit tests (`go test ./internal/services/process/handler/`).

## 0001-skip-oom-nice-wrapper-when-not-firecracker.patch

**Base:** envd 0.7.0 (`8a3f69da6f822c2de2b310dd1076d2c309eef919`)

> Applies unchanged from 0.6.13 through 0.7.0 (one line of offset in `main.go`).
> Upstream reworked the wrapper so ionice/nice are looked up with
> `exec.LookPath` and skipped when absent — but the `oom_score_adj` write is
> still unconditional, which is the half that breaks under Kubernetes, and
> upstream still has no not-Firecracker branch. The patch keeps upstream's
> improvement in the Firecracker branch and only bypasses the whole wrapper in
> not-FC mode.

**Problem.** Before exec-ing each command, envd wraps it as
`/bin/sh -c "echo 100 > /proc/$$/oom_score_adj && exec /usr/bin/nice -n N -- CMD"`.
That priming makes sense only inside a Firecracker microVM (where children would
otherwise inherit envd's protected `oom_score_adj=-1000` / `nice -20`). Under
Kubernetes:

- the kubelet already manages `oom_score_adj` per QoS and pins a floor that
  **forbids lowering it**, so the `echo … > oom_score_adj` write fails with
  `EACCES`; the `&&` then short-circuits and the user's command never runs;
- routing every exec through `/bin/sh` also makes execution depend on a working
  `/bin/sh` in the user's image (busybox multi-call binaries broke outright).

**Fix.** envd already takes a `-isnotfc` flag (set by `agentbox-entrypoint.sh`).
Thread it into `execcontext.Defaults.IsNotFC` and, in the process handler, when
not-FC, exec the command **directly** instead of through the sh/oom/nice wrapper.
cgroup attachment and uid/credential setup are unchanged — those are the real
resource controls under Kubernetes.

Files: `internal/execcontext/context.go`, `main.go`,
`internal/services/process/handler/handler.go` (+ `handler_oom_test.go`).

This replaces the old `install_sh_shim` hack in `agentbox-entrypoint.sh` (which
mutated every image's `/bin/sh` and corrupted busybox images). Candidate for
upstreaming — gating the wrapper on `!isNotFC` is a legitimate change.

## 0002-await-init-gate.patch

**Base:** envd 0.7.0 (`8a3f69da6f822c2de2b310dd1076d2c309eef919`), **with 0001
already applied**.

> Patches are applied in filename order onto one tree, so each is a diff against
> the result of the ones before it — not against pristine upstream. Regenerate
> with 0001 applied, or the two will both try to add the same line and the second
> will fail to find its context.

**Problem.** envd starts listening before the orchestrator has finished setting
the sandbox up. A sandbox's environment variables, its injected trust-store
certificate and its egress credentials all arrive through `POST /init`, which
happens *after* the daemon is up. A command accepted in that window runs in a
sandbox that is not yet the one the caller asked for: an empty environment, an
untrusted CA, an egress path with no credentials armed. It does not fail — it
returns a wrong answer, which is the harder failure to notice.

AgentBox closes this at two other layers already: the create call waits for the
sandbox to be armed, and the data-plane router refuses to route to one that is
not. This patch is the third: it holds even for a caller that reaches the Pod
directly, bypassing both.

**Fix.** A new `-await-init` flag. When set, envd refuses the process and
filesystem Connect RPCs with a `failed_precondition` until the first `/init`
lands. `/health` is deliberately **not** gated: the readiness probe drives the
phase transition that triggers the very `/init` this gate waits for, so gating
health would deadlock the two against each other.

**Rollout order matters.** The control plane must already be sending an
unconditional `/init` before any template turns this on; otherwise sandboxes in
that template are permanently unusable. It ships default-off for that reason.

Files: `main.go`.
