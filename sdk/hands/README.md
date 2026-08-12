# Hands

Confine an agent harness's file and shell tools to an AgentBox sandbox.

The agent keeps running wherever you run it. Its `bash`, `read`, `write`, `edit`,
`grep`, `glob` and `apply_patch` stop touching that machine and start acting on a
sandbox bound to the conversation — which means the work survives a process
restart, can be inspected from a file browser, and is reclaimed on a timer
instead of accumulating in someone's home directory.

```
your agent process                      AgentBox
┌──────────────────────────┐            ┌────────────────────────┐
│ harness (Claude Code,    │            │  sandbox for this      │
│ OpenCode, your own loop) │            │  session               │
│   ↓ tool call            │            │   /home/agents/…       │
│ hands binding            │            │                        │
│   ↓ HTTP                 │            │                        │
│ hands daemon  ───────────┼── E2B API ─┼─→ bash / files         │
│  session → sandbox       │            │                        │
└──────────────────────────┘            └────────────────────────┘
```

## What is here

| Path | What it is |
|---|---|
| `typescript/src/core/` | The seven tools and their behaviour contract. Harness-neutral: nothing here knows what a harness is. |
| `typescript/src/harness/claude-code/` | Claude Agent SDK binding. |
| `typescript/src/harness/opencode/` | OpenCode binding plus the seven override entry files. |
| `typescript/src/harness/mcp/` | Generic MCP binding, for a harness this package has no binding for. |
| `python/agentbox_hands/` | The session-binding daemon and the workspace file API. |
| `python/agentbox_hands/_tenant_classifier.py` | Not part of the contract — a product feature that shipped in the same app and is mounted only on request. See below. |

Three layers, and the middle one is the interesting one. `core` decides what the
tools *do*; a binding only expresses "replace these built-ins with these tools" in
one harness's vocabulary. A binding that reimplements behaviour from `core` is a
bug, because the two copies drift and the drift is invisible from the signatures.

## Confinement is not a security boundary

Read this before describing the package to anyone.

The agent process still runs on your machine, with your files, your environment
and your credentials. What moves into the sandbox is where the agent's *tools*
act. That is worth having — the work is durable, shareable and reclaimable — but
it is cooperative confinement, not isolation. An agent that can install a package
or load a plugin can reach the host again. For actual isolation, run the harness
itself in a container; this package is orthogonal to that and composes with it.

How well the built-ins can be taken away also differs sharply by harness:

| Harness | Mechanism | How complete |
|---|---|---|
| Claude Code | `tools: []` + `disallowedTools` + `mcpServers` + `toolAliases`, all four | Good. Subagents inherit it, and `sandbox-escape.test.ts` drives a real session to check. |
| OpenCode | A tool whose name matches a built-in replaces it | **Unverified — see below.** Against 1.18.16 the override appears to register *alongside* the built-in, not in place of it. |
| Codex | — | **Not achievable today.** Disabling the built-in shell is an open upstream request, and `apply_patch`-shaped commands are intercepted out of `shell` and executed even when the tool is not offered. The `overrides.codex` column is populated in anticipation; there is no Codex binding. |
| Anything else | The generic MCP binding | **MCP can add tools. It cannot remove a harness's built-ins.** An agent that still has its own `bash` will keep using it, so the confinement has to come from that host's own configuration; what the binding supplies is the sandbox-backed toolset, not the confinement. |

### The OpenCode override may not be replacing anything

Measured against OpenCode 1.18.16, with `bash.ts` installed in the config dir:

- `/experimental/tool/ids` lists `bash` **twice**.
- The tool listing returns **both** descriptions — the built-in's and the override's.
- Adding `"tools": {"bash": false}` to `opencode.json` removed neither from that
  endpoint.
- Both `tool/` and `tools/` are scanned, so the directory name is not the problem.

What a real turn dispatches to is **not** established: it needs a live model
credential to observe, and those endpoints may report the pre-gate registry rather
than the tool set a prompt is actually built with. So this is an open question, not
a proven hole — but it is the wrong way round for something load-bearing. If the
built-in wins, an agent's `bash` and `read` act on the machine running the harness,
which is the one thing this package exists to prevent.

Until it is answered, treat OpenCode's file/shell confinement as unverified. The
Brain image therefore defaults to Claude Code, whose mechanism is different and is
checked by `sandbox-escape.test.ts` driving a real session.

Answering it properly means running one turn under each harness and asserting which
process the shell command ran in. That belongs in `sandbox-escape.test.ts`, which
already has the shape for it on the Claude Code side.

## Using it

### Claude Agent SDK

```ts
import { sandboxToolOptions } from '@scitix/agentbox-hands/claude-code'

for await (const msg of query({
  prompt,
  options: { ...sandboxToolOptions({ sessionKey: threadId }) },
})) { … }
```

`sandboxToolOptions` returns all five tool-related options as one object on
purpose: applying four of the five loses the guarantee and nothing complains.

