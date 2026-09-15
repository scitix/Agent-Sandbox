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
// Built from the LIVE deployment rather than shipped as a static file with
// placeholders to fill in. The addresses are the single thing that makes a
// copied snippet work or not, they differ per deployment, and a reader who has
// to go and find them is a reader who gives up — so the document that gets
// copied already carries this one's own.
//
// It is also why the addresses are NOT in the repository: this is public
// source, and one organisation runs several platforms from it. A URL written
// here would publish an internal hostname and point every other deployment's
// users at the wrong one.
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
  /** This console's own origin + basePath, e.g. read off `window.location`. */
  consoleBase?: string
}

/**
 * A short, stable name for this deployment, derived from its own hostname.
 *
 * `console.example.com` → `example`. Derived rather than configured because a
 * context name only has to be memorable and distinct on the reader's machine,
 * and asking every deployment to invent one is a setting that would go unset.
 */
function contextName(consoleBase: string): string {
  try {
    const host = new URL(consoleBase).hostname
    const parts = host.split('.').filter(p => p && p !== 'www')
    // The most specific label that is not the generic front-door word.
    const named = parts.filter(p => !['console', 'app', 'portal'].includes(p))
    return (named[0] || parts[0] || 'agentbox').toLowerCase()
  } catch {
    return 'agentbox'
  }
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

export interface GuideVars {
  e2b: string
  domain: string
  https: boolean
  pool: string
  /** `abx --endpoint` for this deployment: this console's own address. */
  endpoint: string
  /** Name to save the context under. */
  ctx: string
}

export function mcpGuide(input: McpGuideInput, locale: Locale): string {
  const consoleBase = (input.consoleBase || "").replace(/\/+$/, "")
  const v: GuideVars = {
    e2b: input.e2bURL?.replace(/\/+$/, "") || "https://<e2b-api>",
    domain: input.dataURL ? stripScheme(input.dataURL) : "<data-plane>",
    https: input.dataURL ? !isPlainHttp(input.dataURL) : true,
    pool: input.poolName || "YOUR_ENV",
    // This console's own address, and the only one a reader has to supply: one
    // address reaches every cluster, so `--cluster` on a command selects among
    // them rather than being a claim the address cannot honour. Which header
    // the key travels in follows from it too.
    endpoint: consoleBase || "https://<console>/agentbox",
    ctx: contextName(consoleBase),
  }
  return locale === "en" ? english(v) : chinese(v)
}

function english(v: GuideVars): string {
  return `Everything the assistant does on this page, you can do from your own machine. Two tools, and the split between them is the product:

| | |
|---|---|
| **\`abx\`** | the platform — environments, warm pools, autoscaling, quotas, templates |
| **E2B SDK** | the sandboxes themselves — create, exec, files, network |

## 1. Get an API key

Open **API Keys** in the sidebar and create one. It is shown once; the platform keeps a copy you can recover from that page.

## 2. Install \`abx\`

One binary, nothing behind it — no Node, no virtualenv.

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

## 3. Point it at this deployment

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key agbx_...

abx envs
\`\`\`

That is the whole setup — this console's address and the key. One address reaches every cluster this platform has: \`abx clusters\` lists them and any command takes \`--cluster <id>\`, and with a single cluster it is filled in for you. Which header the key travels in follows from the address, so there is nothing else to say.

If you use more than one AgentBox platform, add each as its own context and switch with \`abx context use <name>\`. A context is which platform; \`--cluster\` is which of its clusters, per command.

\`\`\`bash
abx agent-context     # the whole CLI as JSON — hand this to an agent
abx envs ${v.pool} pools
\`\`\`

## 4. Give it to your coding agent

**Claude Code** — the plugin puts your key in the OS keychain, where the agent never sees it:

\`\`\`
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

Then set the endpoint and key in the plugin's settings; a hook writes them to \`~/.config/abx/config.json\` for the CLI to read.

**Codex, or anything else that runs shell** — the install above already placed the binary on your PATH and nine skills in \`~/.agents/skills\`. Point your agent at that directory and it has the platform's vocabulary: rollouts, capacity, evaluations, Docker-in-sandbox, egress isolation, vault secrets.

## 5. Run something in a sandbox

Sandboxes are E2B's surface, so the official SDK works unchanged — one patch call points it here instead of e2b.dev.

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

\`\`\`python
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${v.e2b}",
    domain="${v.domain}",${v.https ? "" : `\n    https=False,`}
)

# patch_e2b() must run BEFORE this import, or the SDK talks to e2b.dev.
from e2b import Sandbox

sbx = Sandbox.create("${v.pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

The first argument is an **environment name**, not an image. Prefix it with a cluster id to reach another cluster: \`Sandbox.create("other-cluster::${v.pool}")\`.

## What the key can see

An API key acts as **you** — your team, your namespace, your quota. It cannot list or reach another user's sandboxes. Delete it on the API Keys page and it stops working everywhere, including in any agent you handed it to.

If you hand a key to an unattended agent, issue it in **agent mode**: its writes wait for you to approve them in the console, it cannot issue itself a new key, and it never sees key material — not in \`abx api-keys\`, and not in a rendered setup snippet.`
}

function chinese(v: GuideVars): string {
  return `这个页面上助手做的事，你在自己电脑上也能做。两个工具，它们之间的分界线就是产品本身：

| | |
|---|---|
| **\`abx\`** | 平台 —— 环境、预热池、自动扩缩容、配额、模板 |
| **E2B SDK** | 沙箱本身 —— 创建、执行、文件、网络 |

## 1. 拿一把 API Key

在左侧「API 密钥」里创建。明文只在创建时展示一次，平台也留了一份，可以从那个页面取回。

## 2. 装 \`abx\`

一个二进制，后面什么都不需要 —— 不用 Node，不用虚拟环境。

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

## 3. 指向这个部署

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key agbx_...

abx envs
\`\`\`

就这么两样 —— 控制台地址和 key。一个地址通向平台的所有集群：\`abx clusters\` 列出它们，任意命令加 \`--cluster <id>\` 即可；只有一个集群时会自动填上。key 走哪个 header 也是从地址推出来的，不用另外配。

如果你同时用多个 AgentBox 平台，各加一个 context，用 \`abx context use <name>\` 切换。context 决定是哪个平台，\`--cluster\` 决定是这个平台的哪个集群（逐命令指定）。

\`\`\`bash
abx agent-context     # 整个 CLI 的 JSON 描述 —— 直接丢给 agent
abx envs ${v.pool} pools
\`\`\`

## 4. 交给你的编码 agent

**Claude Code** —— 插件把 key 存进系统钥匙串，agent 全程看不到它：

\`\`\`
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

然后在插件设置里填 endpoint 和 key，由 hook 写进 \`~/.config/abx/config.json\` 供 CLI 读取。

**Codex 或任何能跑 shell 的工具** —— 上面那条安装命令已经把二进制放进 PATH，并把九个 skill 装进 \`~/.agents/skills\`。把 agent 指到那个目录，它就有了这个平台的词汇：打流、容量、评测、沙箱内 Docker、断网隔离、保管库密钥。

## 5. 在沙箱里跑点东西

沙箱是 E2B 的地盘，官方 SDK 原样可用 —— 一行 patch 把它指到这里，而不是 e2b.dev。

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

\`\`\`python
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${v.e2b}",
    domain="${v.domain}",${v.https ? "" : `\n    https=False,`}
)

# patch_e2b() 必须在这一行 import 之前跑，否则 SDK 连的是 e2b.dev。
from e2b import Sandbox

sbx = Sandbox.create("${v.pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

第一个参数是**环境的名字**，不是镜像。想去别的集群就加前缀：\`Sandbox.create("other-cluster::${v.pool}")\`。

## 这把 key 能看到什么

API Key 以**你的身份**行动 —— 你的团队、你的命名空间、你的配额，看不到也碰不到别人的沙箱。在「API 密钥」页面删掉它，它在所有地方立刻失效，包括你交出去的那个 agent 里。

如果要把 key 交给一个无人值守的 agent，签发时选 **agent 模式**：它的写操作要你在控制台点一下才放行，它不能给自己签新 key，也读不到任何 key 的明文 —— \`abx api-keys\` 里没有，渲染出来的接入文档里也没有。`
}
