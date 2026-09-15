---
name: abx-common
description: How to reach an AgentBox platform with `abx` — endpoint and key, choosing a cluster, machine-readable output, and what happens to a write that needs someone's approval. Use when any abx command fails on authentication, addresses the wrong cluster, or comes back asking for approval. Every other abx skill defers here for those four things.
---

# abx: the shared half

`abx` addresses the **platform**: the environments, warm pools and autoscaling
groups that sandboxes are claimed from. The sandboxes themselves — creating
one, running commands in it, reading its filesystem — are the **E2B SDK's**
job, and that line is the product, not an omission. If the task is "run
something in a sandbox", reach for E2B. If it is "there is nowhere to run it
yet", or "it will not scale", you are in the right place.

## Do this first

```bash
abx agent-context        # the whole tool as one JSON document
```

Every resource, every filter, every column, every write, and the grammar that
assembles them. It is generated from the same registry the CLI dispatches on,
so it cannot describe a command that does not exist. Reading it once costs less
than three `--help` calls and answers more.

## The grammar

```
abx <resource>                        list
abx <resource> <id>                   get
abx <resource> <id> <sub>             sub-list
abx <resource> <id> <sub> <sub-id>    sub-get
abx <path…> apply -f FILE             write the desired state
abx <path…> delete
abx <path…> scale --replicas N
```

The address is the same for reading and writing — having just listed
something, append a word to act on it. It also matches the console URL segment
for segment, so `/clusters/c/envs/e/pools/p` and
`abx envs e pools p --cluster c` are the same thing said twice.

## Authentication

Two settings are required, resolved as flag → environment →
`~/.config/abx/config.json`:

| Setting | Flag | Environment |
|---|---|---|
| Console address | `--endpoint` | `AGENTBOX_ENDPOINT` |
| Credential | `--api-key` | `AGENTBOX_API_KEY` |

That is the whole setup. The endpoint is the console's own address — the one a
person types in a browser — and **one address reaches every cluster** the
platform has.

Which header the key travels in follows from that, so there is no scheme to
set: the console takes `Authorization: Bearer`, a cluster API takes
`AGENTBOX-API-KEY`. `--auth-scheme` is an override for a deployment answering to
neither, and is otherwise unnecessary. Console links in output are derived from
the endpoint too.

**The exception**: a sandbox with no network route to the console is configured
with `--cluster-api` (`AGENTBOX_CLUSTER_API`) pointing at one cluster's own API
instead. Everything below about choosing a cluster then does not apply — that
address answers for one cluster and refuses any other. You will not choose this;
whoever deployed the platform did, and it shows up already set in the
environment.

Under the Claude Code plugin the key is in the OS keychain and a hook writes
the config file. **Do not read that file, echo the key, or pass it on a command
line** — it is deliberately kept out of the conversation, and putting it back
in defeats the arrangement.

`abx whoami` answers who the key acts as, and — the part worth checking before
a write — whether it is an `agent` key.

`abx` speaks as one tenant and has no flag to act as another. That is
deliberate: acting as somebody means holding their key, not asking yours to
pretend. Administrative work across tenants belongs in the console.

## Clusters

Management calls are per cluster, and the cluster is chosen **per command** with
`--cluster` (`AGENTBOX_CLUSTER`). It is not part of the context: a context names
a platform, and that platform may have several clusters, so a default would
quietly answer for whichever one happened to be set.

Through a console — the normal case — `--cluster` reaches any cluster the
platform has, and when there is exactly one it is filled in for you.

In the `--cluster-api` exception above, the address answers for its own cluster
and **refuses** a `--cluster` naming a different one. That refusal is
deliberate: returning the local cluster's rows under another cluster's name is
data that is confidently mislabelled, and a reader cannot tell. Treat the
refusal as the truth about that sandbox, not as something to work around — there
is no route to the other cluster from there.

```bash
abx clusters                    # what this deployment can reach
abx envs --cluster <cluster-id> # choose one for this command
```

## Output

Three registers, and the middle one is the default:

