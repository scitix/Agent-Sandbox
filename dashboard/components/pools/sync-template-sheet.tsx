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
import { Loader2, RefreshCw, X } from "lucide-react"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import type { AgentSandboxPool } from "@/lib/api/client"
import { previewSyncTemplateQueryOptions, useSyncPoolTemplate } from "@/lib/queries"
import { useTranslation } from "@/lib/i18n"
import { YamlDiffView, computeYamlDiff } from "@/components/templates/yaml-diff-view"

// ─── Props ────────────────────────────────────────────────────────────────────

interface SyncTemplateSheetProps {
  pool: AgentSandboxPool | null
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
}

// ─── Outer Shell ─────────────────────────────────────────────────────────────

export function SyncTemplateSheet({ pool, onOpenChange, onSuccess }: SyncTemplateSheetProps) {
  const { t } = useTranslation()
  return (
    <Sheet
      open={!!pool}
      onOpenChange={(open) => {
        if (!open) onOpenChange(false)
      }}
    >
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 sm:max-w-5xl data-[side=right]:sm:max-w-5xl"
      >
        <SheetHeader className="border-border border-b px-6 py-4">
          <SheetTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
            <RefreshCw className="h-4 w-4" />
            {t("pools.syncTemplate")}
            {pool && (
              <span className="text-muted-foreground ml-1 font-normal normal-case">
                — {pool.name}
              </span>
            )}
          </SheetTitle>
        </SheetHeader>
        {pool && <SyncTemplateForm pool={pool} onOpenChange={onOpenChange} onSuccess={onSuccess} />}
      </SheetContent>
    </Sheet>
  )
}

// ─── Inner Form ───────────────────────────────────────────────────────────────

interface SyncTemplateFormProps {
  pool: AgentSandboxPool
  onOpenChange: (open: boolean) => void
  onSuccess?: () => void
}

function SyncTemplateForm({ pool, onOpenChange, onSuccess }: SyncTemplateFormProps) {
  const { t } = useTranslation()
  const { mutate: syncMutate, isPending } = useSyncPoolTemplate()
  const templateName = pool.spec?.templateName ?? ""
  const overrideLabels: string[] = []
  if (pool.overrides?.image) {
    overrideLabels.push(t("pools.form.imageOverride"))
  }
  if (pool.overrides?.resourceMultiplier && pool.overrides.resourceMultiplier > 1) {
    overrideLabels.push(
      `${t("pools.form.resourceMultiplier")} ×${pool.overrides.resourceMultiplier}`,
    )
  }
  const hasOverrides = overrideLabels.length > 0

  // Call the backend preview endpoint to get the exact post-sync specYaml.
  const { data: previewData, isLoading: isLoadingPreview } = useQuery({
    ...previewSyncTemplateQueryOptions(pool.name),
    enabled: !!templateName,
  })

  const [confirmed, setConfirmed] = useState(false)

  const poolYaml = pool.specYaml ?? ""
  const syncedYaml = previewData?.specYaml ?? null
  const templateVersion = previewData?.version ?? "—"
  const diffLines = syncedYaml ? computeYamlDiff(poolYaml, syncedYaml) : []
  const hasDiff = diffLines.some((d) => d.type !== "same")

  const poolVersion = pool.templateVersion ?? "—"

  const handleSync = () => {
    syncMutate(
      { params: { path: { name: pool.name } } },
      {
        onSuccess: () => {
          toast.success(t("pools.syncSuccess", { poolName: pool.name, templateName: templateName }))
          onOpenChange(false)
          onSuccess?.()
        },
      },
    )
  }

  if (!templateName) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-12">
        <p className="text-muted-foreground font-mono text-sm">{t("pools.noAssociatedTemplate")}</p>
        <p className="text-muted-foreground font-mono text-xs">{t("pools.onlyTemplatePools")}</p>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* Meta row */}
      <div className="border-border flex items-center gap-6 border-b px-6 py-3">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs uppercase">
            {t("pools.sourceTemplate")}
          </span>
          <span className="text-foreground font-mono text-xs font-semibold">{templateName}</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs uppercase">
            {t("pools.poolVersion")}
          </span>
          <span className="font-mono text-xs">{poolVersion}</span>
        </div>
        <span className="text-muted-foreground">→</span>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground font-mono text-xs uppercase">
            {t("pools.templateLatest")}
          </span>
          <span className="font-mono text-xs font-semibold">{templateVersion}</span>
        </div>
      </div>
      {hasOverrides && (
        <div className="border-border bg-muted/30 border-b px-6 py-2">
          <p className="text-muted-foreground font-mono text-xs">
            {t("pools.syncPreservesOverrides", { overrides: overrideLabels.join(" · ") })}
          </p>
        </div>
      )}

      {/* Diff view */}
      <div className="min-h-0 flex-1 overflow-auto px-6 py-4">
        {isLoadingPreview ? (
          <div className="flex items-center gap-2 py-8">
            <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
            <span className="text-muted-foreground font-mono text-xs">Loading preview…</span>
          </div>
        ) : !syncedYaml ? (
          <p className="text-muted-foreground py-8 font-mono text-xs">
            {t("pools.templateNotFound")}
          </p>
        ) : !hasDiff ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12">
            <p className="text-muted-foreground font-mono text-sm">{t("pools.alreadyUpToDate")}</p>
            <p className="text-muted-foreground font-mono text-xs">
              {t("pools.noDifferencesDetected")}
            </p>
          </div>
        ) : (
          <div>
            <YamlDiffView oldYaml={poolYaml} newYaml={syncedYaml} />

            {/* Confirmation checkbox */}
            <div className="mt-4 flex items-center gap-2">
              <input
                type="checkbox"
                id="confirm-sync"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
                className="h-4 w-4 rounded"
              />
              <label
                htmlFor="confirm-sync"
                className="text-muted-foreground cursor-pointer font-mono text-xs"
              >
                {t("pools.willOverwriteSpec")}
              </label>
            </div>
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="border-border flex items-center justify-end gap-2 border-t px-6 py-3">
        <Button
          type="button"
          variant="outline"
          onClick={() => onOpenChange(false)}
          className="font-mono text-xs tracking-wider uppercase"
        >
          <X className="mr-1.5 h-3.5 w-3.5" />
          {t("common.cancel")}
        </Button>
        <Button
          type="button"
          disabled={isPending || isLoadingPreview || !hasDiff || !confirmed}
          onClick={handleSync}
          className="bg-foreground text-background hover:bg-foreground/90 font-mono text-xs tracking-wider uppercase"
        >
          {isPending ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          )}
          {t("pools.syncFromTemplate")}
        </Button>
      </div>
    </div>
  )
}
