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
3. Rebuild + run the envd unit tests (`go test ./internal/...`). Two
   filesystem tests need `bindfs` on the host and fail without it; that is an
   environment gap, not a regression.
4. Bump `.github/workflows/build-envd.yml`'s `infra_ref` default to the same
   SHA. The workflow and `build-envd.sh` are checked against each other by
   nothing but this step.

## 0001-skip-oom-nice-wrapper-when-not-firecracker.patch

**Base:** envd 0.9.0 (`0c2108b1b76a66d18cf095d51fd7c51c703648ae`)

> Carried unchanged in substance from 0.6.13 onward. Upstream reworked the
> wrapper so ionice/nice are looked up with `exec.LookPath` and skipped when
> absent — but the `oom_score_adj` write is still unconditional, which is the
> half that breaks under Kubernetes, and upstream still has no not-Firecracker
> branch. The patch keeps upstream's improvement in the Firecracker branch and
> only bypasses the whole wrapper in not-FC mode.
>
> Two hunks were re-made by hand for 0.9.0: `main.go` (upstream moved the
> default user to `execcontext.BuiltinDefaultUser`) and
> `internal/execcontext/context.go` (upstream added a `UserDelivered` field,
> so `IsNotFC` goes after it).

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

**Base:** envd 0.9.0 (`0c2108b1b76a66d18cf095d51fd7c51c703648ae`), **with 0001
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

## 0003-skip-mmds-poll-when-not-firecracker.patch

**Base:** envd 0.9.0 (`0c2108b1b76a66d18cf095d51fd7c51c703648ae`), **with 0001
and 0002 already applied**.

**Problem.** `-isnotfc` already suppresses the MMDS poll `main.go` starts at
boot, but `internal/api/init.go` starts a second one on **every** `/init`,
unconditionally. AgentBox sends an `/init` to every sandbox, so every sandbox
polls.

MMDS is Firecracker's metadata service at `169.254.169.254`. Outside a microVM
nothing answers, so the poll can only run its deadline out: one request every
50ms for 60s — **1200 per sandbox, every time**. Each is a real connection
attempt that the pod's egress filter evaluates and logs, which is how it was
found: a sidecar log that was almost entirely

```
level=INFO msg="egress denied" host=169.254.169.254 port=80 match=ssrf
```

at a steady 50ms cadence for the first minute of every sandbox's life.

**Fix.** Gate that goroutine on `!a.isNotFC`, the same flag `main.go` already
uses for its own poll. The two values it would have fetched
(`E2B_SANDBOX_ID` / `E2B_TEMPLATE_ID`) are known to the orchestrator, which can
put them in the `/init` body it is already sending.

`host.PollForMMDSOpts` is indirected through a package-level `var` so the test
can assert both directions — that the poll does not start outside Firecracker,
and still does inside it. Without the seam the behaviour is only observable as
network traffic.

Files: `internal/api/init.go` (+ `init_test.go`).

Also a candidate for upstreaming: the boot-time poll is already gated, so
gating this one is the same decision applied consistently.