- **table** — a header line, a count, a `view:` link to the same page in the
  console where the console has a page for it, and `hint:` lines naming what to
  do next. Truncated at 200 rows with the filters that would narrow it.
- `--json` — the raw API shape. No header, no hints. Use it when piping.
- `--csv` — flat, for a spreadsheet or `cut`.

`--wide` adds the columns held back by default. `--filter key=value` narrows a
list, where **key is a column heading** — the heading, the filter key and the
CSV column are one name on purpose.

You have a shell. Use it: `abx envs <env> pools --json | jq`, loops, aggregation. That is
the point of a CLI over a tool-per-operation surface, and the sandbox you are
probably running in has no real credentials in it anyway — the egress sidecar
substitutes them on the way out.

## Writes, and approval

`apply` is a **PUT of the desired state** — a field the file leaves out is a
field you are asking to **remove**. The file is NOT the object `--json` prints:
it is only the editable part, and the editable part differs per address:

| Address | The file holds |
|---|---|
| `abx envs <env>` | `{"overrides": {…}}` |
| `abx envs <env> pools <pool>` | `{"replicas": n, "minReplicas": n, "maxReplicas": n, "updateStrategy": {…}}` |
| `abx envs <env> scaling-groups <group>` | `{"enabled": bool, "minReplicas": n, "maxReplicas": n, "scaleUpPolicy": {…}, "scaleDownPolicy": {…}}` |
| `abx admin-templates <t>` (admin key) | `{"crdJson": "<the whole SandboxTemplate as a JSON string>"}` |

So the shape of an edit is:

```bash
cat > pool.json <<'JSON'
{"replicas": 2, "minReplicas": 1, "maxReplicas": 8}
JSON
abx envs demo pools demo-1c2gi apply -f pool.json
```

Creating is the same verb against the collection — `abx envs apply -f env.json`
is the POST — so there is one word for "make it look like this" either way.

Piping `--json` into `apply` is not a valid body and is the fastest way to lose
state: those keys are nested (`spec.replicas`), the PUT reads top-level ones, and
the ones it does not find are the ones it clears. Read the current values with
`--json`, then send back the fields named in the table.

`scale --replicas N` is the exception: one field, no clearing, and it re-sends
the current bounds unchanged. Use it when size is all you are changing.

**An `agent` key's writes are held for a person to release.** The command comes
back saying so, with a link. That is not an error to work around: re-run the
command after the person has acted.

**Three writes are refused outright, with no approval to wait for**: issuing an
API key, promoting one, and deciding an approval. An agent that could mint a
credential could mint one without the agent restriction and leave the gate
entirely, and no approval dialog conveys that. The answer is a console link for
the person to act on themselves. Do not queue, retry, or look for a flag.

For the same reason, an agent key gets key **metadata** without key
**material**: `abx api-keys` lists what exists and which keys are gated, and
the token field is absent. An env's rendered docs come back with
`${AGBX_API_KEY}` intact rather than a live token. Relay the template and tell
the person where to get their key; there is nothing missing to hunt for.

The refusal and the wait are different answers and the error codes say which:
`APPROVAL_REQUIRED` means a person is about to decide, so re-run it shortly.
`FORBIDDEN_FOR_AGENT` means no approval exists or ever will — stop, and hand
over the link.

## Errors carry the recovery

A rejection names the valid set and the next command. Read it before
reformulating — it usually contains the answer, and a guess costs a round trip.

## Where to go next

| You want to | Skill |
|---|---|
| Run RL rollouts at scale | `abx-reinforcement-learning` |
| Put sandboxes behind your own agent | `abx-managed-agent` |
| Size pools, fix autoscaling, read quota | `abx-resource-capacity` |
| Run a benchmark suite | `abx-harbor-framework` |
| Run Docker inside a sandbox | `abx-sandbox-docker` |
| Stop a sandbox reaching the internet | `abx-sandbox-network` |
| Give a sandbox a credential it cannot read | `abx-sandbox-secrets` |
| Work out why something is broken or slow | `abx-observe` |
