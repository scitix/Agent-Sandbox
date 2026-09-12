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

// The shortest path from an empty sandbox list to a running sandbox.
//
// Built from the LIVE deployment for the same reason `assistant-ui/mcp-guide.ts`
// is: the addresses are the one thing that makes a copied snippet work or not,
// they differ per deployment, and one organisation runs several platforms from
// this source. A URL written here would publish an internal hostname and point
// every other deployment's users at the wrong one.
//
// It also cannot be E2B's own quickstart, which the console visibly resembles.
// That snippet talks to e2b.dev: without `patch_e2b()` the SDK never reaches
// this platform, and `Sandbox.create("base")` names a template that exists over
// there and nowhere here. Three lines that look almost identical and fail
// completely is worse than no snippet at all.

/** The placeholders shown when the cluster config carries no gateway. */
const E2B_FALLBACK = "https://<e2b-api>"
const DOMAIN_FALLBACK = "<data-plane>"
const ENV_FALLBACK = "YOUR_ENV"

export interface PythonQuickstartInput {
  /** The cluster's E2B-compatible API base URL, from `gateway.e2bURL`. */
  e2bURL?: string
  /** The data-plane gateway, from `gateway.dataURL` — `E2B_DOMAIN` wants it
   *  without a scheme. */
  dataURL?: string
  /** An Env the caller can actually create against. */
  envName?: string
}

export interface PythonQuickstart {
  /** Shell: create a venv and install both packages. */
  install: string
  /** Python: point the SDK here, then create a sandbox. */
  code: string
  /** The cluster publishes no E2B gateway, so the snippet cannot name one. */
  missingGateway: boolean
  /** The caller has no Env, so `Sandbox.create` names a placeholder. */
  missingEnv: boolean
}

/** `https://host/path` → `host/path`. The E2B SDK's domain carries no scheme. */
function stripScheme(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/+$/, "")
}

export function buildPythonQuickstart(input: PythonQuickstartInput): PythonQuickstart {
  const e2b = input.e2bURL?.replace(/\/+$/, "") || E2B_FALLBACK
  const domain = input.dataURL ? stripScheme(input.dataURL) : DOMAIN_FALLBACK
  const env = input.envName || ENV_FALLBACK

  // A plain-http data plane has to be declared explicitly. The SDK's failure
  // mode when it is not is that it simply never connects, with an error that
  // never mentions the scheme.
  const httpsLine = input.dataURL && input.dataURL.startsWith("http://") ? "\n    https=False," : ""

  return {
    install: ["uv venv && source .venv/bin/activate", "uv pip install e2b agent-sandbox-e2b"].join(
      "\n",
    ),
    code: [
      `from agent_sandbox_e2b import patch_e2b`,
      ``,
      `patch_e2b(`,
      `    api_url="${e2b}",`,
      `    domain="${domain}",${httpsLine}`,
      `)`,
      ``,
      `# patch_e2b() must run BEFORE this import, or the SDK talks to e2b.dev.`,
      `from e2b import Sandbox`,
      ``,
      `sbx = Sandbox.create("${env}", timeout=3600, secure=False)`,
      `print(sbx.commands.run("echo hello from AgentBox").stdout)`,
      `sbx.kill()`,
    ].join("\n"),
    // Reported apart rather than as one "incomplete" flag, because the two have
    // different fixes and the reader can only act on the one that is true:
    // a missing Env is theirs to create, a missing gateway is the deployment's.
    missingGateway: !input.e2bURL || !input.dataURL,
    missingEnv: !input.envName,
  }
}
