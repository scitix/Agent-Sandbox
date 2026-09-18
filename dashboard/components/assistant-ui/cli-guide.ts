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

// "Agent Sandbox CLI Guide" — the document behind the floating entry, and the
// same text the static sites carry.
//
// It is generated from the live deployment rather than shipped as a file with
// blanks in it: the addresses are the one thing that makes a copied snippet
// work or not, they differ per deployment, and a reader who has to go and find
// them is a reader who gives up. Which is also why no address is written into
// this repository — it is public source, and one organisation runs several
// platforms from it.
//
// The API key is the exception in the other direction. It is written as
// `${AGBX_API_KEY}` and only ever inside a code block; the console fills it in
// there (see `use-docs-api-key`), and `fillApiKey` refuses to substitute it
// anywhere else. A document is copied, pasted and logged, so what travels has
// to be a document — and the key has to be somewhere a reader expects a value
// and a screenshot is not the thing being shared.

import type { Locale } from "@/lib/i18n/config"

export interface CliGuideInput {
  /** The cluster's E2B-compatible API base URL, from the cluster config. */
  e2bURL?: string
  /** The data-plane gateway, minus its scheme — what `E2B_DOMAIN` expects. */
  dataURL?: string
  /** Pool or Env name to use in the example, when one is known. */
  poolName?: string
  /** The cluster whose page this is, when the route names one. Env-addressed
   *  commands need it: a platform can reach several clusters, and a command
   *  that names one of them is exactly the case where `--cluster` is asked for. */
  cluster?: string
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
  /** `--cluster` for Env-addressed commands; the real id when the console
   *  knows which cluster the page belongs to. */
  cluster: string
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
    cluster: input.cluster || "YOUR_CLUSTER",
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

AgentBox runs on the command line, which is what makes it usable by an agent as well as by a person: everything the console does, \`abx\` does, and everything a sandbox does, the E2B SDK does. Hand this document to an agent and it can create, use and reclaim its own sandboxes without a browser.

Two tools, and the split between them is the product:

| Tool | Covers |
|---|---|
| \`abx\` | the platform — environments, warm pools, autoscaling, quotas, templates |
| E2B SDK | the sandboxes themselves — create, exec, files, network |

The objects those commands address — templates, envs, pools — are described in the Concepts section, which is also a plain-text document any agent can fetch: <https://scitix.github.io/Agent-Sandbox/docs/concepts/index.md>

Everything below is filled in for this deployment: the console address, the cluster and the E2B endpoints. The one value an agent supplies itself is its API key, and the API key appears in the code blocks and nowhere else.

## 1. Get an API key

Open **API Keys** in the console sidebar and create one. The plaintext is shown once at creation, and the platform keeps a copy you can retrieve from that page at any time.

If the key is going to an unattended agent, issue it in **agent** mode — see section 6.

## 2. Install \`abx\`

\`abx\` is a single binary with nothing behind it: no Node.js, no virtualenv.

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

The script installs the binary to \`~/.local/bin/abx\` (and tells you if that directory is not on your PATH) and writes nine skills to \`~/.agents/skills\` — see section 4.

## 3. Initialise \`abx\`

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key \${AGBX_API_KEY}

abx clusters
\`\`\`

That is the entire configuration: the console address and the key.

- The **address** reaches the platform, not one cluster: \`abx clusters\` lists every cluster it has, and any command takes \`--cluster <id>\`; a single-cluster platform fills it in for you.
- The **first command** is therefore \`abx clusters\`, not \`abx envs\`: listing clusters needs no \`--cluster\`, so it is the one command that cannot fail for want of an id — and it prints the ids every other command takes.
- The **auth header** follows from the address, so there is nothing else to configure.
- To work with several AgentBox platforms, save each as its own context and switch with \`abx context use <name>\`. The context is which platform; \`--cluster\` is which of its clusters, per command.

\`\`\`bash
abx agent-context     # the whole CLI as JSON — hand this to an agent
abx envs ${v.pool} pools --cluster ${v.cluster}
\`\`\`

In CI or other unattended settings, \`AGENTBOX_ENDPOINT\` and \`AGENTBOX_API_KEY\` stand in for a context.

## 4. Using it from an agent

**Claude Code** — the plugin stores your key in the OS keychain, where the agent never sees it:

\`\`\`bash
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

Then set the endpoint and key in the plugin's settings; a hook writes them to \`~/.config/abx/config.json\` for the CLI to read.

**Codex, or anything else that runs shell** — the install in step 2 already placed the binary on your PATH and nine skills in \`~/.agents/skills\`. Point your agent at that directory and it has the platform's vocabulary: rollouts, capacity, evaluations, Docker-in-sandbox, egress isolation, vault secrets. Give it an **agent**-mode key (section 6), never an unrestricted one.

Two habits make an agent reliable here: have it read \`abx agent-context\` once rather than guessing at commands, and have it read the environment's own documentation (section 5) rather than carrying endpoints in its prompt.

## 5. Run code in a sandbox

Every Env carries the documentation its template was written with, rendered for the cluster it belongs to — the E2B API URL, the data-plane domain, and the scheme to use:

\`\`\`bash
abx envs ${v.pool} docs --cluster ${v.cluster}
\`\`\`

The same document is on the Env page in the console, with your own key filled in. Read it before driving an Env through E2B: it is the only place that knows which endpoints this cluster answers on, and it covers what this guide does not — the two access paths, pool and scaling-group selection, cross-cluster routing, and how images are rewritten between regions.

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

## 6. API key permissions

An API key acts as **you** — your team, your namespace, your quota. It cannot list or reach another user's sandboxes. Delete it on the API Keys page and it stops working everywhere, including in any agent you handed it to.

A key is issued in one of two modes:

| Mode | What it may do |
|---|---|
| **Unrestricted** | everything you can do, with no further confirmation. The right mode on your own machine. |
| **Agent** | the same, except that the platform's writes — creating an environment, scaling or deleting anything — wait for your approval in the console. |

Sandbox work is identical in both modes: an agent key starts sandboxes, runs commands and moves files exactly as an unrestricted one does. The gate is on the platform's write surface, which is the part that outlives the sandbox.

An agent key also cannot issue itself another key, and cannot read key material — not from \`abx api-keys\`, and not from a rendered document such as this one. That is why the key appears only inside the code blocks above: what gets copied, pasted and logged is a document, not a secret.`
}

