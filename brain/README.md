# Brain

The harness-independent conversation layer above `sdk/hands`: threads, the AG-UI
event stream, turn management, HITL round-trips, attachment staging, and one backend
per harness.

**Status: the image builds and runs, and carries nothing from the deployment this
was imported from.** What is not done is the console UI that would let a user talk
to a deployed agent from the platform — see [What is left](#what-is-left).

| | |
|---|---|
| `gateway/` | The conversation layer. `bun run typecheck` clean, `bun run test` 129 passing |
| `gateway/workspace.ts` | The per-user workspace root, one fixed path |
| `gateway/agent-events.ts` | Agent event shapes and the compaction marker |
| `entrypoint.sh` | Process contract: daemon, file API, harness runtime, gateway — dies if any dies |
| `Dockerfile` | The image. Build context is the REPOSITORY ROOT, not this directory |
| `image/opencode-tools.sh` | Installs the seven OpenCode tool overrides and verifies each resolves |

The tests are the specification. They pin the wire protocol, tool-call assembly,
HITL surviving an HTTP response, and the telemetry span shape.

## Building it

```bash
docker build -f brain/Dockerfile -t agentbox-brain:dev .   # from the repo root
```

The context is the repo root because the image needs `brain/` and `sdk/hands/`
from the same commit — the gateway depends on the hands package through a `file:`
reference, so a build carrying only one of them would resolve against whatever the
other happened to be.

Verified on build: every OpenCode tool override resolves, `node_modules` contains
no link pointing outside itself, and the Python daemon imports. Verified by running
it: all four processes come up, the gateway serves `/healthz` and `/backends`, and
OpenCode's database lands on the state volume rather than the container filesystem.

### Runtime layout

| | |
|---|---|
| `HOME` | `/home/agents`, uid/gid 1000 — matching the `runAsUser`/`fsGroup` the controller renders |
| `/home/agents/state` | The one mounted volume: OpenCode's DB, Claude Code's transcripts, the gateway's thread map |
| `/home/agents/u` | Per-user workspaces. Pre-created and owned by uid 1000, because every process that makes a directory inside it is unprivileged |
| `/home/agents/node_modules` | At HOME, not beside the gateway: module resolution walks UP, and the OpenCode overrides live under `.config/opencode/tool/` |

**All three state owners must resolve under the mounted volume**, and each is
rendered as the environment variable its owner reads rather than left to a default.
`TestRenderPersistedPathsAreUnderTheMountedVolume` checks the containment, not the
constants — asserting each variable equals its own constant passes for any value,
including the arrangement this replaces, where the transcripts and the thread map
were siblings of the mount point. A restart then kept OpenCode's sessions and lost
the map naming them, so every conversation came back empty with the transcripts
intact on disk.

## Done

- **One copy of the sandbox toolset.** The import carried its own `../sandbox/*`;
  those imports now resolve to `@scitix/agentbox-hands`. Worth doing first because
  two copies of that layer drift silently — the same way the two `endAt`
  implementations in the E2B compatibility layer had already drifted apart.
- **No dependency on the origin deployment.** Its framework packages became local
  modules, its tool wiring and chat-notification callback were removed, and its
  names are gone from the source, the tests and the entrypoint.
- **Tenant tools out of the harness backends.** The Claude Code backend stitched
  two of the origin's own MCP servers into every thread's tool set, and carried
  config for two more that nothing read. A hosted agent's tools cannot be compiled
  into a backend shared by every agent the deployment serves.
- **Dead tenant-shaped code removed.** A trace-attribution class that could only
  ever match a CLI this repo does not ship, reachable from nothing but its own
  tests.
- **The image.** OpenCode + Claude Code + the daemon + the file API + the gateway,
  none of the origin's packages.

Three defects surfaced while wiring the image up, each of which fails silently:

1. **The state volume held none of what it was for** (above).
2. **`tool_output` was not platform-owned.** When the harness truncates an
   oversized tool result it writes the full text to a file *on the pod* and hands
   the agent that path — but the agent's `read` runs in the sandbox and cannot open
   it. The caps are now generated and an overlay cannot lower them; an agent
   created from a prompt alone supplies no overlay, so leaving this to the tenant
   meant it was absent in exactly the default case.
3. **The entrypoint resolved state paths without exporting them.** It derived each
   path, prepared the directory, and left the variable unset — so the owner used
   its own default. Caught on the image's first run by the assertion that was
   already there for it.

## What is left

**1. The console UI.** Nothing in the platform yet lets a user talk to a deployed
agent: no thread list, no streaming transcript, no attachment upload, no workspace
file browser. The gateway serves all of it (`/threads`, `/run`, `/interactions`,
and the file API on 8766) and the ws-proxy already routes to an agent, so this is
frontend work against a reachable API rather than anything new in this layer.

**2. `spec.tools` reaching the harnesses.** The tools removed from the backends
belong there, declared per agent and gated per scenario. Today `spec.tools.mcp`
only *closes* tools in the generated `opencode.json` and never opens them, which is
the safe direction but means the field does not yet do what its name implies.

**The per-scenario gate is load-bearing, not cosmetic:** it is the only thing
stopping an interactive user from having the agent post to a chat system on their
behalf. A re-wiring that grants tools globally is a regression that nothing would
fail on. The gateway has no scenario concept at all today, so this is a real
feature rather than a rename.

**3. OpenCode's file/shell confinement is unverified.** Measured against 1.18.16, a
same-named override appears to register *alongside* the built-in rather than
replacing it. See `sdk/hands/README.md` for the measurements; `image/opencode-tools.sh`
repeats the summary where someone changing it will read it. If the built-in wins,
an agent's shell runs on the Brain pod. This is why the image's default harness is
Claude Code, whose confinement rests on a different mechanism that
`sandbox-escape.test.ts` drives a real session to check.

`gateway/user-dir.test.ts` carries a skipped case for the image's obligation to
pre-create the workspace root owned by the runtime user. The image now does it, but
the test cannot see inside an image; the case stays as the written-down requirement.
