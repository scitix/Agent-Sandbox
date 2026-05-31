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

import { useAtomValue } from "jotai"
import { concurrentSandboxesAtom } from "@/lib/atoms"
import { useTranslation } from "@/lib/i18n"

export function LiveBadge() {
  const count = useAtomValue(concurrentSandboxesAtom)
  const { t } = useTranslation()

  return (
    <div className="border-border bg-card text-card-foreground hidden h-8 items-center gap-3 rounded-md border px-3 xl:flex">
      <span className="flex items-center gap-1.5">
        <span className="relative flex h-2 w-2">
          <span className="bg-success absolute inline-flex h-full w-full animate-ping rounded-full opacity-75" />
          <span className="bg-success relative inline-flex h-2 w-2 rounded-full" />
        </span>
        <span className="text-success text-xs font-bold tracking-wider">{t("status.live")}</span>
      </span>
      <span className="font-mono text-sm">
        <span className="font-bold">{count}</span>
        <span className="text-muted-foreground ml-1.5 text-xs tracking-wide uppercase">
          {t("status.concurrentSandboxes")}
        </span>
      </span>
    </div>
  )
}
