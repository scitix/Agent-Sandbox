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

import { useState } from "react"
import Link from "next/link"
import { useQuery } from "@tanstack/react-query"
import { parseAsString, useQueryState } from "nuqs"
import { Layers, Loader2, Copy, Check } from "lucide-react"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { envPoolQueryOptions } from "@/lib/queries/pool"
import { useTranslation } from "@/lib/i18n"
import { useLocale } from "@/hooks/use-locale"
import { useClusterID } from "@/hooks/use-cluster-id"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"

// ─── nuqs URL param ────────────────────────────────────────────────────────────
//
// Value shape: "envName/poolName" — env-scoped pools require both pieces to
// hit /v1/envs/{name}/sandboxpools/{poolName}. Callers (env detail page) set
// the param via openPoolDocs(envName, poolName).

export const POOL_DOCS_PARAM = "poolDocs"

function parsePoolDocsParam(raw: string | null): { envName: string; poolName: string } | null {
  if (!raw) return null
  const idx = raw.indexOf("/")
  if (idx <= 0 || idx === raw.length - 1) return null
  return { envName: raw.slice(0, idx), poolName: raw.slice(idx + 1) }
}

/** Build the URL-param value the sheet expects. */
export function formatPoolDocsParam(envName: string, poolName: string): string {
  return `${envName}/${poolName}`
}

// ─── Copy button (page content) ───────────────────────────────────────────────

function CopyButton({ content }: { content: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    await navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <Button variant="outline" size="sm" className="h-7 gap-1.5 text-xs" onClick={handleCopy}>
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      {copied ? t("common.copied") : t("common.copyPage")}
    </Button>
  )
}

// ─── Inline copy button (header name) ─────────────────────────────────────────

function InlineCopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button
      onClick={handleCopy}
      className={cn(
        "text-muted-foreground hover:text-foreground rounded p-0.5 transition-colors",
        copied && "text-green-500 hover:text-green-500",
      )}
      title={copied ? "Copied!" : "Copy name"}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </button>
  )
}

// ─── Inner content (only mounted when sheet is open → triggers fetch) ─────────

function PoolDocsContent({ envName, poolName }: { envName: string; poolName: string }) {
  const { t } = useTranslation()

  const { data: poolEnvelope, isLoading: poolLoading } = useQuery(envPoolQueryOptions(envName, poolName))

  const pool = poolEnvelope?.template
  const renderedDocs = pool?.poolDocs ?? ""

  if (poolLoading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-y-auto px-5 py-4">
      {renderedDocs && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <CopyButton content={renderedDocs} />
        </div>
      )}

      {renderedDocs ? (
        <MarkdownRenderer content={renderedDocs} />
      ) : (
        <p className="text-muted-foreground text-sm">{t("pools.noPoolDocs")}</p>
      )}
    </div>
  )
}

// ─── Probe: runs the same query at the sheet level so we can react to the
// API_KEY_REQUIRED error without keeping the Sheet mounted. When we detect the
// error, we close the Sheet and raise the API-Keys-Required Dialog instead.
// ─────────────────────────────────────────────────────────────────────────────

function useApiKeyRequired(target: { envName: string; poolName: string } | null): boolean {
  const { error } = useQuery({
    ...envPoolQueryOptions(target?.envName ?? "", target?.poolName ?? ""),
    enabled: !!target,
  })
  const errorCode = error?.errorCode
  return errorCode === "API_KEY_REQUIRED"
}

// ─── Exported Sheet component ─────────────────────────────────────────────────

export function PoolDocsSheet() {
  const { t } = useTranslation()
  const locale = useLocale()
  const clusterID = useClusterID()

  const [rawParam, setRawParam] = useQueryState(
    POOL_DOCS_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const target = parsePoolDocsParam(rawParam)

  // Probe the pool query from the sheet level (not the inner Sheet children) so
  // we can swap the Sheet for a Dialog when the backend returns 422
  // API_KEY_REQUIRED. Otherwise the Sheet stays mounted and the Dialog close
  // button has no effect.
  const needsApiKey = useApiKeyRequired(target)

  const isOpen = !!target && !needsApiKey
  const isApiKeyDialogOpen = !!target && needsApiKey

  const handleOpenChange = (open: boolean) => {
    if (!open) void setRawParam(null)
  }

  const handleDialogOpenChange = (open: boolean) => {
    if (!open) void setRawParam(null)
  }
  const poolName = target?.poolName ?? ""

  return (
    <>
      <Sheet open={isOpen} onOpenChange={handleOpenChange}>
        <SheetContent
          side="right"
          className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-6xl"
        >
          <SheetHeader className="border-border border-b px-5 py-4">
            <div className="flex items-center gap-3">
              <div className="bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg">
                <Layers className="h-4 w-4" />
              </div>
              <div className="flex min-w-0 items-center gap-2">
                <SheetTitle className="font-mono text-base font-semibold">{poolName}</SheetTitle>
                {poolName && <InlineCopyButton value={poolName} />}
              </div>
            </div>
            <p className="text-muted-foreground mt-0.5 text-xs">
              {t("pools.poolDocsSheet.title")}
            </p>
          </SheetHeader>

          {isOpen && target && <PoolDocsContent envName={target.envName} poolName={target.poolName} />}
        </SheetContent>
      </Sheet>

      <Dialog open={isApiKeyDialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("pools.apiKeyRequired.title")}</DialogTitle>
            <DialogDescription>
              {t("pools.apiKeyRequired.poolDocsDescription")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => void setRawParam(null)}>
              {t("common.cancel")}
            </Button>
            <Button render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}>
              {t("pools.apiKeyRequired.goToApiKeys")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
