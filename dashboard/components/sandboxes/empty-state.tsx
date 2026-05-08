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

import { ExternalLink } from "lucide-react"
import { GridPattern } from "@/components/patterns"
import { useTranslation } from "@/lib/i18n"

export function SandboxEmptyState() {
  const { t } = useTranslation()
  return (
    <div className="flex h-full items-center justify-center">
      <div className="border-border bg-card relative w-full max-w-md border">
        <GridPattern className="opacity-20" />
        <div className="relative flex flex-col items-center gap-4 px-8 py-10 text-center">
          <h3 className="text-foreground font-mono text-lg font-bold tracking-wide uppercase">
            {t("sandboxes.noSandboxesYet")}
          </h3>
          <p className="text-muted-foreground text-sm">
            {t("sandboxes.runningSandboxesObservedHere")}
          </p>
          <a
            href="#"
            className="border-border text-foreground hover:bg-secondary flex w-full items-center justify-center gap-2 border px-6 py-2.5 font-mono text-sm tracking-wider uppercase transition-colors"
          >
            {t("sandboxes.createASandbox")}
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
        </div>
      </div>
    </div>
  )
}
