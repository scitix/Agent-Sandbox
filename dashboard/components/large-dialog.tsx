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

import * as React from "react"
import { XIcon } from "lucide-react"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import {
  Tooltip as UITooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

// ─── LargeDialog ─────────────────────────────────────────────────────────────
//
// Full-size dialog for chart expand view.
// The dialog has no fixed size; it grows to fit its content.
// Pass `contentClassName` on the inner wrapper (e.g. "w-[80dvw] h-[80dvh]")
// to control the chart area, which then drives the dialog size.
//
// Usage:
//   <LargeDialog open={open} onOpenChange={setOpen} title="My Chart" description="…" icon={<BarChart2 />}>
//     <MyContent />
//   </LargeDialog>

export interface LargeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Primary title shown in the toolbar */
  title: React.ReactNode
  /** Optional subtitle shown below the title in smaller muted text */
  description?: string
  /** Optional action buttons rendered to the left of the close button in the toolbar */
  actions?: React.ReactNode
  /**
   * Optional className merged onto the inner content wrapper.
   * Defaults to `overflow-auto p-4` behaviour; callers with self-contained
   * flex layouts should pass `"p-4"` (no overflow) to avoid stray scrollbars.
   */
  contentClassName?: string
  children: React.ReactNode
}

export function LargeDialog({
  open,
  onOpenChange,
  title,
  description,
  actions,
  contentClassName,
  children,
}: LargeDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* No fixed w/h on DialogContent — the inner wrapper drives the size */}
      <DialogContent
        className="flex w-fit flex-col gap-0 overflow-hidden p-0 sm:max-w-[95dvw]"
        showCloseButton={false}
      >
        {/* Accessible title for screen readers */}
        <DialogHeader className="sr-only">
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        <div className="flex max-h-[90dvh] w-[90dvw] flex-col overflow-hidden rounded-xl">
          {/* ── Toolbar ── */}
          <div className="flex shrink-0 items-center justify-between border-b px-4 py-2.5">
            <TooltipProvider delay={300}>
              <UITooltip>
                <TooltipTrigger
                  render={
                    <h3 className="text-muted-foreground cursor-help font-mono text-xs font-bold tracking-[0.15em] uppercase select-none" />
                  }
                >
                  {title}
                </TooltipTrigger>
                <TooltipContent side="top" className="max-w-xs">
                  {description}
                </TooltipContent>
              </UITooltip>
            </TooltipProvider>
            <div className="ml-4 flex shrink-0 items-center gap-1">
              {actions}
              <DialogClose
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground hover:text-foreground"
                  />
                }
              >
                <XIcon className="size-4" />
                <span className="sr-only">Close</span>
              </DialogClose>
            </div>
          </div>

          {/* ── Content ── */}
          <div className={cn("min-h-0 flex-1 overflow-auto p-4", contentClassName)}>{children}</div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
