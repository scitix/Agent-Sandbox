/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Built-in integration guide for the agent Docs tab.
//
// `spec.docs` is the deployment's own guide and always wins. This module is the
// fallback for an agent that carries none: a generic walkthrough of the gateway
// contract, filled in with the agent's own endpoint and scenario names. It is
// prose rather than i18n values because a multi-page Markdown document is
// unreadable once flattened into translation strings.

import type { ManagedAgent } from "@/lib/api/managed-agent-types"

const ENDPOINT_PLACEHOLDER = "http://<agent>.<namespace>.svc.cluster.local:4099"

export function buildFallbackDocs(agent: ManagedAgent): string {
  const endpoint = agent.status?.endpoint || ENDPOINT_PLACEHOLDER
  const scenarios = agent.spec.scenarios ?? []
  const defaultScenario = scenarios.find((s) => s.default)?.name ?? scenarios[0]?.name ?? "default"
  const runtime = agent.spec.runtime?.default ?? "claude-code"
  const scenarioTable = scenarios.length
    ? scenarios
        .map((s) => {
          const pin = s.runtime || "—"
          const tools = s.allow?.length ? s.allow.join(", ") : "sandbox toolset only"
          return `| \`${s.name}\` | ${s.displayName || "—"} | ${pin} | ${tools} |`
        })
        .join("\n")
    : "| — | — | — | — |"

  return `# Integrating \`${agent.name}\`

The agent gateway is reached **from inside the cluster** — it is not published
through an ingress. Everything below is relative to:

\`\`\`
${endpoint}
\`\`\`

A caller outside the cluster needs a port-forward or its own ingress in front of
the gateway Service.

## 1. Open a thread

A thread is the unit of conversation and the unit of sandbox binding. Create one
before the first turn:

\`\`\`bash
curl -sS -X POST ${endpoint}/threads \\
  -H 'Content-Type: application/json' \\
  -d '{"scenario": "${defaultScenario}"}'
\`\`\`

The response carries the \`threadId\` used by every later call. The scenario is
fixed at creation: its environment variables are injected into the sandbox when
the sandbox is created, and a sandbox's environment is immutable for its life —
so a thread cannot move to another scenario afterwards. To switch, open a new
thread.

## 2. Run a turn (AG-UI)

\`\`\`bash
curl -sS -N -X POST ${endpoint}/run \\
  -H 'Content-Type: application/json' \\
  -d '{
    "threadId": "<thread-id>",
    "runId": "<uuid>",
    "messages": [
      {"id": "m1", "role": "user", "content": "list the files in the workspace"}
    ],
    "runtime": "${runtime}"
  }'
\`\`\`

\`/run\` speaks AG-UI and takes **\`messages\`**, an array of
\`{id, role, content}\` — **not** \`input\`. Each message needs its own \`id\`;
\`runId\` identifies this turn and is echoed on every event of the stream. The
response is a stream of AG-UI events (run lifecycle, text deltas, tool calls),
not a single JSON body, so read it incrementally.

\`runtime\` and \`model\` are optional overrides. Omit them and the turn uses the
agent's default harness (\`${runtime}\`) unless the scenario pins one, in which
case the pin wins.

## 3. Scenarios

A scenario is a slice of the same agent — same image, same base prompt,
different persona and different visible tools. Picking one decides four things:

- **Prompt** — the scenario's prompt is *appended* to the agent's base prompt,
  never substituted for it.
- **Tool visibility** — the scenario's allow-list is the set of registered MCP
  servers and client-side tools this scenario may see.
- **Harness pin** — a scenario may pin \`claude-code\` or \`opencode\` so an
  unattended flow cannot be dragged onto an unverified model configuration.
- **Sandbox environment** — the scenario's environment variables are injected
  into the sandbox at creation time.

${
  scenarios.length
    ? `| Scenario | Display name | Harness pin | Allowed tools |
| --- | --- | --- | --- |
${scenarioTable}`
    : "This agent declares no scenarios yet."
}

## 4. Tools

- The **sandbox toolset** (\`bash\`, \`read\`, \`write\`, \`edit\`, \`grep\`,
  \`glob\`, \`apply_patch\`) is on by default in every scenario. Remove
  individual entries with the scenario's disable list.
- **MCP servers and client-side tools are off by default.** Visibility is
  deny-by-default and computed server-side: a tool the scenario does not allow is
  never registered with the harness at all, so the model cannot see it, cannot
  call it, and does not learn it exists.
- Consequently, forgetting to configure a scenario fails towards "the tool is
  invisible" — never towards "the agent can post to a chat group".

## 5. Background work without a browser

\`/run\` is built for an attached client reading the event stream. For unattended
work — scheduled analysis, a webhook handler, a batch job — post to \`/tasks\`
instead and poll the returned task for its result:

\`\`\`bash
curl -sS -X POST ${endpoint}/tasks \\
  -H 'Content-Type: application/json' \\
  -d '{
    "scenario": "${defaultScenario}",
    "messages": [{"id": "m1", "role": "user", "content": "summarise the overnight errors"}]
  }'
\`\`\`

Pin a harness on any scenario used this way, and turn its interactive flag off:
an agent that cannot reach a user must not stop to ask a question nobody will
answer.
`
}