function chinese(v: GuideVars): string {
  return `# Agent Sandbox CLI 指南

AgentBox 的操作都可以在命令行里完成，这也是它适合交给 Agent 的原因：控制台能做的事 \`abx\` 都能做，沙箱能做的事 E2B SDK 都能做。把这份文档交给 Agent，它就能自己创建、使用、回收沙箱，全程不需要浏览器。

只有两个工具，二者的分工就是产品本身的两层：

| 工具 | 负责范围 |
|---|---|
| \`abx\` | 平台：环境（Env）、预热池、自动扩缩容、配额、模板 |
| E2B SDK | 沙箱：创建、命令执行、文件读写、网络 |

这些命令操作的对象——模板、环境、资源池——的模型说明在 Concepts 一节，同时提供可直接抓取的纯文本文档：<https://scitix.github.io/Agent-Sandbox/docs/concepts/index.md>

下面的内容已按当前部署填好：控制台地址、集群、E2B 端点都是可直接使用的真实值。Agent 唯一需要自己提供的只有它的 API Key，而 API Key 只出现在代码块里。

## 1. 获取 API Key

在控制台左侧「API 密钥」页面创建一把 Key。明文仅在创建时展示一次，平台保留一份副本，可随时从该页面取回。

若要交给无人值守的 Agent 使用，请在创建时选择 **Agent** 模式，详见第 6 节。

## 2. 安装 abx

\`abx\` 是单个二进制文件，除自身外没有其他依赖：不需要 Node.js，也不需要虚拟环境。

\`\`\`bash
curl -fsSL https://oss-ap-southeast.scitix.ai/scitix/packages/agentbox/cli/latest/install.sh | sh
\`\`\`

脚本把二进制安装到 \`~/.local/bin/abx\`（该目录不在 PATH 时会给出提示），并把九个 skill 写入 \`~/.agents/skills\`，用途见第 4 节。

## 3. 初始化 abx

\`\`\`bash
abx context set ${v.ctx} \\
  --endpoint '${v.endpoint}' \\
  --api-key \${AGBX_API_KEY}

abx clusters
\`\`\`

配置只有两项：控制台地址与 API Key。

- **地址**对应的是整个平台，而不是某个集群：\`abx clusters\` 列出该平台的全部集群，任意命令加 \`--cluster <id>\` 即可指定；平台只有一个集群时会自动补全。
- **第一条命令**因此是 \`abx clusters\` 而不是 \`abx envs\`：列集群不需要 \`--cluster\`，所以它是唯一不会因为缺 id 而失败的命令，也正是告诉你其余命令该填哪个 id 的命令。
- **认证 header** 由地址推导，无需另行配置。
- 需要同时使用多个 AgentBox 平台时，每个平台保存为一个 context，用 \`abx context use <name>\` 切换。context 决定平台，\`--cluster\` 决定该平台下的集群，按命令指定。

\`\`\`bash
abx agent-context     # 以 JSON 输出整个 CLI 的能力，可直接交给 Agent
abx envs ${v.pool} pools --cluster ${v.cluster}
\`\`\`

在 CI 或无人值守环境中，可以用 \`AGENTBOX_ENDPOINT\` 与 \`AGENTBOX_API_KEY\` 环境变量代替 context。

## 4. 在智能体中使用

**Claude Code** —— 插件把 Key 存入系统钥匙串，Agent 全程接触不到明文：

\`\`\`bash
/plugin marketplace add scitix/agent-sandbox
/plugin install agentbox
\`\`\`

随后在插件设置中填入 endpoint 与 Key；hook 会将其写入 \`~/.config/abx/config.json\` 供 CLI 读取。

**Codex 或其他可执行 shell 的工具** —— 第 2 步的安装脚本已把二进制放入 PATH，并把九个 skill 装到 \`~/.agents/skills\`。把 Agent 指向该目录，它即获得这个平台的词汇：rollout、容量、评测、沙箱内 Docker、出网隔离、保管库密钥。交给它一把 **Agent** 模式的 Key（第 6 节），不要给不受限的。

两个让 Agent 更可靠的用法：让它先读一次 \`abx agent-context\`（而不是猜命令），以及让它去读环境自带的接入文档（第 5 节），而不是把端点写进 prompt。

## 5. 在沙箱中运行代码

每个 Env 都带着所属模板的接入文档，并按它所在的集群渲染好——E2B API 地址、数据面域名、以及该用 http 还是 https：

\`\`\`bash
abx envs ${v.pool} docs --cluster ${v.cluster}
\`\`\`

同一份文档也在控制台的 Env 详情页，并且按你选择的 Key 填好了密钥。通过 E2B 驱动某个 Env 之前先读它：只有它知道这个集群的端点是什么，而上面没有提到的那部分——公网与内网两种接入方式、成员池与扩缩容组的选择、跨集群路由、镜像在 region 间的自动改写——都在那份文档里。

沙箱属于 E2B 的接口，官方 SDK 原样可用；只需在导入前调用一次 \`patch_e2b()\`，把它指向 AgentBox 而不是 e2b.dev。

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

## 6. API Key 的权限范围

API Key 以**你的身份**生效——你的团队、你的命名空间、你的配额；它无法列出或访问其他用户的沙箱。在「API 密钥」页面删除后，该 Key 在所有位置立即失效，包括你此前交给 Agent 的那一份。

Key 签发时有两种模式：

| 模式 | 能做什么 |
|---|---|
| **完全权限（不受限）** | 你能做的事它都能做，不需要额外确认。适合在自己机器上使用。 |
| **Agent** | 与完全权限相同，但平台侧的写操作——创建环境、扩缩容、删除任何对象——需要你在控制台确认后才执行。 |

两种模式下的沙箱操作完全一致：Agent 模式的 Key 创建沙箱、执行命令、读写文件与完全权限没有差别，闸门只设在平台的写操作上，而那才是沙箱回收之后仍然存在的部分。

Agent 模式的 Key 另外还有两条限制：无法为自己签发新的 API Key，也无法读取密钥明文——\`abx api-keys\` 不返回，渲染出的接入文档（包括这份指南）里也不会出现。这也是密钥只出现在代码块里的原因：会被复制、粘贴、进日志的是一份文档，而不是一个凭据。`
}
