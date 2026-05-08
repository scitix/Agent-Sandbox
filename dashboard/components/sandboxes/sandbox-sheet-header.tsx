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

/**
 * SandboxSheetHeader — unified Sheet header for sandbox-scoped sheets.
 *
 * Renders the icon + title row and the "Sandbox: <id>" description line,
 * consistent with the LogsSheet header style.
 */

import { type LucideIcon } from "lucide-react"
import { SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { useTranslation } from "@/lib/i18n"

export interface SandboxSheetHeaderProps {
  /** Lucide icon shown to the left of the title */
  icon: LucideIcon
  /** Sheet title text (e.g. "Logs", "Metrics") */
  title: string
  /** Sandbox ID shown in the description line */
  sandboxId: string | null | undefined
  /** Optional extra content rendered below the description */
  children?: React.ReactNode
}

export function SandboxSheetHeader({
  icon: Icon,
  title,
  sandboxId,
  children,
}: SandboxSheetHeaderProps) {
  const { t } = useTranslation()
  return (
    <SheetHeader className="border-border border-b px-6 py-4">
      <SheetTitle className="flex items-center gap-2 font-mono text-sm tracking-wide uppercase">
        <Icon className="text-brand h-4 w-4" />
        {title}
      </SheetTitle>
      <SheetDescription className="text-muted-foreground font-mono text-xs">
        {t("sandboxes.sandboxLogs", { sandboxId: sandboxId ?? "—" })}
      </SheetDescription>
      {children}
    </SheetHeader>
  )
}
