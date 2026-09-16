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

"use client"

import { use, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Check, Copy } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { ApiKeyPicker } from "@/components/custom/api-key-picker"
import { ResourceLink } from "@/components/custom/resource-link"
import { envQueryOptions } from "@/lib/queries"
import { useDocsApiKey } from "@/hooks/use-docs-api-key"
import type { AgentSandboxEnv } from "@/lib/api/client"
import { useTranslation, type TranslationKey } from "@/lib/i18n"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"

interface PageProps {
  params: Promise<{ clusterID: string; name: string; locale: string }>
}

/**
 * Env detail index — the Overview view. It lives at `…/envs/{name}` (no
 * `/overview` sub-route) so the resource's own URL doubles as its default tab
 * and every breadcrumb ancestor links to a real page. Pools / Autoscaling /
 * Metrics are sibling sub-routes.
 */
export default function EnvOverviewPage({ params }: PageProps) {
  const { name } = use(params)
  const { t } = useTranslation()
  const { data } = useQuery(envQueryOptions(name))
  const env = data?.env

  if (!env) return null

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <EnvOverview env={env} t={t} />
    </div>
  )
}

function DocsCopyButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" onClick={handleCopy}>
      <span
        className={cn("flex items-center gap-1.5", copied && "text-green-600 dark:text-green-400")}
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        {copied ? t("common.copied") : t("common.copyPage")}
      </span>
    </Button>
  )
}

/**
 * The Env's documentation, with the reader's own key filled in.
 *
 * The server leaves `${AGBX_API_KEY}` standing — a response that carried a
 * credential could not be cached, logged or relayed — so the substitution
 * happens here, from the key chosen beside the copy button. Copying takes the
 * rendered text, not the API's: what lands on the clipboard is what the page
 * shows, key included, which is the whole point of the panel.
 */
function EnvDocs({ docs }: { docs: string }) {
  const { t } = useTranslation()
  const { keys, selected, select, fill, loading } = useDocsApiKey()
  const filled = fill(docs)

  return (
    <section>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h3 className="text-muted-foreground font-mono text-xs font-bold tracking-[0.12em] uppercase">
          {t("envs.envDocsSheet.title")}
        </h3>
        <div className="flex items-center gap-2">
          <ApiKeyPicker
            keys={keys}
            value={selected?.keyId}
            onChange={select}
            loading={loading}
          />
          <DocsCopyButton content={filled} />
        </div>
      </div>
      <MarkdownRenderer content={filled} />
    </section>
  )
}

function EnvOverview({
  env,
  t,
}: {
  env: AgentSandboxEnv
  t: (key: TranslationKey, params?: Record<string, string | number>) => string
}) {
  const clusterID = useClusterID()
  const locale = useLocale()
  const memberCount =
    env.status?.memberCount ??
    (env.spec.clusters ?? []).reduce((acc, c) => acc + (c.members?.length ?? 0), 0)
  const groups = env.spec.autoscaling?.groups ?? []
  const enabledGroups = groups.filter((g) => g.enabled).length

  // Cross-cluster scheduling is "on" when the federation has surfaced a
  // same-named Env in another cluster — i.e. status.clusters carries a
  // non-local (isLocal !== true) segment.
  const multiCluster = (env.status?.clusters ?? []).some((c) => c.isLocal !== true)
  const templateHref = `${clusterPath(clusterID, "templates", locale)}/${encodeURIComponent(
    env.spec.templateRef.name,
  )}`

  const cells: { label: string; value: React.ReactNode }[] = [
    {
      label: t("envs.detail.field.template"),
      value: (
        <ResourceLink value={env.spec.templateRef.name} href={templateHref} copyable={false} />
      ),
    },
    { label: t("envs.detail.field.mode"), value: env.spec.mode },
    {
      label: t("envs.detail.field.multiCluster"),
      value: (
        <Badge variant={multiCluster ? "default" : "outline"}>
          {multiCluster ? t("envs.detail.multiCluster.on") : t("envs.detail.multiCluster.off")}
        </Badge>
      ),
    },
    { label: t("envs.detail.field.members"), value: memberCount },
    {
      label: t("envs.detail.field.autoscaling"),
      value: groups.length > 0 ? `${enabledGroups}/${groups.length}` : "—",
    },
    ...(env.spec.defaults
      ? [
          {
            label: t("envs.detail.field.defaults"),
            value: `${env.spec.defaults.instanceType ?? "—"} × ${env.spec.defaults.multiplier ?? 1}`,
          },
        ]
      : []),
  ]

  const padCount = (4 - (cells.length % 4)) % 4

  return (
    <div className="space-y-6 p-6">
      {/* Metadata grid */}
      <div className="border-border bg-border grid grid-cols-2 gap-px overflow-hidden rounded-xl border lg:grid-cols-4">
        {cells.map((cell) => (
          <div key={cell.label} className="bg-card flex flex-col gap-1.5 px-3.5 py-3">
            <span className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
              {cell.label}
            </span>
            <div className="font-mono text-xs font-medium">{cell.value}</div>
          </div>
        ))}
        {Array.from({ length: padCount }).map((_, i) => (
          <div key={`pad-${i}`} className="bg-card" />
        ))}
      </div>

      {/* Inline docs */}
      {env.envDocs ? (
        <EnvDocs docs={env.envDocs} />
      ) : (
        <section>
          <h3 className="text-muted-foreground mb-3 font-mono text-xs font-bold tracking-[0.12em] uppercase">
            {t("envs.envDocsSheet.title")}
          </h3>
          <p className="text-muted-foreground text-sm">{t("envs.noEnvDocs")}</p>
        </section>
      )}
    </div>
  )
}
