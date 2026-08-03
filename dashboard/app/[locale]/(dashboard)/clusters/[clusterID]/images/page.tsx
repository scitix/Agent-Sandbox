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

import { HardDriveDownload, Package, Plus } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTranslation } from "@/lib/i18n"

/**
 * Sandbox image builds. The page describes what a build produces and reserves
 * the route; there is no build API behind it yet, so it reads from nothing and
 * the create action stays disabled rather than calling an endpoint that does
 * not exist.
 */
export default function ImagesPage() {
  const { t } = useTranslation()

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* Toolbar */}
        <div className="border-border border-b px-6 py-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-muted-foreground max-w-3xl text-xs leading-relaxed">
              {t("images.intro")}
            </p>
            <TooltipProvider delay={100}>
              <Tooltip>
                <TooltipTrigger render={<span tabIndex={0} />}>
                  <Button
                    size="sm"
                    disabled
                    className="bg-foreground text-background hover:bg-foreground/90 h-9 gap-1.5 font-mono text-[12px] tracking-wider uppercase"
                  >
                    <Plus className="h-3 w-3" />
                    {t("images.newBuild")}
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">{t("images.newBuildTooltip")}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>

        {/* What a build is */}
        <div className="grid grid-cols-1 gap-4 px-6 py-5 lg:grid-cols-2">
          <div className="bg-card flex flex-col gap-2 rounded-xl border p-5">
            <div className="flex items-center gap-2">
              <Package className="text-muted-foreground h-4 w-4" />
              <h3 className="font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("images.baseTitle")}
              </h3>
            </div>
            <p className="text-muted-foreground text-xs leading-relaxed">{t("images.baseDesc")}</p>
          </div>
          <div className="bg-card flex flex-col gap-2 rounded-xl border p-5">
            <div className="flex items-center gap-2">
              <HardDriveDownload className="text-muted-foreground h-4 w-4" />
              <h3 className="font-mono text-xs font-bold tracking-[0.12em] uppercase">
                {t("images.outputTitle")}
              </h3>
            </div>
            <p className="text-muted-foreground text-xs leading-relaxed">
              {t("images.outputDesc")}
            </p>
          </div>
        </div>

        {/* Empty state */}
        <div className="px-6 pb-8">
          <div className="flex flex-col items-center justify-center rounded-xl border border-dashed py-20 text-center">
            <Package className="text-muted-foreground/40 mb-3 h-10 w-10" />
            <p className="text-muted-foreground text-sm font-medium">{t("images.emptyTitle")}</p>
            <p className="text-muted-foreground mt-1 max-w-md text-xs">{t("images.emptyDesc")}</p>
          </div>
        </div>
      </div>
    </div>
  )
}
