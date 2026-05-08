# E2B JavaScript SDK Patch for AgentBox

Monkey-patches the [E2B JavaScript SDK](https://github.com/e2b-dev/E2B/tree/main/packages/js-sdk) to redirect control-plane API calls and data-plane sandbox connections to an AgentBox deployment.

**Tested against: e2b `2.19.0`**

---

## How It Works

The E2B JS SDK constructs sandbox connection URLs using `ConnectionConfig.getHost()`:

```
// Default E2B (subdomain routing — incompatible with AgentBox)
${port}-${sandboxId}.${domain}

// AgentBox (path-based routing via Envoy ExtProc gateway)
${gateway}/sandboxes/${sandboxId}/${port}
```

`patchE2b()` overrides `ConnectionConfig.prototype.getHost` and `getSandboxUrl` to use the AgentBox path format, and sets `E2B_API_URL` / `E2B_DOMAIN` so the SDK's static getters point at the correct endpoints.

---

## Installation

```bash
npm install e2b
# Copy patch_e2b.js into your project
```

---

## Usage

### ESM (recommended)

```js
// IMPORTANT: patch BEFORE importing Sandbox
import { patchE2b } from './patch_e2b.js';
await patchE2b();

import { Sandbox } from 'e2b';

const sandbox = await Sandbox.create('my-pool//docker.io/library/node:trixie-slim', {
  timeoutMs: 60000,
});

// Poll until ready (envd takes a few seconds to start)
let running = false;
while (!running) {
  running = await sandbox.isRunning();
  if (!running) await new Promise(r => setTimeout(r, 2000));
}

const result = await sandbox.commands.run('node --version');
console.log(result.stdout); // v25.9.0

await sandbox.kill();
```

### CJS

```js
const { patchE2b } = require('./patch_e2b.js');
await patchE2b();

const { Sandbox } = require('e2b');
// ... same as above
```

### Custom addresses

```js
await patchE2b({
  https:  false,
  domain: 'agentbox-data-plane.agentbox-system.svc.cluster.local',
  apiUrl: 'http://agentbox-e2b-api.agentbox-system.svc.cluster.local',
});
```

### Environment variables

```bash
export E2B_API_KEY=agbx_your_key
export E2B_DOMAIN=agentbox-data-plane.agentbox-system.svc.cluster.local
export E2B_API_URL=http://agentbox-e2b-api.agentbox-system.svc.cluster.local
```

```js
patchE2b(); // reads from env, no arguments needed
```

---

## In-Cluster Defaults

| Variable | Default |
|---|---|
| `E2B_DOMAIN` | `agentbox-data-plane.agentbox-system.svc.cluster.local` |
| `E2B_API_URL` | `http://agentbox-e2b-api.agentbox-system.svc.cluster.local` |
| HTTPS | `false` |

---

## Python equivalent

See [`sdk/python/e2b/agentbox/patch_e2b.py`](../../python/e2b/agentbox/patch_e2b.py) for the Python version.
