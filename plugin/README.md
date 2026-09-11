# abx — the AgentBox plugin

Gives an agent the platform half of AgentBox: the environments, warm pools and
autoscaling groups that sandboxes are claimed from, plus quotas, templates,
instance types and the approval queue.

The sandboxes themselves are driven with the **E2B SDK**. That line is the
product, not a gap — `abx` speaks platform concepts, and a sandbox speaks E2B.

## Install — Claude Code

```
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
```

Then set the endpoint and key in the plugin's settings. The key goes to your OS
keychain and is written to `~/.config/abx/config.json` by a hook — **it never
enters the conversation**, because only hooks receive plugin options.

## Install — any other agent

```sh
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
export AGENTBOX_ENDPOINT=...
export AGENTBOX_API_KEY=...
```

The binary lands in `~/.local/bin`, the skills in `~/.agents/skills`. There is
no keychain in this path: the key is an environment variable, so treat it as
one.

## What is in here

| Path | |
|---|---|
| `bin/abx` | shim: picks the platform binary, self-updates from the public bucket, falls back to the bundled copy on any failure |
| `dist/` | compiled binaries, one per platform (populated by `hack/build-plugin.sh`) |
| `hooks/` | writes the keychain-backed config the CLI reads |
| `skills/` | six skills, all deferring to `abx-common` for endpoint, key, cluster and approval |
| `install.sh` | the non-Claude path |

## The grammar

```
abx <resource> [<id> [<sub> [<sub-id>]]] [<verb>]
```

Reading and writing share one address, and it matches the console URL segment
for segment. `abx agent-context` prints the whole thing as JSON — every
resource, filter, column and write — generated from the same registry the CLI
dispatches on, so it cannot describe a command that does not exist.
