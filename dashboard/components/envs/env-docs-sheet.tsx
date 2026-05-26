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
import { Boxes, Loader2, Copy, Check } from "lucide-react"
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
import { envQueryOptions } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { useLocale } from "@/hooks/use-locale"
import { useClusterID } from "@/hooks/use-cluster-id"
import { clusterPath } from "@/lib/cluster-path"
import { cn } from "@/lib/utils"

// ─── nuqs URL param ────────────────────────────────────────────────────────────
//
// Value shape: "envName" — the rendered docs come straight from the Env GET
// response so a single name is enough to drive the sheet. Callers (env detail
// page) set the param via openEnvDocs(envName).

export const ENV_DOCS_PARAM = "envDocs"

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

function EnvDocsContent({ envName }: { envName: string }) {
  const { t } = useTranslation()

  const { data: envelope, isLoading } = useQuery(envQueryOptions(envName))

  const env = envelope?.env
  const renderedDocs = env?.envDocs ?? ""

  if (isLoading) {
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
        <p className="text-muted-foreground text-sm">{t("envs.noEnvDocs")}</p>
      )}
    </div>
  )
}

// ─── Probe: runs the same query at the sheet level so we can react to the
// API_KEY_REQUIRED error without keeping the Sheet mounted. When we detect the
// error, we close the Sheet and raise the API-Keys-Required Dialog instead.
// ─────────────────────────────────────────────────────────────────────────────

function useApiKeyRequired(envName: string | null): boolean {
  const { error } = useQuery({
    ...envQueryOptions(envName ?? ""),
    enabled: !!envName,
  })
  const errorCode = error?.errorCode
  return errorCode === "API_KEY_REQUIRED"
}

// ─── Exported Sheet component ─────────────────────────────────────────────────

export function EnvDocsSheet() {
  const { t } = useTranslation()
  const locale = useLocale()
  const clusterID = useClusterID()

  const [envName, setEnvName] = useQueryState(
    ENV_DOCS_PARAM,
    parseAsString.withOptions({ scroll: false, shallow: true }),
  )
  const target = envName && envName.length > 0 ? envName : null

  // Probe the env query from the sheet level (not the inner Sheet children) so
  // we can swap the Sheet for a Dialog when the backend returns 422
  // API_KEY_REQUIRED. Otherwise the Sheet stays mounted and the Dialog close
  // button has no effect.
  const needsApiKey = useApiKeyRequired(target)

  const isOpen = !!target && !needsApiKey
  const isApiKeyDialogOpen = !!target && needsApiKey

  const handleOpenChange = (open: boolean) => {
    if (!open) void setEnvName(null)
  }

  const handleDialogOpenChange = (open: boolean) => {
    if (!open) void setEnvName(null)
  }

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
                <Boxes className="h-4 w-4" />
              </div>
              <div className="flex min-w-0 items-center gap-2">
                <SheetTitle className="font-mono text-base font-semibold">{target ?? ""}</SheetTitle>
                {target && <InlineCopyButton value={target} />}
              </div>
            </div>
            <p className="text-muted-foreground mt-0.5 text-xs">
              {t("envs.envDocsSheet.title")}
            </p>
          </SheetHeader>

          {isOpen && target && <EnvDocsContent envName={target} />}
        </SheetContent>
      </Sheet>

      <Dialog open={isApiKeyDialogOpen} onOpenChange={handleDialogOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("envs.apiKeyRequired.title")}</DialogTitle>
            <DialogDescription>
              {t("envs.apiKeyRequired.envDocsDescription")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => void setEnvName(null)}>
              {t("common.cancel")}
            </Button>
            <Button render={<Link href={clusterPath(clusterID, "api-keys", locale)} />}>
              {t("envs.apiKeyRequired.goToApiKeys")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
