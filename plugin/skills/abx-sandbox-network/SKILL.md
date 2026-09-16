---
name: abx-sandbox-network
description: Control what a sandbox can reach on the network — denying outbound traffic so an agent under evaluation cannot look up answers or pull extra packages, and allowing only the hosts a task genuinely needs. Use for evaluation isolation, egress policy, or "why can my sandbox still reach the internet". Credential injection lives in abx-sandbox-secrets.
---

# What a sandbox can reach

Two separate decisions, and conflating them is the usual mistake:

```
SandboxEnv     the env carries it      does this environment HAVE a gateway
create call    the sandbox carries it  what THIS sandbox may reach
```

The env carries a switch and no rules. Rules belong to the individual sandbox
and arrive with the create call, in the E2B SDK's own vocabulary — there is no
AgentBox dialect to learn. The env's side is `abx update envs <env> --help`
(one field, and that page names it); the sandbox's side is the SDK, whose shape
is in the image rather than in this file.

## Why the switch is on the env and the rules are not

Installing the proxy sidecar changes the Pod spec, so it rolls the pool. That
genuinely is an environment-level decision. Rules are per sandbox because an
environment is shared: an env-level allowlist would be a default that every
create overrides anyway, so it would buy nothing and cost a second configuration
surface.

**Fail-closed, deliberately:** a create that carries filtering rules against an
env with no gateway is **refused with 400**, not accepted-and-ignored. A Pod
without the sidecar has no redirection either, so accepting it would mean the
rules silently did nothing — which for an evaluation is the worst possible
outcome, because the run completes and the numbers are wrong.

## Cutting an evaluation off from the internet

This is the common case: the agent under test must not fetch the answer, and
must not install its way around a missing dependency.

The create call takes a network policy; **ask for its shape rather than
recalling it** — the fields are in the E2B spec the image ships
(`/opt/agentbox/source/pkg/openapi/e2b/openapi.yaml`), and the installed SDK
will tell you its own signature. `abx-common` has the whole recipe; the short
version is `abx update envs <env> --help` for anything the env owns and the spec
above for anything the sandbox owns.

The call itself needs somewhere to go first: `abx envs <env> docs` prints the
E2B API URL and data-plane domain for the env's cluster, which is what the
create is sent to. Take the public entry it offers unless the caller runs inside
the cluster.

What matters, and does not change with the field names: **deny-all-then-allow**,
never allow-all-then-deny. A denylist is a list of the routes you thought of.

Three things worth checking before declaring a run isolated:

1. **The env has a gateway.** Without it the create is refused — verify you saw
   a sandbox, not a 400.
2. **The package index is not on the allowlist** unless the task needs it. It is
   the most common accidental hole: the agent cannot search, but it can `pip
   install` something that can.
3. **Test it from inside.** `sbx.commands.run("curl -sS -m 5 https://example.com")`
   should fail. An isolation you did not observe failing is an isolation you are
   assuming.

## Allowing one thing and nothing else

An evaluation that needs a model API but nothing else is the same shape:
deny everything, then allow that one host. If the credential for it must not be
readable inside the sandbox, that is `abx-sandbox-secrets` — the sidecar can
inject it on the way out, so the sandbox reaches the API while holding no key.

## When traffic gets through anyway

- Only `:80` and `:443` are parsed at layer 7. Traffic on another port passes
  through without inspection, so a rule written for `:3000` does nothing.
- Check the env actually has the gateway on, and that the sandbox you are
  testing came from that env.

## Related

- `abx-sandbox-secrets` — reaching a service without holding its credential
- `abx-harbor-framework` — running an evaluation suite on top of this
- `abx-common` — endpoint, key, cluster, approval