### OpenCode

Point the config dir's `tools/<name>.ts` at the binding — one line each, and the
filename is what does the overriding:

```ts
// ~/.config/opencode/tools/bash.ts
export { default } from '@scitix/agentbox-hands/opencode/tools/bash'
```

**Also raise `tool_output` in `opencode.json`,** or the confinement leaks:

```json
{ "tool_output": { "max_bytes": 50000000, "max_lines": 1000000 } }
```

OpenCode truncates any oversized tool result — including one from a plugin tool —
by writing the full text to a file **on the machine running the harness** and
handing the agent that path. The agent's `read` runs in the sandbox and cannot
open it. The offload in `core/offload.ts` exists to get there first and write the
file into the sandbox instead, so OpenCode's own limits must be pushed out of the
way. `core/offload.ts` also trips on line count, not only bytes, so a deployment
that forgets this config still degrades to a readable sandbox file rather than an
unreachable local one — but that is a backstop, not a substitute.

### Any other agent, over MCP

```ts
import { createServer } from 'node:http'
import { handsMcpHttpHandler } from '@scitix/agentbox-hands/mcp/http'

createServer(handsMcpHttpHandler()).listen(8766)
```

Every request must carry the conversation's own stable id in `X-Hands-Session` —
the sandbox is bound to it. Reusing the MCP transport's session id instead looks
right and is not: it changes on reconnect, so the conversation quietly moves to a
new sandbox, and it is shared when one connection serves several conversations, so
they end up on one filesystem. The handler rejects a request without the header
rather than inventing one.

It runs stateless, one server per request, because the session state lives in the
daemon; a second session concept at the transport layer would only be a way for the
two to disagree. And it performs **no authentication** — the session key it trusts
is a plain header, so mount it behind whatever the deployment already has.

### Session identity

Every call carries a `sessionKey`, and the daemon derives the sandbox from it, so
it has to be stable for the whole conversation. Each harness maps its own session
concept onto it: OpenCode passes its session id, a gateway passes its thread id
(which is what lets a conversation keep its sandbox across a harness switch).

## Behaviour that looks cosmetic and is not

Most of `core/tools.ts` is load-bearing in ways the signatures do not show. The
tests are the specification; `core/__snapshots__/tools.test.ts.snap` pins every
description and parameter, because these are prompt contract — changing them does
not fail, it just makes the agent worse.

- `read` pages itself and its trailing note is a protocol. The agent resumes from
  `Use offset=N to continue`; without it, it re-reads from line 1 or gives up.
- `read` is the one tool exempt from offload. Its byte cap sits just above the
  offload threshold on purpose, so a large read never becomes a pointer to a copy
  of a file the agent could already read.
- A miss under the attachment root is rewritten into an actionable message,
  because a bare 404 there almost always means the sandbox was recycled.
- `grep` and `glob` cap their result counts. An unbounded grep over a mounted
  source tree returns tens of thousands of lines.
- Every result may carry a one-shot `notice`. It must render as a **leading**
  line and never inside the payload.
- `bash` output shape (`$ cmd`, `(cwd=…, exit=…)`, `--- stdout ---`) is contract.

### Where the upstream built-ins have moved on

Checked against OpenCode at `1f94d8a` (2026-08-12). The paging protocol still
matches byte-for-byte — same 2000-line, 2000-char and 50 KB caps, same three
footer variants. Four things upstream now does that these tools do not:

| Upstream `read` | Here |
|---|---|
| Wraps output in `<path>` / `<type>` / `<content>` | Bare numbered lines plus the footer |
| Lists a directory when the path is one | Not supported |
| Returns images and PDFs as attachments | Text only |
| Rejects binaries; suggests near-miss filenames on a miss | Neither |

Also: upstream's `grep` and `glob` caps are now 100, against 200 and 500 here,
and its empty result reads `No files found` against `(no matches)` here. Those
are divergences to decide about, not bugs — but they should be decided, not
inherited by accident.

## Ported wording changes

The port is byte-faithful to its origin except for two strings, both deliberate,
both visible as the only diff against the recorded baseline:

1. **`bash` description** dropped a clause naming a tenant-specific mount. It is
   restorable exactly via `SBX_WORKSPACE_NOTE` — `workspace-note.test.ts` pins
   that the original wording comes back byte-for-byte.
2. **`grep`'s `path` hint** dropped the same clause and is **not** restorable.
   The judgement is that a parameter hint repeating what `bash`'s description
   already says is redundant; if that is wrong, `grep` needs its own note.

Any deployment migrating off a hand-edited copy should diff its live tool
descriptions against the snapshot before cutting over.

## The tenant classifier

`POST /classify` — "has the user changed the subject, so the UI can offer to split
the conversation" — shipped inside the same FastAPI app as the workspace file API.
It does not belong to this package: it answers a product question, it needs a model
provider and an observability endpoint that nothing else here needs, and its system
prompt is written for one domain, so it is not reusable even in principle.

