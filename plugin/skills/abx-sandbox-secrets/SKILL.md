---
name: abx-sandbox-secrets
description: Give a sandbox the use of a credential without giving it the credential — the vault and outbound injection. Use when sandbox code must call a third-party API with a token, when someone is about to put a real key in a sandbox's environment variables, or when an injected credential is not arriving. Egress filtering lives in abx-sandbox-network.
---

# Secrets a sandbox can use but cannot read

The mechanism, and the reason it is shaped this way:

```
vault (write-only)  →  operator memory  →  sidecar tmpfs  →  outbound header
```

The value never enters the sandbox. The sandbox holds a **decoy** — a
placeholder that looks like a token — and the egress sidecar substitutes the
real one as the request leaves, for the hosts and headers the rule names.

So an agent that can run arbitrary shell in there still cannot print the
credential, because it is not in there. That is the property worth protecting,
and it is why "just put it in envVars" is the wrong answer even when it works.

## Storing one

The vault is the E2B `/secrets` surface, so the SDK you already have speaks it.
Values are write-only: you can list names and overwrite, never read back.

Both surfaces are reached through the same E2B API, whose address and data-plane
domain are the env's: `abx envs <env> docs` prints them for the cluster the env
lives on. Use the public entry it gives unless the caller is inside the cluster.

```python
sbx_secrets.set("OPENAI_KEY", "sk-…")   # stored; not readable afterwards
```

In the console it is the **Vault** page.

## Using one

Reference it by name on the create call. **A plaintext value in `network.rules`
is refused with 400** — deliberately, because accepting it would put the
credential in the request body, the access log, and the caller's source, which
is the exposure the whole feature exists to remove.

The create call carries the sandbox's environment and the injection rules.
**Look its shape up rather than recalling it**: it belongs to the E2B surface,
so it is in `/opt/agentbox/source/pkg/openapi/e2b/openapi.yaml`, and the
installed SDK will print its own signature. `abx-common` has the general recipe
for finding any shape this way.

Two things about it are not about the field names, and are the part to get
right: the value the sandbox's code reads is a **decoy**, and the real one is
referenced by *name* — the sidecar substitutes it on the way out. A literal
credential in the request is a 400, because it would put the secret in the
request body, the access log and the caller's source.

The code inside runs unmodified: it reads `OPENAI_API_KEY`, sends it, and the
sidecar replaces it. Library code that has never heard of AgentBox works.

## Prerequisites, each of which fails silently

The sandbox runs, the request goes out, and the credential is simply not
substituted. Check in this order:

1. **The env has the gateway on** (`overrides.gateway.enabled`). Without it, a
   create carrying rules is refused — so if you have a running sandbox, this
   one is satisfied.
2. **The host matches the rule exactly.** The rule keys on the host it was
   written for; a redirect elsewhere is not covered.
3. **The port is 80 or 443.** Only those are parsed at layer 7. A rule for a
   service on another port never fires, and nothing says so.
4. **The secret name exists in the vault of the acting user.** A name that
   resolves to nothing leaves the decoy in place, and the upstream returns 401 —
   which reads as a bad key rather than a missing one.

`abx whoami` tells you which identity the vault is being read as; a secret
stored by one user is not visible to another.

## For an agent doing an integration

Writing vault secrets **is** allowed for an agent credential, under the normal
approval gate. This is deliberate and worth knowing: the credential lands in
the acting person's own vault and widens nobody's authority, and finishing an
integration end to end is exactly what people want an agent for. Minting an
AgentBox API key is the thing that is refused outright — different act,
different answer. See `abx-common`.

## Related

- `abx-sandbox-network` — deny everything, then allow the one host this rule
  targets
- `abx-managed-agent` — the same mechanism is how the platform's own assistant
  holds no real credential
- `abx-common` — endpoint, key, cluster, approval
