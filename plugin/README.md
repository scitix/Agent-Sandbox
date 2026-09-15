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
```

The binary lands in `~/.local/bin`, the skills in `~/.agents/skills`. Then name
the deployment you are talking to:

```sh
abx context set <name> \
  --endpoint 'https://<console>/agentbox' \
  --api-key agbx_...
```

Two settings, and that is the whole setup. The endpoint is your console's
address — the one you type in a browser — and **one address reaches every
cluster** that platform has: `abx clusters` lists them, and any command takes
`--cluster <id>`. Which header the key travels in follows from the address, so
it is not a flag either. Each deployment's own console prints that line with its
address filled in — this repository ships none, because the addresses belong to
whoever deployed the platform.

### When there is no console to reach

A sandbox running inside a cluster that has no route to the console is the one
exception. It is told so explicitly:

```sh
abx context set <name> \
  --cluster-api 'http://<cluster-api-host>' \
  --api-key agbx_...
```

That address answers for **one** cluster, so `abx --cluster <other>` is refused
rather than quietly returning the local cluster's rows under another cluster's
name, and console links are omitted because there is no console to link to.
This is opt-in on purpose — a CLI that silently fell back to one cluster would
be a platform shrinking to a fraction of itself with no error to notice.

A platform that embeds `abx` sets whichever applies through the environment
(`AGENTBOX_ENDPOINT`, or `AGENTBOX_CLUSTER_API` for the exception), so the
sandbox reaches that deployment and no other.

## More than one platform

`abx context` works like a kubectl context: one binary, several deployments.

```sh
abx context                 # list, with the current one marked
abx context use <name>      # change the default
abx --context <name> envs   # just this command
```

`--cluster` is the other axis and stays independent: a context is which
platform, `--cluster` is which of its clusters. It belongs on the command, not in
the context — `abx clusters` lists what a console reaches, and when there is
exactly one it is filled in for you. With two contexts configured and no default
chosen, commands are refused rather than guessing — a command that quietly ran
against the wrong platform is the failure this prevents.

Environment variables still win over the file, which is what makes a sandbox
work: the platform passes its own address in the environment and injects the
token on the way out.

## What is in here

| Path | |
|---|---|
| `bin/abx` | shim: picks the platform binary, self-updates from the public bucket, falls back to the bundled copy on any failure |
| `dist/` | compiled binaries, one per platform (populated by `hack/build-plugin.sh`) |
| `hooks/` | writes the keychain-backed config the CLI reads |
| `skills/` | nine skills, all deferring to `abx-common` for endpoint, key, cluster and approval |
| `install.sh` | the non-Claude path |

## The grammar

```
abx <resource> [<id> [<sub> [<sub-id>]]] [<verb>]
```

Reading and writing share one address, and it matches the console URL segment
for segment. `abx agent-context` prints the whole thing as JSON — every
resource, filter, column and write — generated from the same registry the CLI
dispatches on, so it cannot describe a command that does not exist.
