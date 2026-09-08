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

// "Use AgentBox from your own tools" — the walkthrough behind the floating
// button on the assistant page.
//
// Built from the LIVE cluster config rather than shipped as a static file with
// placeholders to fill in. The two addresses below are the single thing that
// makes a copied snippet work or not, they differ per cluster, and a reader who
// has to go and find them is a reader who gives up — so the document that gets
// copied already carries this deployment's own.
//
// It is a TypeScript module rather than a Markdown file because Next has no
// `?raw` import; a build-time loader for one document is a configuration
// everyone downstream would have to keep.

import type { Locale } from "@/lib/i18n/config"

export interface McpGuideInput {
  /** The cluster's E2B-compatible API base URL, from the cluster config. */
  e2bURL?: string
  /** The data-plane gateway, minus its scheme — what `E2B_DOMAIN` expects. */
  dataURL?: string
  /** Pool or Env name to use in the example, when one is known. */
  poolName?: string
}

/** `https://host/path` → `host/path`. The E2B SDK's domain carries no scheme. */
function stripScheme(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/+$/, "")
}

/** True when the deployment's data plane is plain HTTP, which the SDK must be
 *  told explicitly — a mismatch here surfaces as the SDK never connecting, with
 *  an error that never mentions the scheme. */
function isPlainHttp(url: string): boolean {
  return url.startsWith("http://")
}

export function mcpGuide(input: McpGuideInput, locale: Locale): string {
  const e2b = input.e2bURL?.replace(/\/+$/, "") || "https://<e2b-api>"
  const domain = input.dataURL ? stripScheme(input.dataURL) : "<data-plane>"
  const https = input.dataURL ? !isPlainHttp(input.dataURL) : true
  const pool = input.poolName || "YOUR_POOL"

  return locale === "en" ? english(e2b, domain, https, pool) : chinese(e2b, domain, https, pool)
}

function english(e2b: string, domain: string, https: boolean, pool: string): string {
  return `Everything the assistant does on this page, you can do from your own machine. The sandboxes are the same sandboxes, on the same pools, against your own quota.

## 1. Get an API key

Open **API Keys** in the sidebar and create one. It is shown once; the platform keeps a copy you can recover from that page.

\`\`\`bash
export E2B_API_KEY=agbx_...
\`\`\`

## 2. Install the SDK

AgentBox speaks the E2B API, so the official E2B SDK works unchanged — one patch call points it at this deployment instead of e2b.dev.

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

## 3. Point it at this cluster

\`\`\`python
import os
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${e2b}",
    domain="${domain}",${https ? "" : `\n    https=False,`}
)

# patch_e2b() must run BEFORE this import, or the SDK talks to e2b.dev.
from e2b import Sandbox

sbx = Sandbox.create("${pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

The first argument is a **pool or environment name**, not an image. Prefix it with a cluster id to reach another cluster: \`Sandbox.create("other-cluster::${pool}")\`.

## 4. Drive it from your coding agent

Hand this document to Claude Code, Codex or any agent that can run Python, and it has everything it needs: the addresses are above, the key is in your environment, and the SDK is the one it already knows. Ask it to start a sandbox and run something in it.

## What the key can see

An API key acts as **you** — your team, your namespace, your quota. It cannot list or reach another user's sandboxes. Delete it on the API Keys page and it stops working everywhere, including in any agent you handed it to.`
}

function chinese(e2b: string, domain: string, https: boolean, pool: string): string {
  return `这个页面上助手做的事，你在自己电脑上也能做。开出来的是同一批沙箱、同一批池子，算你自己的配额。

## 1. 拿一把 API Key

在左侧「API 密钥」里创建。明文只在创建时展示一次，平台也留了一份，可以从那个页面取回。

\`\`\`bash
export E2B_API_KEY=agbx_...
\`\`\`

## 2. 装 SDK

AgentBox 讲的是 E2B 的 API，所以官方 E2B SDK 原样可用 —— 只要一行 patch 把它指到这个部署，而不是 e2b.dev。

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

## 3. 指向这个集群

\`\`\`python
import os
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${e2b}",
    domain="${domain}",${https ? "" : `\n    https=False,`}
)

# patch_e2b() 必须在这一行 import 之前跑，否则 SDK 连的是 e2b.dev。
from e2b import Sandbox

sbx = Sandbox.create("${pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

第一个参数是**池子或环境的名字**，不是镜像。想去别的集群就加前缀：\`Sandbox.create("other-cluster::${pool}")\`。

## 4. 交给你的编码 agent

把这篇文档丢给 Claude Code、Codex 或任何能跑 Python 的 agent，它需要的东西就齐了：地址在上面，key 在环境变量里，SDK 是它本来就熟的那个。让它开个沙箱跑点东西试试。

## 这把 key 能看到什么

API Key 以**你的身份**行动 —— 你的团队、你的命名空间、你的配额，看不到也碰不到别人的沙箱。在「API 密钥」页面删掉它，它在所有地方立刻失效，包括你交出去的那个 agent 里。`
}
