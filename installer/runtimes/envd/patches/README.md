# envd patches

ScitiX-carried fixes applied on top of upstream [`e2b-dev/infra`](https://github.com/e2b-dev/infra)
`packages/envd` at build time. `build-envd.sh` clones the pinned `INFRA_REF`,
applies every `*.patch` here (in filename order) with `git apply`, then builds.

A patch that fails to apply **aborts the build** — that means upstream drifted
from `INFRA_REF`; rebase the patch and bump `INFRA_REF` together. Never ship an
unpatched envd: it reintroduces the bug below.

## Bumping envd

1. Pick the new commit, set `INFRA_REF` (full SHA) in `build-envd.sh`.
2. For each patch: `git -C <infra> apply <patch>` against the new ref; if it
   rejects, re-make the edit by hand and regenerate the patch:
   `git -C <infra> diff packages/envd/... > patches/000X-....patch`.
3. Rebuild + run the envd unit tests (`go test ./internal/services/process/handler/`).

## 0001-skip-oom-nice-wrapper-when-not-firecracker.patch

**Base:** envd 0.5.11 (`b781ad49198da0a1659a09a5cfc038fd52c3d433`)

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
