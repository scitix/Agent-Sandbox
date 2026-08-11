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

import { DialogContent, DialogFooter, DialogHeader } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"

/**
 * A dialog whose body scrolls while its header and footer stay put.
 *
 * `DialogContent` on its own grows with its content, so a dialog listing
 * something unbounded — every cluster, every template, a long form — runs off
 * the top and bottom of the screen and takes its footer buttons with it. Capping
 * the content at a share of the viewport and scrolling only the middle keeps the
 * title and the confirm button reachable no matter how long the list is.
 *
 * Capping alone is not enough: the cap has to sit on `DialogContent` (a child
 * cannot shorten its parent) while the scroll has to sit on the body, and the
 * body needs `min-h-0` or flex refuses to shrink it below its content and the
 * overflow never triggers. That interaction is the reason this is a component
 * rather than a className people copy.
 *
 * Use `overflow-y-auto` on an inner list only when a *section* of the body
 * should scroll independently of the rest.
 */
function ScrollDialogContent({ className, ...props }: React.ComponentProps<typeof DialogContent>) {
  return (
    <DialogContent className={cn("flex max-h-[85dvh] flex-col gap-0 p-0", className)} {...props} />
  )
}

/** Fixed header. Bordered so the scrolled body reads as moving underneath it. */
function ScrollDialogHeader({ className, ...props }: React.ComponentProps<typeof DialogHeader>) {
  return <DialogHeader className={cn("border-border border-b px-6 py-4", className)} {...props} />
}

/**
 * The scrolling region. `min-h-0` is load-bearing — without it a flex child
 * refuses to shrink below its content and nothing ever overflows.
 */
function ScrollDialogBody({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn("flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 py-4", className)}
      {...props}
    />
  )
}

/** Fixed footer. `shrink-0` keeps it from being squeezed by a long body. */
function ScrollDialogFooter({ className, ...props }: React.ComponentProps<typeof DialogFooter>) {
  return (
    <DialogFooter
      className={cn("border-border shrink-0 items-center gap-2 border-t px-6 py-3", className)}
      {...props}
    />
  )
}

export { ScrollDialogContent, ScrollDialogHeader, ScrollDialogBody, ScrollDialogFooter }
