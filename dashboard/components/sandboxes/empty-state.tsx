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

import { useMemo, useState } from "react"
import Link from "next/link"
import { useQuery } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import { Check, Copy, KeyRound, Plus } from "lucide-react"

import { Button } from "@/components/ui/button"
import { GridPattern } from "@/components/patterns"
import { clustersAtom } from "@/lib/atoms"
import { envsQueryOptions } from "@/lib/queries"
import { buildPythonQuickstart } from "@/lib/utils/python-quickstart"
import { clusterPath } from "@/lib/cluster-path"
import { useClusterID } from "@/hooks/use-cluster-id"
import { useLocale } from "@/hooks/use-locale"
import { useTranslation } from "@/lib/i18n"
import { cn } from "@/lib/utils"

function Snippet({ label, code }: { label: string; code: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  return (
    <div className="space-y-1.5">
      <p className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
        {label}
      </p>
      <div className="border-border bg-muted/40 group relative rounded-lg border">
        {/* Wrapped, not scrolled: the gateway URL is long enough to run off the
            card, and a snippet whose only visible part is `api_url="http://…`
            reads as broken even though the copy button has the whole thing. */}
        <pre className="px-3 py-2.5 pr-10 font-mono text-[12px] leading-relaxed break-words whitespace-pre-wrap">
          {code}
        </pre>
        <button
          type="button"
          aria-label={t("common.copy")}
          onClick={async () => {
            await navigator.clipboard.writeText(code)
            setCopied(true)
            setTimeout(() => setCopied(false), 2000)
          }}
          className={cn(
            "text-muted-foreground hover:text-foreground absolute top-2 right-2 rounded-md p-1 transition-colors",
            copied && "text-green-500 hover:text-green-500",
          )}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
    </div>
  )
}

/**
 * What the sandbox list shows before the caller has ever made one.
 *
 * The snippet on the right is the point of the card, and it is built from the
 * cluster's own gateway rather than written out: a quickstart that names
 * e2b.dev looks right, copies cleanly, and reaches a different company's
 * platform. `buildPythonQuickstart` degrades to placeholders when the gateway
 * is unknown, and this warns instead of pretending the snippet is runnable.
 *
 * Python only, so there are no language tabs — a second tab is a second thing
 * to keep true, and the SDKs this platform patches are not at parity.
 */
export function SandboxEmptyState({ onCreate }: { onCreate?: () => void }) {
  const { t } = useTranslation()
  const clusterID = useClusterID()
  const locale = useLocale()
  const clusters = useAtomValue(clustersAtom).clusters
  // The Env list is the snippet's `template` argument. Failing to load it costs
  // a placeholder, not the card, so nothing here waits on it.
  const { data: envs } = useQuery({ ...envsQueryOptions(), retry: false })

  const quickstart = useMemo(() => {
    const gateway = clusters.find((c) => c.id === clusterID)?.gateway
    return buildPythonQuickstart({
      e2bURL: gateway?.e2bURL,
      dataURL: gateway?.dataURL,
      envName: envs?.[0]?.name,
    })
  }, [clusters, clusterID, envs])

  return (
    <div className="flex h-full items-center justify-center p-6">
      <div className="border-border bg-card relative w-full max-w-4xl overflow-hidden rounded-xl border">
        <GridPattern className="opacity-20" />
        <div className="relative grid gap-6 md:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
          <div className="flex flex-col gap-4 border-b px-6 py-7 md:border-r md:border-b-0">
            <h3 className="text-foreground font-mono text-base font-bold tracking-wide uppercase">
              {t("sandboxes.empty.title")}
            </h3>
            <p className="text-muted-foreground text-sm leading-relaxed">
              {t("sandboxes.empty.description")}
            </p>
            <div className="mt-auto flex flex-wrap gap-2 pt-2">
              {onCreate && (
                <Button
                  size="sm"
                  onClick={onCreate}
                  className="bg-foreground text-background hover:bg-foreground/90 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
                >
                  <Plus className="h-3 w-3" />
                  {t("sandboxes.newSandbox")}
                </Button>
              )}
              <Button
                size="sm"
                variant="outline"
                // Rendering as a link, so the native <button> element and its
                // implicit semantics have to be withdrawn explicitly.
                nativeButton={false}
                className="gap-1.5 font-mono text-[12px] tracking-wider uppercase"
                render={<Link href={clusterPath(clusterID, "envs", locale)} />}
              >
                {t("sandboxes.empty.browseEnvs")}
              </Button>
            </div>
          </div>

          <div className="space-y-4 px-6 py-7">
            <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
              <span className="text-muted-foreground">{t("sandboxes.empty.setApiKey")}</span>
              <Link
                href={clusterPath(clusterID, "api-keys", locale)}
                className="text-foreground inline-flex items-center gap-1 underline underline-offset-4"
              >
                <KeyRound className="h-3.5 w-3.5" />
                {t("sandboxes.empty.openApiKeys")}
              </Link>
            </div>

            <Snippet label={t("sandboxes.empty.installSdk")} code={quickstart.install} />
            <Snippet label={t("sandboxes.empty.createSandbox")} code={quickstart.code} />

            <p className="text-muted-foreground border-t pt-3 text-xs leading-relaxed">
              {quickstart.missingGateway
                ? t("sandboxes.empty.noGatewayHint")
                : quickstart.missingEnv
                  ? t("sandboxes.empty.noEnvHint")
                  : t("sandboxes.empty.envNotImageHint")}
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
