---
name: abx-managed-agent
description: Give your own agent a sandbox for its file and shell tools — the AgentBox hands bindings for Claude Agent SDK, OpenCode and generic MCP, plus the platform side that has to exist first. Use when asked to connect sandboxes to an existing agent or harness, to make an agent's work durable across restarts, or to integrate AgentBox into a product. Pool sizing lives in abx-resource-capacity.
---

# Sandboxes as your agent's hands

Your agent keeps running where it runs. Its `bash`, `read`, `write`, `edit`,
`grep`, `glob` and `apply_patch` stop touching that machine and start acting on
a sandbox bound to the conversation — so the work survives a process restart,
can be browsed from a file UI, and is reclaimed on a timer instead of
accumulating in someone's home directory.

```
your agent process                      AgentBox
┌──────────────────────────┐            ┌────────────────────────┐
│ harness                  │            │  sandbox for this      │
│   ↓ tool call            │            │  session               │
│ hands binding            │            │                        │
│   ↓ HTTP                 │            │                        │
│ hands daemon  ───────────┼── E2B API ─┼─→ bash / files         │
└──────────────────────────┘            └────────────────────────┘
```

## Read this before describing it to anyone

**Confinement is not isolation.** The agent process still runs on your machine,
with your files, your environment and your credentials. What moves into the
sandbox is where the agent's *tools* act. That is genuinely worth having, but an
agent that can install a package or load a plugin can reach the host again. For
real isolation, run the harness itself in a container — orthogonal, and they
compose.

Saying otherwise to a user is the one mistake here that matters.

## Three bindings, one behaviour

| Harness | Binding |
|---|---|
| Claude Agent SDK | `sdk/hands/typescript/src/harness/claude-code/` |
| OpenCode | `sdk/hands/typescript/src/harness/opencode/` |
| anything else | generic MCP: `sdk/hands/typescript/src/harness/mcp/` |

`core/` decides what the tools do; a binding only says "replace these built-ins
with these tools" in one harness's vocabulary. **A binding that reimplements
behaviour from `core` is a bug** — two copies drift, and the drift is invisible
from the signatures.

The session-binding daemon and workspace file API are the Python half
(`sdk/hands/python/agentbox_hands/`).

## Ask two things before configuring

1. **Which harness** — Claude Agent SDK, OpenCode, or something else that needs
   the generic MCP binding. The binding decides the whole integration and there
   is no useful generic answer.
2. **How many concurrent conversations**, not how many users. Ten people with
   one session each is ten; the pool is sized against that number.

Then say the confinement sentence below **before** they build anything on it.

## What has to exist on the platform first

1. **An env** whose template is what your agent's tools need — an E2B-compatible
   image with a shell and the language runtimes the work requires.
2. **A pool with idle Pods**, sized to concurrent *conversations*, not total
   users. Ten people chatting with one session each is ten.
3. **A key for the right identity.** Which identity a sandbox is created as
   decides whose namespace and whose quota it lands in.

```bash
abx envs
abx envs <env> pools
abx envs <env> pools <pool> scale --replicas 10
abx whoami          # which identity, and whether this key's writes need approval
abx envs <env> docs # where the SDK points: E2B API URL, data domain, scheme
```

That last one is not optional reading before wiring a binding up. The agent's
tools reach the sandbox through the E2B API, and which URL that is depends on
the cluster the env lives on — `abx envs <env> docs` is where the platform has
already worked it out. Prefer the public entry it prints unless the agent
process itself runs inside the cluster.

## Identity: the decision to make deliberately

Two coherent models, and mixing them is where the confusion comes from:

- **Per-person** — each session is created with that person's own credential.
  Sandboxes land in their namespace and count against their quota, and their
  sandbox list shows their own work.
- **Service-owned** — every session is created as the env's owner, and *who the
  conversation is for* is recorded in sandbox `metadata`. One quota, one
  namespace, and the list needs the metadata column to tell sessions apart.

Pick one. Under the second, all usage bills to one tenant — which is a choice,
not a bug, but only if it was chosen.

Whichever you pick, **a tenant key, never an admin key.** A service holding an
admin credential means every conversation runs as admin, and nothing in the
behaviour reveals it until something goes wrong.

## Credentials inside the sandbox

The sandbox is a remote execution environment that holds **no real
credentials** — decoy values sit in the environment and the egress sidecar
substitutes the real ones on the way out, per host and per header. So an agent
can run arbitrary shell there without that shell being a way to exfiltrate a
key. When you need a third-party token available to the agent's code, that is
what the vault and injection rules are for, not an environment variable.

## Related

- `abx-resource-capacity` — sizing the pool for concurrent sessions
- `abx-observe` — why a session's sandbox failed
- `abx-common` — endpoint, key, cluster, approval
- Reference: `sdk/hands/README.md` in the agent-sandbox repository

## Read more

<https://scitix.github.io/Agent-Sandbox/docs/concepts/envs.md>
