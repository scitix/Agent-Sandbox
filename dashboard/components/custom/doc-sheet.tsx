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
import { FileText, Copy, Check } from "lucide-react"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { MarkdownRenderer } from "@/components/markdown-renderer"
import { useTranslation } from "@/lib/i18n"

// ---------------------------------------------------------------------------
// Copy-page button (reusable within any DocSheet)
// ---------------------------------------------------------------------------

function CopyPageButton({ content }: { content: string }) {
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

// ---------------------------------------------------------------------------
// DocSheet — reusable documentation sheet for templates, pools, etc.
// ---------------------------------------------------------------------------

export interface DocSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Sheet title (e.g. template name) */
  title: string
  /** Optional subtitle below the title */
  subtitle?: string
  /** Icon element rendered in the header. Defaults to FileText. */
  icon?: React.ReactNode
  /** Markdown documentation content. null/undefined/empty shows the empty state. */
  content?: string | null
  /** Message shown when content is empty */
  emptyMessage?: string
  /** Extra action buttons rendered next to "Copy page" */
  actions?: React.ReactNode
}

export function DocSheet({
  open,
  onOpenChange,
  title,
  subtitle,
  icon,
  content,
  emptyMessage,
  actions,
}: DocSheetProps) {
  const { t } = useTranslation()
  const hasContent = !!content?.trim()

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 p-0 data-[side=right]:sm:max-w-6xl"
      >
        <SheetHeader className="border-border border-b px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg">
              {icon ?? <FileText className="h-4 w-4" />}
            </div>
            <div>
              <SheetTitle className="text-base font-semibold">{title}</SheetTitle>
              {subtitle && <p className="text-muted-foreground mt-0.5 text-xs">{subtitle}</p>}
            </div>
          </div>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {/* Action buttons */}
          {(hasContent || actions) && (
            <div className="mb-4 flex flex-wrap items-center gap-2">
              {hasContent && <CopyPageButton content={content!} />}
              {actions}
            </div>
          )}

          {/* Content */}
          {hasContent ? (
            <MarkdownRenderer content={content!} />
          ) : (
            <p className="text-muted-foreground text-sm">
              {emptyMessage ?? t("templates.noDocsAvailable")}
            </p>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
