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
 * DetailTabs — URL-driven underline tab bar for resource detail pages.
 *
 * Wraps Base UI's Tabs.Root so that TabsContent panels (passed as children)
 * stay inside the correct Tabs context. Tab routing uses useQueryState for
 * shallow URL updates; the active tab survives page refreshes and is
 * shareable via URL.
 *
 * Styling is applied via Tailwind className overrides on TabsList /
 * TabsTrigger — no global CSS, no changes to components/ui/tabs.tsx.
 */

import { parseAsString, useQueryState } from "nuqs"
import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"

// Re-export TabsContent under a scoped name so callers don't need to import
// from both this file and components/ui/tabs.
export { TabsContent as DetailTabsContent }

export interface DetailTab {
  value: string
  label: string
  icon?: LucideIcon
  disabled?: boolean
}

export interface DetailTabsProps {
  tabs: DetailTab[]
  defaultTab: string
  /** URL query param name. Defaults to "tab". */
  queryKey?: string
  /** TabsContent panels — rendered inside Tabs.Root for correct context. */
  children: React.ReactNode
  className?: string
}

// ─── Underline tab styling ───────────────────────────────────────────────────
// Applied via className props on TabsList / TabsTrigger, overriding the
// default pill-style variant without touching components/ui/tabs.tsx.

const LIST_CN = "h-9 gap-6 rounded-none bg-transparent p-0"

// Tailwind-merge resolves conflicts: later classes in cn() win over the
// base classes in TabsTrigger.
const TRIGGER_CN = cn(
  // Geometry: natural width (not flex-1 stretch), full h-9 height
  "h-9 flex-none rounded-none",
  // Border: reset all sides, add 2 px bottom only
  "border-0 border-b-2 border-b-transparent",
  // No pill background; text colors
  "bg-transparent px-0 pb-3 pt-2 shadow-none group-data-[variant=default]/tabs-list:data-active:shadow-none",
  "text-muted-foreground hover:text-foreground",
  // Active state: show foreground underline, suppress pill background
  "data-active:border-b-brand data-active:bg-transparent data-active:text-foreground data-active:shadow-none",
  "dark:data-active:border-b-brand dark:data-active:bg-transparent dark:data-active:text-foreground dark:data-active:shadow-none",
  // Focus ring
  "focus-visible:ring-0 focus-visible:outline-none",
)

// ─── Component ───────────────────────────────────────────────────────────────

export function DetailTabs({
  tabs,
  defaultTab,
  queryKey = "tab",
  children,
  className,
}: DetailTabsProps) {
  const [activeTab, setTab] = useQueryState(
    queryKey,
    parseAsString.withDefault(defaultTab).withOptions({ scroll: false, shallow: true }),
  )

  // Guard against stale URL values that no longer match available tabs
  const resolvedTab = tabs.some((t) => t.value === activeTab) ? activeTab : defaultTab

  return (
    <Tabs
      value={resolvedTab}
      onValueChange={(v) => void setTab(v)}
      className={cn("flex min-h-0 flex-1 flex-col gap-0", className)}
    >
      {/* Tab bar — outer border-b acts as the gray track for the underline */}
      <div className="border-border shrink-0 border-b px-6">
        <TabsList variant="default" className={LIST_CN}>
          {tabs.map((tab) => (
            <TabsTrigger
              key={tab.value}
              value={tab.value}
              disabled={tab.disabled}
              className={TRIGGER_CN}
            >
              {tab.icon && <tab.icon className="h-3.5 w-3.5" />}
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      {/* Tab content panels — must be inside Tabs.Root for Base UI context */}
      {children}
    </Tabs>
  )
}