It now lives in `_tenant_classifier.py` as its own router, **unmounted by default**.
Leaving it mounted would make every deployment of the file API — including ones with
no model credentials at all — expose a route that tries to use them.

A deployment that still calls it sets `HANDS_ENABLE_TENANT_CLASSIFIER=1`. That is a
migration path, not a supported configuration: the route should move to whichever
service owns the conversation, and the flag should then go.

## Known gaps

Recorded rather than hidden, because each one is a thing a reader would otherwise
assume is done.

- **No Python packaging yet** (`pyproject.toml`), and the daemon's module docstrings
  still describe the deployment it came from.
- **The generation counter is per-process.** It labels metadata and logs and
  nothing decides from it, but a restart makes it start over, so two sandboxes of
  one session can carry the same `hands.generation`.

## The new-sandbox notice

Sandboxes are provisioned on a session's first tool call and reclaimed when idle,
so a tool call arriving at a fresh sandbox means one of two opposite things: the
session never had one, or the session's previous one is gone along with every file
it held.

**The notice is announced by default and suppressed only on proof.** Any newly
created sandbox prepends one leading line to its first tool result, unless the
ledger can show this is the session's first sandbox. The risk is asymmetric and
that is what sets the direction: a session that never had a sandbox loses one
sentence to a notice it did not need, while a session whose sandbox was replaced
and is not told spends the rest of the conversation reasoning about files that are
not there.

`session_ledger.py` answers in three states, and only the first stays quiet:

| Answer | Meaning | Notice |
|---|---|---|
| `True` | This is the session's first sandbox | suppressed |
| `False` | The session had one before | announced |
| `None` | No durable storage — cannot tell | announced |

Set `HANDS_STATE_DIR` to storage whose lifetime is at least the conversation's; in
a co-located deployment that is the volume the gateway keeps thread state on, which
gives the right property for free — if the volume is gone the conversations are gone
with it, so there is no one left to notify. Leave it unset and every new sandbox is
announced, which is the correct default for a deployment that has not thought about
this yet.

Two things this deliberately does **not** use as the signal:

- **How many turns the conversation has had.** A session can talk for a long time
  without ever calling a tool, so "this thread has history" and "this session has
  had a sandbox" are different facts. Substituting one for the other reports loss
  to conversations that never had anything to lose.
- **The in-process session map.** That is what made the old behaviour fail
  silently: a restarted daemon had never seen the session, treated it as brand new,
  and said nothing.

The wording has to hold in both cases, so it states what happened and what follows
from it without asserting a cause it cannot know — idle reclaim, a restart and a
pool move all arrive here identically — and without claiming files were lost when
there may never have been any. A test pins that.

### Re-attaching instead of rebuilding

When this process has no memory of a session — after a restart, or on a replica
that has never served it — it first checks whether the sandbox the ledger recorded
is still there, and adopts it if so. Announcing a loss accurately is worth less
than not causing one: a rollout used to rebuild every in-flight conversation's
sandbox and discard working state that was never in danger.

Three conditions have to hold, and each rules out a different way of adopting the
wrong thing:

1. **The ledger has an id to try.** No record, no attempt.
2. **The attach succeeds *and* the sandbox answers a liveness probe.** The attach
   alone is not sufficient even though it 404s a reclaimed sandbox now, because
   that is one endpoint's behaviour and this is the invariant the whole design
   rests on.
3. **The marker inside it names this session.** An id can be reused and a sandbox
   can be replaced underneath a stale record; a filesystem that is not the one the
   conversation left is the failure this cannot be allowed to cause. An *unmarked*
   sandbox is also refused — it may be innocent, but the cost of being wrong here
   is one rebuilt sandbox against a conversation silently operating on the wrong
   files.

Any of the three failing falls through to building a new sandbox and announcing
it, which is the behaviour that existed before — so the worst case is the old case.

`session_marker.py` writes `/tmp/.agentbox-hands-session.json`. Under `/tmp`
deliberately: nothing carries it across a rebuild, and a marker that survived one
would be worse than no marker at all.

**The re-attach always sends an explicit timeout.** The API applies a connect's
timeout as `expiry = now + timeout` and the SDK supplies a default of its own when
the caller omits one, so a bare `connect()` shortens a long-lived sandbox to that
default. Re-attaching is supposed to be transparent; cutting an hour-long sandbox
to five minutes because we asked to look at it is not. A test pins the explicit
argument.

Every sandbox is also stamped with `hands.session`, `hands.generation` and the
tenant labels, so it can be traced to its conversation from a sandbox listing
without asking this process. And each classification increments a counter
(`first` / `replaced` / `unknown` / `reattached`): the rate of `replaced` reads
directly as how many conversations are losing their files, and `unknown` as how
many are being told only because no ledger is configured. The original bug survived
in production because nothing counted it.
