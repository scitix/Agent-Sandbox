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

// "Agent Sandbox CLI Guide" — the document behind the button on the assistant
// page, and the same text the static sites carry.
//
// It is written for two readers at once, which is why it reads the way it does:
// a person following it in a shell, and a coding agent that was handed the
// whole thing. That is also why it is generated from the live deployment rather
// than shipped as a file with blanks in it — the addresses are the one thing
// that makes a copied snippet work or not, they differ per deployment, and a
// reader who has to go and find them is a reader who gives up.
//
// Which is why the addresses are NOT in the repository: this is public source,
// and one organisation runs several platforms from it. A URL written here would
// publish an internal hostname and point every other deployment's users at the
// wrong one.
//
// The API key is the exception in the other direction: `${AGBX_API_KEY}` is
// written as a placeholder and left that way here, for the console to fill in
// from the reader's own keys (see use-docs-api-key). A document that arrives
// with a live credential baked in cannot be copied into a chat, a wiki or an
// agent's context without leaking it, and this document is meant to be copied
// into all three.

import type { Locale } from "@/lib/i18n/config"

export interface CliGuideInput {
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
    const parts = host.split(".").filter((p) => p && p !== "www")
    // The most specific label that is not the generic front-door word.
    const named = parts.filter((p) => !["console", "app", "portal"].includes(p))
    return (named[0] || parts[0] || "agentbox").toLowerCase()
  } catch {
    return "agentbox"
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

export function cliGuide(input: CliGuideInput, locale: Locale): string {
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
  return `# Agent Sandbox CLI Guide

The assistant on this page drives AgentBox through MCP tools. Everything it does, you can do from your own machine, and so can a coding agent you hand this document to. Two tools, and the split between them is the product:

| Tool | Covers |
|---|---|
| \`abx\` | the platform — environments, warm pools, autoscaling, quotas, templates |
| E2B SDK | the sandboxes themselves — create, exec, files, network |

This document is written to be read by a person and executed by an agent: copy it whole, and the only thing left to fill in is \`\${AGBX_API_KEY}\`. The console address, the cluster id and the E2B endpoints below are this deployment's real values.

## 1. Get an API key

Open **API Keys** in the console sidebar and create one. The plaintext is shown once at creation; the platform keeps a copy you can retrieve from that page at any time.

If the key is going to an unattended agent, issue it in **agent** mode — see section 6.

## 2. Install \`abx\`

\`abx\` is a single binary with nothing behind it: no Node.js, no virtualenv.

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

The script installs the binary to \`~/.local/bin/abx\` (and tells you if that directory is not on your PATH) and writes nine skills to \`~/.agents/skills\` — see section 4.

## 3. Point it at this deployment

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key \${AGBX_API_KEY}

abx envs
\`\`\`

That is the entire configuration: the console address and the key.

- The **address** reaches the platform, not one cluster: \`abx clusters\` lists every cluster it has, and any command takes \`--cluster <id>\`; a single-cluster platform fills it in for you.
- The **auth header** follows from the address, so there is nothing else to configure.
- To use several AgentBox platforms, save each as its own context and switch with \`abx context use <name>\`. The context is which platform; \`--cluster\` is which of its clusters, per command.

\`\`\`bash
abx agent-context     # the whole CLI as JSON — hand this to an agent
abx envs ${v.pool} pools
\`\`\`

In CI or other unattended settings, \`AGENTBOX_ENDPOINT\` and \`AGENTBOX_API_KEY\` stand in for a context.

## 4. Connect a coding agent

**Claude Code** — the plugin stores your key in the OS keychain, where the agent never sees it:

\`\`\`
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

Then set the endpoint and key in the plugin's settings; a hook writes them to \`~/.config/abx/config.json\` for the CLI to read.

**Codex, or anything else that runs shell** — the install in step 2 already placed the binary on your PATH and nine skills in \`~/.agents/skills\`. Point your agent at that directory and it has the platform's vocabulary: rollouts, capacity, evaluations, Docker-in-sandbox, egress isolation, vault secrets. Hand it an **agent**-mode key (section 6), never an unrestricted one.

## 5. Run code in a sandbox

Every Env carries the documentation its template was written with, rendered for the cluster it belongs to — the E2B API URL, the data-plane domain and the scheme to use:

\`\`\`bash
abx envs ${v.pool} docs
\`\`\`

The same document is on the Env page in the console, with your own key filled in. Read it before driving an Env through E2B; it is the only place that knows which endpoints this cluster answers on.

Sandboxes are E2B's surface, so the official SDK works unchanged — one call before the import points it here instead of e2b.dev.

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

\`\`\`python
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${v.e2b}",
    domain="${v.domain}",${v.https ? "" : "\n    https=False,"}
)

# patch_e2b() must run BEFORE this import, or the SDK talks to e2b.dev.
from e2b import Sandbox

sbx = Sandbox.create("${v.pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

The first argument is an **environment name**, not an image. Prefix it with a cluster id to reach another cluster: \`Sandbox.create("other-cluster::${v.pool}")\`.

## 6. What the key can do

An API key acts as **you** — your team, your namespace, your quota. It cannot list or reach another user's sandboxes. Delete it on the API Keys page and it stops working everywhere, including in any agent you handed it to.

For unattended use, issue the key in **agent** mode:

- the platform writes it makes (create an environment, scale a pool, delete anything) wait for your approval in the console;
- it cannot issue itself another key;
- it cannot read key material — not from \`abx api-keys\`, and not from a rendered setup document, this one included;
- sandbox operations (create, exec, files) behave exactly as in unrestricted mode.

An agent key is the right credential to hand an agent, which is why the guide keeps \`\${AGBX_API_KEY}\` a placeholder rather than printing a live one: what gets copied, pasted and logged is a document, not a secret.`
}

function chinese(v: GuideVars): string {
  return `# Agent Sandbox CLI 指南

本页助手通过 MCP 工具操作 AgentBox。它做的事，你在自己的机器上同样可以做；把本文档交给编码 Agent，它也能做。只需要两个工具，二者的分工就是产品本身的两层：

| 工具 | 负责范围 |
|---|---|
| \`abx\` | 平台：环境（Env）、预热池、自动扩缩容、配额、模板 |
| E2B SDK | 沙箱：创建、命令执行、文件读写、网络 |

本文档既给人读，也给 Agent 执行：整段复制即可，唯一需要替换的是 \`\${AGBX_API_KEY}\`。文中的控制台地址、集群 ID 与 E2B 端点都是本部署的真实值。

## 1. 获取 API Key

在控制台左侧「API 密钥」页面创建一把 Key。明文仅在创建时展示一次，平台保留一份副本，可随时从该页面取回。

若要交给无人值守的 Agent 使用，请在创建时选择 **Agent** 模式，详见第 6 节。

## 2. 安装 abx

\`abx\` 是单个二进制文件，除自身外没有其他依赖：不需要 Node.js，也不需要虚拟环境。

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

脚本把二进制安装到 \`~/.local/bin/abx\`（该目录不在 PATH 时会给出提示），并把九个 skill 写入 \`~/.agents/skills\`，用途见第 4 节。

## 3. 指向本部署

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key \${AGBX_API_KEY}

abx envs
\`\`\`

配置只有两项：控制台地址与 API Key。

- **地址**指向平台，而不是某个集群：\`abx clusters\` 列出该平台的全部集群，任意命令加 \`--cluster <id>\` 即可指定；平台只有一个集群时会自动补全。
- **认证 header** 由地址推导，无需另行配置。
- 需要同时使用多个 AgentBox 平台时，每个平台保存为一个 context，用 \`abx context use <name>\` 切换。context 决定平台，\`--cluster\` 决定该平台下的集群，按命令指定。

\`\`\`bash
abx agent-context     # 以 JSON 输出整个 CLI 的能力，可直接交给 Agent
abx envs ${v.pool} pools
\`\`\`

在 CI 或无人值守环境中，可以用 \`AGENTBOX_ENDPOINT\` 与 \`AGENTBOX_API_KEY\` 环境变量代替 context。

## 4. 接入编码 Agent

**Claude Code** —— 插件把 Key 存入系统钥匙串，Agent 全程接触不到明文：

\`\`\`
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

随后在插件设置中填入 endpoint 与 Key；hook 会将其写入 \`~/.config/abx/config.json\` 供 CLI 读取。

**Codex 或其他可执行 shell 的工具** —— 第 2 步的安装脚本已把二进制放入 PATH，并把九个 skill 装到 \`~/.agents/skills\`。把 Agent 指向该目录，它即获得这个平台的词汇：rollout、容量、评测、沙箱内 Docker、出网隔离、保管库密钥。交给它一把 **Agent** 模式的 Key（第 6 节），不要给不受限的。

## 5. 在沙箱中运行代码

每个 Env 都带着所属模板的接入文档，并按它所在的集群渲染好 —— E2B API 地址、数据面域名、以及该用 http 还是 https：

\`\`\`bash
abx envs ${v.pool} docs
\`\`\`

同一份文档也在控制台的 Env 详情页，并且按你选择的 Key 填好了密钥。通过 E2B 驱动某个 Env 之前先读它：只有它知道这个集群的端点是什么。

沙箱属于 E2B 的接口，官方 SDK 原样可用；只需在导入前调用一次 \`patch_e2b()\`，把它指向本平台而不是 e2b.dev。

\`\`\`bash
uv venv && source .venv/bin/activate
uv pip install e2b agent-sandbox-e2b
\`\`\`

\`\`\`python
from agent_sandbox_e2b import patch_e2b

patch_e2b(
    api_url="${v.e2b}",
    domain="${v.domain}",${v.https ? "" : "\n    https=False,"}
)

# patch_e2b() 必须在下一行 import 之前执行，否则 SDK 连接的是 e2b.dev。
from e2b import Sandbox

sbx = Sandbox.create("${v.pool}", timeout=3600, secure=False)
print(sbx.commands.run("python -V").stdout)
sbx.kill()
\`\`\`

\`Sandbox.create()\` 的第一个参数是**环境名（Env）**，不是镜像名。需要指定其他集群时加集群前缀：\`Sandbox.create("other-cluster::${v.pool}")\`。

## 6. 这把 Key 的权限范围

API Key 以**你的身份**生效 —— 你的团队、你的命名空间、你的配额；它无法列出或访问其他用户的沙箱。在「API 密钥」页面删除后，该 Key 在所有位置立即失效，包括你此前交给 Agent 的那一份。

无人值守场景请在签发时选择 **Agent** 模式：

- 平台侧的写操作（创建环境、扩缩容、删除）需要你在控制台确认后才执行；
- 无法为自己签发新的 API Key；
- 无法读取任何 API Key 的明文 —— \`abx api-keys\` 不返回，渲染出的接入文档（包括本文档）中也不会出现；
- 沙箱操作（创建沙箱、执行命令、读写文件）与不受限模式一致，不受上述限制。

正因如此，本文档把密钥写成 \`\${AGBX_API_KEY}\` 占位符而不是填入真实明文：会被复制、粘贴、进日志的是一份文档，而不是一个凭据。`
}
